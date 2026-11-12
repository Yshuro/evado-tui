package ops

import (
	"context"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/testapi"
)

// shared adds an open todo to the fake's shared Work list.
func shared(f *testapi.API, assignee string) *api.Todo {
	t := api.Todo{ID: "ccc444", ListID: "l2", Title: "Take out the bins"}
	if assignee != "" {
		t.AssigneeID = &assignee
	}
	f.Todos = append(f.Todos, t)
	return f.Find("ccc444")
}

func TestClaimTakesAnAnyoneTodoAndARetryChangesNothing(t *testing.T) {
	f := testapi.New()
	shared(f, "")
	o := ops(t, f)
	ctx := context.Background()

	ch, err := o.Claim(ctx, "ccc")
	if err != nil || !ch.Changed || ch.Todo.AssigneeID == nil || *ch.Todo.AssigneeID != testapi.Me {
		t.Fatalf("claim = %+v, %v; want it assigned to me", ch, err)
	}
	again, err := o.Claim(ctx, "ccc")
	if err != nil || again.Changed || *again.Todo.AssigneeID != testapi.Me {
		t.Errorf("a repeated claim = %+v, %v; want the todo unchanged and no error", again, err)
	}

	ch, err = o.Release(ctx, "ccc")
	if err != nil || !ch.Changed || ch.Todo.AssigneeID != nil {
		t.Fatalf("release = %+v, %v; want it back to Anyone", ch, err)
	}
	again, err = o.Release(ctx, "ccc")
	if err != nil || again.Changed || again.Todo.AssigneeID != nil {
		t.Errorf("a repeated release = %+v, %v; want the todo unchanged and no error", again, err)
	}
}

func TestClaimOfADepartedMembersTodoTakesIt(t *testing.T) {
	f := testapi.New()
	shared(f, "left-the-list")
	ch, err := ops(t, f).Claim(context.Background(), "ccc444")
	if err != nil || *ch.Todo.AssigneeID != testapi.Me {
		t.Errorf("claim = %+v, %v; a todo held by someone who left is Anyone's", ch, err)
	}
}

func TestClaimConflictsSayWhoHasItWithExitSix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		assignee string
		done     bool
		claim    bool
		code     string
		holder   string
		message  string
	}{
		{name: "already claimed", assignee: "u2", claim: true, code: api.CodeAlreadyClaimed, holder: "u2", message: "someone else already claimed this todo; sam@example.com has it"},
		{name: "not assigned to you", assignee: "u2", code: api.CodeNotAssignedToYou, holder: "u2", message: "this todo isn't assigned to you; sam@example.com has it"},
		{name: "held by someone who left", assignee: "gone", code: api.CodeNotAssignedToYou, message: "this todo isn't assigned to you; it is assigned to someone who has left the list"},
		{name: "done", done: true, claim: true, code: api.CodeTodoCompleted, message: "this todo is already done; reopen it first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := testapi.New()
			shared(f, tc.assignee).IsCompleted = tc.done
			o := ops(t, f)
			var err error
			if tc.claim {
				_, err = o.Claim(context.Background(), "ccc444")
			} else {
				_, err = o.Release(context.Background(), "ccc444")
			}
			info := Describe(err)
			if info.ExitCode != ExitRejected || info.Code != tc.code {
				t.Fatalf("info = %+v, want exit 6 and %s", info, tc.code)
			}
			if info.Message != tc.message {
				t.Errorf("message = %q, want %q", info.Message, tc.message)
			}
			if tc.holder != "" && (info.Assignee == nil || info.Assignee.UserID != tc.holder) {
				t.Errorf("assignee = %+v, want %s", info.Assignee, tc.holder)
			}
			if tc.holder == "" && info.Assignee != nil {
				t.Errorf("assignee = %+v, want none", info.Assignee)
			}
			if !tc.done && (info.Todo == nil || info.Todo.ID != "ccc444") {
				t.Errorf("todo = %+v, want the todo as it is now", info.Todo)
			}
			if got := f.Find("ccc444").AssigneeID; deref(got) != tc.assignee {
				t.Errorf("assignee after = %v, want it left as %q", deref(got), tc.assignee)
			}
		})
	}
}

func TestClaimRefusesPersonalAndReadOnlyLists(t *testing.T) {
	f := testapi.New()
	f.Lists = append(f.Lists, api.List{ID: "l3", Name: "Frozen", ReadOnly: true})
	f.Todos = append(f.Todos, api.Todo{ID: "ddd555", ListID: "l3", Title: "Old"})
	o := ops(t, f)
	ctx := context.Background()
	if _, err := o.Claim(ctx, "aaa111"); ExitCode(err) != ExitRejected || api.CodeOf(err) != api.CodePersonalList {
		t.Errorf("personal list: %v (exit %d), want personal_list and exit 6", err, ExitCode(err))
	}
	if _, err := o.Release(ctx, "ddd555"); ExitCode(err) != ExitRejected || api.CodeOf(err) != api.CodeListReadOnly {
		t.Errorf("read-only list: %v (exit %d), want list_read_only and exit 6", err, ExitCode(err))
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
