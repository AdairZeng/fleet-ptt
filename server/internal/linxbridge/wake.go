package linxbridge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	WakeWindow      = 20 * time.Second
	processingLimit = 30 * time.Second
	playbackLimit   = 60 * time.Second
)

// WakeRegistry binds business authorization to a live, authenticated audio socket.
// Linx does not supply a turn ID on MCP calls: authorization is consequently
// scoped to the currently acknowledged turn in that upstream voice session.
type WakeRegistry struct {
	mu       sync.Mutex
	sessions map[string]*WakeSession
}

func NewWakeRegistry() *WakeRegistry { return &WakeRegistry{sessions: make(map[string]*WakeSession)} }

type WakeEvent struct {
	Type      string `json:"type"`
	State     string `json:"state"`
	Text      string `json:"text,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

type WakeSession struct {
	mu            sync.Mutex
	sessionID     string
	state         string
	turnID        string
	ready         chan struct{}
	readyClosed   bool
	turnCtx       context.Context
	turnCancel    context.CancelFunc
	timer         *time.Timer
	emit          func(WakeEvent)
	disconnect    context.CancelFunc
	closed        bool
	resumeStandby bool
}

func (r *WakeRegistry) Open(deviceID, sessionID string, emit func(WakeEvent), disconnect context.CancelFunc) *WakeSession {
	s := &WakeSession{sessionID: sessionID, state: "standby", emit: emit, disconnect: disconnect}
	r.mu.Lock()
	previous := r.sessions[deviceID]
	r.sessions[deviceID] = s
	r.mu.Unlock()
	if previous != nil {
		previous.close()
	}
	return s
}

func (r *WakeRegistry) Close(deviceID string, s *WakeSession) {
	r.mu.Lock()
	if r.sessions[deviceID] == s {
		delete(r.sessions, deviceID)
	}
	r.mu.Unlock()
	if s != nil {
		s.close()
	}
}

var ErrNotAwake = errors.New("请先说你好小路唤醒设备，或按指令键")

// Authorize also waits for the device to acknowledge stopping its RTN uplink.
func (r *WakeRegistry) Authorize(ctx context.Context, deviceID, sessionID string) (context.Context, context.CancelFunc, error) {
	if r == nil {
		return nil, nil, ErrNotAwake
	}
	r.mu.Lock()
	s := r.sessions[deviceID]
	r.mu.Unlock()
	if s == nil || sessionID == "" || s.sessionID != sessionID {
		return nil, nil, ErrNotAwake
	}
	s.mu.Lock()
	if s.closed || s.turnCtx == nil || (s.state != "pending" && s.state != "processing" && s.state != "playing") {
		s.mu.Unlock()
		return nil, nil, ErrNotAwake
	}
	ready, turnCtx := s.ready, s.turnCtx
	s.mu.Unlock()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-turnCtx.Done():
		return nil, nil, ErrNotAwake
	case <-timer.C:
		return nil, nil, ErrNotAwake
	case <-ready:
	}
	s.mu.Lock()
	valid := !s.closed && (s.state == "processing" || s.state == "playing") && s.turnCtx == turnCtx && turnCtx.Err() == nil
	s.mu.Unlock()
	if !valid {
		return nil, nil, ErrNotAwake
	}
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(turnCtx, cancel)
	return context.WithValue(callCtx, wakeContextKey{}, wakeAuthorization{s, turnCtx}), func() { stop(); cancel() }, nil
}

func hasWakePrefix(text string) bool {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return r
	}, text)
	return strings.HasPrefix(normalized, "你好小路") || strings.HasPrefix(normalized, "你好小鹿") || strings.HasPrefix(normalized, "你好小陆")
}

func (s *WakeSession) STT(text string) (WakeEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || strings.TrimSpace(text) == "" || (s.state != "standby" && s.state != "followup") {
		return WakeEvent{}, false
	}
	if s.state == "standby" && !hasWakePrefix(text) {
		return WakeEvent{}, false
	}
	turnID, err := newClientID()
	if err != nil {
		return WakeEvent{}, false
	}
	s.clearTurnLocked()
	s.turnID, s.state = turnID, "pending"
	s.resumeStandby = false
	s.ready = make(chan struct{})
	s.readyClosed = false
	s.turnCtx, s.turnCancel = context.WithCancel(context.Background())
	s.expireLocked(processingLimit)
	event := WakeEvent{Type: "wake", State: "detected", Text: text, SessionID: s.sessionID, TurnID: turnID}
	s.emitLocked(event)
	return event, true
}

func (s *WakeSession) Control(state, turnID string) (WakeEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return WakeEvent{}, false
	}
	switch state {
	case "ready":
		if s.state != "pending" || turnID != s.turnID {
			return WakeEvent{}, false
		}
		s.state = "processing"
		close(s.ready)
		s.readyClosed = true
		return WakeEvent{}, true
	case "manual":
		s.clearTurnLocked()
		s.state = "followup"
		s.expireLocked(WakeWindow)
		event := WakeEvent{Type: "wake", State: "followup", TimeoutMS: WakeWindow.Milliseconds()}
		s.emitLocked(event)
		return event, true
	case "cancel":
		s.clearTurnLocked()
		s.state = "standby"
		event := WakeEvent{Type: "wake", State: "standby"}
		s.emitLocked(event)
		return event, true
	case "playback_done":
		if s.state != "draining" || turnID != s.turnID {
			return WakeEvent{}, false
		}
		standby := s.resumeStandby
		s.clearTurnLocked()
		s.state = "standby"
		event := WakeEvent{Type: "wake", State: "standby"}
		if !standby {
			s.state = "followup"
			s.expireLocked(WakeWindow)
			event.State = "followup"
			event.TimeoutMS = WakeWindow.Milliseconds()
		}
		s.emitLocked(event)
		return event, true
	}
	return WakeEvent{}, false
}

func (s *WakeSession) Output(kind, state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.turnCtx == nil || s.turnCtx.Err() != nil {
		return false
	}
	if kind == "tts" && state == "start" && s.state == "processing" {
		s.state = "playing"
		s.expireLocked(playbackLimit)
		return true
	}
	if kind == "tts" && state == "stop" && (s.state == "playing" || s.state == "processing") {
		s.state = "draining"
		return true
	}
	if kind == "audio" {
		return s.state == "playing"
	}
	return s.state == "processing" || s.state == "playing"
}

func (s *WakeSession) clearTurnLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.turnCancel != nil {
		s.turnCancel()
		s.turnCancel = nil
	}
	if s.ready != nil && !s.readyClosed {
		close(s.ready)
		s.readyClosed = true
	}
	s.turnCtx = nil
	s.turnID = ""
}

func (s *WakeSession) expireLocked(after time.Duration) {
	if s.timer != nil {
		s.timer.Stop()
	}
	// A generation check prevents an already-fired timer from expiring a new turn.
	var timer *time.Timer
	timer = time.AfterFunc(after, func() {
		s.mu.Lock()
		if s.closed || s.timer != timer {
			s.mu.Unlock()
			return
		}
		s.clearTurnLocked()
		s.state = "standby"
		// Serialize the expiry notification with new STT turns, so a stale
		// standby event cannot arrive after a newer detected event.
		s.emitLocked(WakeEvent{Type: "wake", State: "standby"})
		s.mu.Unlock()
	})
	s.timer = timer
}
func (s *WakeSession) close() {
	s.mu.Lock()
	s.closed = true
	s.clearTurnLocked()
	s.mu.Unlock()
	if s.disconnect != nil {
		s.disconnect()
	}
}

func (s *WakeSession) waitReady(ctx context.Context) bool {
	s.mu.Lock()
	if s.closed || s.turnCtx == nil {
		s.mu.Unlock()
		return false
	}
	ready, turnCtx := s.ready, s.turnCtx
	s.mu.Unlock()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-turnCtx.Done():
		return false
	case <-timer.C:
		return false
	case <-ready:
		return turnCtx.Err() == nil
	}
}

// CaptureAllowed suppresses in-flight microphone frames during processing and
// playback in addition to the device-side capture pause.
func (s *WakeSession) CaptureAllowed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && (s.state == "standby" || s.state == "followup")
}

func (s *WakeSession) emitLocked(event WakeEvent) {
	if s.emit != nil {
		s.emit(event)
	}
}

type wakeContextKey struct{}
type wakeAuthorization struct {
	session     *WakeSession
	turnContext context.Context
}

// EndAfterPlayback makes talk_start return to wake-only listening: channel
// speech immediately after the spoken acknowledgement is not another command.
func (r *WakeRegistry) EndAfterPlayback(ctx context.Context) {
	authorization, ok := ctx.Value(wakeContextKey{}).(wakeAuthorization)
	if !ok {
		return
	}
	s := authorization.session
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed && s.turnCtx == authorization.turnContext && s.turnCtx.Err() == nil {
		s.resumeStandby = true
	}
}
