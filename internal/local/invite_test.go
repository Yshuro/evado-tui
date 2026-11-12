package local

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// A list the account does not have yet has nobody to invite to, and the
// service would only answer that it does not know it. The answer comes from
// here, with a code of its own, until a sync has made the list there.
func TestAListNotSyncedYetIsNotSharedWithTheService(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	b := syncedAt(t, filepath.Join(t.TempDir(), "todos.json"), url)
	c := b.Client()
	svc.down.Store(true)
	trip, err := c.CreateList(ctx, "Trip")
	if err != nil {
		t.Fatal(err)
	}
	svc.down.Store(false)

	if got := b.Unsynced(); len(got) != 1 || !got[trip.ID] {
		t.Errorf("unsynced = %v, want only the list made offline", got)
	}
	_, err = c.InviteMember(ctx, trip.ID, api.InviteTarget{Username: "@sam"})
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeListNotSynced || apiErr.StatusCode != http.StatusConflict || apiErr.Message != NotSyncedMessage {
		t.Errorf("invite err = %v, want the not synced answer", err)
	}
	if _, err := c.CreateInvite(ctx, trip.ID); api.CodeOf(err) != api.CodeListNotSynced {
		t.Errorf("invite link err = %v, want the not synced answer", err)
	}
	if len(svc.inviteBodies) != 0 {
		t.Errorf("the service was sent %v, want nothing", svc.inviteBodies)
	}

	// Once a sync has made it there, it goes through.
	mustSync(t, b)
	made := svc.named("Trip")
	if made == nil {
		t.Fatal("the list never reached the service")
	}
	if got := b.Unsynced(); len(got) != 0 {
		t.Errorf("unsynced after the sync = %v", got)
	}
	if _, err := c.InviteMember(ctx, made.ID, api.InviteTarget{Username: "@sam"}); err != nil {
		t.Errorf("invite after the sync: %v", err)
	}
}

// With no account every list is only here, and none is waiting for one.
func TestNoListIsNotSyncedYetWithoutAnAccount(t *testing.T) {
	b := open(t, "", "")
	if _, err := b.Client().CreateList(context.Background(), "Trip"); err != nil {
		t.Fatal(err)
	}
	if got := b.Unsynced(); len(got) != 0 {
		t.Errorf("unsynced = %v, want none", got)
	}
	_, err := b.Client().InviteMember(context.Background(), b.st.Lists[0].ID, api.InviteTarget{Username: "@sam"})
	if api.CodeOf(err) == api.CodeListNotSynced {
		t.Errorf("err = %v, want the account-needed answer, not the not synced one", err)
	}
}

func TestInvitationsAndPeopleGoThroughToTheService(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	b := open(t, "", "")
	if _, err := b.SignIn(ctx, "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	c := b.Client()

	people, err := c.People(ctx)
	if err != nil || len(people) != 1 || people[0].Name() != "@sam" {
		t.Fatalf("people = %+v %v", people, err)
	}
	if _, err := c.InviteMember(ctx, "p1", api.InviteTarget{Username: "@sam"}); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if len(svc.inviteBodies) != 1 || svc.inviteBodies[0] != `{"username":"@sam"}` {
		t.Errorf("the service was sent %v, want exactly the username", svc.inviteBodies)
	}

	// Offline there is nobody to ask, and the answer says so.
	svc.down.Store(true)
	if _, err := c.InviteMember(ctx, "p1", api.InviteTarget{Email: "sam@example.com"}); api.CodeOf(err) != api.CodeOffline {
		t.Errorf("err = %v, want the offline code", err)
	}
	if _, err := c.People(ctx); api.CodeOf(err) != api.CodeOffline {
		t.Errorf("people err = %v, want the offline code", err)
	}
}
