package local

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// pagedStore keeps n todos, a third of them dated, several on the same day
// so that the order depends on its tie-breaks.
func pagedStore(t *testing.T, n int) *api.Client {
	t.Helper()
	c := open(t, "", "").Client()
	ctx := context.Background()
	for i := range n {
		todo, err := c.CreateTodos(ctx, api.NewTodo{Title: fmt.Sprintf("todo %02d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if i%3 == 0 {
			day := fmt.Sprintf("2026-03-%02d", 10+i%4)
			if _, _, err := c.UpdateTodo(ctx, todo[0].ID, api.TodoPatch{ScheduledDay: api.Val(day)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return c
}

func ids(todos []api.Todo) []string {
	out := make([]string, len(todos))
	for i, t := range todos {
		out[i] = t.ID
	}
	return out
}

// Following the cursor visits every todo once, in the order one big read
// gives, whatever the page size and however many todos tie.
func TestAListPagesInTheSameOrderAsOneBigRead(t *testing.T) {
	c := pagedStore(t, 40)
	ctx := context.Background()
	all, err := c.ListTodosPage(ctx, api.TodoFilter{Status: api.StatusAll, Limit: 1000})
	if err != nil || len(all.Todos) != 40 || all.NextCursor != "" {
		t.Fatalf("one read = %d todos, cursor %q, %v; want all 40 and no cursor", len(all.Todos), all.NextCursor, err)
	}
	for _, size := range []int{1, 7, 13, 39, 40} {
		var got []api.Todo
		cursor := ""
		for range 100 {
			page, err := c.ListTodosPage(ctx, api.TodoFilter{Status: api.StatusAll, Limit: size, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, page.Todos...)
			if cursor = page.NextCursor; cursor == "" {
				break
			}
			if len(page.Todos) != size {
				t.Fatalf("size %d: a page with a cursor holds %d", size, len(page.Todos))
			}
		}
		if !slices.Equal(ids(got), ids(all.Todos)) {
			t.Errorf("size %d: pages gave %v, want %v", size, ids(got), ids(all.Todos))
		}
	}
}

func TestADefaultPageIs100AndHasACursor(t *testing.T) {
	c := open(t, "", "").Client()
	ctx := context.Background()
	for range 6 {
		if _, err := c.CreateTodos(ctx, api.NewTodo{Title: strings.TrimSuffix(strings.Repeat("x\n", 20), "\n")}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := c.ListTodosPage(ctx, api.TodoFilter{})
	if err != nil || len(page.Todos) != 100 || page.NextCursor == "" {
		t.Fatalf("default = %d todos, cursor %q, %v; want 100 and a cursor", len(page.Todos), page.NextCursor, err)
	}
	rest, err := c.ListTodosPage(ctx, api.TodoFilter{Cursor: page.NextCursor})
	if err != nil || len(rest.Todos) != 20 || rest.NextCursor != "" {
		t.Errorf("rest = %d todos, cursor %q, %v; want the other 20", len(rest.Todos), rest.NextCursor, err)
	}
}

// The store has nothing to protect from a big page, and the TUI's view of
// everything here asks for one, so unlike the service's it is not cut at 200.
func TestAPageHereIsNotCutAtTwoHundred(t *testing.T) {
	c := open(t, "", "").Client()
	ctx := context.Background()
	for range 11 {
		if _, err := c.CreateTodos(ctx, api.NewTodo{Title: strings.TrimSuffix(strings.Repeat("x\n", 20), "\n")}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := c.ListTodosPage(ctx, api.TodoFilter{Limit: 500})
	if err != nil || len(page.Todos) != 220 || page.NextCursor != "" {
		t.Errorf("page = %d todos, cursor %q, %v; want all 220", len(page.Todos), page.NextCursor, err)
	}
}

// A cursor is a place in the order, not a pointer to a todo, so it still
// works when the todo it was made from has gone.
func TestACursorSurvivesItsTodoBeingDeleted(t *testing.T) {
	c := pagedStore(t, 12)
	ctx := context.Background()
	first, err := c.ListTodosPage(ctx, api.TodoFilter{Limit: 5})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first: %v %q", err, first.NextCursor)
	}
	if err := c.DeleteTodo(ctx, first.Todos[4].ID); err != nil {
		t.Fatal(err)
	}
	rest, err := c.ListTodosPage(ctx, api.TodoFilter{Limit: 100, Cursor: first.NextCursor})
	if err != nil || len(rest.Todos) != 7 {
		t.Fatalf("rest = %d todos, %v; want the other 7", len(rest.Todos), err)
	}
}

func TestAMalformedCursorIsRefusedAsTheServiceDoes(t *testing.T) {
	c := open(t, "", "").Client()
	for _, cursor := range []string{"yesterday", "1:2", "1:2:a b", "a:2:b", strings.Repeat("1", 121) + ":1:a"} {
		_, err := c.ListTodosPage(context.Background(), api.TodoFilter{Cursor: cursor})
		if api.CodeOf(err) != api.CodeInvalidRequest {
			t.Errorf("cursor %q: %v, want invalid_request", cursor, err)
		}
	}
}
