package linxchat

import (
	"testing"
	"time"
)

func TestJoinGateRequiresExplicitConfirmation(t *testing.T) {
	gate := NewJoinGate(JoinConfirmTTL)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	gate.Offer("u1", "384291", now)
	gate.Offer("u1", "3842K9", now.Add(time.Second))
	gate.Offer("u1", "3842K9", now.Add(2*time.Second))
	code, ok := gate.Confirm("u1", now.Add(3*time.Second))
	if !ok || code != "3842K9" {
		t.Fatalf("confirm %q ok %v", code, ok)
	}
	if _, ok := gate.Confirm("u1", now.Add(4*time.Second)); ok {
		t.Fatal("confirmation was reusable")
	}
	gate.Offer("u2", "384291", now)
	gate.Offer("u2", "384291", now.Add(time.Minute))
	if _, ok := gate.Confirm("u2", now.Add(JoinConfirmTTL)); ok {
		t.Fatal("duplicate extended confirmation window")
	}
}

func TestJoinGateExpiresAndCancel(t *testing.T) {
	gate := NewJoinGate(time.Minute)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	gate.Offer("u", "384291", now)
	gate.Offer("u", "384291", now.Add(time.Minute))
	if !gate.Cancel("u", now.Add(time.Minute)) {
		t.Fatal("cancel missed fresh code")
	}
	if gate.Cancel("u", now) {
		t.Fatal("cancel found nothing")
	}
	gate.Offer("u", "222222", now)
	gate.Clear("u")
	if _, ok := gate.Confirm("u", now); ok {
		t.Fatal("clear did not drop pending")
	}
}
