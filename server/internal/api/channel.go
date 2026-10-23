package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
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

// CreateChannel 接受 { name }，创建频道并返回口令。需要 Bearer session。
func CreateChannel(users *user.Service, channels *channel.Service) http.HandlerFunc {
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
		created, err := channels.Create(r.Context(), u.ID, req.Name, time.Now())
		if err != nil {
			writeChannelError(w, err, "创建频道失败")
			return
		}
		writeJSON(w, http.StatusOK, channelBodyFrom(created))
	}
}

// JoinChannel 接受 { code } 或 { name }。口令优先。加入后设为当前频道。
func JoinChannel(users *user.Service, channels *channel.Service) http.HandlerFunc {
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
		joined, err := channels.Join(r.Context(), u.ID, req.Code, req.Name)
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
	case errors.Is(err, channel.ErrInvalidName), errors.Is(err, channel.ErrInvalidJoin):
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
	case errors.Is(err, channel.ErrCodeNotFound), errors.Is(err, channel.ErrNameNotFound):
		writeJSON(w, http.StatusNotFound, errorBody(err.Error()))
	case errors.Is(err, channel.ErrAmbiguousName):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	default:
		log.Printf("channel: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody(fallback))
	}
}
