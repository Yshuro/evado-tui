package local

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// The service's rules for reminders, repeating todos and members who leave
// a list, as they were checked on its staging server. Each test fails if the
// todos kept here stop following them.

// aheadOfService is a backend tied to the stand-in service, with its clock after
// the service's so its edits are never taken for older ones.
func aheadOfService(t *testing.T) (*service, *Backend, *api.Client) {
	t.Helper()
	svc, url := newService(t)
	b := open(t, "", "")
	b.now = func() time.Time { return time.Date(2031, 1, 1, 12, 0, 0, 0, time.UTC) }
	if _, err := b.SignIn(context.Background(), "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	return svc, b, b.Client()
}

func mustSync(t *testing.T, b *Backend) Result {
	t.Helper()
	res, err := b.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func get(t *testing.T, c *api.Client, id string) api.Todo {
	t.Helper()
	todo, err := c.GetTodo(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return todo
}

func stamp3339(at time.Time) json.RawMessage { return api.Val(at.UTC().Format(time.RFC3339)) }

// -- reminders ----------------------------------------------------------------

func TestANewReminderTimeRearmsTheReminder(t *testing.T) {
	ctx := context.Background()
	svc, b, c := aheadOfService(t)
	at := time.Date(2030, 6, 1, 9, 0, 0, 0, time.UTC)
	made, err := c.CreateTodos(ctx, api.NewTodo{Title: "Call the bank", RemindAt: strp(at.Format(time.RFC3339))})
	if err != nil {
		t.Fatal(err)
	}
	id := made[0].ID
	mustSync(t, b)
	svc.fire(id)
	mustSync(t, b)
	if get(t, c, id).RemindedAt == nil {
		t.Fatal("the delivered reminder did not come down")
	}

	// The same time again leaves it delivered.
	if _, _, err := c.UpdateTodo(ctx, id, api.TodoPatch{RemindAt: stamp3339(at)}); err != nil {
		t.Fatal(err)
	}
	if get(t, c, id).RemindedAt == nil {
		t.Error("setting the same reminder time again re-armed it here; the service leaves it delivered")
	}

	// A new time re-arms it, here and on the service.
	later := at.Add(time.Hour)
	if _, _, err := c.UpdateTodo(ctx, id, api.TodoPatch{RemindAt: stamp3339(later)}); err != nil {
		t.Fatal(err)
	}
	if got := get(t, c, id); got.RemindedAt != nil || !got.RemindAt.Equal(later) {
		t.Errorf("after a new reminder time: remindAt %v remindedAt %v, want %v and not yet sent", got.RemindAt, got.RemindedAt, later)
	}
	mustSync(t, b)
	if got := get(t, c, id); got.RemindedAt != nil || svc.todos[id].RemindedAt != nil {
		t.Errorf("after the sync: remindedAt here %v, on the service %v, want both unsent", got.RemindedAt, svc.todos[id].RemindedAt)
	}

	// A deadline set alone moves the reminder to it, and re-arms it.
	svc.fire(id)
	mustSync(t, b)
	due := at.AddDate(0, 0, 3)
	if _, _, err := c.UpdateTodo(ctx, id, api.TodoPatch{DueAt: stamp3339(due)}); err != nil {
		t.Fatal(err)
	}
	if got := get(t, c, id); got.RemindedAt != nil || got.RemindAt == nil || !got.RemindAt.Equal(due) {
		t.Errorf("after a deadline: remindAt %v remindedAt %v, want the deadline and not yet sent", got.RemindAt, got.RemindedAt)
	}
	mustSync(t, b)
	if up := svc.todos[id]; up.RemindAt == nil || !up.RemindAt.Equal(due) || up.RemindedAt != nil {
		t.Errorf("the service reminds at %v, sent %v, want the deadline and not yet sent", up.RemindAt, up.RemindedAt)
	}
}

// A reminder the service delivered at its old time must not show as sent
// here while the new time is still on its way up.
func TestPendingReminderTimeOutlivesAPulledDelivery(t *testing.T) {
	now := time.Now()
	b := &Backend{now: time.Now}
	b.st = fresh(now)
	list := b.st.Lists[0].ID
	old, set := now.Add(-time.Hour), now.Add(time.Hour)
	b.st.Todos = []Todo{{Dirty: []string{fRemind}, Todo: api.Todo{
		ID: "x", ListID: list, Title: "Pay rent", RemindAt: &set, UpdatedAt: now,
	}}}
	ans := syncAnswer{Todos: []wireTodo{{Todo: api.Todo{
		ID: "x", ListID: list, Title: "Pay rent", RemindAt: &old, RemindedAt: &old, UpdatedAt: now.Add(time.Second),
	}}}}
	b.apply(ans, map[string]bool{})
	if got := b.st.Todos[0]; got.RemindedAt != nil || !got.RemindAt.Equal(set) {
		t.Errorf("after the pull: remindAt %v remindedAt %v, want the new time, not yet sent", got.RemindAt, got.RemindedAt)
	}
}

// -- repeating todos ---------------------------------------------------------

// stepsOf is the titles of a checklist, each marked done or open.
func stepsOf(steps []api.ChecklistItem) string {
	var out []string
	for _, s := range steps {
		mark := "open"
		if s.CompletedAt != nil {
			mark = "done"
		}
		out = append(out, s.Title+":"+mark)
	}
	return strings.Join(out, ",")
}

func addSteps(t *testing.T, c *api.Client, id string, titles ...string) []api.ChecklistItem {
	t.Helper()
	var out []api.ChecklistItem
	for _, title := range titles {
		item, err := c.AddChecklistItem(context.Background(), id, title)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, item)
	}
	return out
}

func checklist(t *testing.T, c *api.Client, id string) []api.ChecklistItem {
	t.Helper()
	items, err := c.ListChecklist(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// openRepeats is the open todos with a title, which is how many next
// occurrences exist.
func openRepeats(t *testing.T, c *api.Client, title string) []api.Todo {
	t.Helper()
	todos, err := c.ListTodos(context.Background(), api.TodoFilter{Status: api.StatusOpen, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	var out []api.Todo
	for _, todo := range todos {
		if todo.Title == title {
			out = append(out, todo)
		}
	}
	return out
}

func TestNextOccurrenceCopiesTheChecklistWithNoAccount(t *testing.T) {
	ctx := context.Background()
	b := open(t, "", "")
	c := b.Client()
	todo := add(t, c, "Water the plants every week")
	steps := addSteps(t, c, todo.ID, "ferns", "cactus")
	if _, err := c.ToggleChecklistItem(ctx, steps[0]); err != nil {
		t.Fatal(err)
	}
	_, next, err := c.CompleteTodo(ctx, todo.ID)
	if err != nil || next == nil {
		t.Fatalf("complete: next %v err %v, want the next occurrence", next, err)
	}
	copied := checklist(t, c, next.ID)
	if got := stepsOf(copied); got != "ferns:open,cactus:open" {
		t.Errorf("next occurrence's steps = %s, want the same titles in order, all open", got)
	}
	for i, s := range copied {
		if s.ID == steps[i].ID || s.TodoID != next.ID {
			t.Errorf("copied step %+v, want a new id belonging to the next occurrence", s)
		}
	}
	if got := stepsOf(checklist(t, c, todo.ID)); got != "ferns:done,cactus:open" {
		t.Errorf("the completed todo's steps = %s, want them as they were", got)
	}

	// Reopening and completing again makes no second one, as on the service.
	if _, err := c.UncompleteTodo(ctx, todo.ID); err != nil {
		t.Fatal(err)
	}
	if _, again, err := c.CompleteTodo(ctx, todo.ID); err != nil || again != nil {
		t.Errorf("completing again: next %v err %v, want none", again, err)
	}
	if n := len(openRepeats(t, c, "Water the plants")); n != 1 {
		t.Errorf("%d open occurrences, want 1", n)
	}
}

// With an account the service makes the next occurrence, once, and it
// comes down with its checklist.
func TestServiceMakesTheOnlyNextOccurrence(t *testing.T) {
	ctx := context.Background()
	svc, b, c := aheadOfService(t)
	todo := add(t, c, "Water the plants every week")
	addSteps(t, c, todo.ID, "ferns", "cactus")
	mustSync(t, b)

	svc.down.Store(true)
	if _, next, err := c.CompleteTodo(ctx, todo.ID); err != nil || next != nil {
		t.Fatalf("complete offline: next %v err %v, want none made here", next, err)
	}
	svc.down.Store(false)
	mustSync(t, b)
	mustSync(t, b)
	if len(svc.successor) != 1 || len(svc.todos) != 2 {
		t.Fatalf("service holds %d todos and %d next occurrences, want 2 and 1", len(svc.todos), len(svc.successor))
	}
	next := openRepeats(t, c, "Water the plants")
	if len(next) != 1 || next[0].ID != svc.successor[todo.ID] {
		t.Fatalf("open occurrences here = %+v, want only the service's", next)
	}
	if got := stepsOf(checklist(t, c, next[0].ID)); got != "ferns:open,cactus:open" {
		t.Errorf("next occurrence's steps = %s", got)
	}
}

// A change that edits the checklist and completes the todo goes up as one,
// and the next occurrence copies the checklist as edited.
func TestNextOccurrenceCopiesTheChecklistAsEdited(t *testing.T) {
	ctx := context.Background()
	svc, b, c := aheadOfService(t)
	todo := add(t, c, "Weekly review every week")
	steps := addSteps(t, c, todo.ID, "inbox")
	mustSync(t, b)

	svc.down.Store(true)
	renamed := "empty the inbox"
	if _, err := c.UpdateChecklistItem(ctx, steps[0].ID, &renamed, nil); err != nil {
		t.Fatal(err)
	}
	addSteps(t, c, todo.ID, "plan the week")
	if _, _, err := c.CompleteTodo(ctx, todo.ID); err != nil {
		t.Fatal(err)
	}
	svc.down.Store(false)
	svc.got = nil
	mustSync(t, b)
	if len(svc.got) != 1 || svc.got[0]["checklist"] == nil || svc.got[0]["isCompleted"] == nil {
		t.Fatalf("changes sent = %v, want one carrying the checklist and the completion", svc.got)
	}
	next := svc.todos[svc.successor[todo.ID]]
	if next == nil {
		t.Fatal("no next occurrence")
	}
	if got := stepsOf(next.Checklist); got != "empty the inbox:open,plan the week:open" {
		t.Errorf("next occurrence's steps = %s, want the checklist as submitted", got)
	}
}

// The service makes no next occurrence for a todo it first hears of as
// completed, so a repeating todo made and completed before it could go up
// is sent open and completed after. This computer's clock is behind the
// service's here, so the completion has to be stamped after the create.
func TestRepeatingTodoCompletedBeforeItsFirstSyncStillRepeats(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	b := open(t, "", "")
	c := b.Client()
	if _, err := b.SignIn(ctx, "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	svc.down.Store(true)
	todo := add(t, c, "Water the plants every week")
	addSteps(t, c, todo.ID, "ferns")
	if _, next, err := c.CompleteTodo(ctx, todo.ID); err != nil || next != nil {
		t.Fatalf("complete offline: next %v err %v, want it left to the service", next, err)
	}
	svc.down.Store(false)
	mustSync(t, b)

	if up := svc.todos[todo.ID]; up == nil || !up.IsCompleted {
		t.Fatalf("service has %+v, want the todo completed", up)
	}
	nextID := svc.successor[todo.ID]
	if nextID == "" || len(svc.todos) != 2 {
		t.Fatalf("service holds %d todos, next %q, want the todo and one next occurrence", len(svc.todos), nextID)
	}
	if got := stepsOf(svc.todos[nextID].Checklist); got != "ferns:open" {
		t.Errorf("next occurrence's steps = %s", got)
	}
	if next := openRepeats(t, c, "Water the plants"); len(next) != 1 || next[0].ID != nextID {
		t.Errorf("open occurrences here = %+v, want the service's one", next)
	}
	if st := b.Status(); st.Pending != 0 || st.Refused != 0 {
		t.Errorf("status = %+v, want everything sent", st)
	}
}

// Todos completed before there was an account already made their next
// occurrence here. Signing in sends both up and the service makes no other.
func TestSigningInAfterCompletingMakesNoSecondOccurrence(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	b := open(t, "", "")
	c := b.Client()
	todo := add(t, c, "Water the plants every week")
	addSteps(t, c, todo.ID, "ferns")
	if _, next, err := c.CompleteTodo(ctx, todo.ID); err != nil || next == nil {
		t.Fatalf("complete: next %v err %v", next, err)
	}
	if _, err := b.SignIn(ctx, "evt_x", url, false); err != nil {
		t.Fatal(err)
	}
	mustSync(t, b)
	mustSync(t, b)
	if len(svc.todos) != 2 || len(svc.successor) != 0 {
		t.Errorf("service holds %d todos and made %d next occurrences, want the 2 from here and none of its own", len(svc.todos), len(svc.successor))
	}
	if n := len(openRepeats(t, c, "Water the plants")); n != 1 {
		t.Errorf("%d open occurrences, want 1", n)
	}
}

// -- members who leave -------------------------------------------------------

func TestRemovedMembersTodosGoToAnyone(t *testing.T) {
	ctx := context.Background()
	svc, b, c := aheadOfService(t)
	svc.members = []api.Member{{UserID: "u1", Role: "owner"}, {UserID: "u2", Role: "member"}}
	mustSync(t, b)
	todo := add(t, c, "Fix the gate")
	if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{AssigneeID: api.Val("u2")}); err != nil {
		t.Fatal(err)
	}
	mustSync(t, b)
	if a := svc.todos[todo.ID].AssigneeID; a == nil || *a != "u2" {
		t.Fatalf("service assignee = %v, want u2", a)
	}

	svc.removeMember("u2")
	mustSync(t, b)
	if a := get(t, c, todo.ID).AssigneeID; a != nil {
		t.Errorf("assignee here = %q, want Anyone", *a)
	}
	anyone, err := c.ListTodos(ctx, api.TodoFilter{Assigned: "anyone"})
	if err != nil || len(anyone) != 1 || anyone[0].ID != todo.ID {
		t.Errorf("todos for anyone = %+v %v, want the one u2 had", anyone, err)
	}
	members, err := c.ListMembers(ctx, "p1")
	if err != nil || len(members) != 1 {
		t.Errorf("members = %+v %v, want u2 gone", members, err)
	}
}

// An assignment made here to someone who has since left the list would be
// refused with everything else in the change, so it goes to Anyone, as the
// service did with that person's other todos.
func TestAssignmentToSomeoneWhoLeftDoesNotHoldBackTheChange(t *testing.T) {
	ctx := context.Background()
	svc, b, c := aheadOfService(t)
	svc.members = []api.Member{{UserID: "u1", Role: "owner"}, {UserID: "u2", Role: "member"}}
	todo := add(t, c, "Fix the gate")
	mustSync(t, b)

	// They leave before this computer hears of it.
	svc.removeMember("u2")
	if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{Title: api.Val("Fix the garden gate"), AssigneeID: api.Val("u2")}); err != nil {
		t.Fatal(err)
	}
	res := mustSync(t, b)
	if up := svc.todos[todo.ID]; up.Title != "Fix the garden gate" || up.AssigneeID != nil {
		t.Errorf("service has %q assigned %v, want the rename and Anyone", up.Title, up.AssigneeID)
	}
	if got := get(t, c, todo.ID); got.AssigneeID != nil {
		t.Errorf("assignee here = %q, want Anyone", *got.AssigneeID)
	}
	if st := b.Status(); st.Pending != 0 || st.Refused != 0 || len(res.Refused) != 0 {
		t.Errorf("status = %+v, sync refused %v, want nothing held back or reported", st, res.Refused)
	}
}
