package linxchat

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		msg    string
		intent Intent
		answer string
	}{
		{"建一个装货频道", IntentCreateChannel, "已收到建群指令，稍后播报口令"},
		{"创建一个卸货群", IntentCreateChannel, "已收到建群指令，稍后播报口令"},
		{"建群", IntentCreateChannel, "已收到建群指令，稍后播报口令"},
		{"建议建一个频道", IntentCreateChannel, "已收到建群指令，稍后播报口令"},
		{"加入口令 384291", IntentJoinChannel, "已收到入群指令，请再说一次口令确认"},
		{"进入装货", IntentJoinChannel, "已收到入群指令，请再说一次口令确认"},
		{"切换到卸货", IntentSwitchChannel, "已收到切群指令"},
		{"切到装货", IntentSwitchChannel, "已收到切群指令"},
		{"开始对讲", IntentTalkStart, "已收到开始对讲"},
		{"结束对讲", IntentTalkEnd, "已收到结束对讲"},
		{"我有哪些频道", IntentListChannels, "已收到查询频道"},
		{"今天天气怎么样", IntentEcho, "收到：今天天气怎么样"},
		{"  建议休息  ", IntentEcho, "收到：建议休息"},
	}
	for _, tc := range cases {
		got := Classify(tc.msg)
		if got.Intent != tc.intent || got.Answer != tc.answer {
			t.Fatalf("Classify(%q) = %+v, want intent %s answer %q", tc.msg, got, tc.intent, tc.answer)
		}
	}
}
