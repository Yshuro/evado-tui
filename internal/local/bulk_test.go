package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestApplyManyWritesOnceAndReportsEachTodo(t *testing.T) {
	ctx := context.Background()
	b := open(t, "", "")
	c := b.Client()
	one, two, three := add(t, c, "one"), add(t, c, "two"), add(t, c, "three")
	before := b.writes

	got := b.ApplyMany([]string{one.ID, "missing-id", two.ID, three.ID}, Change{Patch: api.TodoPatch{IsCompleted: api.BoolVal(true)}})
	if len(got) != 4 {
		t.Fatalf("got %d outcomes, want one per id", len(got))
	}
	if !api.IsNotFound(got[1].Err) {
		t.Errorf("unknown id: err = %v, want not found", got[1].Err)
	}
	for _, i := range []int{0, 2, 3} {
		if got[i].Err != nil || !got[i].Todo.IsCompleted {
			t.Errorf("outcome %d = %+v, want the todo completed", i, got[i])
		}
	}
	if n := b.writes - before; n != 1 {
		t.Errorf("batch wrote the file %d times, want once", n)
	}

	// What was acknowledged is what a restart finds.
	again, err := Open(b.path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	open, _ := again.Client().ListTodos(ctx, api.TodoFilter{Status: api.StatusOpen})
	if len(open) != 0 {
		t.Errorf("%d todos open after reopening, want all three done", len(open))
	}
}

func TestApplyManyDeletesAndSkipsWritingWhenNothingChanged(t *testing.T) {
	b := open(t, "", "")
	c := b.Client()
	one, two := add(t, c, "one"), add(t, c, "two")

	before := b.writes
	if got := b.ApplyMany([]string{"nope"}, Change{Delete: true}); got[0].Err == nil {
		t.Error("deleting an unknown todo succeeded")
	}
	if b.writes != before {
		t.Error("a batch that changed nothing still wrote the file")
	}

	for _, o := range b.ApplyMany([]string{one.ID, two.ID}, Change{Delete: true}) {
		if o.Err != nil || o.Todo.ID == "" {
			t.Errorf("delete outcome = %+v", o)
		}
	}
	if b.writes != before+1 {
		t.Errorf("deleting two todos wrote %d times, want once", b.writes-before)
	}
	if got := titles(t, c); len(got) != 0 {
		t.Errorf("after deleting: %v", got)
	}
}

func TestApplyManyReportsEveryChangeWhenTheSaveFails(t *testing.T) {
	b := open(t, "", "")
	c := b.Client()
	one, two := add(t, c, "one"), add(t, c, "two")

	// A file where the directory should be makes the write fail.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	b.path = filepath.Join(blocker, "todos.json")

	got := b.ApplyMany([]string{one.ID, "missing-id", two.ID}, Change{Patch: api.TodoPatch{IsCompleted: api.BoolVal(true)}})
	if got[0].Err == nil || got[2].Err == nil {
		t.Errorf("changed todos = %v, %v, want the save failure on both", got[0].Err, got[2].Err)
	}
	if !api.IsNotFound(got[1].Err) {
		t.Errorf("unknown id: err = %v, want it to keep its own error", got[1].Err)
	}
}

func TestApplyManyMakesTheNextOccurrence(t *testing.T) {
	b := open(t, "", "")
	todo := add(t, b.Client(), "Water the plants every week")
	got := b.ApplyMany([]string{todo.ID}, Change{Patch: api.TodoPatch{IsCompleted: api.BoolVal(true)}})
	if got[0].Err != nil || got[0].Next == nil || got[0].Next.Title != "Water the plants" {
		t.Errorf("outcome = %+v, want the next occurrence", got[0])
	}
}
