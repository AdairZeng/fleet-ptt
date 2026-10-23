package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/AdairZeng/fleet-ptt/server/internal/rtn"
)

func TestLinxChatSSE(t *testing.T) {
	const (
		token   = "linx-test-token"
		device  = "669C4BDA13EA"
		session = "session_12345"
	)
	svc := newService(t)
	h := newMuxWithLinx(svc, rtn.Signer{}, token)
	res := postSession(t, h, `{"nickname":"老王","device_id":"`+device+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("session %d %s", res.Code, res.Body)
	}

	rec := postLinx(t, h, token, linxBody(device, "建一个装货频道", session))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}
	events := parseSSE(t, rec.Body.String())
	if len(events) != 2 {
		t.Fatalf("events %#v", events)
	}
	if events[0]["event"] != "message" || events[0]["type"] != "chat" || events[0]["session_id"] != session {
		t.Fatalf("message %#v", events[0])
	}
	details, _ := events[0]["details"].(map[string]any)
	answer, _ := details["answer"].(string)
	if details["intent"] != "create_channel" || !strings.Contains(answer, "已创建装货。口令是 ") || !strings.Contains(answer, "、") {
		t.Fatalf("details %#v", details)
	}
	if events[1]["event"] != "message_end" || events[1]["session_id"] != session {
		t.Fatalf("end %#v", events[1])
	}
	if _, ok := events[1]["type"]; ok {
		t.Fatalf("message_end has type %#v", events[1])
	}

	echo := postLinx(t, h, token, linxBody(device, "你好车队", "sid/keep"))
	echoEvents := parseSSE(t, echo.Body.String())
	echoDetails, _ := echoEvents[0]["details"].(map[string]any)
	if echo.Code != http.StatusOK || echoDetails["answer"] != "收到：你好车队" || echoEvents[0]["session_id"] != "sid/keep" {
		t.Fatalf("echo status %d body %s", echo.Code, echo.Body)
	}

	badCode := postLinx(t, h, token, linxBody(device, "加入口令 384291", "s-bad-code"))
	if answer := sseAnswer(t, badCode.Body.String()); badCode.Code != http.StatusOK || answer != "口令不正确" {
		t.Fatalf("bad code status %d answer %q", badCode.Code, answer)
	}
}

func TestLinxChatRejectsIllegalCallbacks(t *testing.T) {
	const device = "device-linx"
	svc := newService(t)
	open := newMuxWithLinx(svc, rtn.Signer{}, "")
	res := postSession(t, open, `{"nickname":"老王","device_id":"`+device+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("session %d %s", res.Code, res.Body)
	}
	var sess struct {
		SessionToken string `json:"session_token"`
	}
	decode(t, res, &sess)

	unconfigured := postLinx(t, open, "anything", linxBody(device, "你好", "s-1"))
	if unconfigured.Code != http.StatusInternalServerError || !strings.Contains(unconfigured.Body.String(), "LINX_CHAT_TOKEN") {
		t.Fatalf("unconfigured %d %s", unconfigured.Code, unconfigured.Body)
	}

	h := newMuxWithLinx(svc, rtn.Signer{}, "linx-test-token")
	cases := []struct {
		name   string
		token  string
		body   string
		status int
	}{
		{"missing token", "", linxBody(device, "你好", "s-1"), http.StatusUnauthorized},
		{"wrong token", "other-token", linxBody(device, "你好", "s-1"), http.StatusUnauthorized},
		{"session token", sess.SessionToken, linxBody(device, "你好", "s-1"), http.StatusUnauthorized},
		{"unknown device", "linx-test-token", linxBody("missing-device", "你好", "s-1"), http.StatusNotFound},
		{"bad json", "linx-test-token", `{`, http.StatusBadRequest},
		{"report", "linx-test-token", `{"auth":{"device_id":"` + device + `"},"type":"report","details":{"msg":"你好","session_id":"s-1"}}`, http.StatusBadRequest},
		{"no device", "linx-test-token", `{"auth":{},"type":"chat","details":{"msg":"你好","session_id":"s-1"}}`, http.StatusBadRequest},
		{"no msg", "linx-test-token", linxBody(device, "  ", "s-1"), http.StatusBadRequest},
		{"no session", "linx-test-token", linxBody(device, "你好", ""), http.StatusBadRequest},
	}
	for _, tc := range cases {
		rec := postLinx(t, h, tc.token, tc.body)
		if rec.Code != tc.status {
			t.Fatalf("%s status %d body %s", tc.name, rec.Code, rec.Body)
		}
		if strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
			t.Fatalf("%s leaked sse %s", tc.name, rec.Body)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/linx/chat", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get %d", rec.Code)
	}
}

func TestLinxVoiceCreateJoinSwitchAndTalk(t *testing.T) {
	const token = "linx-test-token"
	svc := newService(t)
	h := newMuxWithLinx(svc, rtn.Signer{}, token)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	captainToken, captainID := sessionPair(t, h, "老王", "device-captain")
	driverToken, _ := sessionPair(t, h, "司机乙", "device-driver")

	captain := dialWS(t, srv, captainToken, false)
	captainBox := collectDeviceWS(t, captain, true)
	events := waitLinx(t, captainBox, 3, func() *httptest.ResponseRecorder {
		return postLinx(t, h, token, linxBody("device-captain", "建一个装货频道", "s-create"))
	})
	createAnswer := sseAnswer(t, events.rec.Body.String())
	got := events.fresh
	if got[0].Type != "channel_created" || got[0].Name != "装货" || got[0].JoinCode == "" {
		t.Fatalf("channel_created %#v", got[0])
	}
	if !strings.Contains(createAnswer, "已创建装货。"+speakDigits(got[0].JoinCode)) || got[2].Type != "notice" || got[2].Message != createAnswer {
		t.Fatalf("announce %q events %#v", createAnswer, got)
	}
	if got[1].Type != "members" || !hasMember(got[1], captainID, "老王", false) {
		t.Fatalf("members %#v", got[1])
	}

	loading := got[0]
	driver := dialWS(t, srv, driverToken, false)
	driverBox := collectDeviceWS(t, driver, true)
	prompt := waitLinx(t, driverBox, 1, func() *httptest.ResponseRecorder {
		return postLinx(t, h, token, linxBody("device-driver", "加入口令 "+loading.JoinCode, "s-join-1"))
	})
	if answer := sseAnswer(t, prompt.rec.Body.String()); answer != "请再说一次口令确认。"+speakDigits(loading.JoinCode) {
		t.Fatalf("prompt %q", answer)
	}
	if prompt.fresh[0].Type != "notice" {
		t.Fatalf("prompt event %#v", prompt.fresh[0])
	}
	me := getAuth(t, h, "/api/v1/me", driverToken)
	if strings.Contains(me.Body.String(), loading.ChannelID) {
		t.Fatalf("joined before confirm %s", me.Body)
	}

	joined := waitLinx(t, driverBox, 3, func() *httptest.ResponseRecorder {
		return postLinx(t, h, token, linxBody("device-driver", "加入口令 "+loading.JoinCode, "s-join-2"))
	})
	if answer := sseAnswer(t, joined.rec.Body.String()); answer != "已加入装货" {
		t.Fatalf("join %q body %s", answer, joined.rec.Body)
	}
	if joined.fresh[0].Type != "channel_switched" || joined.fresh[0].ChannelID != loading.ChannelID || joined.fresh[0].Name != "装货" {
		t.Fatalf("channel_switched %#v", joined.fresh)
	}
	if joined.fresh[2].Type != "notice" || joined.fresh[2].Message != "已加入装货" {
		t.Fatalf("join notice %#v", joined.fresh)
	}

	captainBox.wait(t, 4)
	_ = waitLinx(t, captainBox, 3, func() *httptest.ResponseRecorder {
		return postLinx(t, h, token, linxBody("device-captain", "建一个卸货频道", "s-create-2"))
	})
	beforeTalk := len(captainBox.snapshot())
	if res := postAuth(t, h, "/api/v1/talk/start", captainToken, `{}`); res.Code != http.StatusOK {
		t.Fatalf("talk %d %s", res.Code, res.Body)
	}
	captainBox.wait(t, beforeTalk+2)
	switched := waitLinx(t, captainBox, 5, func() *httptest.ResponseRecorder {
		return postLinx(t, h, token, linxBody("device-captain", "切到装货", "s-switch"))
	})
	if answer := sseAnswer(t, switched.rec.Body.String()); answer != "已切换到装货" {
		t.Fatalf("switch %q events %#v", answer, switched.fresh)
	}
	talkEndAt, switchAt := -1, -1
	for i, ev := range switched.fresh {
		if ev.Type == "talk_end" && ev.UserID == captainID {
			talkEndAt = i
		}
		if ev.Type == "channel_switched" && ev.ChannelID == loading.ChannelID && ev.Name == "装货" {
			switchAt = i
		}
	}
	if talkEndAt < 0 || switchAt < 0 || talkEndAt > switchAt {
		t.Fatalf("order talk_end %d switch %d events %#v", talkEndAt, switchAt, switched.fresh)
	}

	missed := waitLinx(t, captainBox, 1, func() *httptest.ResponseRecorder {
		return postLinx(t, h, token, linxBody("device-captain", "切到不存在", "s-miss"))
	})
	if missed.rec.Code != http.StatusOK {
		t.Fatalf("miss status %d %s", missed.rec.Code, missed.rec.Body)
	}
	if answer := sseAnswer(t, missed.rec.Body.String()); answer != "未匹配到频道" {
		t.Fatalf("miss %q", answer)
	}
	if missed.fresh[0].Type != "error" || missed.fresh[0].Message != "未匹配到频道" {
		t.Fatalf("miss event %#v", missed.fresh)
	}
	still := getAuth(t, h, "/api/v1/me", captainToken)
	if !strings.Contains(still.Body.String(), loading.ChannelID) {
		t.Fatalf("left channel after miss %s", still.Body)
	}

	started := waitLinx(t, captainBox, 3, func() *httptest.ResponseRecorder {
		return postLinx(t, h, token, linxBody("device-captain", "开始对讲", "s-talk"))
	})
	if answer := sseAnswer(t, started.rec.Body.String()); answer != "已开始对讲" {
		t.Fatalf("talk_start %q events %#v", answer, started.fresh)
	}
	var talkStart wsEvent
	for _, ev := range started.fresh {
		if ev.Type == "talk_start" {
			talkStart = ev
		}
	}
	if talkStart.ChannelID != loading.ChannelID || talkStart.UserID != captainID {
		t.Fatalf("talk_start event %#v", started.fresh)
	}

	beforeList := len(driverBox.snapshot())
	listed := postLinx(t, h, token, linxBody("device-captain", "我有哪些频道", "s-list"))
	listAnswer := sseAnswer(t, listed.Body.String())
	if !strings.Contains(listAnswer, "装货") || !strings.Contains(listAnswer, "卸货") || !strings.Contains(listAnswer, "当前是装货") {
		t.Fatalf("list %q", listAnswer)
	}
	driverBox.wait(t, beforeList+2)

	byName := waitLinx(t, driverBox, 3, func() *httptest.ResponseRecorder {
		return postLinx(t, h, token, linxBody("device-driver", "进入卸货", "s-name"))
	})
	if answer := sseAnswer(t, byName.rec.Body.String()); answer != "已加入卸货" {
		t.Fatalf("name join %q events %#v", answer, byName.fresh)
	}
	if byName.fresh[0].Type != "channel_switched" || byName.fresh[0].Name != "卸货" {
		t.Fatalf("name switch %#v", byName.fresh)
	}

	_ = captain.Close(websocket.StatusNormalClosure, "")
	_ = driver.Close(websocket.StatusNormalClosure, "")
}

type linxWait struct {
	rec   *httptest.ResponseRecorder
	fresh []wsEvent
}

func waitLinx(t *testing.T, box *wsBox, n int, fn func() *httptest.ResponseRecorder) linxWait {
	t.Helper()
	before := len(box.snapshot())
	rec := fn()
	box.wait(t, before+n)
	all := box.snapshot()
	if len(all) < before+n {
		t.Fatalf("events %d want %d", len(all), before+n)
	}
	return linxWait{rec: rec, fresh: all[before : before+n]}
}

func sseAnswer(t *testing.T, body string) string {
	t.Helper()
	events := parseSSE(t, body)
	details, _ := events[0]["details"].(map[string]any)
	answer, _ := details["answer"].(string)
	if answer == "" {
		t.Fatalf("missing answer %s", body)
	}
	return answer
}

func speakDigits(code string) string {
	parts := make([]string, 0, len(code))
	for _, r := range code {
		parts = append(parts, string(r))
	}
	return "口令是 " + strings.Join(parts, "、")
}

func postLinx(t *testing.T, h http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/linx/chat", strings.NewReader(body))
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	h.ServeHTTP(rec, req)
	return rec
}

func linxBody(device, msg, session string) string {
	payload, err := json.Marshal(map[string]any{
		"auth": map[string]string{"device_id": device},
		"type": "chat",
		"details": map[string]any{
			"msg":        msg,
			"session_id": session,
			"timestamp":  1640995200000,
		},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func parseSSE(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, chunk := range strings.Split(body, "\n\n") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		line, ok := strings.CutPrefix(chunk, "data: ")
		if !ok {
			t.Fatalf("sse chunk %q", chunk)
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}
