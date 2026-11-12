package local

import (
	"context"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func strp(s string) *string { return &s }

func TestUsernamesArriveWithSyncAndServeOffline(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.user.Username = strp("ann")
	svc.members = []api.Member{
		{UserID: "u1", Username: strp("ann"), Email: "me@example.com", Role: "owner"},
		{UserID: "u2", Username: strp("sam"), Email: "s***@example.com", Role: "member"},
		{UserID: "u3", Username: nil, Email: "j***@example.com", Role: "member"},
	}
	b := open(t, "", "")
	if _, err := b.SignIn(ctx, "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	svc.down.Store(true)
	c := b.Client()
	user, _, err := c.Me(ctx)
	if err != nil || user.Username == nil || *user.Username != "ann" {
		t.Fatalf("me offline = %+v %v, want the cached username", user, err)
	}
	members, err := c.ListMembers(ctx, "p1")
	if err != nil || len(members) != 3 {
		t.Fatalf("members offline = %+v %v", members, err)
	}
	if members[1].Name() != "@sam" || members[2].Username != nil || members[2].Name() != "j***@example.com" {
		t.Errorf("offline labels = %q %q, want the username, else the masked email", members[1].Name(), members[2].Name())
	}
}

func TestSettingAUsernameIsKeptAtOnce(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	b := open(t, "", "")
	if _, err := b.SignIn(ctx, "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	c := b.Client()

	got, err := c.SetUsername(ctx, strp("Ann"))
	if err != nil {
		t.Fatalf("SetUsername: %v", err)
	}
	if len(svc.meBodies) != 1 || svc.meBodies[0] != `{"username":"Ann"}` {
		t.Errorf("the service was sent %v, want only the username", svc.meBodies)
	}
	if got.Username == nil || *got.Username != "Ann" {
		t.Fatalf("answer = %+v", got)
	}
	// With the network gone, the cache still has the new name, on the
	// account and on its own entry in the list's members.
	svc.down.Store(true)
	user, _, err := c.Me(ctx)
	if err != nil || user.Username == nil || *user.Username != "Ann" || user.TZ != "UTC" {
		t.Errorf("me = %+v %v, want the new username and the same timezone", user, err)
	}
	members, err := c.ListMembers(ctx, "p1")
	if err != nil || len(members) != 1 || members[0].Username == nil || *members[0].Username != "Ann" {
		t.Errorf("members = %+v %v, want the account's own entry renamed", members, err)
	}
	svc.down.Store(false)

	// Clearing goes out as null and comes back null.
	if _, err := c.SetUsername(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if svc.meBodies[1] != `{"username":null}` {
		t.Errorf("clearing sent %s", svc.meBodies[1])
	}
	if user, _, _ := c.Me(ctx); user.Username != nil {
		t.Errorf("username = %v after clearing, want none", *user.Username)
	}
}

func TestRefusedUsernameChangesNothingHere(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.user.Username = strp("ann")
	b := open(t, "", "")
	if _, err := b.SignIn(ctx, "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	c := b.Client()

	_, err := c.SetUsername(ctx, strp("taken"))
	if api.CodeOf(err) != api.CodeUsernameTaken {
		t.Fatalf("err = %v, want the service's username_taken", err)
	}
	if user, _, _ := c.Me(ctx); user.Username == nil || *user.Username != "ann" {
		t.Errorf("username = %v, want it left as it was", user.Username)
	}

	// Offline the change cannot be made, and is not pretended.
	svc.down.Store(true)
	_, err = c.SetUsername(ctx, strp("bea"))
	if api.CodeOf(err) != api.CodeOffline {
		t.Errorf("err = %v, want the offline code", err)
	}
	if user, _, _ := c.Me(ctx); user.Username == nil || *user.Username != "ann" {
		t.Errorf("username = %v, want it left as it was", user.Username)
	}
}

func TestUsernameNeedsAnAccountButTimezoneDoesNot(t *testing.T) {
	ctx := context.Background()
	c := open(t, "", "").Client()
	if _, err := c.SetUsername(ctx, strp("ann")); api.CodeOf(err) != api.CodeForbidden {
		t.Errorf("err = %v, want a refusal for a computer with no account", err)
	}
	if u, err := c.SetTimezone(ctx, "Europe/Madrid"); err != nil || u.TZ != "Europe/Madrid" || u.Username != nil {
		t.Errorf("timezone = %+v %v", u, err)
	}
}

func TestSyncReportsAUsernameChangedElsewhere(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	b := open(t, "", "")
	if _, err := b.SignIn(ctx, "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	svc.user.Username = strp("ann")
	res, err := b.Sync(ctx)
	if err != nil || !res.Changed {
		t.Fatalf("sync = %+v %v, want the change reported so labels redraw", res, err)
	}
	if user, _, _ := b.Client().Me(ctx); user.Username == nil || *user.Username != "ann" {
		t.Errorf("username = %v", user.Username)
	}
}
