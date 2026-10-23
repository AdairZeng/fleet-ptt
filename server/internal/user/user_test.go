package user_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

func TestEnterBindsDeviceAndToken(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_100, 0).UTC()
	const device = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

	first, err := svc.Enter(ctx, "  老王  ", device, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.User.DisplayName != "老王" || first.User.DeviceID != device || first.Token == "" {
		t.Fatalf("session %+v", first)
	}

	authed, err := svc.Authenticate(ctx, first.Token)
	if err != nil {
		t.Fatal(err)
	}
	if authed.ID != first.User.ID {
		t.Fatalf("token user %s", authed.ID)
	}

	second, err := svc.Enter(ctx, "老王的车", device, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.User.ID != first.User.ID {
		t.Fatalf("user id changed: %s", second.User.ID)
	}
	if second.Token == first.Token {
		t.Fatal("expected a newly signed token")
	}
	still, err := svc.Authenticate(ctx, first.Token)
	if err != nil {
		t.Fatal(err)
	}
	if still.DisplayName != "老王的车" {
		t.Fatalf("old token did not see the updated nickname: %q", still.DisplayName)
	}

	byDevice, err := svc.UserByDeviceID(ctx, "  "+device+"  ")
	if err != nil {
		t.Fatal(err)
	}
	if byDevice.ID != first.User.ID || byDevice.DisplayName != "老王的车" {
		t.Fatalf("lookup %+v", byDevice)
	}

	if _, err := svc.Authenticate(ctx, first.Token+"x"); !errors.Is(err, user.ErrUnauthorized) {
		t.Fatalf("tampered token err %v", err)
	}
	other := &user.Service{Store: svc.Store, Secret: "other-secret"}
	if _, err := other.Authenticate(ctx, first.Token); !errors.Is(err, user.ErrUnauthorized) {
		t.Fatalf("wrong secret err %v", err)
	}
}

func TestEnterRejectsInput(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_100, 0)
	device := "device-1"

	cases := []struct {
		name     string
		nickname string
		device   string
		svc      *user.Service
		want     error
	}{
		{name: "empty nickname", nickname: "  ", device: device, svc: svc, want: user.ErrInvalidNickname},
		{name: "long nickname", nickname: strings.Repeat("名", 33), device: device, svc: svc, want: user.ErrInvalidNickname},
		{name: "control nickname", nickname: "a\nb", device: device, svc: svc, want: user.ErrInvalidNickname},
		{name: "empty device", nickname: "老王", device: " ", svc: svc, want: user.ErrInvalidDevice},
		{name: "device space", nickname: "老王", device: "has space", svc: svc, want: user.ErrInvalidDevice},
		{name: "no secret", nickname: "老王", device: device, svc: &user.Service{Store: svc.Store}, want: user.ErrNoSecret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.svc.Enter(ctx, tc.nickname, tc.device, now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err %v", err)
			}
		})
	}
}

func newService(t *testing.T) *user.Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &user.Service{Store: st, Secret: "test-secret"}
}
