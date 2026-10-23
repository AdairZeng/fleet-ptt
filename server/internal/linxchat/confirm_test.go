package linxchat

import (
	"testing"
	"time"
)

func TestJoinGateRepeatsCodeBeforeExecute(t *testing.T) {
	gate := NewJoinGate(JoinConfirmTTL)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	if gate.Offer("u1", "384291", now) {
		t.Fatal("first offer executed")
	}
	if gate.Offer("u1", "3842K9", now.Add(time.Second)) {
		t.Fatal("different code executed")
	}
	if !gate.Offer("u1", "3842K9", now.Add(2*time.Second)) {
		t.Fatal("same code did not execute")
	}
	if _, ok := gate.Confirm("u1", now.Add(3*time.Second)); ok {
		t.Fatal("pending survived execute")
	}
	if gate.Offer("u2", "384291", now) {
		t.Fatal("other user shared pending")
	}
	code, ok := gate.Confirm("u2", now.Add(time.Second))
	if !ok || code != "384291" {
		t.Fatalf("confirm %q ok %v", code, ok)
	}
}

func TestJoinGateExpiresAndCancel(t *testing.T) {
	gate := NewJoinGate(time.Minute)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	if gate.Offer("u", "384291", now) {
		t.Fatal("first offer executed")
	}
	if gate.Offer("u", "384291", now.Add(time.Minute)) {
		t.Fatal("expired code executed")
	}
	if !gate.Cancel("u", now.Add(time.Minute)) {
		t.Fatal("cancel missed fresh code")
	}
	if gate.Cancel("u", now) {
		t.Fatal("cancel found nothing")
	}
	if gate.Offer("u", "222222", now) {
		t.Fatal("offer after cancel executed")
	}
	gate.Clear("u")
	if _, ok := gate.Confirm("u", now); ok {
		t.Fatal("clear did not drop pending")
	}
}
