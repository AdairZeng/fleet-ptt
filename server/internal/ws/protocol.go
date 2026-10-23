package ws

import (
	"bytes"
	"encoding/json"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

type talkWire struct {
	Type      string `json:"type"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
}

type memberWire struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Talking     bool   `json:"talking"`
}

type membersWire struct {
	Type      string       `json:"type"`
	ChannelID string       `json:"channel_id"`
	Members   []memberWire `json:"members"`
}

type channelCreatedWire struct {
	Type      string `json:"type"`
	ChannelID string `json:"channel_id"`
	Name      string `json:"name"`
	JoinCode  string `json:"join_code"`
}

type channelSwitchedWire struct {
	Type      string `json:"type"`
	ChannelID string `json:"channel_id"`
	Name      string `json:"name"`
}

type errorWire struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type pongWire struct {
	Type string `json:"type"`
}

func encode(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func wireMembers(members []store.Member) []memberWire {
	out := make([]memberWire, 0, len(members))
	for _, member := range members {
		out = append(out, memberWire{
			UserID:      member.UserID,
			DisplayName: member.DisplayName,
			Talking:     member.Talking,
		})
	}
	return out
}

func memberIDs(members []store.Member) []string {
	ids := make([]string, len(members))
	for i, member := range members {
		ids[i] = member.UserID
	}
	return ids
}
