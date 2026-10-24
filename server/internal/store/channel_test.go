package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
)

func TestCreateChannelAndJoin(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_200, 0).UTC()

	captain, err := st.UpsertUser(ctx, "captain", "队长", "device-captain", now)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := st.UpsertUser(ctx, "driver", "司机", "device-driver", now)
	if err != nil {
		t.Fatal(err)
	}

	loading := store.Channel{
		ID: "ch-1", Name: "装货", JoinCode: "3842K9",
		CreatedBy: captain.ID, CreatedAt: now,
	}
	if err := st.CreateChannel(ctx, loading, "owner"); err != nil {
		t.Fatal(err)
	}
	got, err := st.ChannelByJoinCode(ctx, "3842K9")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != loading.ID || got.Name != "装货" || got.CreatedBy != captain.ID || !got.CreatedAt.Equal(now) {
		t.Fatalf("channel %+v", got)
	}
	current, role, err := st.CurrentChannel(ctx, captain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != loading.ID || role != "owner" {
		t.Fatalf("current %s role %s", current.ID, role)
	}

	dup := loading
	dup.ID = "ch-dup"
	if err := st.CreateChannel(ctx, dup, "owner"); !errors.Is(err, store.ErrJoinCodeTaken) {
		t.Fatalf("duplicate code err %v", err)
	}
	current, _, err = st.CurrentChannel(ctx, captain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != loading.ID {
		t.Fatalf("failed insert changed current channel to %s", current.ID)
	}

	other := store.Channel{
		ID: "ch-2", Name: "装货", JoinCode: "5555AB",
		CreatedBy: captain.ID, CreatedAt: now.Add(time.Second),
	}
	if err := st.CreateChannel(ctx, other, "owner"); err != nil {
		t.Fatal(err)
	}
	names, err := st.ChannelsByName(ctx, "装货")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0].ID != loading.ID || names[1].ID != other.ID {
		t.Fatalf("names %+v", names)
	}

	role, err = st.JoinChannel(ctx, driver.ID, loading.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	if role != "member" {
		t.Fatalf("driver role %s", role)
	}
	current, role, err = st.CurrentChannel(ctx, driver.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != loading.ID || role != "member" {
		t.Fatalf("driver current %s role %s", current.ID, role)
	}

	role, err = st.JoinChannel(ctx, captain.ID, loading.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	if role != "owner" {
		t.Fatalf("owner downgraded to %s", role)
	}

	if _, err := st.JoinChannel(ctx, driver.ID, "missing-channel", "member"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing channel err %v", err)
	}
	current, _, err = st.CurrentChannel(ctx, driver.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != loading.ID {
		t.Fatalf("failed join changed current channel to %s", current.ID)
	}

	if _, err := st.ChannelByJoinCode(ctx, "NOCODE"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing code err %v", err)
	}
	if _, _, err := st.CurrentChannel(ctx, "missing-user"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing current err %v", err)
	}

	memberships, err := st.ListMemberships(ctx, driver.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(memberships) != 1 || memberships[0].Channel.ID != loading.ID || memberships[0].Role != "member" {
		t.Fatalf("driver memberships %+v", memberships)
	}
	captainChannels, err := st.ListMemberships(ctx, captain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(captainChannels) != 2 || captainChannels[0].Channel.ID != loading.ID || captainChannels[1].Channel.ID != other.ID || captainChannels[0].Role != "owner" {
		t.Fatalf("captain memberships %+v", captainChannels)
	}
	if _, stopped, err := st.SwitchCurrentChannel(ctx, driver.ID, "missing-channel"); !errors.Is(err, store.ErrNotFound) || stopped {
		t.Fatalf("missing switch err %v stopped %v", err, stopped)
	}

	if err := st.SetTalking(ctx, driver.ID, true); err != nil {
		t.Fatal(err)
	}
	prev, stopped, err := st.SwitchCurrentChannel(ctx, driver.ID, loading.ID)
	if err != nil {
		t.Fatal(err)
	}
	if prev != loading.ID || stopped {
		t.Fatalf("same channel prev %s stopped %v", prev, stopped)
	}
	talking, err := st.IsTalking(ctx, driver.ID)
	if err != nil || !talking {
		t.Fatalf("same switch talking %v err %v", talking, err)
	}
	if _, err := st.JoinChannel(ctx, driver.ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}
	prev, stopped, err = st.SwitchCurrentChannel(ctx, driver.ID, loading.ID)
	if err != nil {
		t.Fatal(err)
	}
	if prev != other.ID || !stopped {
		t.Fatalf("switch prev %s stopped %v", prev, stopped)
	}
	current, role, err = st.CurrentChannel(ctx, driver.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != loading.ID || role != "member" {
		t.Fatalf("switched current %s role %s", current.ID, role)
	}
	talking, err = st.IsTalking(ctx, driver.ID)
	if err != nil || talking {
		t.Fatalf("after switch talking %v err %v", talking, err)
	}
}

func TestEnsureChannelPreservesSingletonAndRejectsCodeCollision(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	ch := store.Channel{ID: "public", Name: "公共频道", JoinCode: "ABC234", CreatedBy: "system", CreatedAt: time.Unix(1700000000, 0).UTC()}
	got, err := st.EnsureChannel(ctx, ch)
	if err != nil || got != ch {
		t.Fatalf("ensure %+v %v", got, err)
	}
	retry := ch
	retry.JoinCode = "DEF567"
	retry.CreatedAt = ch.CreatedAt.Add(time.Hour)
	got, err = st.EnsureChannel(ctx, retry)
	if err != nil || got != ch {
		t.Fatalf("retry mutated singleton %+v %v", got, err)
	}
	collision := ch
	collision.ID = "other"
	if _, err := st.EnsureChannel(ctx, collision); !errors.Is(err, store.ErrJoinCodeTaken) {
		t.Fatalf("collision error %v", err)
	}
	if _, err := st.ChannelByID(ctx, collision.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("collision created channel: %v", err)
	}
}
