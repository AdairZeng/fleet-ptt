package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/linxchat"
)

func TestLinxCommandValidation(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args string
		ok   bool
	}{
		{"create_channel", `{"name":"装货"}`, true},
		{"create_channel", `{}`, false},
		{"create_channel", `null`, false},
		{"create_channel", `[]`, false},
		{"create_channel", `{"name":null}`, false},
		{"create_channel", `{"name":2}`, false},
		{"create_channel", `{"name":" "}`, false},
		{"create_channel", `{"name":"a\nb"}`, false},
		{"create_channel", `{"name":"装货","device_id":"other"}`, false},
		{"join_channel", `{"code":"234ABC"}`, true},
		{"join_channel", `{"code":"234abc"}`, false},
		{"join_channel", `{"code":"234A01"}`, false},
		{"join_channel", `{"name":"装货"}`, true},
		{"join_channel", `{"name":"装货","code":"234ABC"}`, false},
		{"join_channel", `{}`, false},
		{"switch_channel", `{"index":1}`, true},
		{"switch_channel", `{"index":1.5}`, false},
		{"switch_channel", `{"index":0}`, false},
		{"switch_channel", `{"index":"1"}`, false},
		{"switch_channel", `{"name":"装货","index":1}`, false},
		{"set_volume", `{"level":0}`, true},
		{"set_volume", `{"level":100}`, true},
		{"set_volume", `{"level":101}`, false},
		{"set_volume", `{"level":-1}`, false},
		{"set_volume", `{"level":50.1}`, false},
		{"set_volume", `{"adjustment":"up"}`, true},
		{"set_volume", `{"adjustment":"down"}`, true},
		{"set_volume", `{"adjustment":"loud"}`, false},
		{"set_volume", `{"level":50,"adjustment":"up"}`, false},
		{"set_nickname", `{"name":"小王"}`, true},
		{"confirm_action", `{}`, true},
		{"confirm_action", `{"session_id":"other"}`, false},
		{"cancel_action", `{}`, true},
		{"talk_start", `{"force":true}`, false},
		{"talk_end", ``, true},
		{"unknown", `{}`, false},
	} {
		t.Run(tc.tool+tc.args, func(t *testing.T) {
			_, err := linxCommand(tc.tool, json.RawMessage(tc.args))
			if (err == nil) != tc.ok {
				t.Fatalf("valid = %v, want %v: %v", err == nil, tc.ok, err)
			}
		})
	}
}

func TestLinxConfirmationBelongsToDeviceAndConversation(t *testing.T) {
	now := time.Now()
	gate := linxchat.NewJoinGate(time.Minute)
	first := voiceExec{gate: gate, sessionID: "conversation-one"}
	second := voiceExec{gate: gate, sessionID: "conversation-two"}
	first.offerPending("device-one", "nickname:小王", now)
	if _, ok := second.confirmPending("device-one", now); ok {
		t.Fatal("another conversation consumed confirmation")
	}
	if _, ok := first.confirmPending("device-two", now); ok {
		t.Fatal("another device consumed confirmation")
	}
	if got, ok := first.confirmPending("device-one", now); !ok || got != "nickname:小王" {
		t.Fatalf("original confirmation lost: %q, %v", got, ok)
	}
	if _, ok := first.confirmPending("device-one", now); ok {
		t.Fatal("confirmation consumed twice")
	}
	first.offerPending("device-one", "join:234ABC", now)
	first.offerPending("device-one", "join:234ABC", now.Add(30*time.Second))
	if _, ok := first.confirmPending("device-one", now.Add(time.Minute)); ok {
		t.Fatal("expired confirmation consumed")
	}
}
