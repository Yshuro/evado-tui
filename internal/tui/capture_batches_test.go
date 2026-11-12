package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/local"
)

// A paste of more lines than one request holds is sent in batches, so every
// line becomes a todo, in order, and none is dropped or refused.
func TestCaptureOfManyLinesIsSentInBatches(t *testing.T) {
	ctx := context.Background()
	b, err := local.Open(filepath.Join(t.TempDir(), "todos.json"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	c := b.Client()

	var title strings.Builder
	for i := range 45 {
		fmt.Fprintf(&title, "Task %d\n", i+1)
	}
	msg, ok := createTodo(ctx, c, api.NewTodo{Title: title.String()})().(todoMsg)
	if !ok || msg.err != nil || msg.count != 45 {
		t.Fatalf("capture = %+v, want 45 todos made", msg)
	}
	all, err := c.ListTodos(ctx, api.TodoFilter{})
	if err != nil || len(all) != 45 {
		t.Fatalf("%d todos kept, err %v; want 45", len(all), err)
	}
	for _, t1 := range all {
		if t1.Title == "Task 45" {
			return
		}
	}
	t.Error("the last line was not kept")
}
