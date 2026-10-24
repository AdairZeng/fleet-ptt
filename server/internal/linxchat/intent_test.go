package linxchat

import "testing"

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
