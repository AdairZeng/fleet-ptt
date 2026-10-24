package ws

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func invitationHub(t *testing.T) (*Hub, []store.User) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := NewHub(&user.Service{Store: st}, &channel.Service{Store: st})
	h.SetInvitationNotifier(func(context.Context, string, string) error { return nil })
	var users []store.User
	for i, name := range []string{"小李", "老王", "路人"} {
		id := []string{"a", "b", "c"}[i]
		u, err := st.UpsertUser(context.Background(), id, name, "device-"+id, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, u)
		if _, err := h.BootPublic(context.Background(), u.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return h, users
}
func connectInviteDevice(t *testing.T, h *Hub, u store.User) {
	t.Helper()
	c := &client{userID: u.ID, deviceID: u.DeviceID, send: make(chan []byte, 64)}
	h.add(c)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for data := range c.send {
			var a Action
			if json.Unmarshal(data, &a) == nil && a.Type == "action" {
				payload, _ := json.Marshal(actionResult{Type: "action_result", ID: a.ID, Success: true})
				h.actionResult(c, payload)
			}
		}
	}()
	t.Cleanup(func() { h.mu.Lock(); h.remove(c); h.mu.Unlock(); c.closeSend(); <-done })
}
func TestInvitationConsentIsolationAndIdempotence(t *testing.T) {
	h, users := invitationHub(t)
	sender, target, stranger := users[0], users[1], users[2]
	connectInviteDevice(t, h, sender)
	connectInviteDevice(t, h, target)
	now := time.Now()
	ctx := context.Background()
	notified := 0
	h.SetInvitationNotifier(func(context.Context, string, string) error { notified++; return nil })
	inv, err := h.Invite(ctx, sender, "装货", "老王", "", now)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := h.channels.Current(ctx, target.ID)
	if current.Channel.ID != "public" {
		t.Fatal("recipient switched before consent")
	}
	if _, err = h.channels.RequireMember(ctx, target.ID, inv.ChannelID); !errors.Is(err, channel.ErrNotMember) {
		t.Fatal("recipient joined before consent")
	}
	again, err := h.Invite(ctx, sender, "装货", "", target.ID, now)
	if err != nil || again.ID != inv.ID || notified != 1 {
		t.Fatalf("duplicate %+v %v notices=%d", again, err, notified)
	}
	if len(h.Invitations(stranger.ID, now)) != 0 {
		t.Fatal("leaked invitation")
	}
	if _, err = h.RespondInvitation(ctx, stranger, inv.ID, true, now); err == nil {
		t.Fatal("stranger accepted")
	}
	result, err := h.RespondInvitation(ctx, target, inv.ID, true, now)
	if err != nil || result.Status != "accepted" {
		t.Fatalf("accept: %+v %v", result, err)
	}
	current, _ = h.channels.Current(ctx, target.ID)
	if current.Channel.ID != inv.ChannelID {
		t.Fatal("did not switch after accepting")
	}
	if _, err = h.RespondInvitation(ctx, target, inv.ID, true, now); err != nil {
		t.Fatal("repeated acceptance failed", err)
	}
	if _, err = h.RespondInvitation(ctx, target, inv.ID, false, now); err == nil {
		t.Fatal("accepted invitation later rejected")
	}
}
func TestInvitationOfflineAmbiguityAndExpiry(t *testing.T) {
	h, users := invitationHub(t)
	ctx := context.Background()
	now := time.Now()
	sender, target, other := users[0], users[1], users[2]
	connectInviteDevice(t, h, sender)
	if _, err := h.Invite(ctx, sender, "装货", "老王", "", now); err == nil {
		t.Fatal("offline invitation accepted")
	}
	list, _ := h.channels.List(ctx, sender.ID)
	if len(list) != 1 {
		t.Fatal("offline invite created channel")
	}
	connectInviteDevice(t, h, target)
	connectInviteDevice(t, h, other)
	if err := h.channels.Store.RenameUser(ctx, other.ID, "老王"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Invite(ctx, sender, "装货", "老王", "", now); err == nil || !strings.Contains(err.Error(), "同名") {
		t.Fatal("ambiguous recipient not rejected", err)
	}
	inv, err := h.Invite(ctx, sender, "装货", "", target.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.Invite(ctx, sender, "卸货", "", target.ID, now); err == nil {
		t.Fatal("replaced pending invite")
	}
	if _, err = h.RespondInvitation(ctx, target, inv.ID, true, now.Add(InvitationTTL)); err == nil {
		t.Fatal("expired invite accepted")
	}
	current, _ := h.channels.Current(ctx, target.ID)
	if current.Channel.ID != "public" {
		t.Fatal("expiry changed channel")
	}
	inv, err = h.Invite(ctx, sender, "装货", "", target.ID, now.Add(InvitationTTL))
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.RespondInvitation(ctx, target, inv.ID, false, now.Add(InvitationTTL))
	if err != nil || result.Status != "rejected" {
		t.Fatal("rejection failed", err)
	}
	current, _ = h.channels.Current(ctx, target.ID)
	if current.Channel.ID != "public" {
		t.Fatal("rejection changed channel")
	}
}
func TestInvitationAnnouncementFailureAndDirectory(t *testing.T) {
	h, users := invitationHub(t)
	ctx := context.Background()
	connectInviteDevice(t, h, users[0])
	connectInviteDevice(t, h, users[1])
	dir, err := h.OnlineMembers(ctx, users[0].ID)
	if err != nil || len(dir) != 1 || dir[0].UserID != users[1].ID {
		t.Fatalf("directory %+v %v", dir, err)
	}
	h.SetInvitationNotifier(func(context.Context, string, string) error { return errors.New("voice not connected") })
	if _, err = h.Invite(ctx, users[0], "装货", "", users[1].ID, time.Now()); err == nil {
		t.Fatal("failed announcement reported delivered")
	}
	if len(h.Invitations(users[1].ID, time.Now())) != 0 {
		t.Fatal("failed announcement left pending invite")
	}
}

func TestInvitationDisconnectDropsPending(t *testing.T) {
	h, users := invitationHub(t)
	ctx := context.Background()
	connectInviteDevice(t, h, users[0])
	connectInviteDevice(t, h, users[1])
	inv, err := h.Invite(ctx, users[0], "装货", "", users[1].ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	c := h.devices[users[1].DeviceID]
	h.remove(c)
	h.mu.Unlock()
	if len(h.Invitations(users[1].ID, time.Now())) != 0 {
		t.Fatal("offline invite retained")
	}
	if _, err = h.RespondInvitation(ctx, users[1], inv.ID, true, time.Now()); err == nil {
		t.Fatal("offline invitation accepted")
	}
}
