package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/ops"
	"github.com/nerdlabz/evado-tui/internal/testapi"
)

func withShared(f *testapi.API, assignee string) {
	t := api.Todo{ID: "ccc444", ListID: "l2", Title: "Take out the bins"}
	if assignee != "" {
		t.AssigneeID = &assignee
	}
	f.Todos = append(f.Todos, t)
}

func TestClaimAndReleaseSayWhatHappened(t *testing.T) {
	f := testapi.New()
	withShared(f, "")
	for _, step := range []struct{ args, want string }{
		{"claim ccc", "claimed ccc444  Take out the bins\n"},
		{"claim ccc", "already yours ccc444  Take out the bins\n"},
		{"release ccc", "released to Anyone ccc444  Take out the bins\n"},
		{"release ccc", "already Anyone's ccc444  Take out the bins\n"},
	} {
		out, err := run(t, f, "", strings.Fields(step.args)...)
		if err != nil || out != step.want {
			t.Errorf("%s = %q, %v; want %q", step.args, out, err, step.want)
		}
	}
}

func TestClaimJSONSaysWhetherItChanged(t *testing.T) {
	f := testapi.New()
	withShared(f, "")
	for _, want := range []bool{true, false} {
		out, err := run(t, f, "", "claim", "-json", "ccc444")
		if err != nil {
			t.Fatal(err)
		}
		var ch ops.Change
		if err := json.Unmarshal([]byte(out), &ch); err != nil {
			t.Fatalf("%v in %s", err, out)
		}
		if ch.Changed != want || ch.Todo.AssigneeID == nil || *ch.Todo.AssigneeID != testapi.Me {
			t.Errorf("claim = %+v, want changed %v and assigned to me", ch, want)
		}
	}
}

func TestClaimLostIsExitSixNamingWhoHasIt(t *testing.T) {
	f := testapi.New()
	withShared(f, "u2")
	out, err := run(t, f, "", "claim", "-json", "ccc444")
	if out != "" {
		t.Errorf("a refused claim printed %q", out)
	}
	info := describe(err)
	if info.ExitCode != ops.ExitRejected || info.Code != api.CodeAlreadyClaimed {
		t.Fatalf("info = %+v, want exit 6, already_claimed", info)
	}
	if !strings.Contains(errorText(err), "sam@example.com has it") {
		t.Errorf("text = %q, want who has it", errorText(err))
	}
	b, _ := json.Marshal(map[string]any{"error": info})
	var back struct {
		Error struct {
			Todo     *api.Todo   `json:"todo"`
			Assignee *api.Member `json:"assignee"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &back) != nil || back.Error.Todo == nil || back.Error.Assignee == nil || back.Error.Assignee.UserID != "u2" {
		t.Errorf("JSON error = %s, want the todo and its assignee", b)
	}

	_, err = run(t, f, "", "release", "ccc444")
	if ops.ExitCode(err) != ops.ExitRejected || api.CodeOf(err) != api.CodeNotAssignedToYou {
		t.Errorf("release of someone else's todo: %v, want not_assigned_to_you and exit 6", err)
	}
}

func TestClaimNeedsExactlyOneID(t *testing.T) {
	for _, args := range [][]string{{"claim"}, {"release", "a", "b"}} {
		if _, err := run(t, testapi.New(), "", args...); describe(err).ExitCode != ops.ExitUsage {
			t.Errorf("%v: %v, want a usage error", args, err)
		}
	}
}
