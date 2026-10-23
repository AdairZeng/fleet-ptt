package api

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/linxchat"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/ws"
)

type voiceExec struct {
	channels *channel.Service
	hub      *ws.Hub
	gate     *linxchat.JoinGate
}

type voiceResult struct {
	Intent linxchat.Intent
	Answer string
	Failed bool
}

func (e voiceExec) run(ctx context.Context, u store.User, msg string, now time.Time) linxchat.Result {
	parsed := linxchat.Parse(msg)
	wasTalking, talkErr := e.stopUplink(ctx, u.ID, parsed.Intent)
	if talkErr != nil && !errors.Is(talkErr, channel.ErrNoCurrent) {
		return linxchat.Result{Intent: parsed.Intent, Answer: "停止发送失败，操作未执行"}
	}
	if parsed.Intent != linxchat.IntentJoinChannel &&
		parsed.Intent != linxchat.IntentConfirmJoin &&
		parsed.Intent != linxchat.IntentCancelJoin {
		e.gate.Clear(u.ID)
	}
	var out voiceResult
	switch parsed.Intent {
	case linxchat.IntentCreateChannel:
		out = e.create(ctx, u.ID, parsed.Name, now)
	case linxchat.IntentJoinChannel:
		out = e.join(ctx, u.ID, parsed, now)
	case linxchat.IntentConfirmJoin:
		out = e.confirmJoin(ctx, u.ID, now)
	case linxchat.IntentCancelJoin:
		out = e.cancelJoin(u.ID, now)
	case linxchat.IntentSwitchChannel:
		out = e.switchChannel(ctx, u.ID, parsed)
	case linxchat.IntentTalkStart:
		out = e.talkStart(ctx, u.ID, u.DeviceID)
	case linxchat.IntentTalkEnd:
		out = e.talkEnd(talkErr, wasTalking)
	case linxchat.IntentListChannels:
		out = e.list(ctx, u.ID)
	default:
		out = voiceResult{Intent: linxchat.IntentEcho, Answer: "收到：" + parsed.Text}
	}
	e.hub.PushText(u.ID, voiceKind(out.Failed), out.Answer)
	return linxchat.Result{Intent: out.Intent, Answer: out.Answer}
}

// stopUplink 在执行建群、入群、切群、列举和结束之外的对讲指令前先停上行。
// 开始对讲本身要打开发送，不在这里先关掉。
func (e voiceExec) stopUplink(ctx context.Context, userID string, intent linxchat.Intent) (bool, error) {
	if intent == linxchat.IntentTalkStart {
		return false, nil
	}
	talking, err := e.channels.Talking(ctx, userID)
	if err != nil && !errors.Is(err, channel.ErrNoCurrent) {
		log.Printf("linx: talking: %v", err)
	}
	if talking {
		u, lookupErr := e.channels.Store.UserByID(ctx, userID)
		if lookupErr != nil {
			return talking, lookupErr
		}
		if stopErr := e.hub.Execute(ctx, u.DeviceID, "talk_end", "", ""); stopErr != nil {
			return talking, stopErr
		}
	}
	if _, endErr := e.hub.EndTalk(ctx, userID); endErr != nil && !errors.Is(endErr, channel.ErrNoCurrent) {
		log.Printf("linx: stop uplink: %v", endErr)
	}
	return talking, err
}

func (e voiceExec) create(ctx context.Context, userID, name string, now time.Time) voiceResult {
	if name == "" {
		return voiceResult{Intent: linxchat.IntentCreateChannel, Answer: "请说出频道名，例如建一个装货频道"}
	}
	view, err := e.hub.Create(ctx, userID, name, now)
	if err != nil {
		return fail(linxchat.IntentCreateChannel, voiceErr(err, "创建频道失败"))
	}
	return voiceResult{
		Intent: linxchat.IntentCreateChannel,
		Answer: linxchat.CreatedAnswer(view.Channel.Name, view.Channel.JoinCode),
	}
}

func (e voiceExec) join(ctx context.Context, userID string, parsed linxchat.Utterance, now time.Time) voiceResult {
	if parsed.Code != "" {
		if !channel.ValidJoinCode(parsed.Code) {
			e.gate.Clear(userID)
			return fail(linxchat.IntentJoinChannel, "口令不正确")
		}
		if !e.gate.Offer(userID, parsed.Code, now) {
			return voiceResult{Intent: linxchat.IntentJoinChannel, Answer: linxchat.ConfirmJoinPrompt(parsed.Code)}
		}
		return e.joinNow(ctx, userID, parsed.Code, "")
	}
	e.gate.Clear(userID)
	if parsed.Name == "" {
		return voiceResult{Intent: linxchat.IntentJoinChannel, Answer: "没听清口令。请说加入口令，再逐位说 6 位口令"}
	}
	return e.joinNow(ctx, userID, "", parsed.Name)
}

func (e voiceExec) confirmJoin(ctx context.Context, userID string, now time.Time) voiceResult {
	code, ok := e.gate.Confirm(userID, now)
	if !ok {
		return voiceResult{Intent: linxchat.IntentConfirmJoin, Answer: "没有待确认的口令。请说加入口令，再报 6 位口令"}
	}
	joined := e.joinNow(ctx, userID, code, "")
	if joined.Failed {
		joined.Intent = linxchat.IntentConfirmJoin
	}
	return joined
}

func (e voiceExec) cancelJoin(userID string, now time.Time) voiceResult {
	if e.gate.Cancel(userID, now) {
		return voiceResult{Intent: linxchat.IntentCancelJoin, Answer: "已取消加入"}
	}
	return voiceResult{Intent: linxchat.IntentCancelJoin, Answer: "没有待确认的口令"}
}

func (e voiceExec) joinNow(ctx context.Context, userID, code, name string) voiceResult {
	current, err := e.channels.Current(ctx, userID)
	same := err == nil && sameChannel(current, code, name)
	view, err := e.hub.Join(ctx, userID, code, name)
	if err != nil {
		return fail(linxchat.IntentJoinChannel, voiceErr(err, "加入频道失败"))
	}
	if same {
		return voiceResult{Intent: linxchat.IntentJoinChannel, Answer: "已在" + view.Channel.Name}
	}
	return voiceResult{Intent: linxchat.IntentJoinChannel, Answer: "已加入" + view.Channel.Name}
}

func (e voiceExec) switchChannel(ctx context.Context, userID string, parsed linxchat.Utterance) voiceResult {
	if parsed.Name == "" && parsed.Index == 0 {
		return voiceResult{Intent: linxchat.IntentSwitchChannel, Answer: "要切到哪个频道？可以说切到装货，或切到第 2 个"}
	}
	list, err := e.channels.List(ctx, userID)
	if err != nil {
		return fail(linxchat.IntentSwitchChannel, voiceErr(err, "读取频道列表失败"))
	}
	refs := make([]linxchat.ChannelRef, 0, len(list))
	for _, view := range list {
		refs = append(refs, linxchat.ChannelRef{ID: view.Channel.ID, Name: view.Channel.Name})
	}
	target, message, ok := linxchat.MatchChannel(refs, parsed.Name, parsed.Index)
	if !ok {
		return fail(linxchat.IntentSwitchChannel, message)
	}
	current, err := e.channels.Current(ctx, userID)
	already := err == nil && current.Channel.ID == target.ID
	if _, err := e.hub.Switch(ctx, userID, target.ID); err != nil {
		return fail(linxchat.IntentSwitchChannel, voiceErr(err, "切换频道失败"))
	}
	u, lookupErr := e.channels.Store.UserByID(ctx, userID)
	if lookupErr != nil {
		return fail(linxchat.IntentSwitchChannel, "读取设备失败")
	}
	if err := e.hub.Execute(ctx, u.DeviceID, "switch_channel", target.ID, ""); err != nil {
		return fail(linxchat.IntentSwitchChannel, err.Error())
	}
	if already {
		return voiceResult{Intent: linxchat.IntentSwitchChannel, Answer: "已经在" + target.Name}
	}
	return voiceResult{Intent: linxchat.IntentSwitchChannel, Answer: "已切换到" + target.Name}
}

func (e voiceExec) talkStart(ctx context.Context, userID, deviceID string) voiceResult {
	view, err := e.channels.Current(ctx, userID)
	if err != nil {
		return fail(linxchat.IntentTalkStart, voiceErr(err, "还没有进入频道"))
	}
	if err := e.hub.Execute(ctx, deviceID, "talk_start", view.Channel.ID, ""); err != nil {
		return fail(linxchat.IntentTalkStart, err.Error())
	}
	if _, err := e.hub.StartTalk(ctx, userID); err != nil {
		_ = e.hub.Execute(ctx, deviceID, "talk_end", view.Channel.ID, "")
		return fail(linxchat.IntentTalkStart, voiceErr(err, "更新发送状态失败"))
	}
	return voiceResult{Intent: linxchat.IntentTalkStart, Answer: "已开始对讲"}
}

func (e voiceExec) talkEnd(talkErr error, wasTalking bool) voiceResult {
	if errors.Is(talkErr, channel.ErrNoCurrent) {
		return fail(linxchat.IntentTalkEnd, "还没有进入频道")
	}
	if talkErr != nil {
		return fail(linxchat.IntentTalkEnd, "暂时没能结束对讲")
	}
	if wasTalking {
		return voiceResult{Intent: linxchat.IntentTalkEnd, Answer: "已结束对讲"}
	}
	return voiceResult{Intent: linxchat.IntentTalkEnd, Answer: "当前没有在对讲"}
}

func (e voiceExec) list(ctx context.Context, userID string) voiceResult {
	list, err := e.channels.List(ctx, userID)
	if err != nil {
		return fail(linxchat.IntentListChannels, voiceErr(err, "读取频道列表失败"))
	}
	names := make([]string, 0, len(list))
	for _, view := range list {
		names = append(names, view.Channel.Name)
	}
	currentName := ""
	if current, err := e.channels.Current(ctx, userID); err == nil {
		currentName = current.Channel.Name
	} else if !errors.Is(err, channel.ErrNoCurrent) {
		return fail(linxchat.IntentListChannels, voiceErr(err, "读取频道列表失败"))
	}
	return voiceResult{Intent: linxchat.IntentListChannels, Answer: linxchat.ListAnswer(names, currentName)}
}

func sameChannel(current channel.View, code, name string) bool {
	if code != "" {
		return channel.NormalizeJoinCode(current.Channel.JoinCode) == channel.NormalizeJoinCode(code)
	}
	return name != "" && current.Channel.Name == name
}

func fail(intent linxchat.Intent, answer string) voiceResult {
	return voiceResult{Intent: intent, Answer: answer, Failed: true}
}

func voiceKind(failed bool) string {
	if failed {
		return "error"
	}
	return "notice"
}

func voiceErr(err error, fallback string) string {
	switch {
	case errors.Is(err, channel.ErrInvalidName),
		errors.Is(err, channel.ErrInvalidJoin),
		errors.Is(err, channel.ErrInvalidSwitch),
		errors.Is(err, channel.ErrCodeNotFound),
		errors.Is(err, channel.ErrNameNotFound),
		errors.Is(err, channel.ErrNotMember),
		errors.Is(err, channel.ErrAmbiguousName),
		errors.Is(err, channel.ErrNoCurrent):
		return err.Error()
	default:
		log.Printf("linx: %v", err)
		return fallback
	}
}
