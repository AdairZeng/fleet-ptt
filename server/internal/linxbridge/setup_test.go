package linxbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
	"github.com/coder/websocket"
)

func setupListen(t *testing.T, flow *setupFlow) {
	t.Helper()
	if _, ok := flow.control("playback_done", flow.turn); ok {
		t.Fatal("accepted playback acknowledgement before TTS completed")
	}
	flow.ttsStarted, flow.ttsStopped = true, true
	if _, ok := flow.control("playback_done", "old"); ok {
		t.Fatal("accepted stale playback acknowledgement")
	}
	if _, ok := flow.control("playback_done", flow.turn); !ok {
		t.Fatal("did not listen after completed playback")
	}
}
func TestSetupNicknameConfirmation(t *testing.T) {
	now := time.Now()
	flow := &setupFlow{}
	flow.control("start", "")
	if _, ok := flow.recognize("确认", now); ok {
		t.Fatal("accepted speech during playback")
	}
	setupListen(t, flow)
	event, _ := flow.recognize("确认", now)
	if event.State != "speaking" || flow.nickname != "" {
		t.Fatal("confirmed missing nickname")
	}
	setupListen(t, flow)
	event, _ = flow.recognize("你好小路，叫我老王。", now)
	if event.Nickname != "老王" || event.State != "speaking" {
		t.Fatalf("unexpected candidate: %+v", event)
	}
	if _, ok := flow.recognize("确认", now); ok {
		t.Fatal("accepted same-turn confirmation")
	}
	setupListen(t, flow)
	event, _ = flow.recognize("确认。", now.Add(time.Second))
	if event.State != "confirmed" || event.Nickname != "老王" {
		t.Fatalf("not confirmed: %+v", event)
	}
	if _, ok := flow.recognize("确认", now); ok {
		t.Fatal("duplicate confirmation")
	}
}
func TestSetupCancelExpiryAndValidation(t *testing.T) {
	for _, text := range []string{"", strings.Repeat("王", 33), "老\n王"} {
		flow := &setupFlow{state: "listening"}
		event, _ := flow.recognize(text, time.Now())
		if event.State == "confirmed" || flow.nickname != "" {
			t.Fatalf("accepted invalid name %q", text)
		}
	}
	now := time.Now()
	flow := &setupFlow{state: "listening"}
	flow.recognize("我叫老王", now)
	setupListen(t, flow)
	flow.recognize("取消", now)
	if flow.nickname != "" {
		t.Fatal("cancel retained nickname")
	}
	setupListen(t, flow)
	flow.recognize("小李", now)
	setupListen(t, flow)
	event, _ := flow.recognize("确认", now.Add(2*time.Minute))
	if event.State == "confirmed" || flow.nickname != "" {
		t.Fatal("expired candidate confirmed")
	}
}

func TestSetupBridgeAuthenticationAndConversation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "setup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	users := &user.Service{Store: st, Secret: "setup-test-secret"}
	credential, err := users.RegisterDevice(context.Background(), nil, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	upstreamConnections := make(chan *websocket.Conn, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		upstreamConnections <- c
	}))
	defer upstream.Close()
	manager := NewManager(ProvisionConfig{})
	manager.configs[credential.DeviceID] = Config{URL: upstream.URL, DeviceID: credential.MacID, Token: "upstream"}
	server := httptest.NewServer(SetupHandler{Users: users, Bridge: manager})
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	dial := func(proof string) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.CloseNow() })
		b, _ := json.Marshal(map[string]string{"type": "authenticate", "device_id": credential.DeviceID, "device_credential": proof})
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
		return c
	}
	bad := dial("invalid")
	if _, _, err := bad.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("invalid proof: %v", err)
	}
	browser := dial(credential.Credential)
	var remote *websocket.Conn
	select {
	case remote = <-upstreamConnections:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer remote.CloseNow()
	write := func(c *websocket.Conn, v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
	}
	read := func(c *websocket.Conn) map[string]any {
		t.Helper()
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			t.Fatalf("bad json: %s", b)
		}
		return m
	}
	read(remote) // device hello
	write(remote, map[string]any{"type": "hello", "session_id": "setup-session", "audio_params": map[string]any{"format": "pcm", "sample_rate": 16000}})
	if read(browser)["type"] != "hello" {
		t.Fatal("missing hello")
	}
	write(browser, map[string]string{"type": "onboarding", "state": "start"})
	prompt := read(browser)
	if prompt["state"] != "speaking" {
		t.Fatal(prompt)
	}
	finishPrompt := func() {
		t.Helper()
		if read(remote)["text_response"] == "" {
			t.Fatal("missing server-authored TTS")
		}
		write(remote, map[string]string{"type": "tts", "state": "start"})
		read(browser)
		write(remote, map[string]string{"type": "tts", "state": "stop"})
		read(browser)
		write(browser, map[string]any{"type": "onboarding", "state": "playback_done", "turn_id": prompt["turn_id"]})
		if read(browser)["state"] != "listening" {
			t.Fatal("not listening")
		}
		read(remote)
	}
	finishPrompt()
	write(remote, map[string]string{"type": "stt", "text": "我叫老王"})
	oldRemote := remote
	select {
	case remote = <-upstreamConnections:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer remote.CloseNow()
	read(remote)
	write(remote, map[string]any{"type": "hello", "session_id": "setup-session-2", "audio_params": map[string]any{"format": "pcm", "sample_rate": 16000}})
	if read(browser)["type"] != "hello" {
		t.Fatal("missing refreshed hello")
	}
	oldRemote.CloseNow()
	prompt = read(browser)
	if prompt["nickname"] != "老王" {
		t.Fatal(prompt)
	}
	device, _ := users.Store.DeviceByID(ctx, credential.DeviceID)
	if device.UserID != "" {
		t.Fatal("created user before confirmation")
	}
	finishPrompt()
	write(remote, map[string]string{"type": "stt", "text": "确认"})
	event := read(browser)
	if event["state"] != "confirmed" || event["nickname"] != "老王" {
		t.Fatal(event)
	}
	// The existing authenticated session endpoint owns persistence, not this socket.
	device, _ = users.Store.DeviceByID(ctx, credential.DeviceID)
	if device.UserID != "" {
		t.Fatal("setup bridge created business user")
	}
	if _, err := users.DeviceSession(ctx, "老王", credential.DeviceID, credential.Credential, time.Now()); err != nil {
		t.Fatal(err)
	}
	bound := dial(credential.Credential)
	if _, _, err := bound.Read(ctx); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("bound device allowed: %v", err)
	}
}
