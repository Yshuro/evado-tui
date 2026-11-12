package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The keys of the Todos tab, and where they take the cursor and the view.
// Moving the cursor changes nothing but the view; choosing what is shown
// goes through choose, which is what remembers it for the next run.

// onKey handles keys while the Todos tab is active.
func (v *todosView) onKey(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	// Both the detail pane and its checklist own the keyboard; the list
	// only does when neither is open.
	if m.inDetail() {
		return v.onDetailKey(m, msg)
	}
	if m.focus == focusSidebar {
		if done, cmd := v.onSideKey(m, k); done {
			return m, cmd
		}
		// Anything else is meant for the todos, so the sidebar lets go.
		m.focus = focusList
	}
	if v.repair != nil {
		if done, cmd := v.onRepairKey(m, k); done {
			return m, cmd
		}
	}
	if v.filter.board {
		if done, cmd := v.onBoardKey(m, k); done {
			return m, cmd
		}
	}
	switch k {
	case "up", "k":
		v.move(-1)
		return m, nil
	case "down", "j":
		v.move(1)
		return m, nil
	case "pgup", "ctrl+b":
		v.move(-v.rows.pageSize())
		return m, nil
	case "pgdown", "ctrl+f":
		v.move(v.rows.pageSize())
		return m, nil
	case "ctrl+d":
		v.move(v.rows.halfPage())
		return m, nil
	case "ctrl+u":
		v.move(-v.rows.halfPage())
		return m, nil
	case "g", "home":
		v.rows.top()
		v.snapToTodo(1)
		return m, nil
	case "G", "end":
		v.rows.bottom()
		v.snapToTodo(-1)
		return m, nil
	case "enter", "l", "right":
		t := v.selected()
		if t == nil {
			return m, nil
		}
		return m, v.openDetail(m, t.ID)
	case "esc":
		if len(v.marks) > 0 {
			v.marks = map[string]bool{}
		}
		return m, nil
	case "h", "left":
		v.focusSide(m)
		return m, nil
	case "b":
		f := v.filter
		f.board = !f.board
		return m, v.choose(m, f)
	case "\\":
		v.hideSide = !v.hideSide
		v.remember()
		return m, nil
	case "<", "H":
		return m, v.shiftDay(m, -1)
	case ">":
		return m, v.shiftDay(m, 1)
	case "[":
		return m, v.cycleList(m, -1)
	case "]":
		return m, v.cycleList(m, 1)
	case "v":
		v.toggleMark()
		return m, nil
	case "t":
		return m, v.reschedule(m, 0, "today")
	case "T":
		return m, v.reschedule(m, 1, "tomorrow")
	case "w":
		return m, v.reschedule(m, 7, "next week")
	case "y":
		if t := v.selected(); t != nil {
			return m, m.copyToClipboard(t.Title, "title copied")
		}
		return m, nil
	case "Y":
		if t := v.selected(); t != nil {
			return m, m.copyToClipboard(t.ID, "todo id copied")
		}
		return m, nil
	case "z":
		return m, m.undoLast()
	case "L":
		return m, v.loadMore(m)
	case "m":
		return m, v.openMove(m)
	case " ", "x":
		return m, v.toggle(m)
	case "n":
		return m, v.openCreate(m)
	case "N":
		return m, v.openQuickCapture(m)
	case "e":
		return m, v.openEdit(m)
	case "d":
		return m, v.askDelete(m)
	case "i":
		return m, v.takeOrHandBack(m)
	case "a":
		return m, v.cycleAssigned(m)
	case "@":
		return m, v.openPersonFilter(m)
	case "U":
		return m, v.toggleUndated(m)
	case "s":
		return m, v.openSaveView(m, "", "")
	case "S":
		return m, v.openViewMenu(m)
	case "f":
		return m, v.cycleStatus(m)
	case "u":
		// The inbox is a section now, so u jumps to it rather than
		// filtering the whole list down to it.
		return m, v.jumpTo("inbox")
	case "F":
		return m, v.openListFilter(m)
	case "p":
		return m, v.previewParse(m)
	}
	return m, nil
}

// onRepairKey handles the keys while a view that needs repair is asked
// for. The todos under it belong to the view that was on screen before, so
// the keys that would act on one are swallowed rather than let through.
func (v *todosView) onRepairKey(m *Model, k string) (bool, tea.Cmd) {
	switch k {
	case "enter", "l", "right":
		return true, v.openRepair(m)
	case "esc":
		v.repair = nil
		return true, nil
	case "h", "left":
		v.focusSide(m)
		return true, nil
	case " ", "x", "d", "e", "t", "T", "w", "m", "v", "y", "Y", "<", ">", "H", "p", "L", "s", "z", "i":
		return true, nil
	}
	return false, nil
}

// move moves the cursor by delta todos, stepping over section headings.
func (v *todosView) move(delta int) {
	if v.filter.board {
		if v.bcol >= 0 && v.bcol < len(v.bcols) {
			v.brow = min(max(v.brow+delta, 0), max(len(v.bcols[v.bcol].idxs)-1, 0))
		}
		return
	}
	if len(v.lines) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	cur := v.rows.selected()
	for i := 0; i < len(v.lines); i++ {
		cur += step
		if cur < 0 || cur >= len(v.lines) {
			return // already at the end: stay put rather than wrap
		}
		if !v.lines[cur].header {
			v.rows.cursor = cur
			v.rows.clamp()
			return
		}
	}
}

// snapToTodo moves off a section heading in the given direction, so the
// cursor always rests on something actionable.
func (v *todosView) snapToTodo(dir int) {
	r := v.rows.selected()
	if r < 0 || r >= len(v.lines) || !v.lines[r].header {
		return
	}
	v.move(dir)
}

// jumpTo puts the cursor on the first todo of a section, or says why it
// cannot.
func (v *todosView) jumpTo(key string) tea.Cmd {
	if v.filter.mode != viewEverything {
		// Sections only exist in the Everything view.
		return v.m.okf("sections are in the Everything view, press f")
	}
	for r, l := range v.lines {
		if l.header && l.label == newSectionHeading(key).label {
			// Land on the first todo under the heading.
			if r+1 < len(v.lines) && !v.lines[r+1].header {
				v.rows.cursor = r + 1
				v.rows.clamp()
				return nil
			}
			return v.m.warnf("no todos in %s", strings.ToLower(l.label))
		}
	}
	return v.m.warnf("nothing in %s", strings.ToLower(newSectionHeading(key).label))
}

// reveal puts the cursor on a todo, so the result of an action is never
// something the user has to go looking for. The cursor is the cue: a second
// marker would only duplicate it, and the receipt toast is the record.
func (v *todosView) reveal(id string) {
	v.boardFind(id)
	for r, l := range v.lines {
		if !l.header && v.items[l.index].ID == id {
			v.rows.cursor = r
			v.rows.clamp()
			return
		}
	}
}

// shown reports whether the current view contains a todo, which decides
// whether an action needs to change the view to be seen.
func (v *todosView) shown(id string) bool {
	for _, l := range v.lines {
		if !l.header && v.items[l.index].ID == id {
			return true
		}
	}
	return false
}

// toggleMark marks or unmarks the todo under the cursor and steps down, so
// a run of todos can be picked by pressing v repeatedly.
func (v *todosView) toggleMark() {
	t := v.selected()
	if t == nil {
		return
	}
	if v.marks[t.ID] {
		delete(v.marks, t.ID)
	} else {
		v.marks[t.ID] = true
	}
	v.move(1)
}

// -- sidebar and board ------------------------------------------------------

// focusSide hands the keyboard to the sidebar, starting on whatever the
// list is showing now.
func (v *todosView) focusSide(m *Model) {
	if v.layout(m.width, m.bodyHeight()).sideW == 0 {
		return
	}
	items := v.sideItems()
	v.side = 0
	// A saved view is the narrowest choice, then a list filter, then a view.
	rank := -1
	for i, it := range items {
		if !v.sideActive(it) {
			continue
		}
		r := 1
		switch {
		case it.isSaved:
			r = 3
		case it.isList && v.filter.listID == "":
			continue
		case it.isList:
			r = 2
		}
		if r >= rank {
			v.side, rank = i, r
		}
	}
	m.focus = focusSidebar
}

// onSideKey drives the sidebar. It reports false for a key the sidebar has
// no use for, which then goes to the todos instead.
func (v *todosView) onSideKey(m *Model, k string) (bool, tea.Cmd) {
	items := v.sideItems()
	v.side = min(max(v.side, 0), len(items)-1)
	switch k {
	case "up", "k":
		v.side = max(v.side-1, 0)
	case "down", "j":
		v.side = min(v.side+1, len(items)-1)
	case "g", "home":
		v.side = 0
	case "G", "end":
		v.side = len(items) - 1
	case "esc", "tab", "shift+tab":
		m.focus = focusList
	case "S":
		// Kept in the sidebar, so the menu starts on the view under the
		// cursor.
		return true, v.openViewMenu(m)
	case "enter", " ", "l", "right":
		m.focus = focusList
		return true, v.applySide(m, items[v.side])
	default:
		return false, nil
	}
	return true, nil
}

// applySide shows what a sidebar item stands for.
func (v *todosView) applySide(m *Model, it sideItem) tea.Cmd {
	if it.isSaved {
		return v.openSaved(m, it.label)
	}
	if it.isList {
		return v.showList(m, it.listID)
	}
	return v.setMode(m, it.mode, it.status)
}

// onBoardKey handles the keys that mean something different on the board.
// Everything else falls through to the list's handling, which acts on
// whatever todo the board's cursor is on.
func (v *todosView) onBoardKey(m *Model, k string) (bool, tea.Cmd) {
	switch k {
	case "h", "left":
		if v.bcol == 0 {
			v.focusSide(m)
			return true, nil
		}
		v.boardStep(-1)
	case "l", "right":
		v.boardStep(1)
	case "g", "home":
		v.brow = 0
	case "G", "end":
		if v.bcol < len(v.bcols) {
			v.brow = max(len(v.bcols[v.bcol].idxs)-1, 0)
		}
	case "u":
		v.bcol, v.brow = 0, 0
	case "L":
		return true, v.shiftDay(m, 1)
	default:
		return false, nil
	}
	return true, nil
}

// boardStep moves the cursor a column over, keeping its row where it can.
func (v *todosView) boardStep(d int) {
	v.bcol = min(max(v.bcol+d, 0), max(len(v.bcols)-1, 0))
	if v.bcol < len(v.bcols) {
		v.brow = min(v.brow, max(len(v.bcols[v.bcol].idxs)-1, 0))
	}
}

// -- choosing what is shown -------------------------------------------------

// choose shows a filter combination picked as the view to be on, and
// remembers it for the next run. The todos are asked for again when it
// asks the server for something different, and laid out again when only
// the layout changed. Picking what is already on screen does nothing. It
// is also how a saved view is opened, so a view is always one filter.
func (v *todosView) choose(m *Model, f todoFilter) tea.Cmd {
	// Picking anything is moving on from a view that needed repair.
	v.repair = nil
	if f.is(v.filter) {
		return nil
	}
	keep := v.selectedID()
	reload := f.request(0) != v.filter.request(0)
	relayout := f.board != v.filter.board
	v.filter = f
	if relayout {
		v.rebuild(keep)
	}
	v.remember()
	if !reload {
		return nil
	}
	m.busy = true
	return tea.Batch(m.setBusy(""), v.load())
}

// showList narrows the todos to one list, or to all of them for "".
func (v *todosView) showList(m *Model, id string) tea.Cmd {
	f := v.filter
	f.listID = id
	return v.choose(m, f)
}

// cycleList steps the list filter through every list and back to all.
func (v *todosView) cycleList(m *Model, d int) tea.Cmd {
	ids := []string{""}
	for _, l := range m.lists {
		ids = append(ids, l.ID)
	}
	at := 0
	for i, id := range ids {
		if id == v.filter.listID {
			at = i
		}
	}
	return v.showList(m, ids[(at+d+len(ids))%len(ids)])
}

func (v *todosView) cycleStatus(m *Model) tea.Cmd {
	idx := 0
	for i, f := range filterCycle {
		if v.filter.shows(f.mode, f.status) {
			idx = i
			break
		}
	}
	next := filterCycle[(idx+1)%len(filterCycle)]
	f := v.filter
	f.mode, f.status = next.mode, next.status
	f.undated = f.undated && undatedFits(f)
	return v.choose(m, f)
}

// setMode switches between everything and one status, from the sidebar or
// when an action has to reveal a todo the current view would otherwise
// hide.
func (v *todosView) setMode(m *Model, mode viewMode, status string) tea.Cmd {
	f := v.filter
	f.mode, f.status = mode, status
	f.undated = f.undated && undatedFits(f)
	return v.choose(m, f)
}

// cycleAssigned steps the assignee through everyone, me and unassigned. A
// person picked with @ is not one of those, so the next step is everyone.
func (v *todosView) cycleAssigned(m *Model) tea.Cmd {
	opts := []string{"", "me", "anyone"}
	f := v.filter
	f.assigned = ""
	for i, o := range opts {
		if o == v.filter.assigned {
			f.assigned = opts[(i+1)%len(opts)]
			break
		}
	}
	return v.choose(m, f)
}

// dropMissingList clears a remembered list filter whose list is gone, so a
// list left or deleted elsewhere cannot leave the Todos tab filtered to
// nothing with no way to see why. The todos already requested used the
// stale filter, so they are reloaded. A saved view that is open is not
// given that treatment: dropping its list would show more than the view
// was made to, so it goes to the repair state instead.
func (v *todosView) dropMissingList(m *Model) tea.Cmd {
	if v.filter.listID == "" || m.lists == nil {
		return nil
	}
	if _, ok := m.listByID(v.filter.listID); ok {
		return nil
	}
	if sv, ok := v.activeView(); ok {
		v.enterRepair(sv, v.issuesOf(sv))
		return nil
	}
	v.filter.listID = ""
	v.remember()
	return v.load()
}

// allLists is the picker's entry for no list filter.
const allLists = "All lists"

// openListFilter asks which list to show. A menu scales to many lists in a
// way that stepping through them one at a time does not.
func (v *todosView) openListFilter(m *Model) tea.Cmd {
	opts := []string{allLists}
	for _, l := range m.lists {
		opts = append(opts, l.Name)
	}
	f := newForm(formListFilter, "Show list", m.width)
	f.selectValue("list", "list", opts, m.listName(v.filter.listID))
	return m.openForm(f)
}

// applyListFilter narrows the todos to the list picked in the menu.
func (v *todosView) applyListFilter(m *Model, name string) tea.Cmd {
	id := ""
	if name != allLists {
		for _, l := range m.lists {
			if l.Name == name {
				id = l.ID
				break
			}
		}
	}
	if id == v.filter.listID {
		// Picking the list already shown still reloads it, as it always has.
		m.busy = true
		return tea.Batch(m.setBusy(""), v.load())
	}
	return v.showList(m, id)
}

// -- detail pane ------------------------------------------------------------

func (v *todosView) onDetailKey(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if m.focus == focusChecklist {
		switch k {
		case "up", "k":
			v.checklist.move(-1)
			return m, nil
		case "down", "j":
			v.checklist.move(1)
			return m, nil
		case "esc", "q", "tab", "h", "left":
			m.focus = focusDetail
			return m, nil
		case " ", "x":
			i := v.checklist.selected()
			if i < 0 || i >= len(v.steps) {
				return m, nil
			}
			m.busy = true
			return m, tea.Batch(m.setBusy(""), toggleChecklistItem(m.ctx, m.client, v.steps[i]))
		case "c":
			return m, v.openNewStep(m)
		case "e":
			i := v.checklist.selected()
			if i < 0 || i >= len(v.steps) {
				return m, nil
			}
			return m, v.openRenameStep(m, v.steps[i])
		case "d":
			i := v.checklist.selected()
			if i < 0 || i >= len(v.steps) {
				return m, nil
			}
			item := v.steps[i]
			return m, m.ask("delete-step", "Delete this step?",
				fmt.Sprintf("%q will be removed from the checklist.", item.Title), false,
				func() tea.Cmd { return deleteChecklistItem(m.ctx, m.client, item) })
		}
		return m, nil
	}

	switch k {
	case "esc", "h", "left", "q":
		m.focus = focusList
		return m, nil
	case "tab":
		m.focus = focusChecklist
		v.checklist.setItems(len(v.steps))
		v.checklist.setHeight(min(m.bodyHeight()/2, 8))
		return m, nil
	case " ", "x":
		if v.detail.ID == "" {
			return m, nil
		}
		m.busy = true
		id := v.detail.ID
		if v.detail.Completed() {
			return m, tea.Batch(m.setBusy("reopening"), m.mutate(id, uncompleteTodo(m.ctx, m.client, id)))
		}
		return m, tea.Batch(m.setBusy("completing"), m.mutate(id, completeTodo(m.ctx, m.client, id)))
	case "e":
		return m, v.openEdit(m)
	case "d":
		if v.detail.ID == "" {
			return m, nil
		}
		todo := v.detail
		return m, m.ask("delete-todo", "Delete this todo?",
			fmt.Sprintf("%q will be removed for every device.", todo.Title), false,
			func() tea.Cmd { return m.mutate(todo.ID, deleteTodo(m.ctx, m.client, todo)) })
	case "c":
		return m, v.openNewStep(m)
	case "A":
		return m, v.openAssign(m)
	case "i":
		return m, v.takeOrHandBack(m)
	case "t":
		return m, v.reschedule(m, 0, "today")
	case "T":
		return m, v.reschedule(m, 1, "tomorrow")
	case "w":
		return m, v.reschedule(m, 7, "next week")
	case "y":
		if v.detail.ID == "" {
			return m, nil
		}
		return m, m.copyToClipboard(v.detail.ID, "todo id copied")
	}
	return m, nil
}
