package tui

import (
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// maxUndo bounds the undo history.
const maxUndo = 20

// undoEntry is one action z can take back.
type undoEntry struct {
	action todoAction
	todo   api.Todo
	// seq is when the action was taken among all changes. It orders the
	// history, and tells entries apart so the answer to taking one back
	// finds it wherever the history has moved it to in the meantime.
	seq int
}

// recordUndo remembers a finished action so z can reverse it. The reversal
// of an undo is not recorded, or z would flip between two states forever.
func (m *Model) recordUndo(msg todoMsg) {
	if msg.undo != 0 {
		return
	}
	var e undoEntry
	switch msg.action {
	case actionCompleted, actionReopened:
		if msg.todo.ID == "" {
			return
		}
		e = undoEntry{action: msg.action, todo: msg.todo}
	case actionDeleted:
		// A todo with no list came from a partial record and cannot be
		// recreated faithfully.
		if msg.deleted == nil || msg.deleted.ListID == "" {
			return
		}
		e = undoEntry{action: msg.action, todo: *msg.deleted}
	default:
		return
	}
	e.seq = msg.order
	if e.seq == 0 {
		e.seq = m.muts.issue()
	}
	// Answers about different todos can come back in either order, so the
	// entry goes where its action was taken rather than on the end.
	i := len(m.undos)
	for i > 0 && m.undos[i-1].seq > e.seq {
		i--
	}
	m.undos = slices.Insert(m.undos, i, e)
	if len(m.undos) > maxUndo {
		m.undos = m.undos[len(m.undos)-maxUndo:]
	}
}

// undoLast reverses the most recent complete, reopen or delete. The API has
// no way to restore a deleted todo, so that one is recreated from what was
// on screen; its steps and history are gone.
//
// The entry stays in the history until the server confirms the reversal,
// so one that fails can be tried again. Only one reversal runs at a time:
// a second z sent alongside would take back the entry below it before
// anyone knew whether the first had worked.
func (m *Model) undoLast() tea.Cmd {
	if m.undoing != 0 {
		return m.warnf("still undoing the last change")
	}
	if len(m.undos) == 0 {
		return m.warnf("nothing to undo")
	}
	e := m.undos[len(m.undos)-1]
	m.undoing = e.seq
	m.busy = true
	// The reversal waits behind any change to the same todo still on its
	// way, like any other change to it.
	switch e.action {
	case actionCompleted:
		return tea.Batch(m.setBusy("undoing"), m.mutate(e.todo.ID, asUndo(e.seq, uncompleteTodo(m.ctx, m.client, e.todo.ID))))
	case actionReopened:
		return tea.Batch(m.setBusy("undoing"), m.mutate(e.todo.ID, asUndo(e.seq, completeTodo(m.ctx, m.client, e.todo.ID))))
	default:
		return tea.Batch(m.setBusy("restoring"), m.mutate(e.todo.ID, asUndo(e.seq, createTodo(m.ctx, m.client, restoreOf(e.todo)))))
	}
}

// settleUndo finishes taking an entry back once the server has answered:
// it leaves the history on success, and stays for another try on failure.
func (m *Model) settleUndo(msg todoMsg) {
	if msg.undo == 0 {
		return
	}
	if msg.undo == m.undoing {
		m.undoing = 0
	}
	if msg.err != nil {
		return
	}
	for i, e := range m.undos {
		if e.seq == msg.undo {
			m.undos = append(m.undos[:i], m.undos[i+1:]...)
			return
		}
	}
}

// restoreOf turns a deleted todo back into a create request.
func restoreOf(t api.Todo) api.NewTodo {
	n := api.NewTodo{
		Title:        t.Title,
		Notes:        t.Notes,
		ListID:       t.ListID,
		AssigneeID:   t.AssigneeID,
		ScheduledDay: t.ScheduledDay,
		DoAt:         rfc3339(t.DoAt),
		DueAt:        rfc3339(t.DueAt),
		RemindAt:     rfc3339(t.RemindAt),
	}
	if t.IsCompleted {
		done := true
		n.IsCompleted = &done
		n.CompletedAt = rfc3339(t.CompletedAt)
	}
	return n
}

func rfc3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
