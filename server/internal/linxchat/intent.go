package linxchat

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Intent 是技术方案 §6.3 里的内部能力名。echo 表示没有命中已知话术。
type Intent string

const (
	// IntentCreateChannel 对应「建…频道/群」。
	IntentCreateChannel Intent = "create_channel"
	// IntentJoinChannel 对应「加入口令 / 进…」。
	IntentJoinChannel Intent = "join_channel"
	// IntentConfirmJoin 是在待确认口令上说「确认加入」。
	IntentConfirmJoin Intent = "confirm_join"
	// IntentCancelJoin 是取消尚未执行的口令入群。
	IntentCancelJoin Intent = "cancel_join"
	// IntentSwitchChannel 对应「切换到 / 切到」。
	IntentSwitchChannel Intent = "switch_channel"
	// IntentTalkStart 对应「开始对讲」。
	IntentTalkStart Intent = "talk_start"
	// IntentTalkEnd 对应「结束对讲」。
	IntentTalkEnd Intent = "talk_end"
	// IntentListChannels 对应「我有哪些频道」。
	IntentListChannels Intent = "list_my_channels"
	// IntentEcho 是未识别话术。
	IntentEcho            Intent = "echo"
	IntentPreviousChannel Intent = "previous_channel"
	IntentListSpeakers    Intent = "list_speakers"
	IntentShowJoinCode    Intent = "show_join_code"
	IntentShowJoinQR      Intent = "show_join_qr"
	IntentSetVolume       Intent = "set_volume"
	IntentSetNickname     Intent = "set_nickname"
)

// Utterance 是一句话的归类和槽位。执行业务不在这里。
type Utterance struct {
	Intent Intent
	Name   string
	Code   string
	Index  int
	Text   string
}

// Result 是给灵矽 TTS 的最终回答。
type Result struct {
	Intent Intent
	Answer string
}

// Parse 按关键词归类，并抽出频道名、口令或序号。更具体的话术优先。
func Parse(msg string) Utterance {
	msg = strings.TrimRight(strings.TrimSpace(msg), "。！？!?，, ")
	msg = strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(msg, "你好小路"), "，,。 "))
	switch {
	case exact(msg, "上一频道", "上一个频道", "切回上一频道", "回到上一个频道"):
		return Utterance{Intent: IntentPreviousChannel, Text: msg}
	case strings.Contains(msg, "谁在说话"), strings.Contains(msg, "谁在讲话"), strings.Contains(msg, "谁正在讲话"):
		return Utterance{Intent: IntentListSpeakers, Text: msg}
	case strings.Contains(msg, "二维码"):
		return Utterance{Intent: IntentShowJoinQR, Text: msg}
	case exact(msg, "频道口令", "当前口令", "口令是多少", "查看口令", "我的口令是什么"):
		return Utterance{Intent: IntentShowJoinCode, Text: msg}
	case strings.HasPrefix(msg, "叫我"), strings.HasPrefix(msg, "我叫"), strings.HasPrefix(msg, "把昵称改成"), strings.HasPrefix(msg, "昵称改成"), strings.HasPrefix(msg, "修改昵称"):
		return Utterance{Intent: IntentSetNickname, Name: cutAfter(msg, []string{"叫我", "我叫", "把昵称改成", "昵称改成", "修改昵称"}), Text: msg}
	case exact(msg, "音量小一点", "音量调小一点", "音量调低一点"):
		return Utterance{Intent: IntentSetVolume, Name: "-10", Text: msg}
	case exact(msg, "音量大一点", "音量调大一点", "音量调高一点"):
		return Utterance{Intent: IntentSetVolume, Name: "+10", Text: msg}
	case strings.HasPrefix(msg, "音量"):
		value := strings.TrimSpace(strings.TrimSuffix(cutAfter(msg, []string{"音量调到", "音量设为", "音量"}), "%"))
		value = strings.TrimPrefix(value, "百分之")
		return Utterance{Intent: IntentSetVolume, Name: value, Text: msg}
	case strings.Contains(msg, "结束对讲"):
		return Utterance{Intent: IntentTalkEnd, Text: msg}
	case strings.Contains(msg, "开始对讲"), strings.Contains(msg, "继续对讲"):
		return Utterance{Intent: IntentTalkStart, Text: msg}
	case exact(msg, "确认加入", "确认", "是的", "没错"):
		return Utterance{Intent: IntentConfirmJoin, Text: msg}
	case exact(msg, "取消加入", "取消", "不对", "不是", "不是这个"):
		return Utterance{Intent: IntentCancelJoin, Text: msg}
	case strings.Contains(msg, "切换到"), strings.Contains(msg, "切到"):
		name, index := switchTarget(msg)
		return Utterance{Intent: IntentSwitchChannel, Name: name, Index: index, Text: msg}
	case strings.Contains(msg, "哪些频道"), strings.Contains(msg, "我的频道"):
		return Utterance{Intent: IntentListChannels, Text: msg}
	case isCreate(msg):
		return Utterance{Intent: IntentCreateChannel, Name: createName(msg), Text: msg}
	default:
		if isJoin(msg) {
			code, name := joinSlots(msg)
			return Utterance{Intent: IntentJoinChannel, Code: code, Name: name, Text: msg}
		}
		if code, ok := utteranceCode(msg); ok {
			return Utterance{Intent: IntentJoinChannel, Code: code, Text: msg}
		}
		return Utterance{Intent: IntentEcho, Text: msg}
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

func isJoin(msg string) bool {
	if strings.Contains(msg, "加入") || strings.Contains(msg, "进入") || strings.Contains(msg, "口令") {
		return true
	}
	trimmed := strings.TrimSpace(msg)
	return strings.HasPrefix(trimmed, "进") && !strings.HasPrefix(trimmed, "进度")
}

func exact(msg string, opts ...string) bool {
	for _, opt := range opts {
		if msg == opt {
			return true
		}
	}
	return false
}

func createName(msg string) string {
	rest := cutAfter(msg, []string{"创建一个", "新建一个", "建一个", "建个", "新建", "创建", "建群", "建频道"})
	return cleanName(rest)
}

func joinSlots(msg string) (string, string) {
	if i := strings.Index(msg, "口令"); i >= 0 {
		rest := strings.TrimSpace(msg[i+len("口令"):])
		rest = strings.TrimPrefix(rest, "是")
		rest = strings.TrimSpace(rest)
		if code, ok := utteranceCode(rest); ok {
			return code, ""
		}
		return "", ""
	}
	rest := cutAfter(msg, []string{"进入", "加入", "进"})
	if code, ok := utteranceCode(rest); ok {
		return code, ""
	}
	return "", cleanName(rest)
}

func switchTarget(msg string) (string, int) {
	rest := cutAfter(msg, []string{"切换到", "切到"})
	rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "频道"))
	if n, ok := parseIndex(rest); ok {
		return "", n
	}
	return cleanName(rest), 0
}

func cutAfter(msg string, keys []string) string {
	best := -1
	bestLen := 0
	for _, key := range keys {
		i := strings.Index(msg, key)
		if i < 0 {
			continue
		}
		if best < 0 || i < best || (i == best && len(key) > bestLen) {
			best = i
			bestLen = len(key)
		}
	}
	if best < 0 {
		return strings.TrimSpace(msg)
	}
	return strings.TrimSpace(msg[best+bestLen:])
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "一个")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "频道")
	s = strings.TrimSuffix(s, "群")
	return strings.TrimSpace(s)
}

func parseIndex(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	s = strings.TrimPrefix(s, "第")
	s = strings.TrimSuffix(s, "个频道")
	s = strings.TrimSuffix(s, "个")
	s = strings.TrimSuffix(s, "频道")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 99 {
		return n, true
	}
	runes := []rune(s)
	if len(runes) == 1 {
		if n, ok := cnDigit[runes[0]]; ok {
			return n, true
		}
	}
	return 0, false
}

var cnDigit = map[rune]int{
	'一': 1,
	'二': 2,
	'三': 3,
	'四': 4,
	'五': 5,
	'六': 6,
	'七': 7,
	'八': 8,
	'九': 9,
}

// utteranceCode 把「3、8、4、2、9、1」或带空格的口令收成 6 位。
// 这里不检查字符集。含 0、1、O、I 的口令留给执行阶段拒绝，避免把听错的口令当成没听见。
func utteranceCode(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '、', '，', ',', '。', '.', ':', '：', '-', '－':
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		if digit, ok := cnDigit[r]; ok {
			r = rune('0' + digit)
		} else if r == '零' || r == '〇' {
			r = '0'
		} else if r == '两' {
			r = '2'
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	code := b.String()
	if utf8.RuneCountInString(code) != 6 {
		return "", false
	}
	for _, r := range code {
		if r > unicode.MaxASCII {
			return "", false
		}
	}
	return code, true
}

// ParseVolume accepts decimal or spoken Chinese percentages in the device range.
func ParseVolume(text string) (int, bool) {
	text = strings.TrimSpace(text)
	if n, err := strconv.Atoi(text); err == nil {
		return n, n >= 0 && n <= 100
	}
	if text == "一百" {
		return 100, true
	}
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	chars := []rune(text)
	if len(chars) == 1 {
		if chars[0] == '十' {
			return 10, true
		}
		n, ok := digits[chars[0]]
		return n, ok
	}
	for i, r := range chars {
		if r != '十' {
			continue
		}
		if i > 1 || len(chars)-i > 2 {
			return 0, false
		}
		tens, ones := 1, 0
		if i == 1 {
			var ok bool
			tens, ok = digits[chars[0]]
			if !ok || tens == 0 {
				return 0, false
			}
		}
		if i+1 < len(chars) {
			var ok bool
			ones, ok = digits[chars[i+1]]
			if !ok {
				return 0, false
			}
		}
		return tens*10 + ones, true
	}
	return 0, false
}
