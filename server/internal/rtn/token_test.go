package rtn

import (
	"strings"
	"testing"
	"time"
)

func TestSignMatchesQiniuRoomToken(t *testing.T) {
	signer := Signer{
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "SECRETEXAMPLE",
		AppID:     "app-example",
	}
	now := time.Unix(1_700_000_000, 0).Add(-TokenTTL).UTC()
	got, err := signer.Sign("room-name", "user-id", now)
	if err != nil {
		t.Fatal(err)
	}
	const want = "AKIDEXAMPLE:vAXr3VCJkG4h38dNcqv_CdZ3P2s=:eyJhcHBJZCI6ImFwcC1leGFtcGxlIiwicm9vbU5hbWUiOiJyb29tLW5hbWUiLCJ1c2VySWQiOiJ1c2VyLWlkIiwiZXhwaXJlQXQiOjE3MDAwMDAwMDAsInBlcm1pc3Npb24iOiJ1c2VyIn0="
	if got.Value != want {
		t.Fatalf("token\n got %s\nwant %s", got.Value, want)
	}
	if strings.Contains(got.Value, signer.SecretKey) {
		t.Fatal("secret key leaked into token")
	}
	if got.ExpireAt != 1_700_000_000 || got.RoomName != "room-name" || got.UserID != "user-id" || got.AppID != "app-example" {
		t.Fatalf("token meta %+v", got)
	}
}

func TestSignRejectsBadInput(t *testing.T) {
	ok := Signer{AccessKey: "ak", SecretKey: "sk", AppID: "app"}
	if _, err := (Signer{}).Sign("room-name", "user-id", time.Unix(0, 0)); err != ErrNotConfigured {
		t.Fatalf("empty signer err %v", err)
	}
	if _, err := (Signer{AccessKey: "ak", SecretKey: "sk"}).Sign("room-name", "user-id", time.Unix(0, 0)); err != ErrNotConfigured {
		t.Fatalf("missing app err %v", err)
	}
	if _, err := ok.Sign("ab", "user-id", time.Unix(0, 0)); err != ErrInvalidRoom {
		t.Fatalf("short room err %v", err)
	}
	if _, err := ok.Sign("bad room", "user-id", time.Unix(0, 0)); err != ErrInvalidRoom {
		t.Fatalf("space room err %v", err)
	}
	if _, err := ok.Sign("room-name", "x", time.Unix(0, 0)); err != ErrInvalidUser {
		t.Fatalf("short user err %v", err)
	}
	if _, err := ok.Sign("room-name", "bad user", time.Unix(0, 0)); err != ErrInvalidUser {
		t.Fatalf("space user err %v", err)
	}
}

func TestSignRejectsExampleConfiguration(t *testing.T) {
	for _, signer := range []Signer{
		{AccessKey: "your_qiniu_access_key_here", SecretKey: "sk", AppID: "app"},
		{AccessKey: "ak", SecretKey: "your_qiniu_secret_key_here", AppID: "app"},
		{AccessKey: "ak", SecretKey: "sk", AppID: "your_rtn_app_id_here"},
		{AccessKey: " ", SecretKey: "sk", AppID: "app"},
	} {
		if _, err := signer.Sign("public", "user-id", time.Now()); err != ErrNotConfigured {
			t.Fatalf("expected configuration error, got %v", err)
		}
	}
}
