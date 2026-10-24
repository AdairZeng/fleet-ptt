package api_test

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/rtn"
)

func TestRoomTokenSameRoomForTwoMembers(t *testing.T) {
	secret := "super-secret-value"
	signer := rtn.Signer{AccessKey: "AKIDEXAMPLE", SecretKey: secret, AppID: "app-example"}
	h := newMuxWithSigner(newService(t), signer)
	captain := sessionToken(t, h, "队长", "device-captain")
	driver := sessionToken(t, h, "司机乙", "device-driver")

	createdRes := postAuth(t, h, "/api/v1/channels", captain, `{"name":"装货"}`)
	if createdRes.Code != http.StatusOK {
		t.Fatalf("create %d %s", createdRes.Code, createdRes.Body)
	}
	var created channelJSON
	decode(t, createdRes, &created)

	otherRes := postAuth(t, h, "/api/v1/channels", captain, `{"name":"卸货"}`)
	if otherRes.Code != http.StatusOK {
		t.Fatalf("second create %d %s", otherRes.Code, otherRes.Body)
	}
	var other channelJSON
	decode(t, otherRes, &other)

	noAuth := getToken(t, h, created.ChannelID, "")
	if noAuth.Code != http.StatusUnauthorized {
		t.Fatalf("no auth %d %s", noAuth.Code, noAuth.Body)
	}

	missing := getToken(t, h, "missing-channel", captain)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing %d %s", missing.Code, missing.Body)
	}
	assertError(t, missing, "没有找到这个频道")

	outsider := getToken(t, h, created.ChannelID, driver)
	if outsider.Code != http.StatusForbidden {
		t.Fatalf("outsider %d %s", outsider.Code, outsider.Body)
	}
	assertError(t, outsider, "还不是这个频道的成员")

	joinedRes := postAuth(t, h, "/api/v1/channels/join", driver, `{"code":"`+created.JoinCode+`"}`)
	if joinedRes.Code != http.StatusOK {
		t.Fatalf("join %d %s", joinedRes.Code, joinedRes.Body)
	}

	wrongRoom := getToken(t, h, other.ChannelID, driver)
	if wrongRoom.Code != http.StatusForbidden {
		t.Fatalf("other channel %d %s", wrongRoom.Code, wrongRoom.Body)
	}

	captainToken := readToken(t, getToken(t, h, created.ChannelID, captain))
	driverToken := readToken(t, getToken(t, h, created.ChannelID, driver))
	if captainToken.RoomName != created.ChannelID || driverToken.RoomName != created.ChannelID {
		t.Fatalf("rooms captain %s driver %s channel %s", captainToken.RoomName, driverToken.RoomName, created.ChannelID)
	}
	if captainToken.UserID == driverToken.UserID || captainToken.UserID == "" {
		t.Fatalf("users captain %s driver %s", captainToken.UserID, driverToken.UserID)
	}
	if captainToken.AppID != "app-example" || driverToken.AppID != "app-example" {
		t.Fatalf("app ids %+v %+v", captainToken, driverToken)
	}
	verifyRoomToken(t, signer, captainToken)
	verifyRoomToken(t, signer, driverToken)
	if strings.Contains(captainToken.Token, secret) || strings.Contains(driverToken.Token, secret) {
		t.Fatal("secret key leaked into token")
	}
}

func TestRoomTokenRequiresQiniuConfig(t *testing.T) {
	h := newMuxWithSigner(newService(t), rtn.Signer{})
	captain := sessionToken(t, h, "队长", "device-captain")
	createdRes := postAuth(t, h, "/api/v1/channels", captain, `{"name":"装货"}`)
	var created channelJSON
	decode(t, createdRes, &created)
	if createdRes.Code != http.StatusOK {
		t.Fatalf("create %d %s", createdRes.Code, createdRes.Body)
	}
	res := getToken(t, h, created.ChannelID, captain)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status %d %s", res.Code, res.Body)
	}
	assertError(t, res, "服务器未配置七牛 RTN")
	if strings.Contains(res.Body.String(), `"token"`) {
		t.Fatalf("unconfigured response included a token: %s", res.Body)
	}
}

type tokenJSON struct {
	Token    string `json:"token"`
	AppID    string `json:"app_id"`
	RoomName string `json:"room_name"`
	UserID   string `json:"user_id"`
	ExpireAt int64  `json:"expire_at"`
}

func getToken(t *testing.T, h http.Handler, channelID, session string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/channels/"+channelID+"/rtn-token", nil)
	if session != "" {
		req.Header.Set("Authorization", "Bearer "+session)
	}
	h.ServeHTTP(rec, req)
	return rec
}

func readToken(t *testing.T, rec *httptest.ResponseRecorder) tokenJSON {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("token status %d %s", rec.Code, rec.Body)
	}
	var body tokenJSON
	decode(t, rec, &body)
	if body.Token == "" {
		t.Fatal("empty token")
	}
	return body
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, message string) {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	decode(t, rec, &body)
	if body.Error != message {
		t.Fatalf("error %q", body.Error)
	}
}

func verifyRoomToken(t *testing.T, signer rtn.Signer, body tokenJSON) {
	t.Helper()
	parts := strings.SplitN(body.Token, ":", 3)
	if len(parts) != 3 || parts[0] != signer.AccessKey {
		t.Fatalf("token prefix %q", body.Token)
	}
	mac := hmac.New(sha1.New, []byte(signer.SecretKey))
	mac.Write([]byte(parts[2]))
	if got := base64.URLEncoding.EncodeToString(mac.Sum(nil)); got != parts[1] {
		t.Fatalf("signature %s", parts[1])
	}
	raw, err := base64.URLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	var access struct {
		AppID      string `json:"appId"`
		RoomName   string `json:"roomName"`
		UserID     string `json:"userId"`
		ExpireAt   int64  `json:"expireAt"`
		Permission string `json:"permission"`
	}
	if err := json.Unmarshal(raw, &access); err != nil {
		t.Fatal(err)
	}
	if access.AppID != body.AppID || access.RoomName != body.RoomName || access.UserID != body.UserID || access.ExpireAt != body.ExpireAt {
		t.Fatalf("payload %+v body %+v", access, body)
	}
	if access.Permission != "user" {
		t.Fatalf("permission %s", access.Permission)
	}
	delta := access.ExpireAt - time.Now().Unix()
	ttl := int64(rtn.TokenTTL / time.Second)
	if delta < ttl-30 || delta > ttl+5 {
		t.Fatalf("expire delta %d", delta)
	}
}

func TestRoomTokenRejectsPlaceholderConfig(t *testing.T) {
	h := newMuxWithSigner(newService(t), rtn.Signer{
		AccessKey: "your_qiniu_access_key_here", SecretKey: "your_qiniu_secret_key_here", AppID: "your_rtn_app_id_here",
	})
	token := sessionToken(t, h, "诊断", "placeholder-device")
	res := getToken(t, h, "public", token)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", res.Code)
	}
	assertError(t, res, "服务器未配置七牛 RTN")
	if strings.Contains(res.Body.String(), `"token"`) {
		t.Fatal("placeholder token was issued")
	}
}
