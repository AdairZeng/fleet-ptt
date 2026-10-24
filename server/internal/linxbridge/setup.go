package linxbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/AdairZeng/fleet-ptt/server/internal/user"
	"github.com/coder/websocket"
)

const setupGreeting = "你好，我是小路。怎么称呼你？请说，叫我老王，或者直接说你的昵称。"

// SetupHandler authenticates device proof without creating a user or opening
// a business wake session. Only server-authored setup prompts can be played.
type SetupHandler struct {
	Users  *user.Service
	Bridge *Manager
}

type setupEvent struct {
	Type     string `json:"type"`
	State    string `json:"state"`
	Text     string `json:"text,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	TurnID   string `json:"turn_id,omitempty"`
}

type setupFlow struct {
	state      string
	nickname   string
	expires    time.Time
	turn       string
	ttsStarted bool
	ttsStopped bool
}

func (s *setupFlow) speak(text string) setupEvent {
	s.state = "speaking"
	s.ttsStarted, s.ttsStopped = false, false
	// A monotonically unique token rejects delayed playback acknowledgements.
	s.turn, _ = newClientID()
	return setupEvent{Type: "onboarding", State: "speaking", Text: text, Nickname: s.nickname, TurnID: s.turn}
}

func (s *setupFlow) control(state, turn string) (setupEvent, bool) {
	switch state {
	case "start", "cancel":
		if s.state == "speaking" || s.state == "confirmed" {
			return setupEvent{}, false
		}
		s.nickname = ""
		s.expires = time.Time{}
		return s.speak(setupGreeting), true
	case "playback_done":
		if s.state != "speaking" || !s.ttsStopped || turn == "" || turn != s.turn {
			return setupEvent{}, false
		}
		s.state = "listening"
		return setupEvent{Type: "onboarding", State: "listening", Nickname: s.nickname, Text: "请说出昵称"}, true
	}
	return setupEvent{}, false
}

func setupText(text string) string {
	return strings.TrimFunc(strings.TrimSpace(text), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) })
}

func (s *setupFlow) recognize(text string, now time.Time) (setupEvent, bool) {
	if s.state != "listening" {
		return setupEvent{}, false
	}
	text = setupText(text)
	if text == "" {
		return setupEvent{}, false
	}
	if s.nickname != "" && !now.Before(s.expires) {
		s.nickname = ""
		return s.speak("确认已超时，请重新说出你的昵称。"), true
	}
	switch text {
	case "确认", "确定", "是的", "对", "没错":
		if s.nickname == "" {
			return s.speak("还没有设置昵称，请先告诉我怎么称呼你。"), true
		}
		s.state = "confirmed"
		return setupEvent{Type: "onboarding", State: "confirmed", Nickname: s.nickname, Text: "昵称已确认，正在进入公共频道"}, true
	case "取消", "不对", "不是", "重新设置":
		s.nickname = ""
		return s.speak("好的，请重新说出你的昵称。"), true
	}
	if s.nickname != "" {
		return s.speak("请说确认保存昵称，或者说取消后重新设置。"), true
	}
	for _, prefix := range []string{"你好小路", "你好小鹿", "你好小陆"} {
		if strings.HasPrefix(text, prefix) {
			text = setupText(strings.TrimPrefix(text, prefix))
			break
		}
	}
	for _, prefix := range []string{"以后叫我", "我的昵称是", "我的名字是", "你可以叫我", "我叫", "叫我"} {
		if strings.HasPrefix(text, prefix) {
			text = setupText(strings.TrimPrefix(text, prefix))
			break
		}
	}
	if !user.ValidNickname(text) {
		return s.speak("昵称需要一到三十二个字，请重新说一次。"), true
	}
	s.nickname, s.expires = text, now.Add(2*time.Minute)
	return s.speak("我以后叫你" + text + "，对吗？请说确认，或者说取消重新设置。"), true
}

type setupPacket struct {
	epoch int
	local bool
	kind  websocket.MessageType
	data  []byte
	err   error
}

func (h SetupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Unlike the authenticated app bridge, do not permit cross-origin browsers
	// to submit long-lived device credentials on this bootstrap endpoint.
	local, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer local.CloseNow()
	local.SetReadLimit(maxControlBytes)
	authCtx, authCancel := context.WithTimeout(r.Context(), 5*time.Second)
	kind, payload, err := local.Read(authCtx)
	authCancel()
	var auth struct {
		Type       string `json:"type"`
		DeviceID   string `json:"device_id"`
		Credential string `json:"device_credential"`
	}
	if err != nil || kind != websocket.MessageText || json.Unmarshal(payload, &auth) != nil || auth.Type != "authenticate" {
		_ = local.Close(websocket.StatusPolicyViolation, "设备认证失败")
		return
	}
	device, err := h.Users.AuthenticateDevice(r.Context(), auth.DeviceID, auth.Credential)
	if err != nil || device.UserID != "" {
		_ = local.Close(websocket.StatusPolicyViolation, "设备凭证无效或已完成设置")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	config, err := h.Bridge.Ensure(ctx, device)
	if err != nil {
		_ = local.Close(websocket.StatusInternalError, "灵汐设备绑定失败")
		return
	}
	clientID, err := newClientID()
	if err != nil {
		return
	}
	remote, _, err := websocket.Dial(ctx, config.URL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": {"Bearer " + config.Token}, "Device-Id": {config.DeviceID}, "Client-Id": {clientID}, "Protocol-Version": {"1"},
	}})
	if err != nil {
		_ = local.Close(websocket.StatusInternalError, "灵汐连接失败")
		return
	}
	defer func() { remote.CloseNow() }()
	local.SetReadLimit(maxAudioBytes)
	remote.SetReadLimit(maxAudioBytes)
	localWriter, remoteWriter := &writeConn{c: local}, &writeConn{c: remote}
	if remoteWriter.text(ctx, deviceHello()) != nil {
		return
	}
	// One owner handles flow state and both streams; readers only enqueue frames.
	packets := make(chan setupPacket, 2)
	read := func(conn *websocket.Conn, fromLocal bool, epoch int) {
		for {
			kind, data, err := conn.Read(ctx)
			select {
			case packets <- setupPacket{epoch, fromLocal, kind, data, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}
	remoteEpoch := 1
	go read(local, true, 0)
	go read(remote, false, remoteEpoch)
	flow := &setupFlow{}
	sessionID := ""
	promptSent := false
	var speechDeadline <-chan time.Time
	speechTimer := time.NewTimer(time.Hour)
	speechTimer.Stop()
	defer speechTimer.Stop()
	emit := func(event setupEvent) bool {
		// STT also starts the upstream agent's own reply. There is no shared
		// response ID, so use a fresh upstream session for each fixed prompt.
		// This prevents an old answer or abort-stop from completing the prompt.
		if event.State == "speaking" && promptSent {
			remote.CloseNow()
			remoteEpoch++
			dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
			defer dialCancel()
			id, err := newClientID()
			if err != nil {
				return false
			}
			next, _, err := websocket.Dial(dialCtx, config.URL, &websocket.DialOptions{HTTPHeader: http.Header{
				"Authorization": {"Bearer " + config.Token}, "Device-Id": {config.DeviceID}, "Client-Id": {id}, "Protocol-Version": {"1"},
			}})
			if err != nil {
				return false
			}
			remote = next
			remote.SetReadLimit(maxAudioBytes)
			remoteWriter = &writeConn{c: remote}
			if remoteWriter.text(dialCtx, deviceHello()) != nil {
				return false
			}
			kind, data, err := remote.Read(dialCtx)
			var greeting struct {
				Type      string `json:"type"`
				SessionID string `json:"session_id"`
			}
			if err != nil || kind != websocket.MessageText || json.Unmarshal(data, &greeting) != nil || greeting.Type != "hello" || greeting.SessionID == "" {
				return false
			}
			sessionID = greeting.SessionID
			if localWriter.raw(ctx, kind, data) != nil {
				return false
			}
			go read(remote, false, remoteEpoch)
		}
		if localWriter.text(ctx, event) != nil {
			return false
		}
		if event.State == "speaking" {
			promptSent = true
			speechTimer.Reset(60 * time.Second)
			speechDeadline = speechTimer.C
			return remoteWriter.text(ctx, map[string]string{"type": "listen", "state": "detect", "session_id": sessionID, "text": "首次昵称设置", "text_response": event.Text}) == nil
		}
		speechTimer.Stop()
		speechDeadline = nil
		if event.State == "listening" {
			return remoteWriter.text(ctx, map[string]string{"type": "listen", "state": "start", "mode": "realtime", "session_id": sessionID}) == nil
		}
		return true
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-speechDeadline:
			_ = localWriter.text(ctx, setupEvent{Type: "error", Text: "语音设置播报超时，请按指令键重试"})
			return
		case packet := <-packets:
			if !packet.local && packet.epoch != remoteEpoch {
				continue
			}
			if packet.err != nil {
				return
			}
			if packet.kind == websocket.MessageBinary {
				if packet.local && flow.state == "listening" {
					if remoteWriter.raw(ctx, packet.kind, packet.data) != nil {
						return
					}
				}
				if !packet.local && flow.state == "speaking" && flow.ttsStarted && !flow.ttsStopped {
					if localWriter.raw(ctx, packet.kind, packet.data) != nil {
						return
					}
				}
				continue
			}
			if packet.kind != websocket.MessageText || len(packet.data) > maxControlBytes {
				return
			}
			var msg struct {
				Type      string `json:"type"`
				State     string `json:"state"`
				Text      string `json:"text"`
				SessionID string `json:"session_id"`
				TurnID    string `json:"turn_id"`
				IsAborted bool   `json:"is_aborted"`
			}
			if json.Unmarshal(packet.data, &msg) != nil {
				return
			}
			if packet.local {
				if msg.Type != "onboarding" || sessionID == "" {
					continue
				}
				if event, ok := flow.control(msg.State, msg.TurnID); ok {
					if !emit(event) {
						return
					}
				}
				continue
			}
			switch msg.Type {
			case "hello":
				if sessionID != "" || msg.SessionID == "" {
					return
				}
				sessionID = msg.SessionID
				if localWriter.raw(ctx, packet.kind, packet.data) != nil {
					return
				}
			case "stt":
				if event, ok := flow.recognize(msg.Text, time.Now()); ok {
					if !emit(event) {
						return
					}
					if event.State == "confirmed" {
						return
					}
				}
			case "tts":
				if msg.IsAborted {
					_ = localWriter.text(ctx, setupEvent{Type: "error", Text: "语音设置播报中断，请按指令键重试"})
					return
				}
				if flow.state != "speaking" {
					continue
				}
				if msg.State == "start" && !flow.ttsStarted {
					flow.ttsStarted = true
				} else if msg.State == "stop" && flow.ttsStarted && !flow.ttsStopped {
					flow.ttsStopped = true
				} else {
					continue
				}
				if localWriter.raw(ctx, packet.kind, packet.data) != nil {
					return
				}
			}
		}
	}
}
