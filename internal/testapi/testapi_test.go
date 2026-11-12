package testapi

import (
	"context"
	"errors"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// The service answers 400 invalid_request with the field named when a
// timestamp has no UTC offset; the fake must too, or a client that forgets
// the offset would pass every test.
func TestPatchRefusesTimestampsWithoutAnOffset(t *testing.T) {
	f := New()
	c := f.Client(t)
	id := f.Todos[0].ID
	bad := []struct {
		path  string
		patch api.TodoPatch
	}{
		{"dueAt", api.TodoPatch{DueAt: api.Val("2026-03-10T15:00")}},
		{"remindAt", api.TodoPatch{RemindAt: api.Val("2026-03-10T15:00:00")}},
		{"doAt", api.TodoPatch{DoAt: api.Val("2026-03-10")}},
		{"scheduledDay", api.TodoPatch{ScheduledDay: api.Val("2026-03-10T15:00:00Z")}},
	}
	for _, b := range bad {
		_, _, err := c.UpdateTodo(context.Background(), id, b.patch)
		var e *api.Error
		if !errors.As(err, &e) || e.Code != api.CodeInvalidRequest || len(e.Fields) != 1 ||
			e.Fields[0].Path != b.path || e.Fields[0].Message != "Invalid datetime" {
			t.Errorf("%s: got %v", b.path, err)
		}
	}
	good := []api.TodoPatch{
		{DueAt: api.Val("2026-03-10T15:00:00Z")},
		{DoAt: api.Val("2026-03-10T00:00:00+01:00"), HasTime: api.BoolVal(false)},
		{DueAt: api.Null, ScheduledDay: api.Val("2026-03-10")},
	}
	for _, p := range good {
		if _, _, err := c.UpdateTodo(context.Background(), id, p); err != nil {
			t.Errorf("%v: %v", p, err)
		}
	}
}
