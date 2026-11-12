package local

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestAnOfflineEditNeverRecreatesATodoTheServiceRemoved(t *testing.T) {
	svc, b, c, todo := offlineWith(t, "Original")
	if _, _, err := c.UpdateTodo(context.Background(), todo.ID, api.TodoPatch{Title: api.Val("Offline edit")}); err != nil {
		t.Fatal(err)
	}
	delete(svc.todos, todo.ID)
	svc.down.Store(false)
	res := mustSync(t, b)
	if svc.todos[todo.ID] != nil {
		t.Fatal("offline edit recreated a removed todo")
	}
	b.mu.Lock()
	reason := b.find(todo.ID).Refused
	b.mu.Unlock()
	if len(res.Refused) != 1 || res.Refused[0] != "Offline edit" || !strings.Contains(reason, "not found") {
		t.Fatalf("removed todo was not refused: %+v", res)
	}
	var guarded bool
	for _, ch := range svc.got {
		if string(ch["id"]) == `"`+todo.ID+`"` {
			_ = json.Unmarshal(ch["mustExist"], &guarded)
		}
	}
	if !guarded {
		t.Fatal("existing edit did not require its server todo")
	}
}

// What the service answers to a title edit for a todo in a list the account
// no longer has. Read from the Worker's source on 2026-10-08 and observed on
// staging on 2026-10-10. Losing the list by being removed or by its deletion
// refuses the edit with the same word; the flag only matters once the todo's
// row is gone.
func TestTheStandInRefusesAnEditInAListTheAccountLost(t *testing.T) {
	edit := func(mustExist bool) map[string]json.RawMessage {
		ch := map[string]any{"id": "x1", "title": "Edited", "updatedAt": "2099-01-01T00:00:00Z"}
		if mustExist {
			ch["mustExist"] = true
		}
		raw := map[string]json.RawMessage{}
		for k, v := range ch {
			raw[k], _ = json.Marshal(v)
		}
		return raw
	}
	for _, lose := range []struct {
		name string
		do   func(svc *service)
		gone bool // the todo's row goes with the list
	}{
		{"left", func(svc *service) { svc.leaveList("trip") }, false},
		{"deleted", func(svc *service) { svc.dropList("trip") }, true},
	} {
		for _, must := range []bool{true, false} {
			svc, _ := newService(t)
			svc.lists = append(svc.lists, api.List{ID: "trip", Name: "Trip", Role: "owner"})
			svc.todos["x1"] = &wireTodo{Todo: api.Todo{ID: "x1", ListID: "trip", Title: "Original"}}
			lose.do(svc)
			got := svc.applyChange(edit(must))
			if must || !lose.gone {
				if got != "not found" || svc.todos["x1"].Title != "Original" {
					t.Errorf("%s, mustExist %v: answer %q, todo %+v, want not found and no edit", lose.name, must, got, svc.todos["x1"])
				}
				continue
			}
			// Without the flag the id reads as a create, in the personal list.
			if got != "" || svc.todos["x1"].ListID != "p1" || svc.todos["x1"].DeletedAt != nil {
				t.Errorf("%s, mustExist %v: answer %q, todo %+v, want a todo made in p1", lose.name, must, got, svc.todos["x1"])
			}
		}
	}
}
