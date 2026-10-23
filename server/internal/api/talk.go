package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
	"github.com/AdairZeng/fleet-ptt/server/internal/ws"
)

type talkBody struct {
	ChannelID string `json:"channel_id"`
	Talking   bool   `json:"talking"`
}

// TalkStart 把当前用户标为正在对讲，并广播 talk_start。需要 Bearer session。
func TalkStart(users *user.Service, hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		view, err := hub.StartTalk(r.Context(), u.ID)
		if err != nil {
			writeTalkError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, talkBody{ChannelID: view.Channel.ID, Talking: true})
	}
}

// TalkEnd 结束当前用户的对讲，并在状态有变化时广播 talk_end。
func TalkEnd(users *user.Service, hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		view, err := hub.EndTalk(r.Context(), u.ID)
		if err != nil {
			writeTalkError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, talkBody{ChannelID: view.Channel.ID, Talking: false})
	}
}

func writeTalkError(w http.ResponseWriter, err error) {
	if errors.Is(err, channel.ErrNoCurrent) {
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
		return
	}
	log.Printf("talk: %v", err)
	writeJSON(w, http.StatusInternalServerError, errorBody("更新对讲状态失败"))
}
