package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

// assignment is the service's claim and release, for the fake: the change
// is made only from the state it expects, a retry that already took effect
// returns the todo unchanged, and losing names who has it, except for a
// done todo, where the service sends neither.
func (f *fakeAPI) assignment(w http.ResponseWriter, t *api.Todo, claim bool) {
	write := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if t == nil {
		write(http.StatusNotFound, map[string]any{"error": "Not found", "code": "not_found"})
		return
	}
	for _, l := range f.lists {
		if l.ID == t.ListID && l.IsPersonal {
			write(http.StatusBadRequest, map[string]any{"error": "only todos in shared lists can be claimed or returned to Anyone", "code": api.CodePersonalList})
			return
		}
	}
	if t.IsCompleted {
		write(http.StatusConflict, map[string]any{"error": "this todo is already done", "code": api.CodeTodoCompleted})
		return
	}
	members := f.members
	if len(members) == 0 {
		members = []api.Member{
			{UserID: "u1", Email: "demo@example.com", Role: "owner"},
			{UserID: "u2", Email: "sam@example.com", Role: "member"},
		}
	}
	var holder *api.Member
	for i := range members {
		if t.AssigneeID != nil && members[i].UserID == *t.AssigneeID {
			holder = &members[i]
		}
	}
	me := "u1"
	mine := t.AssigneeID != nil && *t.AssigneeID == me
	switch {
	case claim && mine, !claim && t.AssigneeID == nil:
	case claim && holder == nil:
		t.AssigneeID = &me
		t.UpdatedAt = time.Now()
		f.writes++
	case claim:
		write(http.StatusConflict, map[string]any{"error": "someone else already claimed this todo", "code": api.CodeAlreadyClaimed, "todo": t, "assignee": holder})
		return
	case mine:
		t.AssigneeID = nil
		t.UpdatedAt = time.Now()
		f.writes++
	default:
		write(http.StatusConflict, map[string]any{"error": "this todo isn't assigned to you", "code": api.CodeNotAssignedToYou, "todo": t, "assignee": holder})
		return
	}
	write(http.StatusOK, map[string]any{"todo": t})
}

// sharedAPI has one todo in a personal list and one in a shared list,
// Household, which is Anyone's.
func sharedAPI() *fakeAPI {
	past := time.Now().Add(-48 * time.Hour)
	return &fakeAPI{
		lists: []api.List{
			{ID: "l1", Name: "Personal", IsPersonal: true, Role: "owner"},
			{ID: "l2", Name: "Household", Role: "owner"},
		},
		todos: []api.Todo{
			{ID: "t1", ListID: "l1", Title: "Pay the cable bill", UpdatedAt: past, CreatedAt: past},
			{ID: "t2", ListID: "l2", Title: "Take out the bins", UpdatedAt: past, CreatedAt: past},
		},
		steps: map[string][]api.ChecklistItem{},
	}
}

// pick puts the cursor on a todo.
func (h *harness) pick(id string) {
	h.t.Helper()
	h.press("g")
	for range 20 {
		if t := h.model.todos.selected(); t != nil && t.ID == id {
			return
		}
		h.press("j")
	}
	h.t.Fatalf("could not put the cursor on %s", id)
}

// row is the todo as the list on screen holds it.
func (h *harness) row(id string) api.Todo {
	h.t.Helper()
	for _, t := range h.model.todos.all {
		if t.ID == id {
			return t
		}
	}
	h.t.Fatalf("%s is not in the list", id)
	return api.Todo{}
}

func footerHas(h *harness, key, label string) bool {
	for _, e := range h.model.todos.footer() {
		if e.keys == key && e.desc == label {
			return true
		}
	}
	return false
}

func TestTakeIsOfferedOnlyInSharedLists(t *testing.T) {
	f := sharedAPI()
	h := newHarness(t, f)
	h.pick("t1")
	if footerHas(h, "i", "take") {
		t.Error("the footer offers taking a todo in the personal list")
	}
	writes := f.writes
	h.press("i")
	if !strings.Contains(h.model.toastText(), "shared lists") || f.writes != writes {
		t.Errorf("toast = %q, writes %d; want an explanation and no request", h.model.toastText(), f.writes-writes)
	}

	h.pick("t2")
	if !footerHas(h, "i", "take") {
		t.Errorf("footer = %+v, want i take on a shared list's todo", h.model.todos.footer())
	}
}

func TestTakeAndHandBack(t *testing.T) {
	f := sharedAPI()
	h := newHarness(t, f)
	h.pick("t2")
	h.press("i")
	if got := h.row("t2").AssigneeID; got == nil || *got != "u1" {
		t.Fatalf("row assignee = %v, want you", got)
	}
	if f.todos[1].AssigneeID == nil || !strings.Contains(h.model.toastText(), "yours now") {
		t.Errorf("toast = %q, want it taken", h.model.toastText())
	}
	if !footerHas(h, "i", "hand back") {
		t.Errorf("footer = %+v, want i hand back on your own todo", h.model.todos.footer())
	}

	h.press("i")
	if got := h.row("t2").AssigneeID; got != nil {
		t.Fatalf("row assignee = %v, want Anyone", *got)
	}
	if !strings.Contains(h.model.toastText(), "back to Anyone") {
		t.Errorf("toast = %q", h.model.toastText())
	}
}

func TestTakingWhatSomeoneTookFirstSaysWhoAndShowsIt(t *testing.T) {
	f := sharedAPI()
	h := newHarness(t, f)
	h.pick("t2")
	// Sam took it on another device after the list was loaded here.
	sam := "u2"
	f.todos[1].AssigneeID = &sam
	h.press("i")
	if got := h.row("t2").AssigneeID; got == nil || *got != "u2" {
		t.Errorf("row assignee = %v, want it updated from the answer to sam", got)
	}
	if toast := h.model.toastText(); !strings.Contains(toast, "sam@example.com has it already") {
		t.Errorf("toast = %q, want who has it", toast)
	}
	if h.model.toast.kind != toastWarn {
		t.Errorf("toast kind = %v, want a warning", h.model.toast.kind)
	}
}

func TestHandingBackAReassignedTodoChangesNothing(t *testing.T) {
	f := sharedAPI()
	me := "u1"
	f.todos[1].AssigneeID = &me
	h := newHarness(t, f)
	h.pick("t2")
	sam := "u2"
	f.todos[1].AssigneeID = &sam
	h.press("i")
	if got := h.row("t2").AssigneeID; got == nil || *got != "u2" {
		t.Errorf("row assignee = %v, want sam's from the answer", got)
	}
	if toast := h.model.toastText(); !strings.Contains(toast, "sam@example.com has it now, so nothing changed") {
		t.Errorf("toast = %q", toast)
	}
	if *f.todos[1].AssigneeID != "u2" {
		t.Error("the release took sam's todo away")
	}
}

func TestTakingATodoDoneElsewhereSaysSo(t *testing.T) {
	f := sharedAPI()
	h := newHarness(t, f)
	h.pick("t2")
	f.todos[1].IsCompleted = true
	h.press("i")
	if toast := h.model.toastText(); !strings.Contains(toast, "done now") {
		t.Errorf("toast = %q, want it to say the todo is done", toast)
	}
	if !h.row("t2").IsCompleted {
		t.Error("the list was not reloaded to show the todo done")
	}
}

func TestATakeRetriedAfterItWorkedIsQuiet(t *testing.T) {
	f := sharedAPI()
	h := newHarness(t, f)
	h.pick("t2")
	// The first answer was lost: the service has it as yours already.
	me := "u1"
	f.todos[1].AssigneeID = &me
	h.press("i")
	if got := h.row("t2").AssigneeID; got == nil || *got != "u1" {
		t.Errorf("row assignee = %v, want you", got)
	}
	if h.model.toast == nil || h.model.toast.kind != toastOK {
		t.Errorf("toast = %+v, want no warning for a retry the service had applied", h.model.toast)
	}
}

func TestTakingNeedsAConnectionWhenTheTodosAreKeptHere(t *testing.T) {
	f := sharedAPI()
	srv := httptest.NewServer(f.handler(t))
	h := offlineHarness(t, config.Config{Token: "evt_test", BaseURL: srv.URL})
	h.pick("t2")
	srv.Close()
	h.press("i")
	if toast := h.model.toastText(); !strings.Contains(toast, "could not reach Evado") || !strings.Contains(toast, "nothing changed") {
		t.Errorf("toast = %q, want it to say a connection is needed and nothing changed", toast)
	}
	if h.row("t2").AssigneeID != nil {
		t.Error("an offline take was shown as done")
	}
	if st := h.model.backend.Status(); st.Pending != 0 {
		t.Errorf("pending = %d, want nothing kept to send later", st.Pending)
	}
}

func TestTakingKeptTodosOnlineGoesToTheService(t *testing.T) {
	f := sharedAPI()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	h := offlineHarness(t, config.Config{Token: "evt_test", BaseURL: srv.URL})
	h.pick("t2")
	h.press("i")
	if f.todos[1].AssigneeID == nil || *f.todos[1].AssigneeID != "u1" {
		t.Fatal("the take did not reach the service")
	}
	if got := h.row("t2").AssigneeID; got == nil || *got != "u1" {
		t.Errorf("row assignee = %v, want you", got)
	}
	again := restart(t, config.Config{Token: "evt_test", BaseURL: srv.URL})
	if got := again.row("t2").AssigneeID; got == nil || *got != "u1" {
		t.Errorf("after a restart the assignee = %v, want the claim kept on this computer", got)
	}
}

func TestTakingWithNoAccountSaysOneIsNeeded(t *testing.T) {
	h := offlineHarness(t, config.Config{Local: true})
	ctx := context.Background()
	list, err := h.model.client.CreateList(ctx, "Household")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.model.client.CreateTodos(ctx, api.NewTodo{Title: "Take out the bins", ListID: list.ID}); err != nil {
		t.Fatal(err)
	}
	h.press("r")
	h.pick(h.model.todos.all[0].ID)
	h.press("i")
	if toast := h.model.toastText(); !strings.Contains(toast, "needs an Evado account") {
		t.Errorf("toast = %q, want it to say an account is needed", toast)
	}
}
