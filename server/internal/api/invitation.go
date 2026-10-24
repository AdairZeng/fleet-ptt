package api

import (
	"net/http"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/user"
	"github.com/AdairZeng/fleet-ptt/server/internal/ws"
)

func registerInvitations(mux *http.ServeMux, users *user.Service, hub *ws.Hub) {
	mux.HandleFunc("GET /api/v1/online-members", func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		members, err := hub.OnlineMembers(r.Context(), u.ID)
		if err != nil {
			writeJSON(w, 500, errorBody("读取在线成员失败"))
			return
		}
		writeJSON(w, 200, map[string]any{"members": members})
	})
	mux.HandleFunc("GET /api/v1/invitations", func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		writeJSON(w, 200, map[string]any{"invitations": hub.Invitations(u.ID, time.Now())})
	})
	mux.HandleFunc("POST /api/v1/invitations", func(w http.ResponseWriter, r *http.Request) {
		u, ok := requireUser(users, w, r)
		if !ok {
			return
		}
		var req struct {
			Name          string `json:"name"`
			RecipientName string `json:"recipient_name"`
			RecipientID   string `json:"recipient_id"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Name == "" || (req.RecipientName == "") == (req.RecipientID == "") {
			writeJSON(w, 400, errorBody("必须提供频道 name 和 recipient_name 或 recipient_id 之一"))
			return
		}
		inv, err := hub.Invite(r.Context(), u, req.Name, req.RecipientName, req.RecipientID, time.Now())
		if err != nil {
			writeJSON(w, 409, errorBody(err.Error()))
			return
		}
		writeJSON(w, 200, inv)
	})
	for _, verb := range []string{"accept", "reject"} {
		mux.HandleFunc("POST /api/v1/invitations/{id}/"+verb, func(w http.ResponseWriter, r *http.Request) {
			u, ok := requireUser(users, w, r)
			if !ok {
				return
			}
			inv, err := hub.RespondInvitation(r.Context(), u, r.PathValue("id"), verb == "accept", time.Now())
			if err != nil {
				writeJSON(w, 409, errorBody(err.Error()))
				return
			}
			writeJSON(w, 200, inv)
		})
	}
}
