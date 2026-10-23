package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/AdairZeng/fleet-ptt/server/internal/api"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

func TestSessionAndMe(t *testing.T) {
	svc := newService(t)
	h := newMux(svc)
	const device = "123e4567-e89b-42d3-a456-426614174000"

	res := postSession(t, h, `{"nickname":"  司机甲  ","device_id":"`+device+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status %d body %s", res.Code, res.Body)
	}
	var created struct {
		UserID       string `json:"user_id"`
		SessionToken string `json:"session_token"`
	}
	decode(t, res, &created)
	if created.UserID == "" || created.SessionToken == "" {
		t.Fatalf("body %+v", created)
	}

	again := postSession(t, h, `{"nickname":"司机甲改","device_id":"`+device+`"}`)
	var updated struct {
		UserID string `json:"user_id"`
	}
	decode(t, again, &updated)
	if updated.UserID != created.UserID {
		t.Fatalf("user id %s then %s", created.UserID, updated.UserID)
	}

	got, err := svc.UserByDeviceID(context.Background(), device)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.UserID || got.DisplayName != "司机甲改" {
		t.Fatalf("stored %+v", got)
	}

	me := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+created.SessionToken)
	h.ServeHTTP(me, req)
	if me.Code != http.StatusOK {
		t.Fatalf("me status %d body %s", me.Code, me.Body)
	}
	var profile struct {
		UserID   string `json:"user_id"`
		Nickname string `json:"nickname"`
		DeviceID string `json:"device_id"`
	}
	decode(t, me, &profile)
	if profile.UserID != created.UserID || profile.Nickname != "司机甲改" || profile.DeviceID != device {
		t.Fatalf("profile %+v", profile)
	}

	noAuth := httptest.NewRecorder()
	h.ServeHTTP(noAuth, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if noAuth.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status %d", noAuth.Code)
	}
}

func TestSessionBadRequest(t *testing.T) {
	h := newMux(newService(t))
	cases := []string{
		`{"nickname":"","device_id":"device-1"}`,
		`{"nickname":"老王","device_id":""}`,
		`not-json`,
	}
	for _, body := range cases {
		res := postSession(t, h, body)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("body %s status %d", body, res.Code)
		}
	}
}

func postSession(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session", bytes.NewBufferString(body))
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dest); err != nil {
		t.Fatal(err)
	}
}

func newMux(svc *user.Service) http.Handler {
	mux := http.NewServeMux()
	api.Register(mux, svc)
	return mux
}

func newService(t *testing.T) *user.Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &user.Service{Store: st, Secret: "test-secret"}
}
