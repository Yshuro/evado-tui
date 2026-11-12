package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReadExportVersionsAndLegacy(t *testing.T) {
	for _, raw := range []string{
		`{"format":"evado-export","version":1,"lists":[],"todos":[],"unknown":{"new":true}}`,
		`{"lists":[],"todos":[],"user":{"unexpected":true},"sessions":[]}`,
	} {
		ex, err := ReadExport(strings.NewReader(raw))
		if err != nil || ex.Format != ExportFormat || ex.Version != 1 {
			t.Fatalf("read %s: %+v %v", raw, ex, err)
		}
	}
	for _, raw := range []string{
		`null`, `[]`, `{"format":"other","lists":[],"todos":[]}`, `{"version":2,"lists":[],"todos":[]}`,
		`{"version":0,"lists":[],"todos":[]}`, `{"lists":null,"todos":[]}`, `{"lists":[],"todos":{}}`,
		`{"lists":[],"todos":[]} {}`, `{"lists":[],"todos":[],"version":null}`,
	} {
		if _, err := ReadExport(strings.NewReader(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, err := ReadExport(strings.NewReader(strings.Repeat(" ", MaxImportBytes+1))); err == nil {
		t.Error("accepted oversized file")
	}
	raw := `{"lists":[],"todos":[` + strings.Repeat(`{},`, MaxImportTodos) + `{}]}`
	if _, err := ReadExportJSON(strings.NewReader(raw)); err == nil {
		t.Error("accepted too many todos")
	}
}

func TestPortableExportPreservesTodosWithoutAccountData(t *testing.T) {
	now := time.Date(2026, 10, 6, 18, 0, 0, 0, time.UTC)
	userID, notes, rawInput, rule := "secret-person", "full notes", "call Friday", "weekly:2"
	ex := Export{
		User: User{ID: userID, Email: "private@example.com"}, ExportedAt: now,
		Lists: []ListSnapshot{{List: List{ID: "work", Name: "Work", OwnerID: userID}, Members: []Member{{UserID: userID}, {UserID: "other-person"}}}},
		Todos: []SyncTodo{{Todo: Todo{ID: "live", ListID: "work", Title: "Call", AssigneeID: &userID, Notes: &notes, RawInput: &rawInput, RecurrenceRule: &rule, IsCompleted: true, CompletedAt: &now, DoAt: &now, DueAt: &now, RemindAt: &now, SortOrder: 9}, Checklist: []ChecklistItem{{ID: "step", TodoID: "live", Title: "Prepare", Position: 3, CompletedAt: &now}}}, {Todo: Todo{ID: "deleted", DeletedAt: &now}}},
	}
	var buf bytes.Buffer
	if err := EncodeTodosExport(&buf, ex); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"secret-person", "other-person", "private@example.com", `"ownerId"`, `"assigneeId"`, `"members"`, `"user"`, `"apiTokens"`, `"notificationChannels"`, `"deleted"`} {
		if strings.Contains(buf.String(), private) {
			t.Errorf("portable export includes %s", private)
		}
	}
	got, err := ReadExport(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Todos) != 1 || !got.Lists[0].Shared {
		t.Fatalf("portable contents: %+v", got)
	}
	todo := got.Todos[0]
	if todo.Notes == nil || *todo.Notes != notes || todo.RawInput == nil || *todo.RawInput != rawInput || !todo.Completed() || todo.SortOrder != 9 || todo.RecurrenceRule == nil || todo.DoAt == nil || todo.DueAt == nil || todo.RemindAt == nil || len(todo.Checklist) != 1 || !todo.Checklist[0].Completed() {
		t.Errorf("lost todo fields: %+v", todo)
	}
}

func TestImportSendsOriginalJSONAndReturnsIncompleteReport(t *testing.T) {
	raw := json.RawMessage("{\n \"format\":\"evado-export\", \"version\":1,\"lists\":[],\"todos\":[{\"title\":7}]}\n")
	calls := 0
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if !bytes.Equal(body, raw) {
			t.Errorf("body changed: %s", body)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/import" || r.URL.Query().Get("format") != "evado" || r.URL.Query().Get("dryRun") != "true" || r.URL.Query().Get("listId") != "a/b & c" || r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("request: %s %s %+v", r.Method, r.URL, r.Header)
		}
		_, _ = io.WriteString(w, `{"format":"evado","dryRun":true,"added":10,"complete":false,"remaining":12,"refusedCount":1,"refused":[{"item":1,"reason":"invalid title"}],"lists":{"created":["Work"],"used":[]},"dropped":{"assignees":1}}`)
	}))
	report, err := c.Import(context.Background(), raw, ImportOptions{DryRun: true, ListID: "a/b & c"})
	if err != nil || report.Complete || report.Remaining != 12 || report.Added != 10 || report.Refused[0].Item != 1 || report.Dropped["assignees"] != 1 || calls != 1 {
		t.Fatalf("report=%+v calls=%d err=%v", report, calls, err)
	}
}

func TestTransferErrorsKeepServerCodes(t *testing.T) {
	for _, status := range []int{403, 500} {
		c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"code":"insufficient_scope","error":"no permission"}`)
		}))
		_, err := c.ExportTodos(context.Background())
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.Code != "insufficient_scope" {
			t.Errorf("export error: %v", err)
		}
		_, err = c.Import(context.Background(), json.RawMessage(`{"lists":[],"todos":[]}`))
		if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
			t.Errorf("import error: %v", err)
		}
	}
}

func TestExportTodosUsesPortableEndpoint(t *testing.T) {
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/me/export/todos" {
			t.Errorf("request=%s %s", r.Method, r.URL)
		}
		_, _ = io.WriteString(w, `{"format":"evado-export","version":1,"lists":[],"todos":[]}`)
	}))
	if _, err := c.ExportTodos(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestReadExportIgnoresAccountAndSharingMetadata(t *testing.T) {
	raw := `{"format":"evado-export","version":1,"user":7,"apiTokens":"ignored","lists":[{"id":"list","name":"Work","members":7,"ownerId":{},"createdAt":"2026-10-06T18:00:00Z"}],"todos":[{"id":"todo","title":"Keep","assigneeId":{"ignored":true},"checklist":[]}]}`
	ex, err := ReadExport(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if ex.User.ID != "" || ex.Lists[0].OwnerID != "" || len(ex.Lists[0].Members) != 0 || ex.Todos[0].AssigneeID == nil || *ex.Todos[0].AssigneeID != "" {
		t.Fatalf("retained account metadata: %+v", ex)
	}
}

func TestPortableExportPreservesLargeSortOrder(t *testing.T) {
	const order = 9007199254740993
	var buf bytes.Buffer
	if err := EncodeTodosExport(&buf, Export{Todos: []SyncTodo{{Todo: Todo{ID: "todo", SortOrder: order}}}}); err != nil {
		t.Fatal(err)
	}
	ex, err := ReadExport(&buf)
	if err != nil || ex.Todos[0].SortOrder != order {
		t.Fatalf("sort order changed: %+v %v", ex, err)
	}
}

func TestPortableExportOrdersChecklistWithoutChangingSnapshot(t *testing.T) {
	steps := []ChecklistItem{{ID: "later", Position: 1}, {ID: "first", Position: 0}, {ID: "also-later", Position: 1}}
	var buf bytes.Buffer
	if err := EncodeTodosExport(&buf, Export{Todos: []SyncTodo{{Todo: Todo{ID: "todo"}, Checklist: steps}}}); err != nil {
		t.Fatal(err)
	}
	ex, err := ReadExport(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"first", "later", "also-later"} {
		if got := ex.Todos[0].Checklist[i].ID; got != want {
			t.Errorf("step %d: got %s, want %s", i, got, want)
		}
	}
	for i, want := range []string{"later", "first", "also-later"} {
		if steps[i].ID != want {
			t.Fatal("export changed the original checklist")
		}
	}
}

type transferTransport func(*http.Request) (*http.Response, error)

func (f transferTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLocalTransferRoutesAndOptions(t *testing.T) {
	calls := 0
	c := New("", WithBaseURL(LocalBaseURL), WithHTTPClient(&http.Client{Transport: transferTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"format":"evado-export","version":1,"lists":[],"todos":[]}`
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path != "/api/v1/local/export" {
				t.Errorf("export path=%s", r.URL.Path)
			}
		case http.MethodPost:
			if r.URL.Path != "/api/v1/local/import" || r.URL.Query().Get("dryRun") != "true" || r.URL.Query().Get("listId") != "local list" || r.URL.Query().Has("format") {
				t.Errorf("import URL=%s", r.URL)
			}
			body = `{"dryRun":true,"complete":true,"added":1}`
		default:
			t.Errorf("method=%s", r.Method)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}))
	if _, err := c.ExportTodos(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := c.ImportLocal(context.Background(), Export{Lists: []ListSnapshot{}, Todos: []SyncTodo{}}, ImportOptions{DryRun: true, ListID: "local list"})
	if err != nil || !result.DryRun || result.Added != 1 || calls != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestExportTodosExceedsImportCountLimit(t *testing.T) {
	count := MaxImportTodos + 1
	raw := `{"format":"evado-export","version":1,"lists":[],"todos":[` + strings.Repeat(`{"title":"Keep"},`, count-1) + `{"title":"Keep"}]}`
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, raw)
	}))
	ex, err := c.ExportTodos(context.Background())
	if err != nil || len(ex.Todos) != count {
		t.Fatalf("export count=%d err=%v", len(ex.Todos), err)
	}
	if _, err := ReadExportJSON(strings.NewReader(raw)); err == nil {
		t.Fatal("import count limit stopped working")
	}
}
