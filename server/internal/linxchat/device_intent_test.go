package linxchat

import "testing"

func TestDeviceIntents(t *testing.T) {
	for _, tc := range []struct {
		text   string
		intent Intent
		name   string
	}{
		{"你好小路，切回上一频道", IntentPreviousChannel, ""},
		{"谁在说话", IntentListSpeakers, ""},
		{"查看口令", IntentShowJoinCode, ""},
		{"显示二维码", IntentShowJoinQR, ""},
		{"音量调到百分之五十", IntentSetVolume, "五十"},
		{"叫我老王", IntentSetNickname, "老王"},
	} {
		got := Parse(tc.text)
		if got.Intent != tc.intent || got.Name != tc.name {
			t.Fatalf("%q: %+v", tc.text, got)
		}
	}
}

func TestVolumeRange(t *testing.T) {
	for _, tc := range []struct {
		text  string
		value int
		ok    bool
	}{
		{"零", 0, true}, {"五十", 50, true}, {"十一", 11, true}, {"一百", 100, true},
		{"101", 101, false}, {"-1", -1, false}, {"最大", 0, false},
	} {
		value, ok := ParseVolume(tc.text)
		if value != tc.value || ok != tc.ok {
			t.Fatalf("%q: %d, %v", tc.text, value, ok)
		}
	}
}
