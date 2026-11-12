package local

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// A todo edited here while offline and changed on the service meanwhile is
// merged field by field when the two meet. Each test edits one todo on both
// sides and checks what both sides hold afterwards and what the sync
// reported.

// offlineWith is a backend synced with the stand-in service, holding one
// todo both sides know, with the network then gone. The backend's clock is
// the real one, behind the service's, as a computer's often is.
func offlineWith(t *testing.T, title string) (*service, *Backend, *api.Client, api.Todo) {
	t.Helper()
	svc, url := newService(t)
	b := syncedAt(t, filepath.Join(t.TempDir(), "todos.json"), url)
	c := b.Client()
	todo := add(t, c, title)
	mustSync(t, b)
	svc.down.Store(true)
	return svc, b, c, todo
}

// edit changes a todo on the service, as another device would, stamped
// with the service's clock.
func (s *service) edit(id string, change func(*wireTodo)) {
	change(s.todos[id])
	s.todos[id].UpdatedAt = s.tick()
}

// pushes counts the changes to a todo the service received.
func (s *service) pushes(id string) int {
	n := 0
	for _, ch := range s.got {
		var got string
		_ = json.Unmarshal(ch["id"], &got)
		if got == id {
			n++
		}
	}
	return n
}

func TestAnOfflineEditAndAServiceChangeToAnotherFieldBothSurvive(t *testing.T) {
	ctx := context.Background()
	svc, b, c, todo := offlineWith(t, "Pay rent")
	if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{Title: api.Val("Pay the rent")}); err != nil {
		t.Fatal(err)
	}
	svc.edit(todo.ID, func(w *wireTodo) { w.Notes = strp("from the phone") })

	svc.down.Store(false)
	svc.got = nil
	res := mustSync(t, b)
	if up := svc.todos[todo.ID]; up.Title != "Pay the rent" || up.Notes == nil || *up.Notes != "from the phone" {
		t.Errorf("service has %q with notes %v, want the rename and the phone's notes", up.Title, up.Notes)
	}
	if got := get(t, c, todo.ID); got.Title != "Pay the rent" || got.Notes == nil || *got.Notes != "from the phone" {
		t.Errorf("kept here: %q with notes %v, want the rename and the phone's notes", got.Title, got.Notes)
	}
	if len(res.Replaced) != 0 || b.Status().Pending != 0 {
		t.Errorf("replaced %v, pending %d, want nothing", res.Replaced, b.Status().Pending)
	}
	// The rename went up stamped before the phone's change, and the service
	// skipped it; it went up again stamped after, and was taken.
	if n := svc.pushes(todo.ID); n != 2 {
		t.Errorf("the rename went up %d times, want 2", n)
	}
	before := len(svc.got)
	if mustSync(t, b); len(svc.got) != before {
		t.Errorf("a second sync sent %d more changes", len(svc.got)-before)
	}
}

// The issue that started this: someone leaving a shared list sends their
// todos to Anyone, which stamps them after an edit made here a moment
// before, and used to undo it.
func TestAMemberLeavingDoesNotUndoAnOfflineRename(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	svc.members = []api.Member{{UserID: "u1", Role: "owner"}, {UserID: "u2", Role: "member"}}
	b := syncedAt(t, filepath.Join(t.TempDir(), "todos.json"), url)
	c := b.Client()
	todo := add(t, c, "Fix the gate")
	if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{AssigneeID: api.Val("u2")}); err != nil {
		t.Fatal(err)
	}
	mustSync(t, b)

	svc.down.Store(true)
	if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{Title: api.Val("Fix the garden gate")}); err != nil {
		t.Fatal(err)
	}
	svc.removeMember("u2")
	svc.down.Store(false)
	res := mustSync(t, b)
	if up := svc.todos[todo.ID]; up.Title != "Fix the garden gate" || up.AssigneeID != nil {
		t.Errorf("service has %q assigned %v, want the rename and Anyone", up.Title, up.AssigneeID)
	}
	if got := get(t, c, todo.ID); got.Title != "Fix the garden gate" || got.AssigneeID != nil {
		t.Errorf("kept here: %q assigned %v, want the rename and Anyone", got.Title, got.AssigneeID)
	}
	if len(res.Replaced) != 0 {
		t.Errorf("replaced %v, want nothing", res.Replaced)
	}
}

func TestAFieldChangedOnBothSidesGoesToTheNewer(t *testing.T) {
	ctx := context.Background()
	t.Run("the service's", func(t *testing.T) {
		svc, b, c, todo := offlineWith(t, "Pay rent")
		if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{Title: api.Val("Pay the rent"), Notes: api.Val("by Friday")}); err != nil {
			t.Fatal(err)
		}
		svc.edit(todo.ID, func(w *wireTodo) { w.Title = "Rent paid" })

		svc.down.Store(false)
		res := mustSync(t, b)
		if up := svc.todos[todo.ID]; up.Title != "Rent paid" || up.Notes == nil || *up.Notes != "by Friday" {
			t.Errorf("service has %q with notes %v, want its title and the notes from here", up.Title, up.Notes)
		}
		if got := get(t, c, todo.ID); got.Title != "Rent paid" || got.Notes == nil || *got.Notes != "by Friday" {
			t.Errorf("kept here: %q with notes %v, want the service's title and the notes", got.Title, got.Notes)
		}
		if len(res.Replaced) != 1 || res.Replaced[0].String() != "'Pay the rent' (title)" {
			t.Errorf("replaced = %v, want the title reported once", res.Replaced)
		}
		if again := mustSync(t, b); len(again.Replaced) != 0 || b.Status().Pending != 0 {
			t.Errorf("the next sync replaced %v with %d pending, want nothing", again.Replaced, b.Status().Pending)
		}
	})
	t.Run("this computer's", func(t *testing.T) {
		svc, b, c, todo := offlineWith(t, "Pay rent")
		svc.edit(todo.ID, func(w *wireTodo) { w.Title = "Rent paid" })
		// The edit here is made an hour after the service's.
		b.now = func() time.Time { return svc.clock.Add(time.Hour) }
		if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{Title: api.Val("Pay the rent")}); err != nil {
			t.Fatal(err)
		}

		svc.down.Store(false)
		res := mustSync(t, b)
		if up := svc.todos[todo.ID]; up.Title != "Pay the rent" {
			t.Errorf("service has %q, want the newer title from here", up.Title)
		}
		if got := get(t, c, todo.ID); got.Title != "Pay the rent" {
			t.Errorf("kept here: %q, want the newer title", got.Title)
		}
		if len(res.Replaced) != 0 {
			t.Errorf("replaced = %v, want nothing", res.Replaced)
		}
	})
}

func TestADeletionWinsOverAnEditOnTheOtherSide(t *testing.T) {
	ctx := context.Background()
	t.Run("deleted on the service", func(t *testing.T) {
		svc, b, c, todo := offlineWith(t, "Pay rent")
		if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{Title: api.Val("Pay the rent")}); err != nil {
			t.Fatal(err)
		}
		svc.edit(todo.ID, func(w *wireTodo) { at := svc.clock; w.DeletedAt = &at })

		svc.down.Store(false)
		res := mustSync(t, b)
		if _, err := c.GetTodo(ctx, todo.ID); !api.IsNotFound(err) {
			t.Errorf("the todo is still here: %v", err)
		}
		if svc.todos[todo.ID].DeletedAt == nil {
			t.Error("the edit brought the todo back on the service")
		}
		if len(res.Replaced) != 1 || res.Replaced[0].String() != "'Pay the rent' (deleted)" {
			t.Errorf("replaced = %v, want the deletion reported", res.Replaced)
		}
	})
	t.Run("deleted here", func(t *testing.T) {
		svc, b, c, todo := offlineWith(t, "Pay rent")
		if err := c.DeleteTodo(ctx, todo.ID); err != nil {
			t.Fatal(err)
		}
		svc.edit(todo.ID, func(w *wireTodo) { w.Notes = strp("from the phone") })

		svc.down.Store(false)
		res := mustSync(t, b)
		if svc.todos[todo.ID].DeletedAt == nil {
			t.Error("the deletion made here never reached the service")
		}
		b.mu.Lock()
		left := len(b.st.Todos)
		b.mu.Unlock()
		if left != 0 || len(res.Replaced) != 0 {
			t.Errorf("%d todos kept and %v replaced, want the todo gone and nothing reported", left, res.Replaced)
		}
	})
}

func TestAChecklistEditMeetsAServiceChange(t *testing.T) {
	ctx := context.Background()
	t.Run("to another field", func(t *testing.T) {
		svc, b, c, todo := offlineWith(t, "Pay rent")
		if _, err := c.AddChecklistItem(ctx, todo.ID, "find the chequebook"); err != nil {
			t.Fatal(err)
		}
		svc.edit(todo.ID, func(w *wireTodo) { w.Title = "Pay the rent" })

		svc.down.Store(false)
		res := mustSync(t, b)
		up := svc.todos[todo.ID]
		if up.Title != "Pay the rent" || len(up.Checklist) != 1 || up.Checklist[0].Title != "find the chequebook" {
			t.Errorf("service has %q with steps %+v, want its title and the step from here", up.Title, up.Checklist)
		}
		if got := get(t, c, todo.ID); got.Title != "Pay the rent" || len(res.Replaced) != 0 {
			t.Errorf("kept here: %q, replaced %v; want the service's title and nothing replaced", got.Title, res.Replaced)
		}
	})
	t.Run("to the checklist", func(t *testing.T) {
		svc, b, c, todo := offlineWith(t, "Pay rent")
		if _, err := c.AddChecklistItem(ctx, todo.ID, "find the chequebook"); err != nil {
			t.Fatal(err)
		}
		svc.edit(todo.ID, func(w *wireTodo) {
			w.Checklist = []api.ChecklistItem{{ID: NewID(), TodoID: todo.ID, Title: "pay online"}}
		})

		svc.down.Store(false)
		res := mustSync(t, b)
		if up := svc.todos[todo.ID]; len(up.Checklist) != 1 || up.Checklist[0].Title != "pay online" {
			t.Errorf("service steps = %+v, want its newer checklist", up.Checklist)
		}
		steps, err := c.ListChecklist(ctx, todo.ID)
		if err != nil || len(steps) != 1 || steps[0].Title != "pay online" {
			t.Errorf("steps here = %+v %v, want the service's", steps, err)
		}
		if len(res.Replaced) != 1 || res.Replaced[0].String() != "'Pay rent' (checklist)" {
			t.Errorf("replaced = %v, want the checklist reported", res.Replaced)
		}
	})
}

// A command run while the TUI's push is in the air writes the file in
// between. Its change, the TUI's offline edit and the service's own all
// end up on both sides.
func TestACommandWritingDuringASyncLosesNothing(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	path := filepath.Join(t.TempDir(), "todos.json")
	tui := syncedAt(t, path, url)
	c := tui.Client()
	todo := add(t, c, "Pay rent")
	mustSync(t, tui)

	svc.down.Store(true)
	if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{Title: api.Val("Pay the rent")}); err != nil {
		t.Fatal(err)
	}
	svc.edit(todo.ID, func(w *wireTodo) { w.Notes = strp("from the phone") })
	svc.down.Store(false)

	due := time.Date(2030, 6, 1, 9, 0, 0, 0, time.UTC)
	svc.during = func() {
		svc.during = nil
		cli, err := Open(path, "", "")
		if err != nil {
			t.Error(err)
			return
		}
		if _, _, err := cli.Client().UpdateTodo(ctx, todo.ID, api.TodoPatch{DueAt: stamp3339(due)}); err != nil {
			t.Error(err)
		}
	}
	res := mustSync(t, tui)

	up := svc.todos[todo.ID]
	if up.Title != "Pay the rent" || up.Notes == nil || *up.Notes != "from the phone" || up.DueAt == nil || !up.DueAt.Equal(due) {
		t.Errorf("service has %q, notes %v, due %v; want the TUI's title, the phone's notes and the command's date", up.Title, up.Notes, up.DueAt)
	}
	again, err := Open(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	got := get(t, again.Client(), todo.ID)
	if got.Title != "Pay the rent" || got.Notes == nil || *got.Notes != "from the phone" || got.DueAt == nil || !got.DueAt.Equal(due) {
		t.Errorf("on disk: %q, notes %v, due %v; want all three changes", got.Title, got.Notes, got.DueAt)
	}
	if tui.Reloads() == 0 || tui.Status().Pending != 0 || len(res.Replaced) != 0 {
		t.Errorf("reloads %d, pending %d, replaced %v; want the command's write read and nothing left", tui.Reloads(), tui.Status().Pending, res.Replaced)
	}
}

// A service that stores a field differently from what was sent, as it
// would by trimming a long title its own way, answers each push with
// something else. With this computer's clock ahead of the service's the
// edit here keeps looking newer, and would go up round after round.
func TestAValueTheServiceStoresDifferentlyDoesNotHoldUpTheSync(t *testing.T) {
	ctx := context.Background()
	svc, b, c, todo := offlineWith(t, "Pay rent")
	b.now = func() time.Time { return svc.clock.Add(time.Hour) }
	if _, _, err := c.UpdateTodo(ctx, todo.ID, api.TodoPatch{Title: api.Val("pay the rent")}); err != nil {
		t.Fatal(err)
	}
	svc.down.Store(false)
	svc.during = func() { svc.todos[todo.ID].Title = strings.ToUpper(svc.todos[todo.ID].Title) }
	syncs := svc.syncs
	svc.got = nil
	if _, err := b.Sync(ctx); err != nil {
		t.Fatalf("sync = %v, want it to finish", err)
	}
	if n := svc.pushes(todo.ID); n != maxPushes || svc.syncs-syncs > maxPushes+1 {
		t.Errorf("pushed %d times in %d calls, want %d at most", n, svc.syncs-syncs, maxPushes)
	}
}

// The service keeps milliseconds, so an edit stamped less than one after
// the version it changes would be taken for no newer and skipped.
func TestAnEditIsStampedAWholeMillisecondAfterTheVersionItChanges(t *testing.T) {
	held := time.Date(2030, 1, 1, 10, 0, 0, 500_000_000, time.UTC)
	for _, c := range []struct{ now, want time.Time }{
		{held.Add(400 * time.Microsecond), held.Add(time.Millisecond)},
		{held.Add(-time.Hour), held.Add(time.Millisecond)},
		{held.Add(2 * time.Millisecond), held.Add(2 * time.Millisecond)},
	} {
		todo := Todo{Todo: api.Todo{UpdatedAt: held}}
		todo.touch(c.now, fTitle)
		if !todo.UpdatedAt.Equal(c.want) {
			t.Errorf("edited at %v: stamped %v, want %v", c.now, todo.UpdatedAt, c.want)
		}
	}
}
