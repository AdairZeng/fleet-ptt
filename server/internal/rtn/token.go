package rtn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

const (
	// TokenTTL 是 RoomToken 有效期。短时签发，避免把长期凭证交给浏览器。
	TokenTTL = time.Hour
	// permissionUser 只能进房收听和推自己的音频，不能踢人。
	permissionUser = "user"
)

var (
	// ErrNotConfigured 表示 QINIU_ACCESS_KEY / QINIU_SECRET_KEY / QINIU_RTN_APP_ID 没有配齐。
	ErrNotConfigured = errors.New("服务器未配置七牛 RTN")
	// ErrInvalidRoom 表示 roomName 不满足七牛规则 ^[a-zA-Z0-9_-]{3,64}$。
	ErrInvalidRoom = errors.New("频道标识不能作为 RTN 房间名")
	// ErrInvalidUser 表示 userId 不满足七牛规则 ^[a-zA-Z0-9_-]{3,50}$。
	ErrInvalidUser = errors.New("用户标识不能作为 RTN 用户名")

	// 七牛文档：roomName ^[a-zA-Z0-9_-]{3,64}$，userId ^[a-zA-Z0-9_-]{3,50}$。
	roomNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,64}$`)
	userIDPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,50}$`)
)

// Signer 持有签发 RoomToken 的七牛凭证。零值表示未配置。
type Signer struct {
	AccessKey string
	SecretKey string
	AppID     string
}

// Token 是发给浏览器的进房凭证。SecretKey 不会出现在这里。
type Token struct {
	Value    string
	AppID    string
	RoomName string
	UserID   string
	ExpireAt int64
}

// roomAccess 的字段顺序要和七牛服务端 SDK 一致，签名覆盖的是这份 JSON。
type roomAccess struct {
	AppID      string `json:"appId"`
	RoomName   string `json:"roomName"`
	UserID     string `json:"userId"`
	ExpireAt   int64  `json:"expireAt"`
	Permission string `json:"permission"`
}

// Configured 表示三项凭证都非空。占位符不会在这里被识别，调用七牛时会失败。
func (s Signer) Configured() bool {
	return s.AccessKey != "" && s.SecretKey != "" && s.AppID != ""
}

// Sign 为 roomName（频道 id）和 userID 签发 permission=user 的 RoomToken。
// 算法与七牛 Go SDK GetRoomToken 相同：URL-safe Base64(JSON) 的 HMAC-SHA1。
func (s Signer) Sign(roomName, userID string, now time.Time) (Token, error) {
	if !s.Configured() {
		return Token{}, ErrNotConfigured
	}
	if !roomNamePattern.MatchString(roomName) {
		return Token{}, ErrInvalidRoom
	}
	if !userIDPattern.MatchString(userID) {
		return Token{}, ErrInvalidUser
	}
	expireAt := now.UTC().Add(TokenTTL).Unix()
	body, err := json.Marshal(roomAccess{
		AppID:      s.AppID,
		RoomName:   roomName,
		UserID:     userID,
		ExpireAt:   expireAt,
		Permission: permissionUser,
	})
	if err != nil {
		return Token{}, err
	}
	encoded := make([]byte, base64.URLEncoding.EncodedLen(len(body)))
	base64.URLEncoding.Encode(encoded, body)
	mac := hmac.New(sha1.New, []byte(s.SecretKey))
	mac.Write(encoded)
	token := s.AccessKey + ":" + base64.URLEncoding.EncodeToString(mac.Sum(nil)) + ":" + string(encoded)
	return Token{
		Value:    token,
		AppID:    s.AppID,
		RoomName: roomName,
		UserID:   userID,
		ExpireAt: expireAt,
	}, nil
}
