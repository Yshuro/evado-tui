package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestClaimAndReleasePostAndReturnTheTodo(t *testing.T) {
	var paths []string
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/claim") {
			_, _ = w.Write([]byte(`{"todo":{"id":"t1","listId":"l2","assigneeId":"u1","title":"x"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"todo":{"id":"t1","listId":"l2","assigneeId":null,"title":"x"}}`))
	}))
	ctx := context.Background()
	got, err := c.ClaimTodo(ctx, "t1")
	if err != nil || got.AssigneeID == nil || *got.AssigneeID != "u1" {
		t.Fatalf("claim = %+v, %v", got, err)
	}
	got, err = c.ReleaseTodo(ctx, "t1")
	if err != nil || got.AssigneeID != nil {
		t.Fatalf("release = %+v, %v", got, err)
	}
	want := "POST /todos/t1/claim|POST /todos/t1/release"
	if strings.Join(paths, "|") != want {
		t.Errorf("requests = %v, want %s", paths, want)
	}
}

func TestAssignmentConflictCarriesTheTodoAndWhoHasIt(t *testing.T) {
	for _, tc := range []struct {
		name, body, code, holder string
		todo                     bool
	}{
		{
			name: "already claimed", code: CodeAlreadyClaimed, holder: "@sam", todo: true,
			body: `{"error":"someone else already claimed this todo","code":"already_claimed","todo":{"id":"t1","listId":"l2","assigneeId":"u2","title":"x"},"assignee":{"userId":"u2","username":"sam","email":"s•••@example.com","role":"member"}}`,
		},
		{
			name: "reassigned", code: CodeNotAssignedToYou, holder: "s•••@example.com", todo: true,
			body: `{"error":"this todo isn't assigned to you","code":"not_assigned_to_you","todo":{"id":"t1","listId":"l2","assigneeId":"u2","title":"x"},"assignee":{"userId":"u2","username":null,"email":"s•••@example.com","role":"member"}}`,
		},
		{
			name: "held by someone who left", code: CodeNotAssignedToYou, todo: true,
			body: `{"error":"this todo isn't assigned to you","code":"not_assigned_to_you","todo":{"id":"t1","listId":"l2","assigneeId":"gone","title":"x"},"assignee":null}`,
		},
		{
			// Staging sends neither the todo nor an assignee for this one.
			name: "done", code: CodeTodoCompleted,
			body: `{"error":"this todo is already done","code":"todo_completed"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(tc.body))
			}))
			_, err := c.ClaimTodo(context.Background(), "t1")
			var conflict *AssignmentConflict
			if !errors.As(err, &conflict) {
				t.Fatalf("err = %T %v, want an *AssignmentConflict", err, err)
			}
			if CodeOf(err) != tc.code || conflict.Err.StatusCode != http.StatusConflict {
				t.Errorf("code = %q (%d), want %q", CodeOf(err), conflict.Err.StatusCode, tc.code)
			}
			if (conflict.Todo != nil) != tc.todo {
				t.Errorf("todo = %+v, want one: %v", conflict.Todo, tc.todo)
			}
			if conflict.Holder() != tc.holder {
				t.Errorf("holder = %q, want %q", conflict.Holder(), tc.holder)
			}
		})
	}
}

func TestClaimRefusalsThatAreNotConflictsStayPlainErrors(t *testing.T) {
	for status, code := range map[int]string{http.StatusBadRequest: CodePersonalList, http.StatusForbidden: CodeListReadOnly} {
		c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"no","code":"` + code + `"}`))
		}))
		_, err := c.ReleaseTodo(context.Background(), "t1")
		var conflict *AssignmentConflict
		if errors.As(err, &conflict) || CodeOf(err) != code {
			t.Errorf("%d: err = %T %v, want a plain %s", status, err, err, code)
		}
	}
}
