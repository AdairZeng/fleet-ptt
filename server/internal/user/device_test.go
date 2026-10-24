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
	credential, err := svc.RegisterDevice(ctx, nil, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeviceSession(ctx, "司机", credential.DeviceID, "", now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("missing proof: %v", err)
	}
	other, err := svc.RegisterDevice(ctx, nil, "", now)
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
	byMAC, err := st.UserByMacID(ctx, credential.MacID)
	if err != nil {
		t.Fatalf("mac mapping: %v", err)
	}
	if byMAC.ID != first.User.ID {
		t.Fatalf("mac mapping user %q, want %q", byMAC.ID, first.User.ID)
	}
}

func TestConfirmedNicknameRestoresWithDeviceProof(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "restore.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := &Service{Store: st, Secret: "restore-secret"}
	ctx := context.Background()
	now := time.Now()
	proof, err := svc.RegisterDevice(ctx, nil, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeviceSession(ctx, "", proof.DeviceID, proof.Credential, now); !errors.Is(err, ErrInvalidNickname) {
		t.Fatalf("unset nickname restore: %v", err)
	}
	saved, err := svc.DeviceSession(ctx, "老王", proof.DeviceID, proof.Credential, now)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := svc.DeviceSession(ctx, "", proof.DeviceID, proof.Credential, now)
	if err != nil {
		t.Fatal(err)
	}
	if restored.User.ID != saved.User.ID || restored.User.DisplayName != "老王" {
		t.Fatalf("restore changed identity: %+v", restored.User)
	}
}
