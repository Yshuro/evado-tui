package ops

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/testapi"
)

func TestListTodosReadsPagesUpToTheLimit(t *testing.T) {
	f := testapi.New()
	crowded(f, 450)
	o := ops(t, f)

	all, err := o.ListTodos(context.Background(), ListInput{Status: "all", Limit: 450})
	if err != nil || len(all.Todos) != 450 || all.NextCursor != "" {
		t.Fatalf("limit 450 = %d todos, next %q, %v; want all 450 and nothing more", len(all.Todos), all.NextCursor, err)
	}
	if all.Todos[449].ID != "t0449" {
		t.Errorf("last = %s, want the pages joined in order", all.Todos[449].ID)
	}
	// The last page asks only for what is left.
	var limits, after []string
	for _, raw := range f.ListQueries {
		q, _ := url.ParseQuery(raw)
		limits = append(limits, q.Get("limit"))
		after = append(after, q.Get("cursor")[strings.LastIndex(q.Get("cursor"), ":")+1:])
	}
	if !slices.Equal(limits, []string{"200", "200", "50"}) || !slices.Equal(after, []string{"", "t0199", "t0399"}) {
		t.Errorf("requests = %q, want pages of 200, 200 and 50, each after the last todo of the one before", f.ListQueries)
	}
}

func TestListTodosSaysWhereItStoppedAndResumesThere(t *testing.T) {
	f := testapi.New()
	crowded(f, 450)
	o := ops(t, f)
	ctx := context.Background()

	first, err := o.ListTodos(ctx, ListInput{Status: "all", Limit: 300})
	if err != nil || len(first.Todos) != 300 || first.NextCursor == "" {
		t.Fatalf("first = %d todos, next %q, %v; want 300 and a cursor", len(first.Todos), first.NextCursor, err)
	}
	rest, err := o.ListTodos(ctx, ListInput{Status: "all", Limit: 300, Cursor: first.NextCursor})
	if err != nil || len(rest.Todos) != 150 || rest.NextCursor != "" {
		t.Fatalf("rest = %d todos, next %q, %v; want the other 150", len(rest.Todos), rest.NextCursor, err)
	}
	if first.Todos[299].ID != "t0299" || rest.Todos[0].ID != "t0300" {
		t.Errorf("seam = %s then %s, want t0299 then t0300 with nothing repeated or skipped", first.Todos[299].ID, rest.Todos[0].ID)
	}
}

func TestListTodosDefaultsToOnePageOf200AndSaysThereIsMore(t *testing.T) {
	f := testapi.New()
	crowded(f, 450)
	got, err := ops(t, f).ListTodos(context.Background(), ListInput{Status: "all"})
	if err != nil || len(got.Todos) != 200 || got.NextCursor == "" {
		t.Fatalf("default = %d todos, next %q, %v; want 200 and a cursor", len(got.Todos), got.NextCursor, err)
	}
}

func TestListTodosPassesOnTheServicesRefusalOfACursor(t *testing.T) {
	_, err := ops(t, testapi.New()).ListTodos(context.Background(), ListInput{Cursor: "not-a-cursor"})
	if ExitCode(err) != ExitRejected {
		t.Errorf("a malformed cursor: exit %d (%v), want the service's refusal, %d", ExitCode(err), err, ExitRejected)
	}
}

// A page that fails part way gives no todos at all, so a script cannot take a
// short list for the whole one.
func TestListTodosFailsWholeWhenALaterPageFails(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom","code":"internal"}`))
			return
		}
		_, _ = w.Write([]byte(`{"todos":[{"id":"a1","listId":"l1","title":"one"}],"nextCursor":":1:a1"}`))
	}))
	t.Cleanup(srv.Close)
	got, err := New(api.New("evt_test", api.WithBaseURL(srv.URL))).ListTodos(context.Background(), ListInput{Limit: 500})
	if ExitCode(err) != ExitServer || len(got.Todos) != 0 {
		t.Errorf("got %d todos and exit %d (%v), want none and a server error", len(got.Todos), ExitCode(err), err)
	}
}

func TestListTodosStopsWhenTheCursorDoesNotMove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"todos":[{"id":"a1","listId":"l1","title":"one"}],"nextCursor":":1:a1"}`))
	}))
	t.Cleanup(srv.Close)
	_, err := New(api.New("evt_test", api.WithBaseURL(srv.URL))).ListTodos(context.Background(), ListInput{Limit: 1000})
	var oe *Error
	if !errors.As(err, &oe) || oe.Code != codeListStalled || ExitCode(err) != ExitServer {
		t.Errorf("a cursor that repeats should be a server error, not a loop: %v", err)
	}
}
