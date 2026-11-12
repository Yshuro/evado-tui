package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// The preview beside the list shows the checklist of the todo under the
// cursor. Where the source already sent the steps with the todos, which the
// todos kept on this computer and the service's sync do, they are simply
// there. Anywhere else they are read from the service once the cursor has
// rested on a todo, so holding a movement key does not ask for every row it
// passes. Narrowed service listings omit checklists, so their row counts
// intentionally appear only after that lazy read.

// peekDelay is how long the cursor must rest before the steps are asked
// for. It is a variable so tests do not have to wait for real time.
var peekDelay = 250 * time.Millisecond

// peekMaxAge is how long steps are trusted when the cursor comes back to a
// todo whose own fields have not changed.
const peekMaxAge = 30 * time.Second

// maxKnown bounds the steps kept, so a long session on a large account
// cannot grow without limit.
const maxKnown = 5000

// knownSteps are the steps of one todo as last read. updated is the
// todo's own UpdatedAt then, which tells a changed todo from an unchanged
// one, and failed says the read did not work.
type knownSteps struct {
	items   []api.ChecklistItem
	updated time.Time
	at      time.Time
	failed  bool
}

type (
	// peekDueMsg fires when the cursor has rested. It names the settling it
	// belongs to, so one the cursor has since left is dropped.
	peekDueMsg struct {
		key string
		seq int
	}
	// peekMsg carries steps read for the preview. It names the todo, so it
	// can only ever fill that todo's entry.
	peekMsg struct {
		id      string
		updated time.Time
		items   []api.ChecklistItem
		err     error
	}
)

// holdSteps stores steps for a todo.
func (v *todosView) holdSteps(id string, items []api.ChecklistItem, updated time.Time) {
	if v.known == nil || len(v.known) >= maxKnown {
		v.known = map[string]knownSteps{}
	}
	v.known[id] = knownSteps{items: items, updated: updated, at: v.m.now}
}

// keepSteps takes the steps that came with a read of the todos.
func (v *todosView) keepSteps(steps map[string][]api.ChecklistItem) {
	for id, items := range steps {
		v.holdSteps(id, items, time.Time{})
	}
}

// keepFeedSteps takes the steps carried by change-feed entries.
func (v *todosView) keepFeedSteps(changes []api.SyncTodo) {
	for _, c := range changes {
		switch {
		case c.Tombstoned():
			delete(v.known, c.ID)
		case c.Checklist != nil:
			v.holdSteps(c.ID, c.Checklist, time.Time{})
		}
	}
}

// keepStepsOf takes steps read for the open todo, or changed there.
func (v *todosView) keepStepsOf(id string, items []api.ChecklistItem) {
	v.holdSteps(id, items, time.Time{})
}

// stepCount is how many steps a todo has and how many are done, when known.
func (v *todosView) stepCount(id string) (done, total int) {
	k, ok := v.known[id]
	if !ok || k.failed {
		return 0, 0
	}
	for _, s := range k.items {
		if s.Completed() {
			done++
		}
	}
	return done, len(k.items)
}

// previewShown reports whether the preview pane is on screen beside the
// list, showing the todo under the cursor.
func (v *todosView) previewShown() bool {
	m := v.m
	return m.tab == tabTodos && m.layer == layerNone && !m.inDetail() &&
		v.layout(m.width, m.bodyHeight()).prevW > 0
}

// stale reports whether the steps held for the todo should be read again.
func (v *todosView) stale(t api.Todo) bool {
	k, ok := v.known[t.ID]
	if !ok || k.failed {
		return true
	}
	// The todos kept on this computer are reloaded with their steps after
	// every change, so what they sent is current.
	if v.m.backend != nil {
		return false
	}
	// Steps that came with a list or the feed are as old as that read, and a
	// step can change on another device without the todo's own fields doing
	// so, so they are read again when the cursor returns after a while.
	changed := !k.updated.IsZero() && !k.updated.Equal(t.UpdatedAt)
	return changed || v.m.now.Sub(k.at) > peekMaxAge
}

// watchCursor notices that the cursor has moved to another todo and, if its
// steps are not in hand, asks for them once the cursor has rested. It runs
// after every message, so a move from any key, click or reload is seen.
func (v *todosView) watchCursor() tea.Cmd {
	t := v.selected()
	if t == nil || !v.previewShown() {
		v.peekKey = ""
		return nil
	}
	key := fmt.Sprintf("%s@%d", t.ID, t.UpdatedAt.UnixNano())
	if key == v.peekKey {
		return nil
	}
	v.peekKey = key
	v.peekSeq++
	if !v.stale(*t) {
		return nil
	}
	if peekDelay <= 0 {
		return v.fetchPeek(*t)
	}
	seq := v.peekSeq
	return tea.Tick(peekDelay, func(time.Time) tea.Msg { return peekDueMsg{key: key, seq: seq} })
}

// onPeekDue reads the steps if the cursor is still where it was.
func (v *todosView) onPeekDue(msg peekDueMsg) tea.Cmd {
	t := v.selected()
	if msg.seq != v.peekSeq || t == nil || !v.previewShown() || !v.stale(*t) {
		return nil
	}
	return v.fetchPeek(*t)
}

func (v *todosView) fetchPeek(t api.Todo) tea.Cmd {
	ctx, c, id, updated := v.m.ctx, v.m.client, t.ID, t.UpdatedAt
	return func() tea.Msg {
		items, err := c.ListChecklist(ctx, id)
		return peekMsg{id: id, updated: updated, items: items, err: err}
	}
}

// onPeek files steps read for the preview. A read that was overtaken by
// the todo being opened is dropped: the open pane reads them itself and
// keeps them current as they are edited.
func (v *todosView) onPeek(msg peekMsg) {
	if v.m.inDetail() && v.pane.id == msg.id {
		return
	}
	if msg.err != nil {
		v.holdSteps(msg.id, nil, msg.updated)
		k := v.known[msg.id]
		k.failed = true
		v.known[msg.id] = k
		return
	}
	if msg.items == nil {
		msg.items = []api.ChecklistItem{}
	}
	v.holdSteps(msg.id, msg.items, msg.updated)
}
