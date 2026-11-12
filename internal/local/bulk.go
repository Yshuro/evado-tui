package local

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// Change is what a batch does to each todo it names: apply Patch to it, or
// delete it.
type Change struct {
	Patch  api.TodoPatch
	Delete bool
}

// Outcome is what became of one todo in a batch.
type Outcome struct {
	// Todo is the todo after the change, or as it was for a deletion. Next
	// is the following occurrence, when completing made one.
	Todo api.Todo
	Next *api.Todo
	// Err is why the todo was left alone: it is not here, is in a read-only
	// list, or the patch does not suit it. It is an *api.Error, as the same
	// change made one request at a time would give.
	Err error
}

// ApplyMany makes one change to every todo in ids and writes the file once
// for the lot, where a request per todo would write it for each.
//
// The todos are independent. One that cannot be changed is reported in its
// place in the result and does not stop the others, and the same id twice
// is changed twice, in order. The file is written only when something
// changed, and before the batch is answered, so an Outcome without an
// error is on disk. If that write fails, nothing in the batch was saved
// and every todo that was changed reports the failure. As with a single
// request, the changes stay in memory, and the next write will carry them.
func (b *Backend) ApplyMany(ids []string, ch Change) []Outcome {
	var patch map[string]json.RawMessage
	if !ch.Delete {
		raw, _ := json.Marshal(ch.Patch)
		_ = json.Unmarshal(raw, &patch)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Outcome, len(ids))
	changed := false
	for i, id := range ids {
		var body any
		if ch.Delete {
			_, body = b.deleteTodo(id)
		} else {
			_, body = b.patchTodo(id, patch)
		}
		if p, bad := body.(problem); bad {
			out[i].Err = &api.Error{StatusCode: p.status, Code: p.code, Message: p.msg}
			continue
		}
		changed = true
		if v, ok := body.(map[string]any); ok {
			out[i].Todo, _ = v["todo"].(api.Todo)
			if next, ok := v["next"].(api.Todo); ok {
				out[i].Next = &next
			}
		}
	}
	if !changed {
		return out
	}
	if err := b.persist(); err != nil {
		saveErr := &api.Error{StatusCode: http.StatusInternalServerError, Code: "internal", Message: fmt.Sprint("could not save: ", err)}
		for i := range out {
			if out[i].Err == nil {
				out[i] = Outcome{Err: saveErr}
			}
		}
	}
	return out
}
