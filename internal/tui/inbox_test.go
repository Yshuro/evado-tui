package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

// invitation builds a pending invitation; an empty inviter leaves the
// username null, so the masked email is what identifies them.
func invitation(id, list, inviter string, ago time.Duration) api.Invitation {
	var inv api.Invitation
	inv.ID = id
	inv.List.ID, inv.List.Name = "list-"+id, list
	inv.InvitedBy.UserID, inv.InvitedBy.Email = "u9", "s***@example.com"
	if inviter != "" {
		inv.InvitedBy.Username = &inviter
	}
	inv.CreatedAt = time.Now().Add(-ago)
	return inv
}

// openInbox goes to Lists and opens the inbox.
func openInbox(t *testing.T, f *fakeAPI) *harness {
	t.Helper()
	h := newHarness(t, f)
	h.press("2", "I")
	if !h.model.listsV.inbox.open {
		t.Fatal("I did not open the inbox")
	}
	return h
}

func inboxIDs(h *harness) string {
	var ids []string
	for _, inv := range h.model.listsV.inbox.items {
		ids = append(ids, inv.ID)
	}
	return strings.Join(ids, ",")
}

func TestInboxShowsListInviterAndDateNewestFirst(t *testing.T) {
	f := sampleAPI()
	// The service sends newest first; the screen does not depend on it.
	f.pending = []api.Invitation{
		invitation("i1", "Club", "sam", 72*time.Hour),
		invitation("i2", "Book group", "", 2*time.Hour),
	}
	h := openInbox(t, f)
	h.mustContain("Invitations")
	h.mustContain("Book group")
	h.mustContain("Club")
	h.mustContain("from @sam")
	h.mustContain("from s***@example.com")
	h.mustContain("3 days ago")
	h.mustContain("2 hours ago")
	if got := inboxIDs(h); got != "i2,i1" {
		t.Errorf("order = %s, want the newest first", got)
	}
	h.mustContain("enter")
	h.mustContain("accept")
}

func TestInboxIsReachableFromTheCommandPalette(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	h := newHarness(t, f)
	h.press(":")
	h.typeText("invitations")
	h.press("enter")
	if h.model.tab != tabLists || !h.model.listsV.inbox.open {
		t.Fatalf("tab %v open %v, want the inbox on Lists", h.model.tab, h.model.listsV.inbox.open)
	}
	h.mustContain("Club")
}

func TestInboxEmpty(t *testing.T) {
	h := openInbox(t, sampleAPI())
	h.mustContain("No invitations waiting")
	// Without a username nobody can find you to invite you, so say where
	// to get one.
	h.mustContain("Account, Profile")

	f := sampleAPI()
	f.username = strp("demo")
	h = openInbox(t, f)
	h.mustContain("No invitations waiting")
	h.mustNotContain("Account, Profile")
}

func TestInboxListsAreHintedAtAndLeftWithEscape(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	h := newHarness(t, f)
	h.press("2", "I")
	h.press("esc")
	if h.model.listsV.inbox.open {
		t.Fatal("esc should go back to the lists")
	}
	h.mustContain("Household")

	// Leaving the tab closes it, so coming back finds the lists.
	h.press("I", "1", "2")
	if h.model.listsV.inbox.open {
		t.Error("the inbox stayed open after leaving the tab")
	}
}

func TestInboxLoadFailureCanBeRetried(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	f.invitesErr = &apiFailure{status: 500, code: "internal", msg: "the server fell over"}
	h := openInbox(t, f)
	h.mustContain("Could not load your invitations")
	h.mustContain("the server fell over")
	h.mustContain("try again")

	f.invitesErr = nil
	h.press("r")
	h.mustContain("Club")
	h.mustNotContain("Could not load")
}

func TestAcceptingJoinsTheListAndShowsItsTodos(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{
		invitation("i1", "Club", "sam", 72*time.Hour),
		invitation("i2", "Book group", "kim", time.Hour),
	}
	h := openInbox(t, f)
	reads := f.inviteReads
	h.press("enter") // the newest, Book group

	if len(f.answers) != 1 || f.answers[0] != "accept i2" {
		t.Fatalf("answers = %v, want the newest accepted", f.answers)
	}
	if inboxIDs(h) != "i1" {
		t.Errorf("inbox = %s, want only the other invitation left", inboxIDs(h))
	}
	if f.inviteReads <= reads {
		t.Error("the inbox was not read again after the action")
	}
	if !strings.Contains(h.model.toastText(), `joined "Book group"`) {
		t.Errorf("toast = %q", h.model.toastText())
	}
	joined := false
	for _, l := range h.model.lists {
		joined = joined || l.Name == "Book group"
	}
	if !joined {
		t.Errorf("lists = %+v, want the joined list", h.model.lists)
	}
	h.press("esc")
	h.mustContain("Book group")

	// Its todos arrive and show in the Todos tab.
	h.press("1")
	h.mustContain("Already in Book group")
}

func TestDecliningRemovesItAndNobodyIsTold(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	h := openInbox(t, f)
	h.press("d")

	if len(f.answers) != 1 || f.answers[0] != "decline i1" {
		t.Fatalf("answers = %v", f.answers)
	}
	h.mustContain("No invitations waiting")
	h.mustContain("Declined \"Club\"")
	h.mustContain("u undoes it")
	if txt := h.model.toastText(); !strings.Contains(txt, "Nobody is told") {
		t.Errorf("toast = %q, want to say the inviter is not told", txt)
	}
	// A decline is not a membership change, so the lists are as they were.
	for _, l := range h.model.lists {
		if l.Name == "Club" {
			t.Error("declining must not join the list")
		}
	}
}

func TestUndoingADeclineBringsItBack(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	h := openInbox(t, f)
	h.press("d", "u")

	if len(f.answers) != 2 || f.answers[1] != "restore i1" {
		t.Fatalf("answers = %v, want a restore of the declined id", f.answers)
	}
	if inboxIDs(h) != "i1" {
		t.Errorf("inbox = %q, want the invitation back", inboxIDs(h))
	}
	h.mustContain("Club")
	h.mustNotContain("Declined")
	if !strings.Contains(h.model.toastText(), "waiting for you again") {
		t.Errorf("toast = %q", h.model.toastText())
	}
	// It has been used up: a second undo has nothing to do.
	h.press("u")
	if len(f.answers) != 2 {
		t.Errorf("answers = %v, a second undo must not send anything", f.answers)
	}
}

func TestSeveralDeclinesAreUndoneLatestFirst(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{
		invitation("i1", "Club", "sam", 3*time.Hour),
		invitation("i2", "Book group", "kim", 2*time.Hour),
	}
	h := openInbox(t, f)
	h.press("d", "d") // the newest, then the other
	h.press("u")
	if inboxIDs(h) != "i1" {
		t.Errorf("inbox = %q, want the latest decline undone", inboxIDs(h))
	}
}

func TestRestoreFailureExplainsAndKeepsTheInboxConsistent(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	f.answerErr = map[string]*apiFailure{"restore": {status: 404, code: "not_found", msg: "Not found"}}
	h := openInbox(t, f)
	h.press("d", "u")

	txt := h.model.toastText()
	if !strings.Contains(txt, "no longer available") || !strings.Contains(txt, "cannot be undone") {
		t.Errorf("toast = %q, want it to explain the invitation is unavailable", txt)
	}
	// The inbox shows what the service holds, which is nothing, and the
	// dead entry is not offered again.
	if inboxIDs(h) != "" {
		t.Errorf("inbox = %q, want it to match the service", inboxIDs(h))
	}
	if len(h.model.listsV.inbox.declined) != 0 {
		t.Error("an invitation that cannot be restored is still offered for undo")
	}
	h.mustNotContain("Declined")
}

func TestAnInvitationThatWasWithdrawnIsExplained(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	h := openInbox(t, f)
	f.pending = nil // withdrawn after the inbox was read
	h.press("enter")

	if txt := h.model.toastText(); !strings.Contains(txt, "no longer available") {
		t.Errorf("toast = %q", txt)
	}
	if inboxIDs(h) != "" {
		t.Errorf("inbox = %q, want it read again and empty", inboxIDs(h))
	}
	for _, l := range h.model.lists {
		if l.Name == "Club" {
			t.Error("a failed accept must not add the list")
		}
	}
}

func TestAFailedActionLeavesTheInvitationWhereItWas(t *testing.T) {
	for _, action := range []struct{ key, name string }{{"enter", "accept"}, {"d", "decline"}} {
		f := sampleAPI()
		f.pending = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
		f.answerErr = map[string]*apiFailure{action.name: {status: 500, code: "internal", msg: "the server fell over"}}
		h := openInbox(t, f)
		h.press(action.key)

		if inboxIDs(h) != "i1" {
			t.Errorf("%s: inbox = %q, want the invitation still there", action.name, inboxIDs(h))
		}
		if txt := h.model.toastText(); !strings.Contains(txt, "the server fell over") {
			t.Errorf("%s: toast = %q, want the error", action.name, txt)
		}
		if len(h.model.listsV.inbox.declined) != 0 {
			t.Errorf("%s: a failed action left something to undo", action.name)
		}
	}
}

func TestInboxActionsDoNothingWithNothingSelected(t *testing.T) {
	f := sampleAPI()
	h := openInbox(t, f)
	h.press("enter", "d", "u")
	if len(f.answers) != 0 {
		t.Errorf("answers = %v, want none", f.answers)
	}
}

func TestRateLimitedInboxSaysSo(t *testing.T) {
	f := sampleAPI()
	f.invitesErr = &apiFailure{status: 429, code: "rate_limited", msg: "Too many requests"}
	h := openInbox(t, f)
	h.mustContain("Could not load your invitations")
	h.mustContain("Too many requests")
}

func TestALateReadDoesNotPutBackWhatWasAnswered(t *testing.T) {
	f := sampleAPI()
	f.pending = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	h := openInbox(t, f)
	before := h.model.listsV.inbox.seq
	old := append([]api.Invitation(nil), h.model.listsV.inbox.items...)

	// A background read was asked for before the answer and lands after it.
	h.press("d")
	h.send(invitationsMsg{seq: before, list: api.InvitationList{Invitations: old}})
	h.run(h.cmds...)
	if inboxIDs(h) != "" {
		t.Errorf("inbox = %q, want the declined invitation to stay gone", inboxIDs(h))
	}
}

// -- with the todos kept on this computer -------------------------------------

// inviteAccount is an Evado account with invitations, reached through the
// same sign-in and sync the program uses.
type inviteAccount struct {
	mu      sync.Mutex
	invites []api.Invitation
	lists   []api.List
	todos   []api.Todo
	answers []string
	down    atomic.Bool
	// noSnapshot leaves the invitations out of /sync.
	noSnapshot bool
}

func newInviteAccount(t *testing.T) (*inviteAccount, string) {
	a := &inviteAccount{lists: []api.List{{ID: "p1", Name: "Personal", IsPersonal: true, Role: "owner"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.down.Load() {
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.URL.Path == "/me":
			write(map[string]any{"user": api.User{ID: "u1", Email: "me@example.com", TZ: "UTC"}, "lists": a.lists})
		case r.URL.Path == "/lists":
			write(map[string]any{"lists": a.lists})
		case r.URL.Path == "/sync":
			answer := map[string]any{"todos": a.todos, "lists": a.lists, "cursor": "1", "hasMore": false}
			if !a.noSnapshot {
				answer["invitations"] = append([]api.Invitation{}, a.invites...)
			}
			write(answer)
		case r.URL.Path == "/invitations":
			write(map[string]any{"invitations": append([]api.Invitation{}, a.invites...)})
		case parts[0] == "invitations" && len(parts) == 3:
			a.answers = append(a.answers, parts[2]+" "+parts[1])
			for i, inv := range a.invites {
				if inv.ID != parts[1] {
					continue
				}
				a.invites = append(a.invites[:i:i], a.invites[i+1:]...)
				if parts[2] == "accept" {
					joined := api.List{ID: inv.List.ID, Name: inv.List.Name, Role: "member"}
					a.lists = append(a.lists, joined)
					a.todos = append(a.todos, api.Todo{ID: "t-" + inv.ID, ListID: joined.ID, Title: "Already in " + joined.Name, UpdatedAt: time.Now()})
					write(map[string]any{"list": joined})
					return
				}
				write(map[string]any{"ok": true})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			write(map[string]any{"error": "Not found", "code": "not_found"})
		default:
			w.WriteHeader(http.StatusNotFound)
			write(map[string]any{"error": "not found", "code": "not_found"})
		}
	}))
	t.Cleanup(srv.Close)
	return a, srv.URL
}

// signedInHarness starts the program signed in to a, with the todos kept
// in a private directory and one sync done.
func signedInHarness(t *testing.T, a *inviteAccount, url string) *harness {
	t.Helper()
	h := offlineHarness(t, config.Config{Token: "evt_good", BaseURL: url})
	h.run(h.model.syncNow())
	return h
}

func TestAcceptingWithLocalTodosJoinsTheListAndItsTodos(t *testing.T) {
	a, url := newInviteAccount(t)
	a.invites = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	h := signedInHarness(t, a, url)

	// The sync already brought the invitation, and the Lists tab says so.
	h.press("2")
	h.mustContain("1 invitation (I)")
	h.press("I")
	h.mustContain("Club")
	h.press("enter")

	if len(a.answers) != 1 || a.answers[0] != "accept i1" {
		t.Fatalf("answers = %v", a.answers)
	}
	joined := false
	for _, l := range h.model.lists {
		joined = joined || l.Name == "Club"
	}
	if !joined {
		t.Fatalf("lists = %+v, want the joined list", h.model.lists)
	}
	if h.model.backend.Status().Invitations != 0 {
		t.Error("the accepted invitation is still cached here")
	}
	h.press("esc", "1")
	h.mustContain("Already in Club")
}

func TestOfflineInboxShowsWhatWasSavedAndSaysWhyActionsWait(t *testing.T) {
	a, url := newInviteAccount(t)
	a.invites = []api.Invitation{
		invitation("i1", "Club", "sam", 72*time.Hour),
		invitation("i2", "Book group", "", time.Hour),
	}
	h := signedInHarness(t, a, url)
	a.down.Store(true)

	h.press("2", "I")
	h.mustContain("Book group")
	h.mustContain("Club")
	h.mustContain("from @sam")
	h.mustContain("Offline")
	h.mustContain("needs a connection")

	// Answering makes no request and says why.
	h.press("enter")
	if len(a.answers) != 0 {
		t.Errorf("answers = %v, want none while offline", a.answers)
	}
	if txt := h.model.toastText(); !strings.Contains(txt, "needs a connection") {
		t.Errorf("toast = %q", txt)
	}
	h.press("d")
	if len(a.answers) != 0 {
		t.Errorf("answers = %v, want none while offline", a.answers)
	}

	// Back online, r reads it fresh and the actions work.
	a.down.Store(false)
	h.press("r")
	h.mustNotContain("Offline")
	h.press("enter")
	if len(a.answers) != 1 {
		t.Errorf("answers = %v, want the accept to go through once online", a.answers)
	}
}

func TestOfflineWithAnEmptySavedInboxIsEmptyNotBroken(t *testing.T) {
	a, url := newInviteAccount(t)
	h := signedInHarness(t, a, url)
	a.down.Store(true)
	h.press("2", "I")
	h.mustContain("No invitations waiting")
	h.mustNotContain("unavailable")
}

func TestOfflineWithNothingSavedIsUnavailable(t *testing.T) {
	a, url := newInviteAccount(t)
	a.noSnapshot = true
	h := signedInHarness(t, a, url)
	a.down.Store(true)
	h.press("2", "I")
	h.mustContain("unavailable offline")
	h.mustContain("press r")
}

func TestActionFailingWithNoConnectionLeavesTheInbox(t *testing.T) {
	a, url := newInviteAccount(t)
	a.invites = []api.Invitation{invitation("i1", "Club", "sam", time.Hour)}
	h := signedInHarness(t, a, url)
	h.press("2", "I")
	h.mustNotContain("Offline")

	// The connection drops between reading the inbox and answering it.
	a.down.Store(true)
	h.press("enter")
	if txt := h.model.toastText(); !strings.Contains(txt, "needs a connection") {
		t.Errorf("toast = %q", txt)
	}
	if inboxIDs(h) != "i1" {
		t.Errorf("inbox = %q, want the invitation untouched", inboxIDs(h))
	}
	for _, l := range h.model.lists {
		if l.Name == "Club" {
			t.Error("nothing was joined")
		}
	}
}

func TestGuestsAreSentToSignIn(t *testing.T) {
	h := offlineHarness(t, config.Config{Local: true})
	h.press("2", "I")
	h.mustContain("Invitations need an Evado account")
	h.mustContain("sign in")

	h.press("enter", "d", "u")
	if h.model.layer == layerLogin {
		t.Fatal("answering with no account must not jump to sign-in on its own")
	}
	h.press("s")
	if h.model.layer != layerLogin {
		t.Errorf("layer = %v, want the sign-in screen", h.model.layer)
	}
}

func TestSyncBringingAnInvitationRefreshesAnOpenInbox(t *testing.T) {
	a, url := newInviteAccount(t)
	h := signedInHarness(t, a, url)
	h.press("2", "I")
	h.mustContain("No invitations waiting")

	a.mu.Lock()
	a.invites = []api.Invitation{invitation("i1", "Club", "sam", time.Minute)}
	a.mu.Unlock()
	h.run(h.model.syncNow())
	h.mustContain("Club")
}
