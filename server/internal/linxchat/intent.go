package linxchat

import "strings"

// Intent 是技术方案 §6.3 里的内部能力名。echo 表示没有命中已知话术。
type Intent string

const (
	// IntentCreateChannel 对应「建…频道/群」。
	IntentCreateChannel Intent = "create_channel"
	// IntentJoinChannel 对应「加入口令 / 进…」。
	IntentJoinChannel Intent = "join_channel"
	// IntentSwitchChannel 对应「切换到 / 切到」。
	IntentSwitchChannel Intent = "switch_channel"
	// IntentTalkStart 对应「开始对讲」。
	IntentTalkStart Intent = "talk_start"
	// IntentTalkEnd 对应「结束对讲」。
	IntentTalkEnd Intent = "talk_end"
	// IntentListChannels 对应「我有哪些频道」。
	IntentListChannels Intent = "list_my_channels"
	// IntentEcho 是未识别话术，Answer 回声原文。
	IntentEcho Intent = "echo"
)

// Result 是一句话的归类结果。Answer 供灵矽 TTS，不表示业务已经执行。
type Result struct {
	Intent Intent
	Answer string
}

// Classify 按关键词归类。更具体的话术优先。
// 建群、入群、切群与对讲的真正执行不在这里。
func Classify(msg string) Result {
	msg = strings.TrimSpace(msg)
	switch {
	case strings.Contains(msg, "结束对讲"):
		return Result{Intent: IntentTalkEnd, Answer: "已收到结束对讲"}
	case strings.Contains(msg, "开始对讲"):
		return Result{Intent: IntentTalkStart, Answer: "已收到开始对讲"}
	case strings.Contains(msg, "切换到"), strings.Contains(msg, "切到"):
		return Result{Intent: IntentSwitchChannel, Answer: "已收到切群指令"}
	case strings.Contains(msg, "哪些频道"), strings.Contains(msg, "我的频道"):
		return Result{Intent: IntentListChannels, Answer: "已收到查询频道"}
	case isCreate(msg):
		return Result{Intent: IntentCreateChannel, Answer: "已收到建群指令，稍后播报口令"}
	case strings.Contains(msg, "加入口令"), strings.Contains(msg, "加入"), strings.Contains(msg, "进入"):
		return Result{Intent: IntentJoinChannel, Answer: "已收到入群指令，请再说一次口令确认"}
	default:
		return Result{Intent: IntentEcho, Answer: "收到：" + msg}
	}
}

func isCreate(msg string) bool {
	if !strings.Contains(msg, "频道") && !strings.Contains(msg, "群") {
		return false
	}
	return strings.Contains(msg, "创建") ||
		strings.Contains(msg, "新建") ||
		strings.Contains(msg, "建一个") ||
		strings.Contains(msg, "建个") ||
		strings.Contains(msg, "建群") ||
		strings.Contains(msg, "建频道")
}
