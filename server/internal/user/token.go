package user

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// session_token 是 HMAC-SHA256 签名的无状态令牌，密钥为 SESSION_SECRET。
// 载荷只有 user_id 与签发时间，不另建会话表；进程重启后仍可校验。
// MVP 不设过期（口令过期是产品 P1）。同一设备再次进入会签发新 token，
// 旧 token 在该用户仍存在时继续有效。后续 REST 用 Authorization: Bearer。

type claims struct {
	UserID string `json:"uid"`
	IAT    int64  `json:"iat"`
}

func sign(secret, userID string, now time.Time) (string, error) {
	raw, err := json.Marshal(claims{UserID: userID, IAT: now.Unix()})
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig, nil
}

func verify(secret, token string) (string, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" || sig == "" || strings.Contains(sig, ".") {
		return "", ErrUnauthorized
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return "", ErrUnauthorized
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", ErrUnauthorized
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil || c.UserID == "" {
		return "", ErrUnauthorized
	}
	return c.UserID, nil
}
