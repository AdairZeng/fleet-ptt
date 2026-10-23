package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCaptainCreatesAndDriverJoins(t *testing.T) {
	h := newMux(newService(t))
	captain := sessionToken(t, h, "队长", "device-captain")
	driver := sessionToken(t, h, "司机乙", "device-driver")

	res := postAuth(t, h, "/api/v1/channels", captain, `{"name":"  装货  "}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create status %d body %s", res.Code, res.Body)
	}
	var created channelJSON
	decode(t, res, &created)
	if created.Name != "装货" || created.Role != "owner" || created.ChannelID == "" {
		t.Fatalf("created %+v", created)
	}
	if !readableJoinCode(created.JoinCode) {
		t.Fatalf("join code %q", created.JoinCode)
	}

	me := getAuth(t, h, "/api/v1/me", captain)
	var captainMe meJSON
	decode(t, me, &captainMe)
	if captainMe.Channel == nil || captainMe.Channel.ChannelID != created.ChannelID || captainMe.Channel.Role != "owner" {
		t.Fatalf("captain me %+v", captainMe)
	}

	before := getAuth(t, h, "/api/v1/me", driver)
	var driverBefore meJSON
	decode(t, before, &driverBefore)
	if driverBefore.Channel != nil {
		t.Fatalf("driver already in %+v", driverBefore.Channel)
	}

	bad := postAuth(t, h, "/api/v1/channels/join", driver, `{"code":"000000"}`)
	if bad.Code != http.StatusNotFound {
		t.Fatalf("wrong code status %d body %s", bad.Code, bad.Body)
	}
	var errBody struct {
		Error string `json:"error"`
	}
	decode(t, bad, &errBody)
	if errBody.Error != "口令不正确" {
		t.Fatalf("error %q", errBody.Error)
	}
	stillOut := getAuth(t, h, "/api/v1/me", driver)
	var driverStill meJSON
	decode(t, stillOut, &driverStill)
	if driverStill.Channel != nil {
		t.Fatalf("wrong code joined %+v", driverStill.Channel)
	}

	spaced := strings.ToLower(created.JoinCode[:3] + " " + created.JoinCode[3:])
	joinedRes := postAuth(t, h, "/api/v1/channels/join", driver, `{"code":"`+spaced+`"}`)
	if joinedRes.Code != http.StatusOK {
		t.Fatalf("join status %d body %s", joinedRes.Code, joinedRes.Body)
	}
	var joined channelJSON
	decode(t, joinedRes, &joined)
	if joined.ChannelID != created.ChannelID || joined.Name != "装货" || joined.JoinCode != created.JoinCode || joined.Role != "member" {
		t.Fatalf("joined %+v", joined)
	}

	driverMe := getAuth(t, h, "/api/v1/me", driver)
	var driverAfter meJSON
	decode(t, driverMe, &driverAfter)
	if driverAfter.Channel == nil || driverAfter.Channel.ChannelID != created.ChannelID || driverAfter.Channel.Role != "member" {
		t.Fatalf("driver me %+v", driverAfter)
	}

	again := postAuth(t, h, "/api/v1/channels/join", captain, `{"code":"`+created.JoinCode+`"}`)
	var owner channelJSON
	decode(t, again, &owner)
	if again.Code != http.StatusOK || owner.Role != "owner" {
		t.Fatalf("rejoin status %d %+v", again.Code, owner)
	}

	shortCode := postAuth(t, h, "/api/v1/channels/join", driver, `{"code":"12"}`)
	if shortCode.Code != http.StatusNotFound {
		t.Fatalf("short code status %d", shortCode.Code)
	}
	decode(t, shortCode, &errBody)
	if errBody.Error != "口令不正确" {
		t.Fatalf("short code error %q", errBody.Error)
	}
}

func TestJoinByName(t *testing.T) {
	h := newMux(newService(t))
	captain := sessionToken(t, h, "队长", "device-captain")
	driver := sessionToken(t, h, "司机乙", "device-driver")

	first := postAuth(t, h, "/api/v1/channels", captain, `{"name":"卸货"}`)
	var unloading channelJSON
	decode(t, first, &unloading)
	if first.Code != http.StatusOK {
		t.Fatalf("create %d %s", first.Code, first.Body)
	}

	byName := postAuth(t, h, "/api/v1/channels/join", driver, `{"name":"  卸货  "}`)
	var joined channelJSON
	decode(t, byName, &joined)
	if byName.Code != http.StatusOK || joined.ChannelID != unloading.ChannelID || joined.Role != "member" {
		t.Fatalf("by name status %d %+v", byName.Code, joined)
	}

	second := postAuth(t, h, "/api/v1/channels", captain, `{"name":"装货"}`)
	third := postAuth(t, h, "/api/v1/channels", captain, `{"name":"装货"}`)
	if second.Code != http.StatusOK || third.Code != http.StatusOK {
		t.Fatalf("dup create %d %d", second.Code, third.Code)
	}
	ambiguous := postAuth(t, h, "/api/v1/channels/join", driver, `{"name":"装货"}`)
	if ambiguous.Code != http.StatusConflict {
		t.Fatalf("ambiguous status %d body %s", ambiguous.Code, ambiguous.Body)
	}
	var errBody struct {
		Error string `json:"error"`
	}
	decode(t, ambiguous, &errBody)
	if errBody.Error != "有多个同名频道，请改用口令加入" {
		t.Fatalf("ambiguous error %q", errBody.Error)
	}

	missing := postAuth(t, h, "/api/v1/channels/join", driver, `{"name":"不存在"}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status %d", missing.Code)
	}
	decode(t, missing, &errBody)
	if errBody.Error != "没有找到这个频道" {
		t.Fatalf("missing error %q", errBody.Error)
	}
}

func TestChannelRequiresSession(t *testing.T) {
	h := newMux(newService(t))
	cases := []struct {
		path   string
		body   string
		status int
	}{
		{path: "/api/v1/channels", body: `{"name":"装货"}`, status: http.StatusUnauthorized},
		{path: "/api/v1/channels/join", body: `{"code":"3842K9"}`, status: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		res := postAuth(t, h, tc.path, "", tc.body)
		if res.Code != tc.status {
			t.Fatalf("%s status %d body %s", tc.path, res.Code, res.Body)
		}
	}

	token := sessionToken(t, h, "队长", "device-captain")
	bad := []struct {
		path   string
		body   string
		status int
	}{
		{path: "/api/v1/channels", body: `{"name":""}`, status: http.StatusBadRequest},
		{path: "/api/v1/channels", body: `not-json`, status: http.StatusBadRequest},
		{path: "/api/v1/channels/join", body: `{}`, status: http.StatusBadRequest},
	}
	for _, tc := range bad {
		res := postAuth(t, h, tc.path, token, tc.body)
		if res.Code != tc.status {
			t.Fatalf("%s %s status %d body %s", tc.path, tc.body, res.Code, res.Body)
		}
	}
}

type channelJSON struct {
	ChannelID string `json:"channel_id"`
	Name      string `json:"name"`
	JoinCode  string `json:"join_code"`
	Role      string `json:"role"`
}

type meJSON struct {
	UserID  string       `json:"user_id"`
	Channel *channelJSON `json:"channel"`
}

func readableJoinCode(code string) bool {
	const allowed = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	if len(code) != 6 {
		return false
	}
	for _, r := range code {
		if !strings.ContainsRune(allowed, r) {
			return false
		}
	}
	return true
}

func sessionToken(t *testing.T, h http.Handler, nickname, device string) string {
	t.Helper()
	res := postSession(t, h, `{"nickname":"`+nickname+`","device_id":"`+device+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("session %d %s", res.Code, res.Body)
	}
	var body struct {
		SessionToken string `json:"session_token"`
	}
	decode(t, res, &body)
	if body.SessionToken == "" {
		t.Fatal("empty session token")
	}
	return body.SessionToken
}

func postAuth(t *testing.T, h http.Handler, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	h.ServeHTTP(rec, req)
	return rec
}

func getAuth(t *testing.T, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status %d body %s", path, rec.Code, rec.Body)
	}
	return rec
}
