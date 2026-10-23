package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestTalkHTTPRequiresSessionAndChannel(t *testing.T) {
	h := newMux(newService(t))
	for _, path := range []string{"/api/v1/talk/start", "/api/v1/talk/end"} {
		res := postAuth(t, h, path, "", `{}`)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s status %d", path, res.Code)
		}
	}

	token := sessionToken(t, h, "队长", "device-captain")
	res := postAuth(t, h, "/api/v1/talk/start", token, `{}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("start status %d body %s", res.Code, res.Body)
	}
	var body struct {
		Error string `json:"error"`
	}
	decode(t, res, &body)
	if body.Error != "没有当前频道" {
		t.Fatalf("error %q", body.Error)
	}
}

func TestWebSocketAuth(t *testing.T) {
	h := newMux(newService(t))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	for _, rawURL := range []string{srv.URL + "/api/v1/device/ws", srv.URL + "/api/v1/device/ws?token=not-a-token"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, res, err := websocket.Dial(ctx, rawURL, nil)
		cancel()
		if err == nil {
			t.Fatalf("dial %s succeeded", rawURL)
		}
		if res == nil || res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("dial %s status %v err %v", rawURL, res, err)
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		if !strings.Contains(string(body), "未登录或会话无效") {
			t.Fatalf("body %s", body)
		}
	}

	token, _ := sessionPair(t, h, "队长", "device-captain")
	conn := dialWS(t, srv, token, true)
	writeWS(t, conn, `{"type":"ping"}`)
	ev := readWS(t, conn)
	if ev.Type != "pong" {
		t.Fatalf("ping reply %#v", ev)
	}
	writeWS(t, conn, `{"type":"nope"}`)
	ev = readWS(t, conn)
	if ev.Type != "error" || ev.Message != "无法识别的信令" {
		t.Fatalf("unknown %#v", ev)
	}
	writeWS(t, conn, `not-json`)
	ev = readWS(t, conn)
	if ev.Type != "error" || ev.Message != "请求格式无效" {
		t.Fatalf("bad json %#v", ev)
	}
	writeWS(t, conn, `{"type":"talk_end"}`)
	ev = readWS(t, conn)
	if ev.Type != "error" || ev.Message != "没有当前频道" {
		t.Fatalf("talk_end %#v", ev)
	}
}

func TestTalkFanout(t *testing.T) {
	h := newMux(newService(t))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	captainToken, captainID := sessionPair(t, h, "队长", "device-captain")
	driverToken, driverID := sessionPair(t, h, "司机乙", "device-driver")
	outsiderToken, _ := sessionPair(t, h, "路人", "device-outsider")

	created := createChannel(t, h, captainToken, "装货")
	joinCode(t, h, driverToken, created.JoinCode)

	driver := dialWS(t, srv, driverToken, false)
	driverBox := collectWS(t, driver)
	driverBox.wait(t, 1)
	greet := driverBox.snapshot()[0]
	if greet.Type != "members" || greet.ChannelID != created.ChannelID || !strings.Contains(greet.raw, `"talking":false`) {
		t.Fatalf("greet %#v", greet)
	}
	if !hasMember(greet, captainID, "队长", false) || !hasMember(greet, driverID, "司机乙", false) {
		t.Fatalf("greet members %#v", greet.Members)
	}

	captain := dialWS(t, srv, captainToken, true)
	collectWS(t, captain)
	outsider := dialWS(t, srv, outsiderToken, false)
	outsiderBox := collectWS(t, outsider)

	res := postAuth(t, h, "/api/v1/talk/start", captainToken, `{}`)
	if res.Code != http.StatusOK {
		t.Fatalf("start %d %s", res.Code, res.Body)
	}
	var started struct {
		ChannelID string `json:"channel_id"`
		Talking   bool   `json:"talking"`
	}
	decode(t, res, &started)
	if started.ChannelID != created.ChannelID || !started.Talking {
		t.Fatalf("started %+v", started)
	}

	driverBox.wait(t, 3)
	events := driverBox.snapshot()
	startEv, membersEv := events[1], events[2]
	if startEv.Type != "talk_start" || startEv.ChannelID != created.ChannelID || startEv.UserID != captainID {
		t.Fatalf("talk_start %#v", startEv)
	}
	if membersEv.Type != "members" || !strings.Contains(membersEv.raw, `"talking":true`) || !hasMember(membersEv, captainID, "队长", true) {
		t.Fatalf("members %#v", membersEv)
	}
	if hasMember(membersEv, driverID, "司机乙", true) {
		t.Fatalf("driver marked talking %#v", membersEv.Members)
	}
	if time.Sleep(150 * time.Millisecond); len(outsiderBox.snapshot()) != 0 {
		t.Fatalf("outsider saw %#v", outsiderBox.snapshot())
	}

	res = postAuth(t, h, "/api/v1/talk/end", captainToken, `{}`)
	if res.Code != http.StatusOK {
		t.Fatalf("end %d %s", res.Code, res.Body)
	}
	var ended struct {
		Talking bool `json:"talking"`
	}
	decode(t, res, &ended)
	if ended.Talking {
		t.Fatal("end left talking true")
	}
	driverBox.wait(t, 5)
	events = driverBox.snapshot()
	if events[3].Type != "talk_end" || events[3].UserID != captainID || events[4].Type != "members" || hasMember(events[4], captainID, "队长", true) {
		t.Fatalf("after end %#v %#v", events[3], events[4])
	}
	if !strings.Contains(events[4].raw, `"talking":false`) {
		t.Fatalf("raw %s", events[4].raw)
	}

	if res = postAuth(t, h, "/api/v1/talk/start", captainToken, `{}`); res.Code != http.StatusOK {
		t.Fatalf("restart %d %s", res.Code, res.Body)
	}
	driverBox.wait(t, 7)
	writeWS(t, captain, `{"type":"talk_end"}`)
	driverBox.wait(t, 9)
	events = driverBox.snapshot()
	if events[7].Type != "talk_end" || events[7].UserID != captainID || hasMember(events[8], captainID, "队长", true) {
		t.Fatalf("ws talk_end %#v %#v", events[7], events[8])
	}

	_ = captain.Close(websocket.StatusNormalClosure, "")
	_ = driver.Close(websocket.StatusNormalClosure, "")
	_ = outsider.Close(websocket.StatusNormalClosure, "")
}

func TestDisconnectEndsTalk(t *testing.T) {
	h := newMux(newService(t))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	captainToken, captainID := sessionPair(t, h, "队长", "device-captain")
	driverToken, _ := sessionPair(t, h, "司机乙", "device-driver")
	created := createChannel(t, h, captainToken, "装货")
	joinCode(t, h, driverToken, created.JoinCode)

	driver := dialWS(t, srv, driverToken, false)
	driverBox := collectWS(t, driver)
	driverBox.wait(t, 1)
	captain := dialWS(t, srv, captainToken, false)
	if res := postAuth(t, h, "/api/v1/talk/start", captainToken, `{}`); res.Code != http.StatusOK {
		t.Fatalf("start %d %s", res.Code, res.Body)
	}
	driverBox.wait(t, 3)
	if err := captain.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	driverBox.wait(t, 5)
	events := driverBox.snapshot()
	if events[3].Type != "talk_end" || events[3].UserID != captainID || hasMember(events[4], captainID, "队长", true) {
		t.Fatalf("disconnect %#v %#v", events[3], events[4])
	}
	_ = driver.Close(websocket.StatusNormalClosure, "")
}

func TestChannelSignals(t *testing.T) {
	h := newMux(newService(t))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	captainToken, captainID := sessionPair(t, h, "队长", "device-captain")
	driverToken, _ := sessionPair(t, h, "司机乙", "device-driver")
	conn := dialWS(t, srv, captainToken, false)
	box := collectWS(t, conn)

	created := createChannel(t, h, captainToken, "装货")
	box.wait(t, 2)
	events := box.snapshot()
	if events[0].Type != "channel_created" || events[0].ChannelID != created.ChannelID || events[0].Name != "装货" || events[0].JoinCode != created.JoinCode {
		t.Fatalf("created %#v", events[0])
	}
	if events[1].Type != "members" || !hasMember(events[1], captainID, "队长", false) {
		t.Fatalf("members %#v", events[1])
	}

	joinCode(t, h, driverToken, created.JoinCode)
	box.wait(t, 3)
	if res := postAuth(t, h, "/api/v1/talk/start", captainToken, `{}`); res.Code != http.StatusOK {
		t.Fatalf("start %d %s", res.Code, res.Body)
	}
	box.wait(t, 5)

	driver := dialWS(t, srv, driverToken, false)
	driverBox := collectWS(t, driver)
	driverBox.wait(t, 1)
	if greet := driverBox.snapshot()[0]; greet.Type != "members" || !hasMember(greet, captainID, "队长", true) {
		t.Fatalf("driver greet %#v", greet)
	}

	second := createChannel(t, h, captainToken, "卸货")
	driverBox.wait(t, 3)
	left := driverBox.snapshot()
	if left[1].Type != "talk_end" || left[1].UserID != captainID || left[1].ChannelID != created.ChannelID {
		t.Fatalf("old channel talk_end %#v", left[1])
	}
	if left[2].Type != "members" || hasMember(left[2], captainID, "队长", true) {
		t.Fatalf("old members %#v", left[2])
	}
	box.wait(t, 9)
	events = box.snapshot()
	if events[5].Type != "talk_end" || events[7].Type != "channel_created" || events[7].ChannelID != second.ChannelID || events[7].Name != "卸货" || events[7].JoinCode != second.JoinCode {
		t.Fatalf("captain after switch %#v", events[5:])
	}

	joinCode(t, h, captainToken, created.JoinCode)
	box.wait(t, 11)
	events = box.snapshot()
	if events[9].Type != "channel_switched" || events[9].ChannelID != created.ChannelID || events[9].Name != "装货" {
		t.Fatalf("switched %#v", events[9])
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
	_ = driver.Close(websocket.StatusNormalClosure, "")
}

func TestSwitchChannelStopsTalk(t *testing.T) {
	h := newMux(newService(t))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	captainToken, captainID := sessionPair(t, h, "队长", "device-captain")
	driverToken, driverID := sessionPair(t, h, "司机乙", "device-driver")
	loading := createChannel(t, h, captainToken, "装货")
	unloading := createChannel(t, h, captainToken, "卸货")
	joinCode(t, h, captainToken, loading.JoinCode)
	joinCode(t, h, driverToken, loading.JoinCode)

	driver := dialWS(t, srv, driverToken, false)
	driverBox := collectWS(t, driver)
	driverBox.wait(t, 1)
	captain := dialWS(t, srv, captainToken, false)
	captainBox := collectWS(t, captain)
	captainBox.wait(t, 1)

	if res := postAuth(t, h, "/api/v1/talk/start", captainToken, `{}`); res.Code != http.StatusOK {
		t.Fatalf("start %d %s", res.Code, res.Body)
	}
	driverBox.wait(t, 3)
	captainBox.wait(t, 3)

	res := postAuth(t, h, "/api/v1/channels/current", captainToken, `{"channel_id":"`+unloading.ChannelID+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("switch %d %s", res.Code, res.Body)
	}
	var switched channelJSON
	decode(t, res, &switched)
	if switched.ChannelID != unloading.ChannelID || switched.Name != "卸货" || switched.Role != "owner" {
		t.Fatalf("switched %+v", switched)
	}

	driverBox.wait(t, 5)
	left := driverBox.snapshot()
	if left[3].Type != "talk_end" || left[3].ChannelID != loading.ChannelID || left[3].UserID != captainID {
		t.Fatalf("talk_end %#v", left[3])
	}
	if left[4].Type != "members" || left[4].ChannelID != loading.ChannelID || hasMember(left[4], captainID, "队长", true) {
		t.Fatalf("old members %#v", left[4])
	}
	for _, ev := range left {
		if ev.Type == "channel_switched" {
			t.Fatalf("driver saw switch %#v", ev)
		}
	}
	if hasMember(left[4], driverID, "司机乙", true) {
		t.Fatalf("driver marked talking %#v", left[4].Members)
	}

	captainBox.wait(t, 7)
	events := captainBox.snapshot()
	if events[3].Type != "talk_end" || events[3].ChannelID != loading.ChannelID || events[3].UserID != captainID {
		t.Fatalf("captain talk_end %#v", events[3])
	}
	if events[4].Type != "members" || events[4].ChannelID != loading.ChannelID {
		t.Fatalf("captain old members %#v", events[4])
	}
	if events[5].Type != "channel_switched" || events[5].ChannelID != unloading.ChannelID || events[5].Name != "卸货" {
		t.Fatalf("channel_switched %#v", events[5])
	}
	if events[6].Type != "members" || events[6].ChannelID != unloading.ChannelID || !hasMember(events[6], captainID, "队长", false) {
		t.Fatalf("new members %#v", events[6])
	}
	if hasMember(events[6], driverID, "司机乙", false) {
		t.Fatalf("driver listed on new channel %#v", events[6].Members)
	}

	again := postAuth(t, h, "/api/v1/channels/current", captainToken, `{"channel_id":"`+unloading.ChannelID+`"}`)
	if again.Code != http.StatusOK {
		t.Fatalf("same %d %s", again.Code, again.Body)
	}
	if time.Sleep(150 * time.Millisecond); len(captainBox.snapshot()) != 7 || len(driverBox.snapshot()) != 5 {
		t.Fatalf("same channel emitted captain %#v driver %#v", captainBox.snapshot(), driverBox.snapshot())
	}
	_ = captain.Close(websocket.StatusNormalClosure, "")
	_ = driver.Close(websocket.StatusNormalClosure, "")
}

type wsEvent struct {
	raw       string
	Type      string `json:"type"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	JoinCode  string `json:"join_code"`
	Message   string `json:"message"`
	Members   []struct {
		UserID      string `json:"user_id"`
		DisplayName string `json:"display_name"`
		Talking     bool   `json:"talking"`
	} `json:"members"`
}

type wsBox struct {
	mu   sync.Mutex
	cond *sync.Cond
	ev   []wsEvent
}

func collectWS(t *testing.T, conn *websocket.Conn) *wsBox {
	return collectDeviceWS(t, conn, false)
}

func collectDeviceWS(t *testing.T, conn *websocket.Conn, acknowledge bool) *wsBox {
	t.Helper()
	box := &wsBox{}
	box.cond = sync.NewCond(&box.mu)
	go func() {
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var ev wsEvent
			if err := json.Unmarshal(data, &ev); err != nil {
				continue
			}
			if acknowledge && ev.Type == "action" {
				var action struct {
					ID string `json:"action_id"`
				}
				if json.Unmarshal(data, &action) == nil {
					raw, _ := json.Marshal(map[string]any{"type": "action_result", "action_id": action.ID, "success": true})
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					_ = conn.Write(ctx, websocket.MessageText, raw)
					cancel()
				}
				continue
			}
			if ev.Type == "channel_state" {
				continue
			}
			ev.raw = string(data)
			box.mu.Lock()
			box.ev = append(box.ev, ev)
			box.cond.Broadcast()
			box.mu.Unlock()
		}
	}()
	return box
}

func (b *wsBox) snapshot() []wsEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]wsEvent(nil), b.ev...)
}

func (b *wsBox) wait(t *testing.T, n int) {
	t.Helper()
	done := make(chan []wsEvent, 1)
	go func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for len(b.ev) < n {
			b.cond.Wait()
		}
		done <- append([]wsEvent(nil), b.ev...)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for %d events, have %#v", n, b.snapshot())
	}
}

func hasMember(ev wsEvent, userID, name string, talking bool) bool {
	for _, member := range ev.Members {
		if member.UserID == userID && member.DisplayName == name && member.Talking == talking {
			return true
		}
	}
	return false
}

func sessionPair(t *testing.T, h http.Handler, nickname, device string) (string, string) {
	t.Helper()
	res := postSession(t, h, `{"nickname":"`+nickname+`","device_id":"`+device+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("session %d %s", res.Code, res.Body)
	}
	var body struct {
		UserID       string `json:"user_id"`
		SessionToken string `json:"session_token"`
	}
	decode(t, res, &body)
	if body.UserID == "" || body.SessionToken == "" {
		t.Fatalf("session %+v", body)
	}
	return body.SessionToken, body.UserID
}

func createChannel(t *testing.T, h http.Handler, token, name string) channelJSON {
	t.Helper()
	res := postAuth(t, h, "/api/v1/channels", token, `{"name":"`+name+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create %d %s", res.Code, res.Body)
	}
	var created channelJSON
	decode(t, res, &created)
	return created
}

func joinCode(t *testing.T, h http.Handler, token, code string) {
	t.Helper()
	res := postAuth(t, h, "/api/v1/channels/join", token, `{"code":"`+code+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("join %d %s", res.Code, res.Body)
	}
}

func dialWS(t *testing.T, srv *httptest.Server, token string, header bool) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rawURL := srv.URL + "/api/v1/device/ws"
	var opts *websocket.DialOptions
	if header {
		opts = &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
		}
	} else {
		rawURL += "?token=" + token
	}
	conn, _, err := websocket.Dial(ctx, rawURL, opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func writeWS(t *testing.T, conn *websocket.Conn, payload string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		t.Fatal(err)
	}
}

func readWS(t *testing.T, conn *websocket.Conn) wsEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ev wsEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("json %s: %v", data, err)
	}
	ev.raw = string(data)
	return ev
}
