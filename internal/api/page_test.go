package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

func TestListTodosPageSendsTheCursorAndReturnsTheNextOne(t *testing.T) {
	var query url.Values
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{"todos":[{"id":"t1","listId":"l1","title":"x","checklist":[{"id":"c1","todoId":"t1","title":"step"}]}],"nextCursor":"1700:1600:t1"}`))
	}))

	page, err := c.ListTodosPage(context.Background(), TodoFilter{Status: StatusAll, Limit: 200, Cursor: "9:8:t0"})
	if err != nil {
		t.Fatal(err)
	}
	if query.Get("cursor") != "9:8:t0" || query.Get("limit") != "200" || query.Get("status") != "all" {
		t.Errorf("query = %v, want the cursor sent with the same filters", query)
	}
	if len(page.Todos) != 1 || page.NextCursor != "1700:1600:t1" || len(page.Steps["t1"]) != 1 {
		t.Errorf("page = %+v, want the todo, its steps and the next cursor", page)
	}
	if query.Has("since") {
		t.Error("a page of a list must not look like the change feed")
	}
}

func TestListTodosPageNullCursorIsTheLastPage(t *testing.T) {
	for name, body := range map[string]string{
		"null":    `{"todos":[],"nextCursor":null}`,
		"absent":  `{"todos":[]}`,
		"emptied": `{"todos":[],"nextCursor":""}`,
	} {
		c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		page, err := c.ListTodosPage(context.Background(), TodoFilter{})
		if err != nil || page.NextCursor != "" {
			t.Errorf("%s: cursor %q, %v, want none", name, page.NextCursor, err)
		}
	}
}

func TestNoCursorIsSentWhenThereIsNone(t *testing.T) {
	var raw string
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"todos":[]}`))
	}))
	if _, err := c.ListTodos(context.Background(), TodoFilter{Status: StatusOpen}); err != nil {
		t.Fatal(err)
	}
	if raw != "status=open" {
		t.Errorf("query = %q, want the old request unchanged", raw)
	}
}

func TestAMalformedCursorIsTheServicesInvalidRequest(t *testing.T) {
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid request","code":"invalid_request"}`))
	}))
	_, err := c.ListTodosPage(context.Background(), TodoFilter{Cursor: "nope"})
	if CodeOf(err) != CodeInvalidRequest {
		t.Errorf("err = %v, want invalid_request", err)
	}
}
