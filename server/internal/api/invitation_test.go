package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInvitationHTTPRequiresAuthentication(t *testing.T) {
	h := newMux(newService(t))
	for _, path := range []string{"/api/v1/online-members", "/api/v1/invitations"} {
		res := httptest.NewRecorder()
		h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s status %d", path, res.Code)
		}
	}
	for _, path := range []string{"/api/v1/invitations", "/api/v1/invitations/unknown/accept", "/api/v1/invitations/unknown/reject"} {
		if res := postAuth(t, h, path, "", `{}`); res.Code != http.StatusUnauthorized {
			t.Fatalf("%s status %d", path, res.Code)
		}
	}
}
func TestInvitationHTTPValidatesRecipientAndRejectsOffline(t *testing.T) {
	h := newMux(newService(t))
	token, _ := sessionPair(t, h, "小李", "invite-http")
	for _, body := range []string{`{}`, `{"name":"装货"}`, `{"name":"装货","recipient_name":"老王","recipient_id":"x"}`} {
		if res := postAuth(t, h, "/api/v1/invitations", token, body); res.Code != http.StatusBadRequest {
			t.Fatalf("%s status %d: %s", body, res.Code, res.Body)
		}
	}
	if res := postAuth(t, h, "/api/v1/invitations", token, `{"name":"装货","recipient_name":"老王"}`); res.Code != http.StatusConflict {
		t.Fatalf("offline status %d", res.Code)
	}
	if res := postAuth(t, h, "/api/v1/invitations/unknown/accept", token, `{}`); res.Code != http.StatusConflict {
		t.Fatalf("unknown invitation status %d", res.Code)
	}
	for _, path := range []string{"/api/v1/online-members", "/api/v1/invitations"} {
		if res := getAuth(t, h, path, token); res.Code != http.StatusOK {
			t.Fatalf("%s status %d", path, res.Code)
		}
	}
}
