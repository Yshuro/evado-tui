package tui

import (
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/config"
)

// startedOnHousehold opens the app on one list, the way a remembered list
// or saved view does, so the todos asked for are not the whole account.
func startedOnHousehold(t *testing.T) *harness {
	t.Helper()
	return newHarnessWith(t, sampleAPI(), func(c *config.Config) { c.ListID = "l2" })
}

func TestCountsDescribeTheWholeAccountWhenStartedOnOneList(t *testing.T) {
	h := startedOnHousehold(t)
	if got := h.model.todos.filter.listID; got != "l2" {
		t.Fatalf("listID = %q, the test needs a narrowed start", got)
	}
	if len(h.model.todos.all) != 1 {
		t.Fatalf("loaded %d todos, want only Household's one", len(h.model.todos.all))
	}
	s := h.model.todos.stats()
	if s.open != 2 || s.done != 1 {
		t.Errorf("open/done = %d/%d, want 2/1 for the whole account", s.open, s.done)
	}
	if s.perList["l1"] != 1 || s.perList["l2"] != 1 {
		t.Errorf("perList = %v, want one open in Personal and one in Household", s.perList)
	}
	if got := h.model.countsView(); !strings.Contains(got, "2 open") {
		t.Errorf("header = %q, want 2 open", got)
	}

	h.press("F", "right", "right", "enter") // All lists
	if s := h.model.todos.stats(); s.open != 2 || s.done != 1 {
		t.Errorf("after changing the view open/done = %d/%d, want 2/1", s.open, s.done)
	}
}

func TestNarrowedViewKeepsCountsCurrentAfterAChange(t *testing.T) {
	h := startedOnHousehold(t)
	h.press("g", "space") // complete Renew passport
	s := h.model.todos.stats()
	if s.open != 1 || s.done != 2 {
		t.Errorf("open/done = %d/%d, want 1/2 after completing a todo", s.open, s.done)
	}
}

func TestListsTabCountsEveryListWhileTodosIsNarrowed(t *testing.T) {
	h := startedOnHousehold(t)
	h.press("2")
	h.mustContain("1 open")
	h.mustContain("1 done")
	if strings.Contains(h.view(), "nothing in this list yet") {
		t.Error("Personal has todos but the Lists tab says it is empty")
	}
}
