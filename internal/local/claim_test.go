package local

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// serveAssignment is the service's claim and release: each changes the
// todo only from the state it expects, a retry that already took effect
// answers the todo unchanged, and losing names who has the todo, except
// for a done todo, where the service sends neither.
func (s *service) serveAssignment(w http.ResponseWriter, r *http.Request) {
	write := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	seg := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	t := s.todos[seg[1]]
	if t == nil || t.DeletedAt != nil {
		write(http.StatusNotFound, map[string]any{"error": "not found", "code": "not_found"})
		return
	}
	for _, l := range s.lists {
		if l.ID == t.ListID && l.IsPersonal {
			write(http.StatusBadRequest, map[string]any{"error": "only todos in shared lists can be claimed or returned to Anyone", "code": "personal_list"})
			return
		}
	}
	if t.IsCompleted {
		write(http.StatusConflict, map[string]any{"error": "this todo is already done", "code": "todo_completed"})
		return
	}
	var holder *api.Member
	for i := range s.members {
		if t.AssigneeID != nil && s.members[i].UserID == *t.AssigneeID {
			holder = &s.members[i]
		}
	}
	me := s.user.ID
	mine := t.AssigneeID != nil && *t.AssigneeID == me
	switch claim := seg[2] == "claim"; {
	case claim && mine, !claim && t.AssigneeID == nil:
	case claim && holder == nil:
		t.AssigneeID = &me
		t.UpdatedAt = s.tick()
	case claim:
		write(http.StatusConflict, map[string]any{"error": "someone else already claimed this todo", "code": "already_claimed", "todo": t.Todo, "assignee": holder})
		return
	case mine:
		t.AssigneeID = nil
		t.UpdatedAt = s.tick()
	default:
		write(http.StatusConflict, map[string]any{"error": "this todo isn't assigned to you", "code": "not_assigned_to_you", "todo": t.Todo, "assignee": holder})
		return
	}
	s.claims++
	write(http.StatusOK, map[string]any{"todo": t.Todo})
}

// sharedAccount signs a backend in to an account with a shared list, Work,
// that it shares with sam, and one todo in it that is Anyone's, with a
// step, already synced.
func sharedAccount(t *testing.T) (*service, *Backend, *api.Client) {
	t.Helper()
	svc, url := newService(t)
	svc.lists = append(svc.lists, api.List{ID: "w1", Name: "Work", Role: "owner"})
	svc.members = []api.Member{
		{UserID: "u1", Email: "me@example.com", Role: "owner"},
		{UserID: "u2", Username: strp("sam"), Email: "s•••@example.com", Role: "member"},
	}
	svc.todos["t1"] = &wireTodo{
		Todo:      api.Todo{ID: "t1", ListID: "w1", Title: "Take out the bins", CreatedAt: svc.clock, UpdatedAt: svc.tick()},
		Checklist: []api.ChecklistItem{{ID: "s1", TodoID: "t1", Title: "Recycling first", CreatedAt: svc.clock}},
	}
	b := open(t, "", "")
	ctx := context.Background()
	if _, err := b.SignIn(ctx, "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	return svc, b, b.Client()
}

// assignee is who the copy kept here says has a todo, "" for Anyone.
func assignee(t *testing.T, c *api.Client, id string) string {
	t.Helper()
	got, err := c.GetTodo(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.AssigneeID == nil {
		return ""
	}
	return *got.AssigneeID
}

func TestClaimAndReleaseGoToTheServiceAndAreKeptHere(t *testing.T) {
	svc, b, c := sharedAccount(t)
	ctx := context.Background()

	got, err := c.ClaimTodo(ctx, "t1")
	if err != nil || got.AssigneeID == nil || *got.AssigneeID != "u1" {
		t.Fatalf("claim = %+v, %v", got, err)
	}
	if *svc.todos["t1"].AssigneeID != "u1" {
		t.Error("the service did not record the claim")
	}
	if assignee(t, c, "t1") != "u1" {
		t.Error("the claim the service granted is not kept here")
	}
	// The answer is the service's own state, not a change of this
	// computer's to send it later.
	if st := b.Status(); st.Pending != 0 {
		t.Errorf("pending = %d after a claim, want 0", st.Pending)
	}
	steps, err := c.ListChecklist(ctx, "t1")
	if err != nil || len(steps) != 1 {
		t.Errorf("checklist = %+v %v; the answer has none, and the one kept here must stay", steps, err)
	}

	// A retry the service already applied changes nothing.
	again, err := c.ClaimTodo(ctx, "t1")
	if err != nil || *again.AssigneeID != "u1" || !again.UpdatedAt.Equal(got.UpdatedAt) {
		t.Errorf("repeated claim = %+v, %v; want it unchanged", again, err)
	}

	if got, err := c.ReleaseTodo(ctx, "t1"); err != nil || got.AssigneeID != nil {
		t.Fatalf("release = %+v, %v", got, err)
	}
	if assignee(t, c, "t1") != "" {
		t.Error("the release is not kept here")
	}
	if _, err := c.ReleaseTodo(ctx, "t1"); err != nil {
		t.Errorf("repeated release: %v, want the todo back unchanged", err)
	}
}

func TestLosingAClaimShowsWhoHasItAndKeepsTheirAssignment(t *testing.T) {
	for _, tc := range []struct {
		name     string
		holder   string
		claim    bool
		code     string
		who      string
		reassign func(*service)
	}{
		{name: "someone took it first", holder: "u2", claim: true, code: api.CodeAlreadyClaimed, who: "@sam"},
		{name: "reassigned before the release", holder: "u2", code: api.CodeNotAssignedToYou, who: "@sam"},
		{name: "held by someone who left", holder: "gone", code: api.CodeNotAssignedToYou},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, b, c := sharedAccount(t)
			// Another device changed it, and this computer has not synced.
			svc.todos["t1"].AssigneeID = &tc.holder
			svc.todos["t1"].UpdatedAt = svc.tick()

			call := c.ReleaseTodo
			if tc.claim {
				call = c.ClaimTodo
			}
			_, err := call(context.Background(), "t1")
			var conflict *api.AssignmentConflict
			if !errors.As(err, &conflict) || api.CodeOf(err) != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
			if conflict.Holder() != tc.who || conflict.Todo == nil {
				t.Errorf("holder = %q, todo %+v; want %q and the todo", conflict.Holder(), conflict.Todo, tc.who)
			}
			if assignee(t, c, "t1") != tc.holder {
				t.Error("the todo kept here was not brought up to date from the answer")
			}
			if b.Status().Pending != 0 || *svc.todos["t1"].AssigneeID != tc.holder {
				t.Error("losing must change nothing on either side")
			}
		})
	}
}

func TestClaimingADoneTodoChangesNothing(t *testing.T) {
	svc, _, c := sharedAccount(t)
	svc.todos["t1"].IsCompleted = true
	_, err := c.ClaimTodo(context.Background(), "t1")
	var conflict *api.AssignmentConflict
	if !errors.As(err, &conflict) || api.CodeOf(err) != api.CodeTodoCompleted || conflict.Todo != nil {
		t.Fatalf("err = %v, want todo_completed with no todo, as the service sends it", err)
	}
	if assignee(t, c, "t1") != "" {
		t.Error("a refused claim changed the todo kept here")
	}
}

func TestClaimNeedsTheServiceToPickAWinner(t *testing.T) {
	svc, b, c := sharedAccount(t)
	ctx := context.Background()
	svc.down.Store(true)
	_, err := c.ClaimTodo(ctx, "t1")
	if api.CodeOf(err) != api.CodeOffline || !strings.Contains(err.Error(), "nothing changed") {
		t.Fatalf("err = %v, want the offline code and that nothing changed", err)
	}
	if assignee(t, c, "t1") != "" || b.Status().Pending != 0 {
		t.Error("an offline claim must not be shown or kept to send later")
	}
	svc.down.Store(false)
	if svc.claims != 0 || svc.todos["t1"].AssigneeID != nil {
		t.Error("the claim reached the service after all")
	}
}

func TestClaimWithNoAccountIsRefused(t *testing.T) {
	b := open(t, "", "")
	c := b.Client()
	ctx := context.Background()
	personal := add(t, c, "Mine")
	if _, err := c.ClaimTodo(ctx, personal.ID); api.CodeOf(err) != api.CodePersonalList {
		t.Errorf("personal list: %v, want personal_list", err)
	}
	list, err := c.CreateList(ctx, "Work")
	if err != nil {
		t.Fatal(err)
	}
	made, err := c.CreateTodos(ctx, api.NewTodo{Title: "Ours", ListID: list.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ClaimTodo(ctx, made[0].ID)
	if api.CodeOf(err) != "forbidden" || !strings.Contains(err.Error(), "needs an Evado account") {
		t.Errorf("no account: %v, want it to say an account is needed", err)
	}
	if assignee(t, c, made[0].ID) != "" {
		t.Error("a refused claim changed the todo")
	}
}

func TestChangesNotSentYetReachTheServiceBeforeTheClaim(t *testing.T) {
	svc, _, c := sharedAccount(t)
	ctx := context.Background()

	// Assigned to sam here, while the service still has it as Anyone: the
	// service has to judge the claim against sam's assignment.
	if _, _, err := c.UpdateTodo(ctx, "t1", api.TodoPatch{AssigneeID: api.Val("u2")}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ClaimTodo(ctx, "t1"); api.CodeOf(err) != api.CodeAlreadyClaimed {
		t.Errorf("err = %v, want already_claimed by the assignment made here", err)
	}

	// A todo the service has never seen goes up first, then is claimed.
	made, err := c.CreateTodos(ctx, api.NewTodo{Title: "New here", ListID: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.ClaimTodo(ctx, made[0].ID)
	if err != nil || got.AssigneeID == nil || *got.AssigneeID != "u1" {
		t.Fatalf("claim of a todo made here = %+v, %v", got, err)
	}
	if svc.todos[made[0].ID] == nil || *svc.todos[made[0].ID].AssigneeID != "u1" {
		t.Error("the new todo did not reach the service before the claim")
	}
}
