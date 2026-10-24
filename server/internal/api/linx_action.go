package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/linxchat"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
	"github.com/AdairZeng/fleet-ptt/server/internal/ws"
)

type voiceExec struct {
	channels  *channel.Service
	hub       *ws.Hub
	gate      *linxchat.JoinGate
	sessionID string
}

type voiceResult struct {
	Intent linxchat.Intent
	Answer string
	Failed bool
}

func (e voiceExec) run(ctx context.Context, u store.User, parsed linxchat.Utterance, now time.Time) voiceResult {
	wasTalking, talkErr := e.stopUplink(ctx, u.ID, parsed.Intent)
	if talkErr != nil && !errors.Is(talkErr, channel.ErrNoCurrent) {
		return fail(parsed.Intent, "停止发送失败，操作未执行")
	}
	if parsed.Intent != linxchat.IntentSetNickname && parsed.Intent != linxchat.IntentJoinChannel &&
		parsed.Intent != linxchat.IntentConfirmJoin && parsed.Intent != linxchat.IntentCancelJoin {
		e.gate.Clear(u.DeviceID)
	}
	var out voiceResult
	switch parsed.Intent {
	case linxchat.IntentListOnline:
		members, err := e.hub.OnlineMembers(ctx, u.ID)
		if err != nil {
			out = fail(parsed.Intent, "读取在线成员失败")
			break
		}
		if len(members) == 0 {
			out = voiceResult{Intent: parsed.Intent, Answer: "当前没有其他在线成员"}
			break
		}
		parts := []string{}
		for _, m := range members {
			parts = append(parts, fmt.Sprintf("%s（成员 ID %s）", m.DisplayName, m.UserID))
		}
		out = voiceResult{Intent: parsed.Intent, Answer: "当前在线成员：" + strings.Join(parts, "、")}
	case linxchat.IntentInvite:
		inv, err := e.hub.Invite(ctx, u, parsed.Name, parsed.RecipientName, parsed.RecipientID, now)
		if err != nil {
			out = fail(parsed.Intent, err.Error())
		} else {
			out = voiceResult{Intent: parsed.Intent, Answer: "已进入" + inv.ChannelName + "并送达邀请，等待对方接受"}
		}
	case linxchat.IntentAcceptInvitation, linxchat.IntentRejectInvitation:
		out = e.respondInvitation(ctx, u, parsed.InvitationID, parsed.Intent == linxchat.IntentAcceptInvitation, now)
	case linxchat.IntentPreviousChannel:
		target, err := e.channels.Store.PreviousChannel(ctx, u.ID)
		if err != nil {
			out = fail(parsed.Intent, "没有上一频道")
			break
		}
		if _, err := e.hub.Switch(ctx, u.ID, target); err != nil {
			out = fail(parsed.Intent, voiceErr(err, "回切失败"))
			break
		}
		if err := e.hub.Execute(ctx, u.DeviceID, "switch_channel", target, ""); err != nil {
			out = fail(parsed.Intent, err.Error())
			break
		}
		out = voiceResult{Intent: parsed.Intent, Answer: "已回到上一频道"}
	case linxchat.IntentSetNickname:
		if !user.ValidNickname(parsed.Name) {
			e.gate.Clear(u.DeviceID)
			out = fail(parsed.Intent, "请说 1 到 32 字的昵称")
			break
		}
		e.offerPending(u.DeviceID, "nickname:"+parsed.Name, now)
		out = voiceResult{Intent: parsed.Intent, Answer: "以后叫你" + parsed.Name + "，请说确认或取消"}
	case linxchat.IntentSetVolume:
		if parsed.Name == "+10" || parsed.Name == "-10" {
			if err := e.hub.Execute(ctx, u.DeviceID, "set_volume", "", parsed.Name); err != nil {
				out = fail(parsed.Intent, err.Error())
				break
			}
			answer := "音量已调高"
			if parsed.Name == "-10" {
				answer = "音量已调低"
			}
			out = voiceResult{Intent: parsed.Intent, Answer: answer}
			break
		}
		volume, err := strconv.Atoi(parsed.Name)
		if err != nil || volume < 0 || volume > 100 {
			out = fail(parsed.Intent, "请说音量调到 0 到 100 的数字")
			break
		}
		if err := e.hub.Execute(ctx, u.DeviceID, "set_volume", "", strconv.Itoa(volume)); err != nil {
			out = fail(parsed.Intent, err.Error())
			break
		}
		out = voiceResult{Intent: parsed.Intent, Answer: "音量已调到" + strconv.Itoa(volume)}
	case linxchat.IntentListSpeakers, linxchat.IntentShowJoinCode, linxchat.IntentShowJoinQR:
		out = e.channelInformation(ctx, u, parsed.Intent)
	case linxchat.IntentCreateChannel:
		out = e.create(ctx, u.ID, parsed.Name, now)
	case linxchat.IntentJoinChannel:
		out = e.join(ctx, u, parsed, now)
	case linxchat.IntentConfirmJoin:
		out = e.confirmJoin(ctx, u, now)
	case linxchat.IntentCancelJoin:
		if _, ok := e.gate.Pending(u.DeviceID, now); !ok && len(e.hub.Invitations(u.ID, now)) > 0 {
			out = e.respondInvitation(ctx, u, "", false, now)
		} else {
			out = e.cancelJoin(u.DeviceID, now)
		}
	case linxchat.IntentSwitchChannel:
		out = e.switchChannel(ctx, u.ID, parsed)
	case linxchat.IntentTalkStart:
		out = e.talkStart(ctx, u.ID, u.DeviceID)
	case linxchat.IntentTalkEnd:
		out = e.talkEnd(talkErr, wasTalking)
	case linxchat.IntentListChannels:
		out = e.list(ctx, u.ID)
	default:
		out = fail(parsed.Intent, "不支持的业务操作")
	}
	e.hub.PushText(u.ID, voiceKind(out.Failed), out.Answer)
	return out
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

func (e voiceExec) join(ctx context.Context, u store.User, parsed linxchat.Utterance, now time.Time) voiceResult {
	if parsed.Code != "" {
		if !channel.ValidJoinCode(parsed.Code) {
			e.gate.Clear(u.DeviceID)
			return fail(linxchat.IntentJoinChannel, "口令不正确")
		}
		e.offerPending(u.DeviceID, "join:"+parsed.Code, now)
		return voiceResult{Intent: linxchat.IntentJoinChannel, Answer: linxchat.ConfirmJoinPrompt(parsed.Code)}
	}
	e.gate.Clear(u.DeviceID)
	if parsed.Name == "" {
		return voiceResult{Intent: linxchat.IntentJoinChannel, Answer: "没听清口令。请说加入口令，再逐位说 6 位口令"}
	}
	return e.joinNow(ctx, u.ID, "", parsed.Name)
}

// Pending actions belong to both the physical device and its Linx conversation.
// A second conversation cannot confirm or consume another conversation's action.
func (e voiceExec) offerPending(deviceID, action string, now time.Time) {
	e.gate.Offer(deviceID, e.sessionID+"\x00"+action, now)
}

func (e voiceExec) confirmPending(deviceID string, now time.Time) (string, bool) {
	pending, ok := e.gate.Pending(deviceID, now)
	if !ok {
		return "", false
	}
	action, ok := strings.CutPrefix(pending, e.sessionID+"\x00")
	if !ok {
		return "", false
	}
	e.gate.Clear(deviceID)
	return action, true
}

func (e voiceExec) confirmJoin(ctx context.Context, u store.User, now time.Time) voiceResult {
	pending, ok := e.confirmPending(u.DeviceID, now)
	if !ok {
		if len(e.hub.Invitations(u.ID, now)) > 0 {
			return e.respondInvitation(ctx, u, "", true, now)
		}
		return fail(linxchat.IntentConfirmJoin, "没有本次会话的有效待确认操作，请重新说出指令")
	}
	if nickname, ok := strings.CutPrefix(pending, "nickname:"); ok {
		return e.rename(ctx, u, nickname)
	}
	code, ok := strings.CutPrefix(pending, "join:")
	if !ok {
		return fail(linxchat.IntentConfirmJoin, "确认内容无效，请重新说出指令")
	}
	joined := e.joinNow(ctx, u.ID, code, "")
	if joined.Failed {
		joined.Intent = linxchat.IntentConfirmJoin
	}
	return joined
}

func (e voiceExec) cancelJoin(deviceID string, now time.Time) voiceResult {
	pending, ok := e.confirmPending(deviceID, now)
	if !ok {
		return voiceResult{Intent: linxchat.IntentCancelJoin, Answer: "没有有效的待确认操作"}
	}
	answer := "已取消加入"
	if strings.HasPrefix(pending, "nickname:") {
		answer = "已取消修改昵称"
	}
	return voiceResult{Intent: linxchat.IntentCancelJoin, Answer: answer}
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

func (e voiceExec) channelInformation(ctx context.Context, u store.User, intent linxchat.Intent) voiceResult {
	current, err := e.channels.Current(ctx, u.ID)
	if err != nil {
		return fail(intent, "还没有进入频道")
	}
	if intent == linxchat.IntentShowJoinCode {
		return voiceResult{Intent: intent, Answer: "频道口令是" + strings.Join(strings.Split(current.Channel.JoinCode, ""), "、")}
	}
	if intent == linxchat.IntentShowJoinQR {
		if err := e.hub.Execute(ctx, u.DeviceID, "show_join_qr", current.Channel.ID, ""); err != nil {
			return fail(intent, err.Error())
		}
		return voiceResult{Intent: intent, Answer: "二维码已显示"}
	}
	members, err := e.channels.Members(ctx, current.Channel.ID)
	if err != nil {
		return fail(intent, "读取讲话成员失败")
	}
	names := []string{}
	for _, m := range members {
		if m.Talking {
			names = append(names, m.DisplayName)
		}
	}
	if len(names) == 0 {
		return voiceResult{Intent: intent, Answer: "当前没有人讲话"}
	}
	return voiceResult{Intent: intent, Answer: strings.Join(names, "、") + "正在讲话"}
}

func (e voiceExec) rename(ctx context.Context, u store.User, nickname string) voiceResult {
	if err := e.channels.Store.RenameUser(ctx, u.ID, nickname); err != nil {
		return fail(linxchat.IntentSetNickname, "修改昵称失败")
	}
	e.hub.RefreshMembers(ctx, u.ID)
	if err := e.hub.Execute(ctx, u.DeviceID, "set_nickname", "", nickname); err != nil {
		return fail(linxchat.IntentSetNickname, "昵称已保存，设备显示未同步："+err.Error())
	}
	return voiceResult{Intent: linxchat.IntentSetNickname, Answer: "以后叫你" + nickname}
}

func (e voiceExec) respondInvitation(ctx context.Context, u store.User, id string, accept bool, now time.Time) voiceResult {
	intent := linxchat.IntentRejectInvitation
	if accept {
		intent = linxchat.IntentAcceptInvitation
	}
	inv, err := e.hub.RespondInvitation(ctx, u, id, accept, now)
	if err != nil {
		return fail(intent, err.Error())
	}
	answer := "已拒绝邀请"
	if accept {
		answer = "已加入" + inv.ChannelName
	}
	return voiceResult{Intent: intent, Answer: answer}
}
