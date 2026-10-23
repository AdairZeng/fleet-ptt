package channel

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

func TestJoinCodeAlphabet(t *testing.T) {
	if strings.ContainsAny(joinCodeAlphabet, "01OI") {
		t.Fatalf("alphabet %q contains a lookalike", joinCodeAlphabet)
	}
	if len(joinCodeAlphabet) != 32 {
		t.Fatalf("alphabet len %d", len(joinCodeAlphabet))
	}
	seen := map[string]bool{}
	for range 200 {
		code, err := newJoinCode()
		if err != nil {
			t.Fatal(err)
		}
		if !validCode(code) {
			t.Fatalf("code %q", code)
		}
		seen[code] = true
	}
	if len(seen) < 190 {
		t.Fatalf("codes look stuck: %d unique", len(seen))
	}
}

func TestCreateAndJoin(t *testing.T) {
	svc := newChannelService(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_300, 0).UTC()

	created, err := svc.Create(ctx, "captain", "  装货  ", now)
	if err != nil {
		t.Fatal(err)
	}
	if created.Channel.Name != "装货" || created.Role != RoleOwner || !validCode(created.Channel.JoinCode) {
		t.Fatalf("created %+v", created)
	}
	if !created.Channel.CreatedAt.Equal(now) {
		t.Fatalf("created_at %s", created.Channel.CreatedAt)
	}
	current, err := svc.Current(ctx, "captain")
	if err != nil {
		t.Fatal(err)
	}
	if current.Channel.ID != created.Channel.ID || current.Role != RoleOwner {
		t.Fatalf("current %+v", current)
	}

	code := created.Channel.JoinCode
	spaced := strings.ToLower(code[:3] + " " + code[3:])
	joined, err := svc.Join(ctx, "driver", spaced, "")
	if err != nil {
		t.Fatal(err)
	}
	if joined.Channel.ID != created.Channel.ID || joined.Channel.JoinCode != code || joined.Role != RoleMember {
		t.Fatalf("joined %+v", joined)
	}

	again, err := svc.Join(ctx, "captain", code, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.Role != RoleOwner {
		t.Fatalf("owner role %s", again.Role)
	}

	if _, err := svc.Join(ctx, "driver", "000000", ""); !errors.Is(err, ErrCodeNotFound) {
		t.Fatalf("bad code err %v", err)
	}
	if _, err := svc.Join(ctx, "other", "", ""); !errors.Is(err, ErrInvalidJoin) {
		t.Fatalf("empty join err %v", err)
	}
	if _, err := svc.Create(ctx, "captain", " \n ", now); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("blank name err %v", err)
	}
	if _, err := svc.Create(ctx, "captain", strings.Repeat("装", 33), now); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("long name err %v", err)
	}

	if _, err := svc.Create(ctx, "captain", "装货", now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Join(ctx, "driver", "", "装货"); !errors.Is(err, ErrAmbiguousName) {
		t.Fatalf("ambiguous err %v", err)
	}
	current, err = svc.Current(ctx, "driver")
	if err != nil {
		t.Fatal(err)
	}
	if current.Channel.ID != created.Channel.ID {
		t.Fatalf("ambiguous join switched channel to %s", current.Channel.ID)
	}

	unloading, err := svc.Create(ctx, "captain", "卸货", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Join(ctx, "driver", "000000", "卸货"); !errors.Is(err, ErrCodeNotFound) {
		t.Fatalf("code should win over name: %v", err)
	}
	byName, err := svc.Join(ctx, "driver", "", "  卸货 ")
	if err != nil {
		t.Fatal(err)
	}
	if byName.Channel.ID != unloading.Channel.ID || byName.Role != RoleMember {
		t.Fatalf("by name %+v", byName)
	}
	if _, err := svc.Join(ctx, "driver", "", "不存在"); !errors.Is(err, ErrNameNotFound) {
		t.Fatalf("missing name err %v", err)
	}
	if _, err := svc.Current(ctx, "nobody"); !errors.Is(err, ErrNoCurrent) {
		t.Fatalf("no current err %v", err)
	}
}

func TestRequireMember(t *testing.T) {
	svc := newChannelService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, "captain", "装货", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.RequireMember(ctx, "captain", created.Channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != RoleOwner || got.Channel.ID != created.Channel.ID {
		t.Fatalf("owner %+v", got)
	}
	if _, err := svc.RequireMember(ctx, "driver", created.Channel.ID); !errors.Is(err, ErrNotMember) {
		t.Fatalf("non-member err %v", err)
	}
	if _, err := svc.RequireMember(ctx, "captain", "missing-channel"); !errors.Is(err, ErrNameNotFound) {
		t.Fatalf("missing channel err %v", err)
	}
}

func newChannelService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Service{Store: st}
}
