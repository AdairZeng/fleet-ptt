package linxchat

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		msg    string
		intent Intent
		name   string
		code   string
		index  int
	}{
		{"建一个装货频道", IntentCreateChannel, "装货", "", 0},
		{"创建一个卸货群", IntentCreateChannel, "卸货", "", 0},
		{"建群", IntentCreateChannel, "", "", 0},
		{"建议建一个频道", IntentCreateChannel, "", "", 0},
		{"加入口令 384291", IntentJoinChannel, "", "384291", 0},
		{"加入口令 3、8、4、2、9、1", IntentJoinChannel, "", "384291", 0},
		{"口令是 3842K9", IntentJoinChannel, "", "3842K9", 0},
		{"进入装货", IntentJoinChannel, "装货", "", 0},
		{"进装货群", IntentJoinChannel, "装货", "", 0},
		{"3842K9", IntentJoinChannel, "", "3842K9", 0},
		{"切换到卸货", IntentSwitchChannel, "卸货", "", 0},
		{"切到装货频道", IntentSwitchChannel, "装货", "", 0},
		{"切到频道 2", IntentSwitchChannel, "", "", 2},
		{"切到第二个", IntentSwitchChannel, "", "", 2},
		{"开始对讲", IntentTalkStart, "", "", 0},
		{"结束对讲", IntentTalkEnd, "", "", 0},
		{"我有哪些频道", IntentListChannels, "", "", 0},
		{"我的频道", IntentListChannels, "", "", 0},
		{"确认加入", IntentConfirmJoin, "", "", 0},
		{"取消", IntentCancelJoin, "", "", 0},
		{"今天天气怎么样", IntentEcho, "", "", 0},
		{"  建议休息  ", IntentEcho, "", "", 0},
		{"进度怎么样", IntentEcho, "", "", 0},
	}
	for _, tc := range cases {
		got := Parse(tc.msg)
		if got.Intent != tc.intent || got.Name != tc.name || got.Code != tc.code || got.Index != tc.index {
			t.Fatalf("Parse(%q) = %+v, want intent %s name %q code %q index %d", tc.msg, got, tc.intent, tc.name, tc.code, tc.index)
		}
	}
	if got := Parse("  建议休息  "); got.Text != "建议休息" {
		t.Fatalf("text %q", got.Text)
	}
}

func TestSpeakCodeAndAnswers(t *testing.T) {
	if got := SpeakCode("384291"); got != "口令是 3、8、4、2、9、1" {
		t.Fatalf("speak %q", got)
	}
	if got := CreatedAnswer("装货", "3842K9"); got != "已创建装货。口令是 3、8、4、2、K、9" {
		t.Fatalf("created %q", got)
	}
	if got := ConfirmJoinPrompt("384291"); got != "口令是 3、8、4、2、9、1。请说确认加入或取消" {
		t.Fatalf("confirm %q", got)
	}
	if got := ListAnswer(nil, ""); got != "你还没有频道。可以说建一个装货频道，或加入口令" {
		t.Fatalf("empty list %q", got)
	}
	got := ListAnswer([]string{"装货", "卸货"}, "装货")
	if got != "你有 2 个频道：1、装货，2、卸货。当前是装货。" {
		t.Fatalf("list %q", got)
	}
}

func TestMatchChannel(t *testing.T) {
	list := []ChannelRef{{ID: "a", Name: "装货"}, {ID: "b", Name: "卸货"}, {ID: "c", Name: "装货"}}
	if _, msg, ok := MatchChannel(list, "不存在", 0); ok || msg != "未匹配到频道" {
		t.Fatalf("miss ok %v msg %q", ok, msg)
	}
	got, _, ok := MatchChannel(list, "", 2)
	if !ok || got.ID != "b" {
		t.Fatalf("index %+v ok %v", got, ok)
	}
	if _, msg, ok := MatchChannel(list, "", 9); ok || msg != "没有第 9 个频道" {
		t.Fatalf("range ok %v msg %q", ok, msg)
	}
	if _, msg, ok := MatchChannel(list, "装货", 0); ok || msg != "有多个同名频道，请说切到第几个" {
		t.Fatalf("ambiguous ok %v msg %q", ok, msg)
	}
	got, _, ok = MatchChannel(list, "卸货", 0)
	if !ok || got.ID != "b" {
		t.Fatalf("name %+v", got)
	}
}
