package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/linxbridge"
	"github.com/AdairZeng/fleet-ptt/server/internal/rtn"
)

type mcpReply struct {
	Result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error json.RawMessage `json:"error"`
}

func (r mcpReply) text() string {
	if len(r.Result.Content) == 0 {
		return ""
	}
	return r.Result.Content[0].Text
}

type mcpClient struct {
	t       *testing.T
	srv     *httptest.Server
	headers http.Header
	id      int
}

func newMCPClient(t *testing.T, srv *httptest.Server, device, session string) *mcpClient {
	t.Helper()
	c := &mcpClient{t: t, srv: srv, headers: make(http.Header)}
	c.headers.Set("Authorization", "Bearer linx-test-token")
	c.headers.Set("X-Linx-Device-Id", device)
	c.headers.Set("X-Linx-Agent-Id", "test-agent")
	c.headers.Set("X-Linx-Session-Id", session)
	c.headers.Set("Accept", "application/json, text/event-stream")
	c.headers.Set("MCP-Protocol-Version", "2025-06-18")
	c.rpc("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "test", "version": "1"}})
	if c.headers.Get("Mcp-Session-Id") == "" {
		t.Fatal("missing transport session")
	}
	c.post(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, http.StatusAccepted)
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/mcp", nil)
		req.Header = c.headers.Clone()
		res, err := srv.Client().Do(req)
		if err == nil {
			res.Body.Close()
		}
	})
	return c
}
func (c *mcpClient) post(body any, status int) []byte {
	c.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, c.srv.URL+"/api/v1/mcp", bytes.NewReader(b))
	req.Header = c.headers.Clone()
	req.Header.Set("Content-Type", "application/json")
	res, err := c.srv.Client().Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if res.StatusCode != status {
		c.t.Fatalf("POST %d %s", res.StatusCode, data)
	}
	if session := res.Header.Get("Mcp-Session-Id"); session != "" {
		c.headers.Set("Mcp-Session-Id", session)
	}
	if status == http.StatusOK && !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		c.t.Fatal("expected JSON response")
	}
	return data
}
func (c *mcpClient) rpc(method string, params any) mcpReply {
	c.t.Helper()
	c.id++
	data := c.post(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params}, http.StatusOK)
	var result mcpReply
	if err := json.Unmarshal(data, &result); err != nil {
		c.t.Fatal(err)
	}
	return result
}
func (c *mcpClient) call(name string, args any) mcpReply {
	return c.rpc("tools/call", map[string]any{"name": name, "arguments": args})
}
func requireAnswer(t *testing.T, r mcpReply, want string) {
	t.Helper()
	if len(r.Error) > 0 || r.Result.IsError || !strings.Contains(r.text(), want) {
		t.Fatalf("want %q got %+v", want, r)
	}
}

func TestLinxMCPBusinessFlow(t *testing.T) {
	svc := newService(t)
	h := newMuxWithLinx(svc, rtn.Signer{}, "linx-test-token")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	captainToken, captainID := sessionPair(t, h, "老王", "captain-mac")
	driverToken, _ := sessionPair(t, h, "司机乙", "driver-mac")
	captainWS := dialWS(t, srv, captainToken, false)
	captainBox := collectDeviceWS(t, captainWS, true)
	driverWS := dialWS(t, srv, driverToken, false)
	collectDeviceWS(t, driverWS, true)
	captainWake := armMCP(t, h, "captain-mac", "voice-captain")
	armMCP(t, h, "driver-mac", "voice-driver")
	captain := newMCPClient(t, srv, "captain-mac", "voice-captain")
	driver := newMCPClient(t, srv, "driver-mac", "voice-driver")
	requireAnswer(t, captain.call("create_channel", map[string]any{"name": "装货"}), "已创建装货")
	captainBox.wait(t, 3)
	var loading wsEvent
	for _, event := range captainBox.snapshot() {
		if event.Type == "channel_created" {
			loading = event
			break
		}
	}
	requireAnswer(t, driver.call("join_channel", map[string]any{"code": loading.JoinCode}), "请说确认加入或取消")
	if strings.Contains(getAuth(t, h, "/api/v1/me", driverToken).Body.String(), loading.ChannelID) {
		t.Fatal("joined before confirmation")
	}
	// Another device and another conversation cannot confirm this pending operation.
	captain.call("confirm_action", map[string]any{})
	other := newMCPClient(t, srv, "driver-mac", "other-conversation")
	other.call("confirm_action", map[string]any{})
	if strings.Contains(getAuth(t, h, "/api/v1/me", driverToken).Body.String(), loading.ChannelID) {
		t.Fatal("cross-session confirmation succeeded")
	}
	requireAnswer(t, driver.call("confirm_action", map[string]any{}), "已加入装货")
	requireAnswer(t, captain.call("create_channel", map[string]any{"name": "卸货"}), "已创建卸货")
	captainWake.Control("cancel", "")
	talkTurn, ok := captainWake.STT("你好小路，开始对讲")
	if !ok {
		t.Fatal("talk wake rejected")
	}
	captainWake.Control("ready", talkTurn.TurnID)
	requireAnswer(t, captain.call("talk_start", map[string]any{}), "已开始对讲")
	captainWake.Output("tts", "start")
	captainWake.Output("tts", "stop")
	completion, ok := captainWake.Control("playback_done", talkTurn.TurnID)
	if !ok || completion.State != "standby" {
		t.Fatal("talk_start left command followup active")
	}
	if _, ok := captainWake.STT("大家开始装货"); ok {
		t.Fatal("channel speech accepted as command")
	}
	next, _ := captainWake.STT("你好小路，切换装货频道")
	captainWake.Control("ready", next.TurnID)

	requireAnswer(t, captain.call("switch_channel", map[string]any{"name": "装货"}), "已切换到装货")
	events := captainBox.snapshot()
	endAt, switchAt := -1, -1
	for i, ev := range events {
		if ev.Type == "talk_end" && ev.UserID == captainID {
			endAt = i
		}
		if ev.Type == "channel_switched" && ev.ChannelID == loading.ChannelID {
			switchAt = i
		}
	}
	if endAt < 0 || switchAt < endAt {
		t.Fatalf("talk end before switch: %+v", events)
	}
	miss := captain.call("switch_channel", map[string]any{"name": "不存在"})
	if !miss.Result.IsError {
		t.Fatalf("business failure lost: %+v", miss)
	}
	requireAnswer(t, captain.call("list_my_channels", map[string]any{}), "装货")
	requireAnswer(t, captain.call("set_volume", map[string]any{"level": 0}), "音量已调到0")
	requireAnswer(t, driver.call("set_nickname", map[string]any{"name": "小张"}), "请说确认或取消")
	requireAnswer(t, driver.call("confirm_action", map[string]any{}), "小张")
	if !strings.Contains(getAuth(t, h, "/api/v1/me", driverToken).Body.String(), "小张") {
		t.Fatal("nickname not persisted")
	}
	requireAnswer(t, driver.call("set_nickname", map[string]any{"name": "小李"}), "确认")
	driver.call("cancel_action", map[string]any{})
	driver.call("confirm_action", map[string]any{})
	if strings.Contains(getAuth(t, h, "/api/v1/me", driverToken).Body.String(), "小李") {
		t.Fatal("cancelled nickname applied")
	}
}

func TestLinxMCPRejectsInvalidTools(t *testing.T) {
	svc := newService(t)
	h := newMuxWithLinx(svc, rtn.Signer{}, "linx-test-token")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	token, _ := sessionPair(t, h, "老王", "captain-mac")
	c := newMCPClient(t, srv, "captain-mac", "voice-1")
	for _, tc := range []struct {
		name string
		args any
	}{
		{"create_channel", map[string]any{"name": "装货", "device_id": "victim"}},
		{"create_channel", map[string]any{"name": " "}},
		{"create_channel", map[string]any{"name": nil}},
		{"join_channel", map[string]any{"name": "装货", "code": "ABCDEF"}},
		{"switch_channel", map[string]any{"index": 1.5}},
		{"set_volume", map[string]any{"level": 101}},
		{"set_volume", map[string]any{"level": nil}},
		{"set_volume", map[string]any{"level": 50, "adjustment": "up"}},
		{"talk_start", map[string]any{"device_id": "victim"}},
		{"unknown", map[string]any{}},
	} {
		r := c.call(tc.name, tc.args)
		if len(r.Error) == 0 && !r.Result.IsError {
			t.Errorf("accepted %s %+v: %+v", tc.name, tc.args, r)
		}
	}
	unknown := newMCPClient(t, srv, "unknown-mac", "voice-1")
	r := unknown.call("list_my_channels", map[string]any{})
	if !r.Result.IsError && len(r.Error) == 0 {
		t.Fatal("unknown device accepted")
	}
	// Device-dependent actions must fail without an online device acknowledgement.
	armMCP(t, h, "captain-mac", "voice-1")
	r = c.call("set_volume", map[string]any{"level": 50})
	if !r.Result.IsError {
		t.Fatalf("offline action reported success: %+v", r)
	}
	if !strings.Contains(getAuth(t, h, "/api/v1/me", token).Body.String(), `"channel_id":"public"`) {
		t.Fatal("invalid args changed channel")
	}
}

func armMCP(t *testing.T, h http.Handler, mac, session string) *linxbridge.WakeSession {
	t.Helper()
	fixture := h.(*fixtureMux)
	u, err := fixture.users.Store.UserByMacID(context.Background(), mac)
	if err != nil {
		t.Fatal(err)
	}
	s := fixture.wake.Open(u.DeviceID, session, nil, nil)
	t.Cleanup(func() { fixture.wake.Close(u.DeviceID, s) })
	event, ok := s.STT("你好小路，执行指令")
	if !ok {
		t.Fatal("wake rejected")
	}
	if _, ok := s.Control("ready", event.TurnID); !ok {
		t.Fatal("wake ack rejected")
	}
	return s
}

func TestLinxMCPRequiresAcknowledgedVoiceWake(t *testing.T) {
	svc := newService(t)
	h := newMuxWithLinx(svc, rtn.Signer{}, "linx-test-token")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	token, _ := sessionPair(t, h, "老王", "captain-mac")
	client := newMCPClient(t, srv, "captain-mac", "voice-1")
	result := client.call("create_channel", map[string]any{"name": "禁止创建"})
	if !result.Result.IsError {
		t.Fatal("unawake tool succeeded")
	}
	if !strings.Contains(getAuth(t, h, "/api/v1/me", token).Body.String(), `"channel_id":"public"`) {
		t.Fatal("unawake call changed channel")
	}
	s := armMCP(t, h, "captain-mac", "voice-1")
	requireAnswer(t, client.call("list_my_channels", map[string]any{}), "频道")
	wrong := newMCPClient(t, srv, "captain-mac", "voice-other")
	if !wrong.call("list_my_channels", map[string]any{}).Result.IsError {
		t.Fatal("wrong voice session accepted")
	}
	s.Control("cancel", "")
	s.Control("manual", "")
	if !client.call("list_my_channels", map[string]any{}).Result.IsError {
		t.Fatal("manual window authorized without STT")
	}
	event, ok := s.STT("我有哪些频道")
	if !ok {
		t.Fatal("manual STT rejected")
	}
	done := make(chan struct{})
	go func() { defer close(done); time.Sleep(20 * time.Millisecond); s.Control("ready", event.TurnID) }()
	requireAnswer(t, client.call("list_my_channels", map[string]any{}), "频道")
	<-done
}
