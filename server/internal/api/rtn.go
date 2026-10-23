package api

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/rtn"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

type roomTokenBody struct {
	Token    string `json:"token"`
	AppID    string `json:"app_id"`
	RoomName string `json:"room_name"`
	UserID   string `json:"user_id"`
	ExpireAt int64  `json:"expire_at"`
}

// RoomToken 为当前登录用户签发加入该频道的七牛 RoomToken。
// userId 固定为会话里的用户，roomName 固定为频道 id；不是成员不能拿凭证。
func RoomToken(users *user.Service, channels *channel.Service, signer rtn.Signer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		view, err := channels.RequireMember(r.Context(), u.ID, r.PathValue("id"))
		if err != nil {
			writeRoomTokenError(w, err)
			return
		}
		issued, err := signer.Sign(view.Channel.ID, u.ID, time.Now())
		if err != nil {
			writeRoomTokenError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, roomTokenBody{
			Token:    issued.Value,
			AppID:    issued.AppID,
			RoomName: issued.RoomName,
			UserID:   issued.UserID,
			ExpireAt: issued.ExpireAt,
		})
	}
}

func writeRoomTokenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, channel.ErrNameNotFound):
		writeJSON(w, http.StatusNotFound, errorBody(err.Error()))
	case errors.Is(err, channel.ErrNotMember):
		writeJSON(w, http.StatusForbidden, errorBody(err.Error()))
	case errors.Is(err, rtn.ErrNotConfigured):
		log.Printf("rtn: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		log.Printf("rtn: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("签发 RoomToken 失败"))
	}
}
