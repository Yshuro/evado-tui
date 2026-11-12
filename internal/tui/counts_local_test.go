package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
	"github.com/nerdlabz/evado-tui/internal/local"
)

// seedComputer fills the todos kept on this computer, before the program
// starts: personal open todos in the personal list and work more in a list
// of its own. It returns the ids of the two lists.
func seedComputer(t *testing.T, personal, work int) (personalID, workID string) {
	t.Helper()
	t.Setenv("EVADO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	cfg := config.Config{Local: true}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	path, err := local.Path()
	if err != nil {
		t.Fatal(err)
	}
	b, err := local.Open(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, c := context.Background(), b.Client()
	l, err := c.CreateList(ctx, "Work")
	if err != nil {
		t.Fatal(err)
	}
	lists, err := c.ListLists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range lists {
		if x.IsPersonal {
			personalID = x.ID
		}
	}
	add := func(list string, n int) {
		for i := range n {
			if _, err := c.CreateTodos(ctx, api.NewTodo{Title: fmt.Sprintf("Task %d", i), ListID: list}); err != nil {
				t.Fatal(err)
			}
		}
	}
	add(personalID, personal)
	add(l.ID, work)
	return personalID, l.ID
}

func TestCountsHoldEveryTodoKeptOnThisComputer(t *testing.T) {
	personal, work := seedComputer(t, 200, 150)
	h := restart(t, config.Config{Local: true})
	v := &h.model.todos

	if len(v.snapshot) != 350 || len(v.all) != 350 {
		t.Fatalf("loaded %d, snapshot %d, want all 350 without pressing L", len(v.all), len(v.snapshot))
	}
	if v.moreToLoad() {
		t.Error("the whole account was read, but the view says it was cut short")
	}
	if s := v.stats(); s.open != 350 || s.perList[personal] != 200 || s.perList[work] != 150 {
		t.Errorf("open = %d, per list = %v, want 350 and 200/150", s.open, s.perList)
	}
	h.mustContain("350 open")
	h.mustNotContain("showing the first")

	h.press("2")
	h.mustContain("200 open")
	h.mustContain("150 open")
}

func TestCountsHoldEveryTodoWhileTheViewIsNarrowedOnThisComputer(t *testing.T) {
	personal, work := seedComputer(t, 200, 150)
	h := restart(t, config.Config{Local: true, ListID: work})
	v := &h.model.todos

	if v.filter.listID != work {
		t.Fatalf("listID = %q, the test needs a narrowed start", v.filter.listID)
	}
	if len(v.all) != 150 || v.moreToLoad() {
		t.Fatalf("Work shows %d todos, atLimit = %v, want its 150 in one page", len(v.all), v.moreToLoad())
	}
	// The snapshot is read apart from the list, and without a limit: left
	// out, the store would answer with a page of 100.
	if len(v.snapshot) != 350 {
		t.Fatalf("snapshot holds %d todos, want 350", len(v.snapshot))
	}
	if s := v.stats(); s.open != 350 || s.perList[personal] != 200 {
		t.Errorf("narrowed: open = %d, per list = %v, want 350 and 200 in the personal list", s.open, s.perList)
	}
	h.mustContain("350 open")

	h.press("2")
	h.mustContain("200 open")
	h.mustContain("150 open")
}

func TestAFilteredViewOnThisComputerStillStopsAtTheLimit(t *testing.T) {
	personal, _ := seedComputer(t, 350, 5)
	h := restart(t, config.Config{Local: true, ListID: personal})
	v := &h.model.todos

	if len(v.all) != pageSize || !v.moreToLoad() {
		t.Fatalf("loaded %d, atLimit = %v, want a page of %d that says it was cut short", len(v.all), v.moreToLoad(), pageSize)
	}
	h.mustContain(fmt.Sprintf("showing the first %d", pageSize))
	// The counts still cover the account.
	if s := v.stats(); s.open != 355 {
		t.Errorf("open = %d, want 355 though the list shows a page", s.open)
	}
	h.mustContain("355 open")

	h.press("L")
	if len(v.all) != 350 || v.moreToLoad() {
		t.Errorf("after L: %d loaded, atLimit = %v, want all 350 of the list", len(v.all), v.moreToLoad())
	}
	if strings.Contains(h.view(), "showing the first") {
		t.Error("the notice stayed after the whole list was loaded")
	}
}
