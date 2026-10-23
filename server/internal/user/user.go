// Package user 处理昵称进入、device 绑定与 session_token。
package user

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

const (
	maxNicknameRunes = 32
	maxDeviceIDBytes = 128
)

var (
	// ErrInvalidNickname 表示昵称为空、超长或含控制字符。
	ErrInvalidNickname = errors.New("昵称需为 1～32 个字符")
	// ErrInvalidDevice 表示 device_id 为空、超长或含空白 / 非 ASCII。
	ErrInvalidDevice = errors.New("device_id 无效")
	// ErrUnauthorized 表示缺少或无法校验 session_token。
	ErrUnauthorized = errors.New("未登录或会话无效")
	// ErrNoSecret 表示进程没有 SESSION_SECRET，无法签发会话。
	ErrNoSecret = errors.New("服务器未配置 SESSION_SECRET")
)

// Service 签发会话并把昵称绑定到 device_id。
type Service struct {
	Store  *store.Store
	Secret string
}

// Session 是一次成功进入的结果。
type Session struct {
	User  store.User
	Token string
}

// Enter 校验昵称与 device_id，按设备 upsert 用户，并签发 session_token。
func (s *Service) Enter(ctx context.Context, nickname, deviceID string, now time.Time) (Session, error) {
	nickname = strings.TrimSpace(nickname)
	deviceID = strings.TrimSpace(deviceID)
	if !validNickname(nickname) {
		return Session{}, ErrInvalidNickname
	}
	if !validDeviceID(deviceID) {
		return Session{}, ErrInvalidDevice
	}
	if s.Secret == "" {
		return Session{}, ErrNoSecret
	}
	id, err := newID()
	if err != nil {
		return Session{}, err
	}
	u, err := s.Store.UpsertUser(ctx, id, nickname, deviceID, now.UTC())
	if err != nil {
		return Session{}, err
	}
	token, err := sign(s.Secret, u.ID, now.UTC())
	if err != nil {
		return Session{}, err
	}
	return Session{User: u, Token: token}, nil
}

// Authenticate 校验 Bearer token 并加载对应用户。
func (s *Service) Authenticate(ctx context.Context, token string) (store.User, error) {
	if s.Secret == "" {
		return store.User{}, ErrUnauthorized
	}
	userID, err := verify(s.Secret, token)
	if err != nil {
		return store.User{}, ErrUnauthorized
	}
	u, err := s.Store.UserByID(ctx, userID)
	if err != nil {
		return store.User{}, ErrUnauthorized
	}
	return u, nil
}

// UserByDeviceID 按 device_id 查用户。调用方传入的空白会被去掉。
func (s *Service) UserByDeviceID(ctx context.Context, deviceID string) (store.User, error) {
	return s.Store.UserByDeviceID(ctx, strings.TrimSpace(deviceID))
}

func validNickname(name string) bool {
	n := 0
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
		n++
	}
	return n >= 1 && n <= maxNicknameRunes
}

func validDeviceID(id string) bool {
	if id == "" || len(id) > maxDeviceIDBytes {
		return false
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
