package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestWholeAccountKeepsInlineChecklistWithoutASeparateRead(t *testing.T) {
	f := sampleAPI()
	f.steps = map[string][]api.ChecklistItem{f.todos[0].ID: {{ID: "inline-step", TodoID: f.todos[0].ID, Title: "From the sync"}}}
	h := newHarness(t, f)
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.run(fetchAccount(h.model.ctx, h.model.client, 0))
	if len(h.model.todos.known[f.todos[0].ID].items) != 1 {
		t.Fatal("inline checklist was not cached")
	}
	before := len(f.stepReads)
	h.mustContain("From the sync")
	if len(f.stepReads) != before {
		t.Fatal("inline checklist needed another read")
	}
}
