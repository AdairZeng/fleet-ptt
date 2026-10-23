package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/AdairZeng/fleet-ptt/server/internal/linxchat"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

const maxLinxSessionIDBytes = 128

type linxChatRequest struct {
	Auth struct {
		DeviceID string `json:"device_id"`
	} `json:"auth"`
	Type    string `json:"type"`
	Details struct {
		Msg       string `json:"msg"`
		SessionID string `json:"session_id"`
	} `json:"details"`
}

type linxSSEEvent struct {
	Event     string          `json:"event"`
	Type      string          `json:"type,omitempty"`
	SessionID string          `json:"session_id"`
	Details   *linxSSEDetails `json:"details,omitempty"`
}

type linxSSEDetails struct {
	Answer string `json:"answer"`
	Intent string `json:"intent"`
}

// LinxChat 处理 POST /api/v1/linx/chat。
// token 为 LINX_CHAT_TOKEN，对应灵矽请求头 Authorization: Bearer。
// 为空时拒绝全部回调。device_id 必须已绑定用户。意图只返回占位文案。
func LinxChat(users *user.Service, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			log.Print("linx chat: LINX_CHAT_TOKEN is empty")
			writeJSON(w, http.StatusInternalServerError, errorBody("服务器未配置 LINX_CHAT_TOKEN"))
			return
		}
		got, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok || !secureEqual(got, token) {
			writeJSON(w, http.StatusUnauthorized, errorBody("回调未授权"))
			return
		}
		var req linxChatRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Type != "chat" {
			writeJSON(w, http.StatusBadRequest, errorBody("仅支持 chat"))
			return
		}
		deviceID := strings.TrimSpace(req.Auth.DeviceID)
		msg := strings.TrimSpace(req.Details.Msg)
		sessionID := req.Details.SessionID
		if deviceID == "" {
			writeJSON(w, http.StatusBadRequest, errorBody("device_id 无效"))
			return
		}
		if msg == "" || strings.TrimSpace(sessionID) == "" || len(sessionID) > maxLinxSessionIDBytes {
			writeJSON(w, http.StatusBadRequest, errorBody("缺少话术或 session_id"))
			return
		}
		if _, err := users.UserByDeviceID(r.Context(), deviceID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, errorBody("设备未绑定用户"))
				return
			}
			log.Printf("linx chat: %v", err)
			writeJSON(w, http.StatusInternalServerError, errorBody("读取用户失败"))
			return
		}
		writeLinxSSE(w, sessionID, linxchat.Classify(msg))
	}
}

func writeLinxSSE(w http.ResponseWriter, sessionID string, result linxchat.Result) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	events := []linxSSEEvent{
		{
			Event:     "message",
			Type:      "chat",
			SessionID: sessionID,
			Details:   &linxSSEDetails{Answer: result.Answer, Intent: string(result.Intent)},
		},
		{
			Event:     "message_end",
			SessionID: sessionID,
		},
	}
	for _, ev := range events {
		payload, err := json.Marshal(ev)
		if err != nil {
			log.Printf("linx chat: %v", err)
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			log.Printf("linx chat: %v", err)
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func secureEqual(got, want string) bool {
	if want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
