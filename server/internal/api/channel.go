package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
	"github.com/AdairZeng/fleet-ptt/server/internal/ws"
)

type channelBody struct {
	ChannelID string `json:"channel_id"`
	Name      string `json:"name"`
	JoinCode  string `json:"join_code"`
	Role      string `json:"role"`
}

type meBody struct {
	UserID   string       `json:"user_id"`
	Nickname string       `json:"nickname"`
	DeviceID string       `json:"device_id"`
	Channel  *channelBody `json:"channel,omitempty"`
}

// ListChannels 返回当前用户已加入的频道。需要 Bearer session。
func ListChannels(users *user.Service, channels *channel.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		list, err := channels.List(r.Context(), u.ID)
		if err != nil {
			log.Printf("channel: %v", err)
			writeJSON(w, http.StatusInternalServerError, errorBody("读取频道列表失败"))
			return
		}
		body := make([]channelBody, 0, len(list))
		for _, view := range list {
			body = append(body, channelBodyFrom(view))
		}
		writeJSON(w, http.StatusOK, struct {
			Channels []channelBody `json:"channels"`
		}{Channels: body})
	}
}

// SwitchChannel 接受 { channel_id }，把已加入的频道设为当前频道。
// 若正在对讲，先结束上行再切换，并给该用户推 channel_switched。
func SwitchChannel(users *user.Service, hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		var req struct {
			ChannelID string `json:"channel_id"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		switched, err := hub.Switch(r.Context(), u.ID, req.ChannelID)
		if err != nil {
			writeChannelError(w, err, "切换频道失败")
			return
		}
		writeJSON(w, http.StatusOK, channelBodyFrom(switched))
	}
}

// CreateChannel 接受 { name }，创建频道并返回口令。需要 Bearer session。
// 创建者的 WebSocket 会收到 channel_created；若此前正在对讲，旧频道收到 talk_end。
func CreateChannel(users *user.Service, hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		created, err := hub.Create(r.Context(), u.ID, req.Name, time.Now())
		if err != nil {
			writeChannelError(w, err, "创建频道失败")
			return
		}
		writeJSON(w, http.StatusOK, channelBodyFrom(created))
	}
}

// JoinChannel 接受 { code } 或 { name }。口令优先。加入后设为当前频道。
// 频道变化时给该用户推 channel_switched，包括第一次进房；若离开的频道上正在对讲，先广播 talk_end。
func JoinChannel(users *user.Service, hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		var req struct {
			Code string `json:"code"`
			Name string `json:"name"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		joined, err := hub.Join(r.Context(), u.ID, req.Code, req.Name)
		if err != nil {
			writeChannelError(w, err, "加入频道失败")
			return
		}
		writeJSON(w, http.StatusOK, channelBodyFrom(joined))
	}
}

func channelBodyFrom(view channel.View) channelBody {
	return channelBody{
		ChannelID: view.Channel.ID,
		Name:      view.Channel.Name,
		JoinCode:  view.Channel.JoinCode,
		Role:      view.Role,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	if err := dec.Decode(dest); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("请求格式无效"))
		return false
	}
	return true
}

func writeChannelError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, channel.ErrInvalidName), errors.Is(err, channel.ErrInvalidJoin), errors.Is(err, channel.ErrInvalidSwitch):
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
	case errors.Is(err, channel.ErrCodeNotFound), errors.Is(err, channel.ErrNameNotFound):
		writeJSON(w, http.StatusNotFound, errorBody(err.Error()))
	case errors.Is(err, channel.ErrNotMember):
		writeJSON(w, http.StatusForbidden, errorBody(err.Error()))
	case errors.Is(err, channel.ErrAmbiguousName):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	default:
		log.Printf("channel: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody(fallback))
	}
}
