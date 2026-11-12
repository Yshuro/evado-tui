package local

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// invite builds a pending invitation.
func invite(id, list, inviter string, at time.Time) api.Invitation {
	var inv api.Invitation
	inv.ID = id
	inv.List.ID, inv.List.Name = "list-"+id, list
	inv.InvitedBy.UserID, inv.InvitedBy.Email = "u9", "s***@example.com"
	if inviter != "" {
		inv.InvitedBy.Username = &inviter
	}
	inv.CreatedAt = at
	return inv
}

// serveInvitations is the service's invitation endpoints: list, accept,
// decline and restore.
func (s *service) serveInvitations(w http.ResponseWriter, r *http.Request) {
	write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 1 {
		s.inviteReads++
		write(map[string]any{"invitations": append([]api.Invitation{}, s.invites...)})
		return
	}
	id, action := parts[1], parts[2]
	take := func(from *[]api.Invitation) (api.Invitation, bool) {
		for i, inv := range *from {
			if inv.ID == id {
				*from = append((*from)[:i:i], (*from)[i+1:]...)
				return inv, true
			}
		}
		return api.Invitation{}, false
	}
	missing := func() {
		w.WriteHeader(http.StatusNotFound)
		write(map[string]any{"error": "Not found", "code": "not_found"})
	}
	switch action {
	case "accept":
		inv, ok := take(&s.invites)
		if !ok {
			missing()
			return
		}
		joined := api.List{ID: inv.List.ID, Name: inv.List.Name, Role: "member"}
		s.lists = append(s.lists, joined)
		// Joining moves the list's todos into the feed.
		s.todos["joined-"+id] = &wireTodo{Todo: api.Todo{
			ID: "joined-" + id, ListID: joined.ID, Title: "Already in " + joined.Name, CreatedAt: s.clock, UpdatedAt: s.tick(),
		}}
		write(map[string]any{"list": joined})
	case "decline":
		inv, ok := take(&s.invites)
		if !ok {
			missing()
			return
		}
		s.declined = append(s.declined, inv)
		write(map[string]any{"ok": true})
	case "restore":
		inv, ok := take(&s.declined)
		if !ok {
			missing()
			return
		}
		s.invites = append([]api.Invitation{inv}, s.invites...)
		write(map[string]any{"invitation": inv})
	}
}

var t0 = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

// signedIn is a backend signed in to svc, already synced once.
func signedIn(t *testing.T, svc *service, url string) *Backend {
	t.Helper()
	b := open(t, "", "")
	if _, err := b.SignIn(context.Background(), "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	return b
}

func inboxIDs(list api.InvitationList) string {
	var ids []string
	for _, inv := range list.Invitations {
		ids = append(ids, inv.ID)
	}
	return strings.Join(ids, ",")
}

func TestSyncSnapshotReplacesTheCachedInvitations(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0), invite("i2", "Household", "", t0.Add(time.Hour))}
	b := signedIn(t, svc, url)
	if got := b.Status().Invitations; got != 2 {
		t.Fatalf("kept %d invitations, want the snapshot's 2", got)
	}

	// Offline, the cached set is served newest first and marked as cached.
	svc.down.Store(true)
	got, err := b.Client().Invitations(ctx)
	if err != nil || !got.Cached || inboxIDs(got) != "i2,i1" {
		t.Fatalf("offline = %+v %v, want the cached set, newest first", got, err)
	}
	if got.Invitations[0].InvitedBy.Username != nil || got.Invitations[1].InvitedBy.Username == nil {
		t.Errorf("inviter usernames were not kept as they were: %+v", got.Invitations)
	}

	// A new snapshot with one fewer replaces the set rather than adding to it.
	svc.down.Store(false)
	svc.invites = svc.invites[:1]
	res, err := b.Sync(ctx)
	if err != nil || !res.InvitationsChanged {
		t.Fatalf("sync = %+v %v, want the change reported", res, err)
	}
	svc.down.Store(true)
	if got, _ := b.Client().Invitations(ctx); inboxIDs(got) != "i1" {
		t.Errorf("after the smaller snapshot: %q, want i1", inboxIDs(got))
	}
}

func TestEmptySnapshotClearsTheCachedInvitations(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	b := signedIn(t, svc, url)

	svc.invites = nil
	res, err := b.Sync(ctx)
	if err != nil || !res.InvitationsChanged {
		t.Fatalf("sync = %+v %v", res, err)
	}
	if got := b.Status().Invitations; got != 0 {
		t.Errorf("kept %d invitations after an empty snapshot", got)
	}
	svc.down.Store(true)
	got, err := b.Client().Invitations(ctx)
	if err != nil || !got.Cached || got.Invitations == nil || len(got.Invitations) != 0 {
		t.Errorf("offline = %+v %v, want a cached empty set, not an error", got, err)
	}
}

func TestAbsentSnapshotLeavesTheCachedInvitations(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	b := signedIn(t, svc, url)

	// A response with no invitations key says nothing about them.
	svc.noSnapshot = true
	res, err := b.Sync(ctx)
	if err != nil || res.InvitationsChanged {
		t.Fatalf("sync = %+v %v", res, err)
	}
	svc.down.Store(true)
	if got, err := b.Client().Invitations(ctx); err != nil || inboxIDs(got) != "i1" {
		t.Errorf("after a sync with no snapshot: %+v %v, want i1 kept", got, err)
	}
}

func TestOfflineWithNothingSavedSaysSo(t *testing.T) {
	svc, url := newService(t)
	svc.noSnapshot = true
	b := signedIn(t, svc, url)
	svc.down.Store(true)
	_, err := b.Client().Invitations(context.Background())
	if api.CodeOf(err) != api.CodeOffline {
		t.Errorf("err = %v, want the offline code, not an empty inbox", err)
	}
}

func TestOnlineInboxIsLiveAndRefreshesTheCache(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	b := signedIn(t, svc, url)

	// The invitation arrives after the last sync, but asking finds it.
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	got, err := b.Client().Invitations(ctx)
	if err != nil || got.Cached || inboxIDs(got) != "i1" {
		t.Fatalf("online = %+v %v, want the live answer", got, err)
	}
	if b.Status().Invitations != 1 {
		t.Error("the live answer should replace the cached set")
	}
}

func TestInvitationsSurviveARestart(t *testing.T) {
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	path := filepath.Join(t.TempDir(), "todos.json")
	b, err := Open(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.SignIn(context.Background(), "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	svc.down.Store(true)
	again, err := Open(path, "evt_x", url)
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.Client().Invitations(context.Background())
	if err != nil || !got.Cached || inboxIDs(got) != "i1" {
		t.Errorf("after a restart, offline: %+v %v", got, err)
	}
}

func TestInvitationsBelongToTheAccountAndServer(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	b := signedIn(t, svc, url)

	// Another account signs in on this computer: the first one's
	// invitations must not be shown to it, not even for a moment.
	other, otherURL := newService(t)
	other.user = api.User{ID: "u2", Email: "you@example.com", TZ: "UTC"}
	other.noSnapshot = true
	if _, err := b.SignIn(ctx, "evt_y", otherURL, false); err != nil {
		t.Fatal(err)
	}
	if got := b.Status().Invitations; got != 0 {
		t.Fatalf("the new account inherited %d invitations", got)
	}
	other.down.Store(true)
	if _, err := b.Client().Invitations(ctx); api.CodeOf(err) != api.CodeOffline {
		t.Errorf("err = %v, want nothing to show the new account offline", err)
	}

	// The same account id on another server is not assumed to be the same
	// account.
	svc2, url2 := newService(t)
	svc2.noSnapshot = true
	b2 := signedIn(t, svc, url)
	if b2.Status().Invitations != 1 {
		t.Fatal("setup: the invitation should be kept")
	}
	if _, err := b2.SignIn(ctx, "evt_x", url2, false); err != nil {
		t.Fatal(err)
	}
	if got := b2.Status().Invitations; got != 0 {
		t.Errorf("kept %d invitations after the server changed", got)
	}
}

func TestSignOutForgetsInvitations(t *testing.T) {
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	b := signedIn(t, svc, url)
	if err := b.Forget(); err != nil {
		t.Fatal(err)
	}
	if got := b.Status().Invitations; got != 0 {
		t.Errorf("kept %d invitations after signing out", got)
	}
}

func TestGuestHasNoInbox(t *testing.T) {
	_, err := open(t, "", "").Client().Invitations(context.Background())
	if api.CodeOf(err) != api.CodeForbidden {
		t.Errorf("err = %v, want a refusal that points at signing in", err)
	}
}

func TestAcceptingJoinsTheListAndBringsItsTodos(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	b := signedIn(t, svc, url)
	c := b.Client()

	joined, err := c.AcceptInvitation(ctx, "i1")
	if err != nil || joined.Name != "Work" {
		t.Fatalf("accept = %+v %v", joined, err)
	}
	lists, err := c.ListLists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range lists {
		found = found || l.Name == "Work"
	}
	if !found {
		t.Errorf("lists = %+v, want the joined list without waiting for another sync", lists)
	}
	if got := strings.Join(titles(t, c), ","); got != "Already in Work" {
		t.Errorf("todos = %q, want the joined list's todos pulled", got)
	}
	if b.Status().Invitations != 0 {
		t.Error("an accepted invitation is still in the cache")
	}
}

func TestAcceptingOnceAnotherSyncHasFinishedStillJoins(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	b := signedIn(t, svc, url)

	// A sync is running when the answer is given; the list must still
	// arrive, because Sync alone would return at once and skip it.
	b.mu.Lock()
	b.syncing = true
	b.mu.Unlock()
	go func() {
		time.Sleep(60 * time.Millisecond)
		b.mu.Lock()
		b.syncing = false
		b.mu.Unlock()
	}()
	if _, err := b.Client().AcceptInvitation(ctx, "i1"); err != nil {
		t.Fatal(err)
	}
	lists, _ := b.Client().ListLists(ctx)
	if len(lists) != 2 {
		t.Errorf("lists = %+v, want the joined list", lists)
	}
}

func TestDeclineAndRestore(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	b := signedIn(t, svc, url)
	c := b.Client()

	if err := c.DeclineInvitation(ctx, "i1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Invitations(ctx); len(got.Invitations) != 0 || b.Status().Invitations != 0 {
		t.Errorf("after declining: %+v", got)
	}
	back, err := c.RestoreInvitation(ctx, "i1")
	if err != nil || back.ID != "i1" {
		t.Fatalf("restore = %+v %v", back, err)
	}
	if got, _ := c.Invitations(ctx); inboxIDs(got) != "i1" {
		t.Errorf("after restoring: %q", inboxIDs(got))
	}
	// Restoring what is not declined is the service's 404, passed on.
	if _, err := c.RestoreInvitation(ctx, "i1"); !api.IsNotFound(err) {
		t.Errorf("second restore err = %v, want not found", err)
	}
}

func TestMembershipActionsNeedAConnection(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.invites = []api.Invitation{invite("i1", "Work", "sam", t0)}
	b := signedIn(t, svc, url)
	c := b.Client()
	svc.down.Store(true)

	if _, err := c.AcceptInvitation(ctx, "i1"); api.CodeOf(err) != api.CodeOffline {
		t.Errorf("accept err = %v, want the offline code", err)
	}
	if err := c.DeclineInvitation(ctx, "i1"); api.CodeOf(err) != api.CodeOffline {
		t.Errorf("decline err = %v, want the offline code", err)
	}
	if b.Status().Invitations != 1 {
		t.Error("an action that could not be made must leave the cached inbox as it was")
	}
}
