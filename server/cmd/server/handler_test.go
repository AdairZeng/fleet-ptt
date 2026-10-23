package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/rtn"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

func TestHealthz(t *testing.T) {
	users, channels := testDeps(t)
	h := newHandler(filepath.Join(t.TempDir(), "missing"), users, channels, rtn.Signer{}, "")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Body.String() != "OK\n" {
		t.Fatalf("body %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post status %d", rec.Code)
	}
}

func TestStaticSite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("js"), 0o644); err != nil {
		t.Fatal(err)
	}

	users, channels := testDeps(t)
	h := newHandler(dir, users, channels, rtn.Signer{}, "")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "js" {
		t.Fatalf("asset status %d body %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nickname", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "home" {
		t.Fatalf("spa status %d body %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/join/3842K9", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "home" {
		t.Fatalf("join link status %d body %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Body.String() != "OK\n" {
		t.Fatalf("healthz shadowed by static: %q", rec.Body.String())
	}
}

func TestSessionNotShadowedByStatic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	users, channels := testDeps(t)
	h := newHandler(dir, users, channels, rtn.Signer{}, "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"nickname":"老王","device_id":"device-1"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "home") {
		t.Fatal("session response was the static page")
	}
}

func TestChannelNotShadowedByStatic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	users, channels := testDeps(t)
	h := newHandler(dir, users, channels, rtn.Signer{}, "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"nickname":"队长","device_id":"device-1"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session %d %s", rec.Code, rec.Body)
	}
	var sess struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/channels", strings.NewReader(`{"name":"装货"}`))
	req.Header.Set("Authorization", "Bearer "+sess.SessionToken)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "home") || !strings.Contains(rec.Body.String(), `"join_code"`) {
		t.Fatalf("body %s", rec.Body)
	}
}

func TestRoomTokenNotShadowedByStatic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	users, channels := testDeps(t)
	h := newHandler(dir, users, channels, rtn.Signer{
		AccessKey: "test-ak",
		SecretKey: "test-sk-should-not-leak",
		AppID:     "test-app",
	}, "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"nickname":"队长","device_id":"device-1"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session %d %s", rec.Code, rec.Body)
	}
	var sess struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/channels", strings.NewReader(`{"name":"装货"}`))
	req.Header.Set("Authorization", "Bearer "+sess.SessionToken)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("channel %d %s", rec.Code, rec.Body)
	}
	var created struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/channels/"+created.ChannelID+"/rtn-token", nil)
	req.Header.Set("Authorization", "Bearer "+sess.SessionToken)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if strings.Contains(body, "home") || !strings.Contains(body, `"token"`) || strings.Contains(body, "test-sk-should-not-leak") {
		t.Fatalf("body %s", body)
	}
}

func TestSignalingNotShadowedByStatic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	users, channels := testDeps(t)
	h := newHandler(dir, users, channels, rtn.Signer{}, "")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil))
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "home") || !strings.Contains(rec.Body.String(), "未登录") {
		t.Fatalf("ws status %d body %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/talk/start", nil))
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "home") {
		t.Fatalf("talk status %d body %s", rec.Code, rec.Body)
	}
}

func TestLinxChatNotShadowedByStatic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	users, channels := testDeps(t)
	h := newHandler(dir, users, channels, rtn.Signer{}, "linx-test-token")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"nickname":"老王","device_id":"device-linx"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/linx/chat", strings.NewReader(`{"auth":{"device_id":"device-linx"},"type":"chat","details":{"msg":"你好","session_id":"s-1"}}`))
	req.Header.Set("Authorization", "Bearer linx-test-token")
	req.Header.Set("Accept", "text/event-stream")
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Contains(body, "home") || !strings.Contains(body, `"answer"`) || !strings.Contains(body, `"session_id":"s-1"`) {
		t.Fatalf("linx status %d body %s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}
}

func TestPublicBaseURL(t *testing.T) {
	if got := publicBaseURL(" https://fleet.example/ "); got != "https://fleet.example" {
		t.Fatalf("base %q", got)
	}
	if got := publicBaseURL("https://user:secret@fleet.example"); got != "" {
		t.Fatalf("userinfo %q", got)
	}
	if got := publicBaseURL("not a url"); got != "" {
		t.Fatalf("invalid %q", got)
	}
}

func testDeps(t *testing.T) (*user.Service, *channel.Service) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &user.Service{Store: st, Secret: "test-secret"}, &channel.Service{Store: st}
}
