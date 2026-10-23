package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

func TestChannelMembersTalkingFollowsCurrentChannel(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_400, 0).UTC()

	captain, err := st.UpsertUser(ctx, "captain", "队长", "device-captain", now)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := st.UpsertUser(ctx, "driver", "司机", "device-driver", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.IsTalking(ctx, captain.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("talking before session: %v", err)
	}
	if err := st.SetTalking(ctx, captain.ID, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("set talking before session: %v", err)
	}

	loading := store.Channel{
		ID: "ch-load", Name: "装货", JoinCode: "3842K9",
		CreatedBy: captain.ID, CreatedAt: now,
	}
	if err := st.CreateChannel(ctx, loading, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.JoinChannel(ctx, driver.ID, loading.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTalking(ctx, captain.ID, true); err != nil {
		t.Fatal(err)
	}
	talking, err := st.IsTalking(ctx, captain.ID)
	if err != nil || !talking {
		t.Fatalf("talking %v err %v", talking, err)
	}

	members, err := st.ChannelMembers(ctx, loading.ID)
	if err != nil {
		t.Fatal(err)
	}
	talkingByUser := map[string]bool{}
	for _, member := range members {
		talkingByUser[member.UserID] = member.Talking
	}
	if len(members) != 2 || !talkingByUser[captain.ID] || talkingByUser[driver.ID] {
		t.Fatalf("members %+v", members)
	}

	unloading := store.Channel{
		ID: "ch-unload", Name: "卸货", JoinCode: "ABCD23",
		CreatedBy: captain.ID, CreatedAt: now.Add(time.Minute),
	}
	if err := st.CreateChannel(ctx, unloading, "owner"); err != nil {
		t.Fatal(err)
	}
	loadingMembers, err := st.ChannelMembers(ctx, loading.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range loadingMembers {
		if member.UserID == captain.ID && member.Talking {
			t.Fatal("captain still talking in the channel they left")
		}
	}
	unloadingMembers, err := st.ChannelMembers(ctx, unloading.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unloadingMembers) != 1 || !unloadingMembers[0].Talking || unloadingMembers[0].DisplayName != "队长" {
		t.Fatalf("unloading %+v", unloadingMembers)
	}
}
