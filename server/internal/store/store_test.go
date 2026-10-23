package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

func TestUpsertBindsDeviceAndLookup(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	created := time.Unix(1_700_000_000, 0).UTC()
	const device = "11111111-1111-4111-8111-111111111111"

	first, err := st.UpsertUser(ctx, "user-first", "王师傅", device, created)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "user-first" || first.DisplayName != "王师傅" || first.DeviceID != device {
		t.Fatalf("first user %+v", first)
	}
	if !first.CreatedAt.Equal(created) {
		t.Fatalf("created_at %s", first.CreatedAt)
	}

	second, err := st.UpsertUser(ctx, "user-second", "王师傅-改名", device, created.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("device rebound to a new user: %s", second.ID)
	}
	if second.DisplayName != "王师傅-改名" {
		t.Fatalf("nickname %q", second.DisplayName)
	}
	if !second.CreatedAt.Equal(created) {
		t.Fatalf("created_at changed: %s", second.CreatedAt)
	}

	got, err := st.UserByDeviceID(ctx, device)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID || got.DisplayName != "王师傅-改名" {
		t.Fatalf("lookup %+v", got)
	}

	other, err := st.UpsertUser(ctx, "user-other", "王师傅-改名", "22222222-2222-4222-8222-222222222222", created)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == first.ID {
		t.Fatal("same nickname on another device reused the user")
	}

	_, err = st.UserByDeviceID(ctx, "missing-device")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing device err %v", err)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
