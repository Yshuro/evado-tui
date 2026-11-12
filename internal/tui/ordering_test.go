package tui

import (
	"context"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

// capture sends keys and hands back the commands they produced without
// running them, so a test decides when, and in what order, requests go out
// and their answers come back.
func (h *harness) capture(keys ...string) []tea.Cmd {
	h.t.Helper()
	for _, k := range keys {
		msg := tea.KeyMsg{Type: keyType(k)}
		if msg.Type == tea.KeyRunes {
			msg.Runes = []rune(k)
		}
		h.send(msg)
	}
	cmds := h.cmds
	h.cmds = nil
	return cmds
}

// execute runs commands in the order given, opening batches as it goes, and
// returns what they produced in the order they produced it.
func execute(cmds ...tea.Cmd) []tea.Msg {
	var out []tea.Msg
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			out = append(out, execute(batch...)...)
			continue
		}
		if msg != nil {
			out = append(out, msg)
		}
	}
	return out
}

// reversed runs commands last first, the way a network can take requests
// sent a moment apart and carry them out the other way round.
func reversed(cmds []tea.Cmd) []tea.Msg {
	var out []tea.Msg
	for i := len(cmds) - 1; i >= 0; i-- {
		out = append(out, execute(cmds[i])...)
	}
	return out
}

// deliver hands answers to the model in the order given, then lets anything
// they set off finish normally.
func (h *harness) deliver(msgs ...tea.Msg) {
	h.t.Helper()
	for _, msg := range msgs {
		h.send(msg)
	}
	cmds := h.cmds
	h.cmds = nil
	h.run(cmds...)
}

// -- the detail pane --------------------------------------------------------

func TestDetailIgnoresRepliesForAnEarlierTodo(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.press("g")
	a := h.model.todos.selectedID()
	openA := h.capture("enter")
	if len(openA) == 0 {
		t.Fatal("opening a todo asked for nothing")
	}
	if more := h.capture("esc", "j"); len(more) != 0 {
		t.Fatalf("moving in the list sent %d requests", len(more))
	}
	b := h.model.todos.selectedID()
	if a == b || a != "t1" {
		t.Fatalf("selected %q then %q, want t1 and another", a, b)
	}
	openB := h.capture("enter")

	// B answers first; A's answers turn up afterwards.
	h.deliver(execute(openB...)...)
	h.deliver(execute(openA...)...)

	if got := h.model.todos.selectedID(); got != b {
		t.Errorf("selected = %q, want %q", got, b)
	}
	if got := h.model.todos.detail.ID; got != b {
		t.Errorf("the detail pane shows %q, want %q", got, b)
	}
	for _, s := range h.model.todos.steps {
		if s.TodoID != b {
			t.Errorf("the checklist holds step %q of %q while %q is open", s.Title, s.TodoID, b)
		}
	}
	h.mustNotContain("read the meter")
}

func TestReopeningAnotherTodoClearsTheLastOne(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.press("g", "enter")
	if len(h.model.todos.steps) == 0 {
		t.Fatal("the first todo's steps did not load")
	}
	h.press("esc", "j")
	b := h.model.todos.selectedID()
	h.capture("enter")
	if got := h.model.todos.detail.ID; got != b {
		t.Errorf("before any answer the pane shows %q, want %q", got, b)
	}
	if n := len(h.model.todos.steps); n != 0 {
		t.Errorf("the checklist still holds %d steps of the todo left behind", n)
	}
}

func TestClosingTheDetailDropsItsLateReplies(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	h.press("g")
	open := h.capture("enter")
	h.press("esc")
	// The todo goes away before the requests land, so the detail request
	// fails while the checklist one still answers.
	f.todos = f.todos[1:]
	h.model.toast = nil
	h.deliver(execute(open...)...)

	if h.model.toast != nil {
		t.Errorf("a reply for a closed pane still spoke up: %q", h.model.toastText())
	}
	if n := len(h.model.todos.steps); n != 0 {
		t.Errorf("a reply for a closed pane filled the checklist with %d steps", n)
	}
	if h.model.focus != focusList {
		t.Errorf("focus = %v, want the list", h.model.focus)
	}
}

func TestLateRepliesForTheOpenTodoStillLand(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.press("g")
	open := h.capture("enter")
	h.deliver(reversed(open)...)
	if got := h.model.todos.detail.ID; got != "t1" {
		t.Errorf("detail = %q, want t1", got)
	}
	if n := len(h.model.todos.steps); n != 2 {
		t.Errorf("steps = %d, want 2", n)
	}
}

// -- undo -------------------------------------------------------------------

// refusing makes the fake server fail requests whose method and path end
// match, and nothing else.
func refusing(method, suffix string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		return r.Method == method && strings.HasSuffix(r.URL.Path, suffix)
	}
}

// undoActions lists the undo history, oldest first.
func (h *harness) undoActions() []todoAction {
	var out []todoAction
	for _, e := range h.model.undos {
		out = append(out, e.action)
	}
	return out
}

func TestFailedUndoOfACompletionCanBeRetried(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	h.press("g", "space")
	if !findTodo(f, "t1").IsCompleted || len(h.model.undos) != 1 {
		t.Fatalf("completing left %d undo entries", len(h.model.undos))
	}

	f.refuse = refusing(http.MethodPost, "/uncomplete")
	h.press("z")
	if !findTodo(f, "t1").IsCompleted {
		t.Fatal("the refused undo changed the todo")
	}
	if n := len(h.model.undos); n != 1 {
		t.Fatalf("a failed undo left %d entries, want the one it could not take back", n)
	}
	if !strings.Contains(h.model.toastText(), "fell over") {
		t.Errorf("toast = %q, want the failure", h.model.toastText())
	}

	f.refuse = nil
	h.press("z")
	if findTodo(f, "t1").IsCompleted {
		t.Error("retrying the undo did not reopen the todo")
	}
	if n := len(h.model.undos); n != 0 {
		t.Errorf("a successful undo left %d entries", n)
	}
}

func TestFailedUndoOfAReopeningCanBeRetried(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	h.model.todos.reveal("t3")
	if h.model.todos.selectedID() != "t3" {
		t.Fatal("could not select the completed todo")
	}
	h.press("space")
	if findTodo(f, "t3").IsCompleted {
		t.Fatal("space should reopen the completed todo")
	}

	f.refuse = refusing(http.MethodPost, "/complete")
	h.press("z")
	if findTodo(f, "t3").IsCompleted {
		t.Fatal("the refused undo changed the todo")
	}
	if got := h.undoActions(); len(got) != 1 || got[0] != actionReopened {
		t.Fatalf("history after a failed undo = %v, want the reopening kept", got)
	}

	f.refuse = nil
	h.press("z")
	if !findTodo(f, "t3").IsCompleted {
		t.Error("retrying the undo did not complete the todo again")
	}
	if n := len(h.model.undos); n != 0 {
		t.Errorf("a successful undo left %d entries", n)
	}
}

func TestFailedRestoreOfADeletedTodoCanBeRetried(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	h.press("g", "d", "enter")
	if findTodo(f, "t1") != nil {
		t.Fatal("the todo should be deleted")
	}

	f.refuse = refusing(http.MethodPost, "/todos")
	h.press("z")
	if f.writes != 0 {
		t.Fatalf("writes = %d, want the refused restore to have made nothing", f.writes)
	}
	if got := h.undoActions(); len(got) != 1 || got[0] != actionDeleted {
		t.Fatalf("history after a failed restore = %v, want the deletion kept", got)
	}

	f.refuse = nil
	h.press("z")
	restored := false
	for _, todo := range f.todos {
		restored = restored || todo.Title == "Pay the cable bill"
	}
	if !restored {
		t.Error("retrying the undo did not bring the todo back")
	}
	if n := len(h.model.undos); n != 0 {
		t.Errorf("a successful restore left %d entries", n)
	}
}

func TestUndoWaitsForTheOneBeforeIt(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	h.press("g", "space")
	h.model.todos.reveal("t2")
	h.press("space")
	if got := h.undoActions(); len(got) != 2 {
		t.Fatalf("history = %v, want two completions", got)
	}
	newest := h.model.undos[1].todo.ID

	// Both presses land before the server answers the first, and it fails.
	f.refuse = refusing(http.MethodPost, "/uncomplete")
	first := h.capture("z")
	second := h.capture("z")
	h.deliver(reversed(append(first, second...))...)

	if got := h.undoActions(); len(got) != 2 {
		t.Fatalf("history = %v, want both completions still there", got)
	}
	if got := h.model.undos[1].todo.ID; got != newest {
		t.Errorf("newest entry = %q, want %q", got, newest)
	}
	for _, id := range []string{"t1", "t2"} {
		if !findTodo(f, id).IsCompleted {
			t.Errorf("%s was reopened although every undo failed", id)
		}
	}

	f.refuse = nil
	h.press("z")
	if findTodo(f, newest).IsCompleted {
		t.Errorf("the retried undo did not reopen %s, the newest action", newest)
	}
	if got := h.undoActions(); len(got) != 1 {
		t.Errorf("history = %v, want the older completion left", got)
	}
}

// -- changes to one todo ----------------------------------------------------

// eachBackend runs a test against the service directly and against the
// todos kept on this computer, which answer the same requests in-process.
// Either can carry out two requests sent a moment apart the other way
// round. The todo passed in is open and under the cursor.
func eachBackend(t *testing.T, run func(t *testing.T, h *harness, id string)) {
	t.Run("online", func(t *testing.T) {
		h := newHarness(t, sampleAPI())
		h.press("g")
		run(t, h, h.model.todos.selectedID())
	})
	t.Run("local", func(t *testing.T) {
		h := offlineHarness(t, config.Config{Local: true})
		h.press("n")
		h.typeText("Call Sam")
		h.press("enter")
		sel := h.model.todos.selected()
		if sel == nil || sel.Title != "Call Sam" {
			t.Fatalf("selected %+v, want the todo just made", sel)
		}
		run(t, h, sel.ID)
	})
}

// stored is the todo as the backend holds it.
func (h *harness) stored(id string) api.Todo {
	h.t.Helper()
	got, err := h.model.client.GetTodo(context.Background(), id)
	if err != nil {
		h.t.Fatalf("get %s: %v", id, err)
	}
	return got
}

// shownTodo is the todo as the list shows it.
func (h *harness) shownTodo(id string) api.Todo {
	h.t.Helper()
	for _, todo := range h.model.todos.all {
		if todo.ID == id {
			return todo
		}
	}
	h.t.Fatalf("%s is not in the list", id)
	return api.Todo{}
}

func TestRapidCompleteThenReopenKeepsTheReopen(t *testing.T) {
	eachBackend(t, func(t *testing.T, h *harness, id string) {
		cmds := h.capture("space", "space")
		if h.shownTodo(id).Completed() {
			t.Fatal("the list should show the reopening straight away")
		}
		h.deliver(reversed(cmds)...)

		if h.stored(id).Completed() {
			t.Error("the backend ended up completed: the reopen ran before the completion")
		}
		if h.shownTodo(id).Completed() {
			t.Error("the list ended up completed after complete then reopen")
		}
		got := h.undoActions()
		if len(got) != 2 || got[0] != actionCompleted || got[1] != actionReopened {
			t.Errorf("history = %v, want the completion then the reopening", got)
		}
	})
}

func TestRepeatedReschedulingKeepsTheLastDay(t *testing.T) {
	eachBackend(t, func(t *testing.T, h *harness, id string) {
		want := h.model.now.In(h.model.loc).AddDate(0, 0, 7).Format("2006-01-02")
		cmds := h.capture("T", "w")
		h.deliver(reversed(cmds)...)

		if got := deref(h.stored(id).ScheduledDay); got != want {
			t.Errorf("the backend holds %s, want %s from the later move", got, want)
		}
		if got := deref(h.shownTodo(id).ScheduledDay); got != want {
			t.Errorf("the list shows %s, want %s from the later move", got, want)
		}
	})
}

func TestAnEarlierAnswerDoesNotUndoWhatIsShown(t *testing.T) {
	eachBackend(t, func(t *testing.T, h *harness, id string) {
		cmds := h.capture(">", ">")
		want := deref(h.shownTodo(id).ScheduledDay)
		if want == "" {
			t.Fatal("moving the todo did not show a day")
		}

		// The first move's answer arrives while the second is still to come.
		for _, msg := range execute(cmds[0]) {
			h.send(msg)
		}
		if got := deref(h.shownTodo(id).ScheduledDay); got != want {
			t.Errorf("the answer to the first move put the list back to %s, want %s", got, want)
		}

		rest := h.cmds
		h.cmds = nil
		h.deliver(reversed(append(cmds[1:], rest...))...)
		if got := deref(h.stored(id).ScheduledDay); got != want {
			t.Errorf("the backend holds %s, want %s", got, want)
		}
		if got := deref(h.shownTodo(id).ScheduledDay); got != want {
			t.Errorf("the list shows %s, want %s", got, want)
		}
	})
}

func TestUndoFollowsTheOrderActionsWereTaken(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	h.press("g")
	first := h.capture("space")
	h.model.todos.reveal("t2")
	second := h.capture("space")
	// The second completion is answered before the first.
	h.deliver(reversed(append(first, second...))...)

	if len(h.model.undos) != 2 || h.model.undos[1].todo.ID != "t2" {
		t.Fatalf("history = %+v, want t2's completion newest", h.model.undos)
	}
	h.press("z")
	if findTodo(f, "t2").IsCompleted || !findTodo(f, "t1").IsCompleted {
		t.Error("z should take back t2, the completion made last")
	}
}

// -- bulk actions -----------------------------------------------------------

func TestABulkChangeWaitsForAnEarlierChangeToOneOfItsTodos(t *testing.T) {
	eachBackend(t, func(t *testing.T, h *harness, id string) {
		want := h.model.now.In(h.model.loc).AddDate(0, 0, 1).Format("2006-01-02")
		single := h.capture(">")
		if deref(h.shownTodo(id).ScheduledDay) == want {
			t.Fatal("the single move should land somewhere other than tomorrow")
		}
		h.press("v")
		bulk := h.capture("T")
		// The bulk move is carried out first if it is let go at all.
		h.deliver(reversed(append(single, bulk...))...)

		if got := deref(h.stored(id).ScheduledDay); got != want {
			t.Errorf("the backend holds %s, want %s from the bulk move made last", got, want)
		}
		if got := deref(h.shownTodo(id).ScheduledDay); got != want {
			t.Errorf("the list shows %s, want %s from the bulk move made last", got, want)
		}
		if n := len(h.model.todos.marks); n != 0 {
			t.Errorf("%d marks left after the bulk move", n)
		}
	})
}

func TestAChangeToOneTodoWaitsForABulkChangeBeforeIt(t *testing.T) {
	eachBackend(t, func(t *testing.T, h *harness, id string) {
		h.press("v")
		bulk := h.capture("T")
		h.model.todos.reveal(id)
		single := h.capture(">")
		want := deref(h.shownTodo(id).ScheduledDay)
		tomorrow := h.model.now.In(h.model.loc).AddDate(0, 0, 1).Format("2006-01-02")
		if want == "" || want == tomorrow {
			t.Fatalf("the single move shows %q, want a day other than tomorrow", want)
		}
		h.deliver(reversed(append(bulk, single...))...)

		if got := deref(h.stored(id).ScheduledDay); got != want {
			t.Errorf("the backend holds %s, want %s from the move made last", got, want)
		}
		if got := deref(h.shownTodo(id).ScheduledDay); got != want {
			t.Errorf("the list shows %s, want %s from the move made last", got, want)
		}
	})
}

func TestAnOvertakenBulkChangeStillShowsItsOtherTodos(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.press("g", "v", "v")
	if got := h.model.todos.markedIDs(); len(got) != 2 || got[0] != "t1" {
		t.Fatalf("marked %v, want t1 and one more", got)
	}
	other := h.model.todos.markedIDs()[1]
	bulk := h.capture("T")
	h.model.todos.reveal("t1")
	single := h.capture(">")
	h.deliver(reversed(append(bulk, single...))...)

	// The answer to the move of t1 alone must not be all that is shown.
	want := h.model.now.In(h.model.loc).AddDate(0, 0, 1).Format("2006-01-02")
	if got := deref(h.shownTodo(other).ScheduledDay); got != want {
		t.Errorf("%s shows %q, want %s from the bulk move", other, got, want)
	}
}

// A local bulk action goes through ApplyMany, whose single write is counted
// in internal/local: the file is not observable from here. What is checked
// here is that each todo is still reported on its own.
func TestALocalBulkActionReportsEachTodo(t *testing.T) {
	h := offlineHarness(t, config.Config{Local: true})
	for _, title := range []string{"Call Sam", "Buy stamps"} {
		h.press("n")
		h.typeText(title)
		h.press("enter")
	}
	h.press("g", "v", "v")
	ids := h.model.todos.markedIDs()
	if len(ids) != 2 {
		t.Fatalf("marked %v, want both todos", ids)
	}
	// One goes away behind the list's back, so only the other can change.
	gone, kept := ids[0], ids[1]
	if err := h.model.client.DeleteTodo(context.Background(), gone); err != nil {
		t.Fatal(err)
	}
	h.press("space")

	if !h.stored(kept).Completed() {
		t.Error("the todo still there was not completed")
	}
	if got := h.model.toastText(); !strings.Contains(got, "completed 1 of 2") {
		t.Errorf("toast = %q, want one of two completed", got)
	}
	if n := len(h.model.todos.marks); n != 0 {
		t.Errorf("%d marks left after the bulk action", n)
	}
}
