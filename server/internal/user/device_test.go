package user

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

func TestDeviceCredentialRequiredAndNicknameStable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "device.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := &Service{Store: st, Secret: "test-device-secret"}
	ctx, now := context.Background(), time.Now()
	credential, err := svc.RegisterDevice(ctx, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeviceSession(ctx, "司机", credential.DeviceID, "", now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("missing proof: %v", err)
	}
	other, err := svc.RegisterDevice(ctx, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeviceSession(ctx, "司机", credential.DeviceID, other.Credential, now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong proof: %v", err)
	}
	first, err := svc.DeviceSession(ctx, "司机", credential.DeviceID, credential.Credential, now)
	if err != nil {
		t.Fatal(err)
	}
	next, err := svc.DeviceSession(ctx, "改名", credential.DeviceID, credential.Credential, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.User.ID != next.User.ID || next.User.DisplayName != "改名" {
		t.Fatal("nickname changed identity")
	}
	saved, err := st.DeviceByID(ctx, credential.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CredentialHash == credential.Credential {
		t.Fatal("stored plaintext credential")
	}
	if _, err := st.UserByLinxDeviceID(ctx, credential.DeviceID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("implicit linx mapping: %v", err)
	}
}
