package channel

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

const (
	// RoleOwner 是创建频道的人。
	RoleOwner = "owner"
	// RoleMember 是后来用口令或频道名加入的人。
	RoleMember = "member"

	maxNameRunes = 32
)

var (
	// ErrInvalidName 表示频道名为空、超长或含控制字符。
	ErrInvalidName = errors.New("频道名需为 1～32 个字符")
	// ErrInvalidJoin 表示口令和频道名都没填。
	ErrInvalidJoin = errors.New("请填写口令或频道名")
	// ErrCodeNotFound 表示口令不存在，或不是 6 位可读口令。
	ErrCodeNotFound = errors.New("口令不正确")
	// ErrNameNotFound 表示没有这个频道名。
	ErrNameNotFound = errors.New("没有找到这个频道")
	// ErrAmbiguousName 表示同名频道不止一个，必须改用口令。
	ErrAmbiguousName = errors.New("有多个同名频道，请改用口令加入")
	// ErrNoCurrent 表示这个用户还没有当前频道。
	ErrNoCurrent = errors.New("没有当前频道")
	// ErrNotMember 表示用户不在这个频道里，不能拿进房凭证。
	ErrNotMember = errors.New("还不是这个频道的成员")
)

// View 是一次创建或加入的结果，Role 是该用户在频道里的角色。
type View struct {
	Channel store.Channel
	Role    string
}

// Service 创建频道并维护成员与当前频道。
type Service struct {
	Store *store.Store
}

// Create 以 name 建频道，生成口令，创建者为 owner，并设为当前频道。
func (s *Service) Create(ctx context.Context, userID, name string, now time.Time) (View, error) {
	name = strings.TrimSpace(name)
	if !validName(name) {
		return View{}, ErrInvalidName
	}
	now = now.UTC()
	var last error
	for range maxCodeAttempts {
		id, err := newID()
		if err != nil {
			return View{}, err
		}
		code, err := newJoinCode()
		if err != nil {
			return View{}, err
		}
		ch := store.Channel{
			ID:        id,
			Name:      name,
			JoinCode:  code,
			CreatedBy: userID,
			CreatedAt: now,
		}
		err = s.Store.CreateChannel(ctx, ch, RoleOwner)
		if errors.Is(err, store.ErrJoinCodeTaken) {
			last = err
			continue
		}
		if err != nil {
			return View{}, err
		}
		return View{Channel: ch, Role: RoleOwner}, nil
	}
	if last == nil {
		last = errors.New("join code exhausted")
	}
	return View{}, last
}

// Join 按口令或频道名加入，并设为当前频道。
// 口令和频道名都有内容时以口令为准。再次加入不会把 owner 降成 member。
func (s *Service) Join(ctx context.Context, userID, code, name string) (View, error) {
	code = normalizeCode(code)
	name = strings.TrimSpace(name)
	if code != "" {
		return s.joinByCode(ctx, userID, code)
	}
	if name != "" {
		return s.joinByName(ctx, userID, name)
	}
	return View{}, ErrInvalidJoin
}

// RequireMember 确认 userID 是 channelID 的成员。频道不存在与不是成员分开返回。
func (s *Service) RequireMember(ctx context.Context, userID, channelID string) (View, error) {
	ch, err := s.Store.ChannelByID(ctx, channelID)
	if errors.Is(err, store.ErrNotFound) {
		return View{}, ErrNameNotFound
	}
	if err != nil {
		return View{}, err
	}
	role, err := s.Store.MemberRole(ctx, userID, channelID)
	if errors.Is(err, store.ErrNotFound) {
		return View{}, ErrNotMember
	}
	if err != nil {
		return View{}, err
	}
	return View{Channel: ch, Role: role}, nil
}

// Current 返回该用户的当前频道。还没有时返回 ErrNoCurrent。
func (s *Service) Current(ctx context.Context, userID string) (View, error) {
	ch, role, err := s.Store.CurrentChannel(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return View{}, ErrNoCurrent
	}
	if err != nil {
		return View{}, err
	}
	return View{Channel: ch, Role: role}, nil
}

func (s *Service) joinByCode(ctx context.Context, userID, code string) (View, error) {
	if !validCode(code) {
		return View{}, ErrCodeNotFound
	}
	ch, err := s.Store.ChannelByJoinCode(ctx, code)
	if errors.Is(err, store.ErrNotFound) {
		return View{}, ErrCodeNotFound
	}
	if err != nil {
		return View{}, err
	}
	return s.joinExisting(ctx, userID, ch)
}

func (s *Service) joinByName(ctx context.Context, userID, name string) (View, error) {
	if !validName(name) {
		return View{}, ErrInvalidName
	}
	list, err := s.Store.ChannelsByName(ctx, name)
	if err != nil {
		return View{}, err
	}
	switch len(list) {
	case 0:
		return View{}, ErrNameNotFound
	case 1:
		return s.joinExisting(ctx, userID, list[0])
	default:
		return View{}, ErrAmbiguousName
	}
}

func (s *Service) joinExisting(ctx context.Context, userID string, ch store.Channel) (View, error) {
	role, err := s.Store.JoinChannel(ctx, userID, ch.ID, RoleMember)
	if err != nil {
		return View{}, err
	}
	return View{Channel: ch, Role: role}, nil
}

func validName(name string) bool {
	n := 0
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
		n++
	}
	return n >= 1 && n <= maxNameRunes
}
