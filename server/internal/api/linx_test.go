package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if details["answer"] != "已收到建群指令，稍后播报口令" || details["intent"] != "create_channel" {
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

func TestLinxChatDoesNotExecuteIntent(t *testing.T) {
	const device = "device-linx"
	svc := newService(t)
	h := newMuxWithLinx(svc, rtn.Signer{}, "linx-test-token")
	res := postSession(t, h, `{"nickname":"老王","device_id":"`+device+`"}`)
	var sess struct {
		SessionToken string `json:"session_token"`
	}
	decode(t, res, &sess)

	rec := postLinx(t, h, "linx-test-token", linxBody(device, "建一个装货频道", "s-1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("chat %d %s", rec.Code, rec.Body)
	}

	list := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/channels", nil)
	req.Header.Set("Authorization", "Bearer "+sess.SessionToken)
	h.ServeHTTP(list, req)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"channels":[]`) {
		t.Fatalf("channels %d %s", list.Code, list.Body)
	}
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
