package api

import (
	"net/http"
	"strings"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

// UserFromRequest 读取 Authorization: Bearer <session_token> 并解析用户。
// 后续需要登录的 REST 都走这里。WebSocket 若用 query token，不经过本函数。
func UserFromRequest(svc *user.Service, r *http.Request) (store.User, error) {
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		return store.User{}, user.ErrUnauthorized
	}
	return svc.Authenticate(r.Context(), token)
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, " ") {
		return "", false
	}
	return token, true
}
