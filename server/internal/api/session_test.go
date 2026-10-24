package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/api"
	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/rtn"
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
		if res.Code != http.StatusBadRequest && res.Code != http.StatusUnauthorized {
			t.Fatalf("body %s status %d", body, res.Code)
		}
	}
}

func TestRegisterDeviceReturnsHardwareIdentity(t *testing.T) {
	svc := newService(t)
	h := newMux(svc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/device/register", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var got struct {
		DeviceID   string `json:"device_id"`
		MacID      string `json:"mac_id"`
		Credential string `json:"device_credential"`
	}
	decode(t, rec, &got)
	if got.DeviceID == "" || got.MacID == "" || len(got.Credential) != 64 {
		t.Fatalf("unexpected device proof %+v", got)
	}
	device, err := svc.Store.DeviceByID(context.Background(), got.DeviceID)
	if err != nil || device.MacID != got.MacID {
		t.Fatalf("stored device %+v err=%v", device, err)
	}
}

// Scenario fixtures create a new device proof before calling the production handler.
func postSession(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody struct {
		Nickname string `json:"nickname"`
		DeviceID string `json:"device_id"`
	}
	if json.Unmarshal([]byte(body), &reqBody) == nil && reqBody.Nickname != "" && reqBody.DeviceID != "" {
		fixture := h.(*fixtureMux)
		owner, err := fixture.users.Enter(context.Background(), reqBody.Nickname, reqBody.DeviceID, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		proof, err := fixture.users.RegisterDevice(context.Background(), &owner.User, reqBody.DeviceID, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(map[string]string{"nickname": reqBody.Nickname, "device_id": proof.DeviceID, "device_credential": proof.Credential})
		if err != nil {
			t.Fatal(err)
		}
		body = string(raw)
	}
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
	return newMuxWithSigner(svc, rtn.Signer{
		AccessKey: "test-ak",
		SecretKey: "test-sk",
		AppID:     "test-app",
	})
}

func newMuxWithSigner(svc *user.Service, signer rtn.Signer) http.Handler {
	return newMuxWithLinx(svc, signer, "")
}

func newMuxWithLinx(svc *user.Service, signer rtn.Signer, linxToken string) http.Handler {
	mux := http.NewServeMux()
	api.Register(mux, svc, &channel.Service{Store: svc.Store}, signer, linxToken, "test-agent")
	return &fixtureMux{Handler: mux, users: svc}
}

type fixtureMux struct {
	http.Handler
	users *user.Service
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
