package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

const InvitationTTL = 2 * time.Minute

type OnlineMember struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type Invitation struct {
	ID          string    `json:"id"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	SenderID    string    `json:"sender_id"`
	SenderName  string    `json:"sender_name"`
	RecipientID string    `json:"recipient_id"`
	ExpiresAt   time.Time `json:"expires_at"`
	Status      string    `json:"status"`
	delivered   bool
	timer       *time.Timer
}

func (h *Hub) SetInvitationNotifier(fn func(context.Context, string, string) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.invitationNotifier = fn
}

// Online means an authenticated device signalling connection is currently open.
// It does not promise that the device is listening to the caller's channel.
func (h *Hub) OnlineMembers(ctx context.Context, caller string) ([]OnlineMember, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.onlineMembers(ctx, caller)
}
func (h *Hub) onlineMembers(ctx context.Context, caller string) ([]OnlineMember, error) {
	out := []OnlineMember{}
	for id, conns := range h.conns {
		if id == caller || len(conns) == 0 {
			continue
		}
		u, err := h.users.Store.UserByID(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, OnlineMember{UserID: id, DisplayName: u.DisplayName})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayName == out[j].DisplayName {
			return out[i].UserID < out[j].UserID
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out, nil
}

func (h *Hub) expireInvitations(now time.Time) {
	for id, inv := range h.invitations {
		if !now.Before(inv.ExpiresAt) {
			if inv.Status == "pending" {
				inv.Status = "expired"
				h.publishInvitation(inv)
			}
			if inv.timer != nil {
				inv.timer.Stop()
			}
			delete(h.invitations, id)
		}
	}
}
func (h *Hub) publishInvitation(inv *Invitation) {
	h.enqueue([]string{inv.SenderID, inv.RecipientID}, encode(struct {
		Type       string      `json:"type"`
		Invitation *Invitation `json:"invitation"`
	}{"invitation", inv}))
}
func (h *Hub) Invitations(userID string, now time.Time) []Invitation {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expireInvitations(now)
	out := []Invitation{}
	for _, inv := range h.invitations {
		if inv.RecipientID == userID && inv.Status == "pending" && inv.delivered {
			out = append(out, *inv)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExpiresAt.Before(out[j].ExpiresAt) })
	return out
}

// Invite creates or reuses a channel belonging to the sender. Membership of the
// recipient is never changed here. All invitations are bounded, in-memory and online-only.
func (h *Hub) Invite(ctx context.Context, sender store.User, name, recipientName, recipientID string, now time.Time) (Invitation, error) {
	h.mu.Lock()
	h.expireInvitations(now)
	members, err := h.onlineMembers(ctx, sender.ID)
	if err != nil {
		h.mu.Unlock()
		return Invitation{}, err
	}
	var target string
	var matches []OnlineMember
	for _, m := range members {
		if recipientID != "" && m.UserID == recipientID || recipientID == "" && m.DisplayName == recipientName {
			matches = append(matches, m)
		}
	}
	if len(matches) == 0 {
		h.mu.Unlock()
		return Invitation{}, errors.New("对方不在线，邀请未送达")
	}
	if len(matches) > 1 {
		h.mu.Unlock()
		parts := []string{}
		for _, m := range matches {
			parts = append(parts, m.DisplayName+"（成员 ID "+m.UserID+"）")
		}
		return Invitation{}, fmt.Errorf("有多个同名成员，请确认成员 ID 后重试：%s", strings.Join(parts, "、"))
	}
	target = matches[0].UserID
	for _, inv := range h.invitations {
		if inv.Status == "pending" && inv.RecipientID == target {
			if inv.SenderID == sender.ID && inv.ChannelName == strings.TrimSpace(name) && inv.delivered {
				v := *inv
				h.mu.Unlock()
				if _, err := h.Switch(ctx, sender.ID, v.ChannelID); err != nil {
					return Invitation{}, err
				}
				if err := h.Execute(ctx, sender.DeviceID, "switch_channel", v.ChannelID, ""); err != nil {
					return Invitation{}, err
				}
				return v, nil
			}
			h.mu.Unlock()
			return Invitation{}, errors.New("对方有待处理的邀请，请稍后重试")
		}
	}
	if len(h.invitations) >= 256 {
		h.mu.Unlock()
		return Invitation{}, errors.New("邀请繁忙，请稍后重试")
	}
	recipient, err := h.users.Store.UserByID(ctx, target)
	if err != nil {
		h.mu.Unlock()
		return Invitation{}, err
	}
	prev, wasTalking, err := h.currentTalk(ctx, sender.ID)
	if err != nil {
		h.mu.Unlock()
		return Invitation{}, err
	}
	view, created, err := h.channels.ResolveInvitationChannel(ctx, sender.ID, name, now)
	if err != nil {
		h.mu.Unlock()
		return Invitation{}, err
	}
	if !created {
		_, err = h.channels.Switch(ctx, sender.ID, view.Channel.ID)
	}
	if err == nil && wasTalking {
		err = h.channels.SetTalking(ctx, sender.ID, false)
	}
	if err != nil {
		h.mu.Unlock()
		return Invitation{}, err
	}
	if wasTalking && prev != "" {
		h.publishTalk(ctx, prev, sender.ID, false)
	}
	h.enqueueUser(sender.ID, encode(channelSwitchedWire{Type: "channel_switched", ChannelID: view.Channel.ID, Name: view.Channel.Name}))
	h.publishMembers(ctx, view.Channel.ID)
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		h.mu.Unlock()
		return Invitation{}, err
	}
	inv := &Invitation{ID: hex.EncodeToString(random[:]), ChannelID: view.Channel.ID, ChannelName: view.Channel.Name, SenderID: sender.ID, SenderName: sender.DisplayName, RecipientID: target, ExpiresAt: now.Add(InvitationTTL), Status: "pending"}
	h.invitations[inv.ID] = inv
	inv.timer = time.AfterFunc(InvitationTTL, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.invitations[inv.ID] == inv {
			if inv.Status == "pending" {
				inv.Status = "expired"
				h.publishInvitation(inv)
			}
			delete(h.invitations, inv.ID)
		}
	})
	notifier := h.invitationNotifier
	payload, _ := json.Marshal(inv)
	h.mu.Unlock()
	err = h.Execute(ctx, sender.DeviceID, "switch_channel", view.Channel.ID, "")
	if err == nil {
		err = h.Execute(ctx, recipient.DeviceID, "announce_invitation", "", string(payload))
	}
	if err == nil && notifier == nil {
		err = errors.New("语音通知服务未连接")
	}
	if err == nil && notifier != nil {
		err = notifier(ctx, recipient.DeviceID, fmt.Sprintf("%s邀请你加入%s，请说接受或拒绝。", sender.DisplayName, view.Channel.Name))
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil && h.invitations[inv.ID] != inv {
		err = errors.New("对方已离线")
	}
	if err != nil {
		inv.Status = "expired"
		h.publishInvitation(inv)
		inv.timer.Stop()
		delete(h.invitations, inv.ID)
		return Invitation{}, fmt.Errorf("邀请未送达：%w", err)
	}
	inv.delivered = true
	h.publishInvitation(inv)
	return *inv, nil
}

func (h *Hub) RespondInvitation(ctx context.Context, recipient store.User, id string, accept bool, now time.Time) (Invitation, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.expireInvitations(now)
	if id == "" {
		for _, inv := range h.invitations {
			if inv.RecipientID == recipient.ID && inv.Status == "pending" {
				id = inv.ID
				break
			}
		}
	}
	inv := h.invitations[id]
	if inv == nil || inv.RecipientID != recipient.ID {
		return Invitation{}, errors.New("没有有效邀请，可能已过期")
	}
	wanted := "rejected"
	if accept {
		wanted = "accepted"
	}
	if inv.Status == wanted {
		result := *inv
		if accept {
			h.mu.Unlock()
			err := h.Execute(ctx, recipient.DeviceID, "switch_channel", inv.ChannelID, "")
			h.mu.Lock()
			if err != nil {
				return result, fmt.Errorf("已接受邀请，但设备切换未完成：%w", err)
			}
		}
		return result, nil
	}
	if inv.Status != "pending" {
		return Invitation{}, errors.New("邀请已处理")
	}
	if !inv.delivered {
		return Invitation{}, errors.New("邀请正在发送，请稍后回应")
	}
	if accept {
		prev, talking, err := h.currentTalk(ctx, recipient.ID)
		if err != nil {
			return Invitation{}, err
		}
		view, err := h.channels.JoinByID(ctx, recipient.ID, inv.ChannelID)
		if err != nil {
			return Invitation{}, err
		}
		if talking {
			if err = h.channels.SetTalking(ctx, recipient.ID, false); err != nil {
				return Invitation{}, err
			}
			h.publishTalk(ctx, prev, recipient.ID, false)
		}
		h.enqueueUser(recipient.ID, encode(channelSwitchedWire{Type: "channel_switched", ChannelID: view.Channel.ID, Name: view.Channel.Name}))
		h.publishMembers(ctx, view.Channel.ID)
	}
	inv.Status = wanted
	h.publishInvitation(inv)
	result := *inv
	if accept {
		h.mu.Unlock()
		err := h.Execute(ctx, recipient.DeviceID, "switch_channel", inv.ChannelID, "")
		h.mu.Lock()
		if err != nil {
			return result, fmt.Errorf("已接受邀请，但设备切换未完成：%w", err)
		}
	}
	return result, nil
}

func (h *Hub) BootPublic(ctx context.Context, userID string, now time.Time) (channel.View, error) {
	ctx = context.WithoutCancel(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, err := h.channels.EnsurePublic(ctx, now)
	if err != nil {
		return channel.View{}, err
	}
	prev, talking, err := h.currentTalk(ctx, userID)
	if err != nil {
		return channel.View{}, err
	}
	view, err := h.channels.JoinByID(ctx, userID, ch.ID)
	if err != nil {
		return channel.View{}, err
	}
	if err = h.channels.SetTalking(ctx, userID, false); err != nil {
		return channel.View{}, err
	}
	if talking {
		h.publishTalk(ctx, prev, userID, false)
	}
	h.enqueueUser(userID, encode(channelSwitchedWire{Type: "channel_switched", ChannelID: ch.ID, Name: ch.Name}))
	h.publishMembers(ctx, ch.ID)
	return view, nil
}
