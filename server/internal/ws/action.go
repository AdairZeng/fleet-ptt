package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type Action struct {
	Type      string `json:"type"`
	ID        string `json:"action_id"`
	Action    string `json:"action"`
	ChannelID string `json:"channel_id,omitempty"`
	Value     string `json:"value,omitempty"`
}

type actionResult struct {
	Type    string `json:"type"`
	ID      string `json:"action_id"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

type pendingAction struct {
	client *client
	result chan actionResult
}

// Execute targets the active connection of an authenticated device. It never
// interprets a channel broadcast as an instruction to open a microphone.
func (h *Hub) Execute(ctx context.Context, deviceID, action, channelID, value string) error {
	switch action {
	case "talk_start", "talk_end", "switch_channel", "set_volume", "show_join_qr", "set_nickname":
	default:
		return errors.New("未知设备动作")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(random[:])
	h.mu.Lock()
	c := h.devices[deviceID]
	if c == nil {
		h.mu.Unlock()
		return errors.New("设备未连接")
	}
	pending := pendingAction{client: c, result: make(chan actionResult, 1)}
	if len(h.actions) >= 256 {
		h.mu.Unlock()
		return errors.New("设备动作繁忙")
	}
	for _, p := range h.actions {
		if p.client == c {
			h.mu.Unlock()
			return errors.New("设备正在处理其他动作")
		}
	}
	h.actions[id] = pending
	sent := c.sendMsg(encode(Action{Type: "action", ID: id, Action: action, ChannelID: channelID, Value: value}))
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.actions, id); h.mu.Unlock() }()
	if !sent {
		return errors.New("设备消息发送失败")
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case result := <-pending.result:
		if !result.Success {
			return errors.New("设备执行失败：" + result.Message)
		}
		return nil
	case <-ctx.Done():
		c.sendMsg(encode(Action{Type: "action_cancel", ID: id}))
		return ctx.Err()
	case <-timer.C:
		c.sendMsg(encode(Action{Type: "action_cancel", ID: id}))
		return errors.New("设备执行超时")
	}
}

func (h *Hub) actionResult(c *client, data []byte) {
	var required struct {
		Success *bool `json:"success"`
	}
	var result actionResult
	if json.Unmarshal(data, &required) != nil || required.Success == nil || json.Unmarshal(data, &result) != nil || result.ID == "" || len(result.Message) > 1024 {
		h.fail(c, "动作回执无效")
		return
	}
	h.mu.Lock()
	pending, ok := h.actions[result.ID]
	if ok && pending.client == c {
		select {
		case pending.result <- result:
		default:
		}
	}
	h.mu.Unlock()
}
