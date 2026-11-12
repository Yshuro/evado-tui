package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestListModeLeavesTheBoardUntilItIsShown(t *testing.T) {
	m := benchModel(t, 60)
	v := &m.todos
	if v.filter.board || !v.boardStale || len(v.bcols) != 0 {
		t.Fatalf("list mode: board = %v stale = %v columns = %d, want the board left unbuilt", v.filter.board, v.boardStale, len(v.bcols))
	}
	keep := v.selectedID()

	v.onKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if !v.filter.board || v.boardStale || len(v.bcols) == 0 {
		t.Fatalf("board on: board = %v stale = %v columns = %d, want it built", v.filter.board, v.boardStale, len(v.bcols))
	}
	if got := v.selectedID(); got != keep {
		t.Errorf("board cursor is on %q, want the todo the list had, %q", got, keep)
	}

	// Edits while the board is on keep it current.
	id := v.selectedID()
	v.patchLocal(id, func(td *api.Todo) { td.Title = "moved" })
	if v.boardStale || v.selected() == nil || v.selected().Title != "moved" {
		t.Error("the board was not rebuilt after an edit")
	}

	v.onKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	v.patchLocal(id, func(td *api.Todo) { td.Title = "again" })
	if !v.boardStale {
		t.Error("list mode built the board after an edit")
	}
	if v.boardFind(id) {
		t.Error("boardFind read a board that is out of date")
	}
}

func TestLiveSearchFollowsEditsToTitlesAndNotes(t *testing.T) {
	m := benchModel(t, 30)
	v := &m.todos
	count := func(q string) int {
		v.setLive(q)
		return len(v.items)
	}
	if n := count("ITEM big"); n != 30 {
		t.Fatalf("every word, any case: %d matched, want all 30", n)
	}
	if n := count("item 27 account"); n != 1 {
		t.Fatalf("all words: %d matched, want only item 27", n)
	}
	if n := count("zebra"); n != 0 {
		t.Fatalf("zebra matched %d before any edit", n)
	}

	id := v.all[7].ID
	v.patchLocal(id, func(td *api.Todo) { td.Title = "Zebra crossing" })
	if n := count("zebra"); n != 1 {
		t.Errorf("after a title edit zebra matched %d, want 1", n)
	}
	note := "buy a zebra"
	v.patchLocal(v.all[9].ID, func(td *api.Todo) { td.Notes = &note })
	if n := count("zebra"); n != 2 {
		t.Errorf("after a notes edit zebra matched %d, want 2", n)
	}

	v.setItems([]api.Todo{{ID: "new-1", Title: "Fresh zebra"}, {ID: "new-2", Title: "Other"}})
	if n := count("zebra"); n != 1 || v.items[0].ID != "new-1" {
		t.Errorf("after a reload zebra matched %d, want the new todo", n)
	}
	v.merge([]api.Todo{{ID: "new-2", Title: "Zebra too"}}, []string{"new-1"})
	if n := count("zebra"); n != 1 || v.items[0].ID != "new-2" {
		t.Errorf("after a merge zebra matched %d, want new-2", n)
	}

	// An empty query gives back everything loaded.
	if n := count(" "); n != 1 {
		t.Errorf("empty query shows %d, want everything loaded", n)
	}
}

func TestLiveSearchPrunesMarksAndKeepsTheCursor(t *testing.T) {
	m := benchModel(t, 30)
	v := &m.todos
	v.setLive("item 1")
	var keep, hidden string
	for _, td := range v.all {
		switch td.Title {
		case "Item 1 in the big account":
			keep = td.ID
		case "Item 2 in the big account":
			hidden = td.ID
		}
	}
	v.marks[keep], v.marks[hidden] = true, true
	v.reveal(keep)

	v.setLive("item 1 ")
	if v.marks[hidden] || !v.marks[keep] {
		t.Errorf("marks = %v, want only the todo still on screen marked", v.marks)
	}
	if got := v.selectedID(); got != keep {
		t.Errorf("cursor moved to %q, want it to stay on %q", got, keep)
	}
}
