package testapi

import (
	"context"
	"fmt"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func fill(f *API, n int) {
	f.Todos = nil
	for i := range n {
		f.Todos = append(f.Todos, api.Todo{ID: fmt.Sprintf("t%03d", i), ListID: "l1", Title: fmt.Sprintf("todo %d", i)})
	}
}

// Like the service: a page of limit todos, a cursor while more match, and
// none on the last page, so a client always knows whether it has seen all.
func TestListPagesWithACursorUntilNullOnTheLastPage(t *testing.T) {
	f := New()
	fill(f, 25)
	c := f.Client(t)
	ctx := context.Background()

	var ids []string
	cursor := ""
	pages := 0
	for {
		page, err := c.ListTodosPage(ctx, api.TodoFilter{Limit: 10, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, todo := range page.Todos {
			ids = append(ids, todo.ID)
		}
		if page.NextCursor == "" {
			break
		}
		if len(page.Todos) != 10 {
			t.Fatalf("page %d holds %d, a page with a cursor is full", pages, len(page.Todos))
		}
		cursor = page.NextCursor
	}
	if pages != 3 || len(ids) != 25 || ids[10] != "t010" || ids[24] != "t024" {
		t.Errorf("pages = %d, ids = %v, want 3 pages in order with nothing repeated", pages, ids)
	}
}

func TestAListThatEndsOnAPageBoundaryHasNoCursor(t *testing.T) {
	f := New()
	fill(f, 20)
	page, err := f.Client(t).ListTodosPage(context.Background(), api.TodoFilter{Limit: 20})
	if err != nil || len(page.Todos) != 20 || page.NextCursor != "" {
		t.Errorf("page = %d todos, cursor %q, %v, want all 20 and no cursor", len(page.Todos), page.NextCursor, err)
	}
}

func TestACursorKeepsToTheFiltersItIsSentWith(t *testing.T) {
	f := New()
	fill(f, 30)
	for i := range f.Todos {
		if i%3 == 0 {
			f.Todos[i].IsCompleted = true
		}
	}
	c := f.Client(t)
	ctx := context.Background()
	first, err := c.ListTodosPage(ctx, api.TodoFilter{Status: api.StatusOpen, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	next, err := c.ListTodosPage(ctx, api.TodoFilter{Status: api.StatusOpen, Limit: 10, Cursor: first.NextCursor})
	if err != nil || len(next.Todos) != 10 || next.NextCursor != "" {
		t.Fatalf("next = %d todos, cursor %q, %v; want the other 10 open ones", len(next.Todos), next.NextCursor, err)
	}
	for _, todo := range next.Todos {
		if todo.IsCompleted {
			t.Errorf("%s is completed, but the filter was open", todo.ID)
		}
	}
}

func TestListRefusesWhatTheServiceRefuses(t *testing.T) {
	f := New()
	c := f.Client(t)
	ctx := context.Background()
	for name, filter := range map[string]api.TodoFilter{
		"a malformed cursor":    {Cursor: "yesterday"},
		"a cursor of two parts": {Cursor: "1:abc"},
		"a cursor with spaces":  {Cursor: "1:2:a b"},
		"a limit over 200":      {Limit: 201},
	} {
		if _, err := c.ListTodosPage(ctx, filter); api.CodeOf(err) != api.CodeInvalidRequest {
			t.Errorf("%s: %v, want 400 invalid_request", name, err)
		}
	}
	// Cursors in the service's shape are accepted, dated or not.
	for _, cursor := range []string{":1:abc", "1700000000000:1600000000000:abc-1"} {
		if _, err := c.ListTodosPage(ctx, api.TodoFilter{Cursor: cursor}); err != nil {
			t.Errorf("cursor %q: %v", cursor, err)
		}
	}
}
