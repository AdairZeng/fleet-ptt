package linxbridge

import (
	"context"
	"testing"
	"time"
)

func TestWakePhraseAndReadiness(t *testing.T) {
	r := NewWakeRegistry()
	s := r.Open("device", "session", nil, nil)
	defer r.Close("device", s)
	for _, text := range []string{"创建频道", "他说你好小路就能唤醒", "你好小度"} {
		if _, ok := s.STT(text); ok {
			t.Fatalf("ambient woke device: %q", text)
		}
	}
	if _, _, err := r.Authorize(context.Background(), "device", "session"); err == nil {
		t.Fatal("standby authorized")
	}
	event, ok := s.STT("你好，小路！创建频道")
	if !ok {
		t.Fatal("punctuated wake rejected")
	}
	if _, ok := s.Control("ready", "older-turn"); ok {
		t.Fatal("stale ack accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := r.Authorize(ctx, "device", "session"); err == nil {
		t.Fatal("authorized before RTN stop ack")
	}
	if _, ok := s.Control("ready", event.TurnID); !ok {
		t.Fatal("ack rejected")
	}
	callCtx, done, err := r.Authorize(context.Background(), "device", "session")
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	s.Control("cancel", "")
	select {
	case <-callCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel did not cancel business context")
	}
}

func TestWakeAcceptedTranscriptions(t *testing.T) {
	for _, phrase := range []string{"你好小路", "你好，小鹿！", "你好小陆，开始对讲"} {
		if !hasWakePrefix(phrase) {
			t.Errorf("approved transcription rejected: %s", phrase)
		}
	}
	for _, phrase := range []string{"你好小度", "他说你好小鹿", "你好小卢"} {
		if hasWakePrefix(phrase) {
			t.Errorf("unapproved transcription accepted: %s", phrase)
		}
	}
}

func TestWakeOutputAndFollowup(t *testing.T) {
	r := NewWakeRegistry()
	s := r.Open("device", "session", nil, nil)
	defer r.Close("device", s)
	if s.Output("tts", "start") || s.Output("audio", "") {
		t.Fatal("unsolicited output allowed")
	}
	event, _ := s.STT("你好小路")
	if s.Output("tts", "start") {
		t.Fatal("audio before RTN ack")
	}
	s.Control("ready", event.TurnID)
	if !s.Output("tts", "start") || !s.Output("audio", "") {
		t.Fatal("accepted speech suppressed")
	}
	if _, ok := s.STT("你好小路"); ok {
		t.Fatal("self wake during playback")
	}
	if _, done, err := r.Authorize(context.Background(), "device", "session"); err != nil {
		t.Fatal("spoken preamble blocked tool")
	} else {
		done()
	}
	if !s.Output("tts", "stop") {
		t.Fatal("stop rejected")
	}
	if s.Output("audio", "") {
		t.Fatal("audio after stop")
	}
	if _, _, err := r.Authorize(context.Background(), "device", "session"); err == nil {
		t.Fatal("draining turn permits new tools")
	}
	if _, ok := s.Control("playback_done", "old"); ok {
		t.Fatal("stale drain accepted")
	}
	if event, ok := s.Control("playback_done", event.TurnID); !ok || event.State != "followup" {
		t.Fatal("no followup")
	}
	if _, _, err := r.Authorize(context.Background(), "device", "session"); err == nil {
		t.Fatal("followup authorized without STT")
	}
	if _, ok := s.STT("确认"); !ok {
		t.Fatal("followup requires wake word")
	}
}

func TestWakeExpiryReplacementAndIsolation(t *testing.T) {
	events := make(chan WakeEvent, 8)
	r := NewWakeRegistry()
	s := r.Open("device", "session", func(e WakeEvent) { events <- e }, nil)
	event, _ := s.STT("你好小路")
	s.Control("ready", event.TurnID)
	if _, _, err := r.Authorize(context.Background(), "other", "session"); err == nil {
		t.Fatal("cross device")
	}
	if _, _, err := r.Authorize(context.Background(), "device", "other"); err == nil {
		t.Fatal("cross session")
	}
	s.mu.Lock()
	s.expireLocked(time.Millisecond)
	s.mu.Unlock()
	select {
	case e := <-events:
		if e.State == "detected" {
			e = <-events
		}
		if e.State != "standby" {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("expiry notification absent")
	}
	if _, _, err := r.Authorize(context.Background(), "device", "session"); err == nil {
		t.Fatal("expired turn authorized")
	}
	if _, ok := s.STT("确认"); ok {
		t.Fatal("expired followup accepted")
	}
	event, _ = s.STT("你好小路")
	s.Control("ready", event.TurnID)
	ctx, done, err := r.Authorize(context.Background(), "device", "session")
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	replacement := r.Open("device", "new-session", nil, nil)
	defer r.Close("device", replacement)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("replaced connection remains authorized")
	}
	r.Close("device", s)
	event, _ = replacement.STT("你好小路")
	replacement.Control("ready", event.TurnID)
	if _, done, err := r.Authorize(context.Background(), "device", "new-session"); err != nil {
		t.Fatal("old socket closed replacement")
	} else {
		done()
	}
	r.Close("device", replacement)
	if _, _, err := r.Authorize(context.Background(), "device", "new-session"); err == nil {
		t.Fatal("disconnected authorized")
	}
}

func TestTalkStartReturnsToWakeStandby(t *testing.T) {
	r := NewWakeRegistry()
	s := r.Open("device", "session", nil, nil)
	defer r.Close("device", s)
	event, _ := s.STT("你好小路开始对讲")
	s.Control("ready", event.TurnID)
	ctx, cancel, err := r.Authorize(context.Background(), "device", "session")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	r.EndAfterPlayback(ctx)
	s.Output("tts", "start")
	s.Output("tts", "stop")
	result, ok := s.Control("playback_done", event.TurnID)
	if !ok || result.State != "standby" {
		t.Fatalf("talk completion: %+v", result)
	}
	if _, ok := s.STT("大家开始装货"); ok {
		t.Fatal("channel speech became command after talk_start")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("playback completion did not revoke authorization")
	}
	next, _ := s.STT("你好小路我有哪些频道")
	s.Control("ready", next.TurnID)
	r.EndAfterPlayback(ctx) // Late completion from the old turn must not mark this turn.
	s.Output("tts", "start")
	s.Output("tts", "stop")
	result, ok = s.Control("playback_done", next.TurnID)
	if !ok || result.State != "followup" {
		t.Fatal("old operation changed new turn")
	}
}

func TestWakeNotificationsSerializedWithTransitions(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	events := make(chan WakeEvent, 4)
	r := NewWakeRegistry()
	s := r.Open("device", "session", func(e WakeEvent) {
		if e.State == "followup" {
			close(entered)
			<-release
		}
		events <- e
	}, nil)
	defer r.Close("device", s)
	controlDone := make(chan struct{})
	go func() { defer close(controlDone); s.Control("manual", "") }()
	<-entered
	sttDone := make(chan struct{})
	go func() { defer close(sttDone); s.STT("创建频道") }()
	select {
	case <-sttDone:
		t.Fatal("STT bypassed pending followup notification")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	<-controlDone
	<-sttDone
	if first, second := <-events, <-events; first.State != "followup" || second.State != "detected" {
		t.Fatalf("out-of-order events: %+v %+v", first, second)
	}
}
