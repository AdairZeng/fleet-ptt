package linxchat

import (
	"fmt"
	"strings"
)

// ChannelRef 是切群时用来匹配的已加入频道。
type ChannelRef struct {
	ID   string
	Name string
}

// SpeakCode 把口令拆成逐位朗读，例如「口令是 3、8、4、2、9、1」。
func SpeakCode(code string) string {
	parts := make([]string, 0, len(code))
	for _, r := range code {
		parts = append(parts, string(r))
	}
	return "口令是 " + strings.Join(parts, "、")
}

// CreatedAnswer 是建群成功后的 TTS 文案，口令逐位念出。
func CreatedAnswer(name, code string) string {
	return "已创建" + name + "。" + SpeakCode(code)
}

// ConfirmJoinPrompt 请司机把刚听到的口令再说一遍。第一句口令不会进房。
func ConfirmJoinPrompt(code string) string {
	return "请再说一次口令确认。" + SpeakCode(code)
}

// ListAnswer 念出已加入的频道。current 为空表示没有当前频道。
func ListAnswer(names []string, current string) string {
	if len(names) == 0 {
		return "你还没有频道。可以说建一个装货频道，或加入口令"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "你有 %d 个频道：", len(names))
	for i, name := range names {
		if i > 0 {
			b.WriteString("，")
		}
		fmt.Fprintf(&b, "%d、%s", i+1, name)
	}
	if current != "" {
		fmt.Fprintf(&b, "。当前是%s", current)
	}
	b.WriteString("。")
	return b.String()
}

// MatchChannel 在已加入列表里按名称或 1 起的序号找频道。
// 失败时 message 直接给 TTS，不能是空字符串。
func MatchChannel(list []ChannelRef, name string, index int) (ChannelRef, string, bool) {
	if index > 0 {
		if index > len(list) {
			return ChannelRef{}, fmt.Sprintf("没有第 %d 个频道", index), false
		}
		return list[index-1], "", true
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ChannelRef{}, "要切到哪个频道？可以说切到装货，或切到第 2 个", false
	}
	var hits []ChannelRef
	for _, item := range list {
		if item.Name == name {
			hits = append(hits, item)
		}
	}
	switch len(hits) {
	case 0:
		return ChannelRef{}, "未匹配到频道", false
	case 1:
		return hits[0], "", true
	default:
		return ChannelRef{}, "有多个同名频道，请说切到第几个", false
	}
}
