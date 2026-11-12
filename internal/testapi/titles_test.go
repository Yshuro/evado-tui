package testapi

import (
	"context"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// The service stores title.trim().slice(0, 500) and refuses a step title
// over 500 outright; the fake must, or a test of a long title proves
// nothing.
func TestTitlesAreCutLikeTheService(t *testing.T) {
	ctx := context.Background()
	f := New()
	c := f.Client(t)
	id := f.Todos[0].ID
	long := strings.Repeat("a", 600)

	got, _, err := c.UpdateTodo(ctx, id, api.TodoPatch{Title: api.Val("  " + long)})
	if err != nil || got.Title != strings.Repeat("a", 500) {
		t.Errorf("edit kept %d characters (%v), want 500", len(got.Title), err)
	}
	made, err := c.CreateTodos(ctx, api.NewTodo{Title: strings.Repeat("😀", 300)})
	if err != nil || len(made) != 1 || made[0].Title != strings.Repeat("😀", 250) {
		t.Errorf("create kept %v (%v), want 250 emoji", made, err)
	}
	if _, err := c.AddChecklistItem(ctx, id, long); err == nil {
		t.Error("a step of 600 characters was taken")
	}
	item, err := c.AddChecklistItem(ctx, id, " Call the bank ")
	if err != nil || item.Title != "Call the bank" {
		t.Errorf("step = %q (%v), want it trimmed", item.Title, err)
	}
	if _, err := c.UpdateChecklistItem(ctx, item.ID, &long, nil); err == nil {
		t.Error("a step renamed to 600 characters was taken")
	}
}
