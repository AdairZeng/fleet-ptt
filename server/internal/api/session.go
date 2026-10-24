package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/linxbridge"
	"github.com/AdairZeng/fleet-ptt/server/internal/linxchat"
	"github.com/AdairZeng/fleet-ptt/server/internal/rtn"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
	"github.com/AdairZeng/fleet-ptt/server/internal/ws"
)

// Register 挂上会话、当前用户、频道、RTN RoomToken、WebSocket 信令，以及灵矽 Chat 回调。
// linxToken 是 LINX_CHAT_TOKEN。为空时回调一律拒绝。
func Register(mux *http.ServeMux, users *user.Service, channels *channel.Service, tokens rtn.Signer, linxToken string, bridges ...linxbridge.Config) {
	bridge := linxbridge.Config{}
	if len(bridges) > 0 {
		bridge = bridges[0]
	}
	hub := ws.NewHub(users, channels)
	gate := linxchat.NewJoinGate(linxchat.JoinConfirmTTL)
	mux.HandleFunc("POST /api/v1/session", CreateSession(users))
	mux.HandleFunc("POST /api/v1/device/register", RegisterDevice(users))
	mux.HandleFunc("POST /api/v1/device/session", CreateDeviceSession(users))
	mux.HandleFunc("GET /api/v1/me", Me(users, channels))
	mux.HandleFunc("GET /api/v1/channels", ListChannels(users, channels))
	mux.HandleFunc("POST /api/v1/channels", CreateChannel(users, hub))
	mux.HandleFunc("POST /api/v1/channels/join", JoinChannel(users, hub))
	mux.HandleFunc("POST /api/v1/channels/current", SwitchChannel(users, hub))
	mux.HandleFunc("GET /api/v1/channels/{id}/rtn-token", RoomToken(users, channels, tokens))
	mux.HandleFunc("POST /api/v1/talk/start", TalkStart(users, hub))
	mux.HandleFunc("POST /api/v1/talk/end", TalkEnd(users, hub))
	mux.HandleFunc("GET /api/v1/device/ws", hub.ServeHTTP)
	mux.Handle("GET /api/v1/linx/ws", linxbridge.Handler{Users: users, Config: bridge})
	mux.HandleFunc("POST /api/v1/linx/chat", LinxChat(users, channels, hub, gate, linxToken))
}

// CreateSession 接受 { nickname, device_id }，返回 user_id 与 session_token。
// CreateSession keeps the endpoint but requires the same device proof as device/session.
func CreateSession(svc *user.Service) http.HandlerFunc { return CreateDeviceSession(svc) }

func RegisterDevice(svc *user.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var owner *store.User
		if r.Header.Get("Authorization") != "" {
			u, ok := requireUser(svc, w, r)
			if !ok {
				return
			}
			owner = &u
		}
		credential, err := svc.RegisterDevice(r.Context(), owner, time.Now())
		if err != nil {
			writeEnterError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, credential)
	}
}

func CreateDeviceSession(svc *user.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Nickname   string `json:"nickname"`
			DeviceID   string `json:"device_id"`
			Credential string `json:"device_credential"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		sess, err := svc.DeviceSession(r.Context(), req.Nickname, req.DeviceID, req.Credential, time.Now())
		if err != nil {
			writeEnterError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"user_id": sess.User.ID, "session_token": sess.Token,
			"device_id": sess.User.DeviceID, "nickname": sess.User.DisplayName,
		})
	}
}

// Me 用 Bearer session_token 返回已绑定的用户。已有当前频道时一并返回。
func Me(users *user.Service, channels *channel.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		body := meBody{
			UserID:   u.ID,
			Nickname: u.DisplayName,
			DeviceID: u.DeviceID,
		}
		current, err := channels.Current(r.Context(), u.ID)
		if errors.Is(err, channel.ErrNoCurrent) {
			writeJSON(w, http.StatusOK, body)
			return
		}
		if err != nil {
			log.Printf("me: %v", err)
			writeJSON(w, http.StatusInternalServerError, errorBody("读取当前频道失败"))
			return
		}
		ch := channelBodyFrom(current)
		body.Channel = &ch
		writeJSON(w, http.StatusOK, body)
	}
}

func writeEnterError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrInvalidNickname), errors.Is(err, user.ErrInvalidDevice):
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
	case errors.Is(err, user.ErrNoSecret):
		log.Printf("session: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	case errors.Is(err, user.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, errorBody(err.Error()))
	case errors.Is(err, user.ErrDeviceBinding):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	default:
		log.Printf("session: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("进入失败"))
	}
}

func errorBody(message string) map[string]string {
	return map[string]string{"error": message}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}
