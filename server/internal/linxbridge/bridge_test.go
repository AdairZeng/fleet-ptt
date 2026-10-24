package linxbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestConfigReady(t *testing.T) {
	if (Config{URL: DefaultURL, DeviceID: "02:46:50:50:54:01", Token: "token"}).Ready() != true {
		t.Fatal("expected complete config to be ready")
	}
	if (Config{URL: DefaultURL, DeviceID: "", Token: "token"}).Ready() {
		t.Fatal("expected missing device id to disable bridge")
	}
}

func TestDeviceHello(t *testing.T) {
	got := deviceHello()
	if got.Type != "hello" || got.Transport != "websocket" || got.Version != 1 {
		t.Fatalf("unexpected hello: %+v", got)
	}
	if got.Features.(map[string]any)["mcp"] != false {
		t.Fatal("cloud MCP must not advertise unsupported device MCP")
	}
	if got.AudioParams.Format != "pcm" || got.AudioParams.SampleRate != 16000 || got.AudioParams.FrameSize != 320 {
		t.Fatalf("unexpected audio params: %+v", got.AudioParams)
	}
}

func TestNewClientID(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first, err := newClientID()
	if err != nil || !pattern.MatchString(first) {
		t.Fatalf("invalid client id %q: %v", first, err)
	}
	second, err := newClientID()
	if err != nil || first == second {
		t.Fatalf("client ids are not unique: %q %q", first, second)
	}
}

func socketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	t.Cleanup(srv.Close)
	client, _, err := websocket.Dial(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := <-accepted
	t.Cleanup(func() { client.CloseNow(); server.CloseNow() })
	return client, server
}

func TestBridgeSuppressesAmbientResponsesAndRequiresAck(t *testing.T) {
	upstream, remote := socketPair(t)
	browser, local := socketPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	registry := NewWakeRegistry()
	state := &bridgeWake{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Handler{Wake: registry}.remoteToLocal(ctx, remote, &writeConn{c: local}, cancel, "device", state)
	}()
	defer func() { cancel(); <-done; registry.Close("device", state.get()) }()
	send := func(typ websocket.MessageType, payload string) {
		t.Helper()
		if err := upstream.Write(ctx, typ, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	read := func(wantType, wantState string) {
		t.Helper()
		typ, data, err := browser.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if typ != websocket.MessageText || json.Unmarshal(data, &got) != nil || got["type"] != wantType || (wantState != "" && got["state"] != wantState) {
			t.Fatalf("unexpected message: %s", data)
		}
	}
	send(websocket.MessageText, `{"type":"hello","session_id":"voice-1"}`)
	read("hello", "")
	send(websocket.MessageText, `{"type":"stt","text":"创建一个频道"}`)
	read("wake_missed", "")
	send(websocket.MessageText, `{"type":"tts","state":"start"}`)
	send(websocket.MessageBinary, "ambient audio")
	send(websocket.MessageText, `{"type":"tts","state":"stop"}`)
	send(websocket.MessageText, `{"type":"stt","text":"你好小路，创建装货频道"}`)
	typ, data, err := browser.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("wake read: %v", err)
	}
	var event WakeEvent
	if json.Unmarshal(data, &event) != nil || event.State != "detected" || event.SessionID != "voice-1" {
		t.Fatalf("ambient output leaked: %s", data)
	}
	read("stt", "")
	send(websocket.MessageText, `{"type":"tts","state":"start"}`)
	// The upstream may begin its answer before the browser has stopped RTN.
	state.get().Control("ready", event.TurnID)
	read("tts", "start")
	send(websocket.MessageBinary, "accepted audio")
	typ, data, err = browser.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != "accepted audio" {
		t.Fatalf("accepted output missing: %s %v", data, err)
	}
	send(websocket.MessageText, `{"type":"tts","state":"stop"}`)
	read("tts", "stop")
	if state.get().CaptureAllowed() {
		t.Fatal("microphone allowed while draining")
	}
	state.get().Control("playback_done", event.TurnID)
	if !state.get().CaptureAllowed() {
		t.Fatal("followup microphone suppressed")
	}
}
