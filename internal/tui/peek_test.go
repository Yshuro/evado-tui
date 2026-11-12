package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

// wide makes the terminal big enough for the preview beside the list.
func (h *harness) wide() {
	h.t.Helper()
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.run(h.cmds...)
	h.cmds = nil
}

func (h *harness) selectTodo(id string) {
	h.t.Helper()
	h.press("g")
	for range 20 {
		if h.model.todos.selectedID() == id {
			return
		}
		h.press("j")
	}
	h.t.Fatalf("could not move the cursor to %s", id)
}

func TestPreviewShowsTheChecklistOfTheTodoUnderTheCursor(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.wide()
	h.selectTodo("t1")
	h.mustContain("CHECKLIST")
	h.mustContain("read the meter")
	h.mustContain("call the provider")
	h.mustContain("1/2")

	h.selectTodo("t2")
	h.mustContain("no steps")
	h.mustNotContain("read the meter")
}

func TestPreviewReadsStepsOnlyWhereTheCursorRests(t *testing.T) {
	old := peekDelay
	peekDelay = 20 * time.Millisecond
	t.Cleanup(func() { peekDelay = old })

	f := sampleAPI()
	h := newHarness(t, f)
	h.wide()
	f.stepReads = nil
	h.model.todos.known = nil

	// Pass over every row without resting: nothing is asked for.
	var pending []tea.Cmd
	for _, k := range []string{"j", "j", "k", "j"} {
		next, cmd := h.model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		h.model = next.(*Model)
		pending = append(pending, cmd)
	}
	if len(f.stepReads) != 0 {
		t.Fatalf("steps were read while the cursor moved: %v", f.stepReads)
	}
	// Every settling but the last is overtaken and does nothing.
	h.run(pending...)
	if len(f.stepReads) != 1 {
		t.Fatalf("steps were read for %v, want just the row the cursor rests on", f.stepReads)
	}
	if id := h.model.todos.selectedID(); f.stepReads[0] != id {
		t.Errorf("read the steps of %s, but the cursor rests on %s", f.stepReads[0], id)
	}
}

func TestAStepsAnswerForAnotherTodoIsNotShownUnderThisOne(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.wide()
	h.selectTodo("t2")
	h.send(peekMsg{id: "t1", items: []api.ChecklistItem{{ID: "x", TodoID: "t1", Title: "late arrival"}}})
	h.mustNotContain("late arrival")
	h.selectTodo("t1")
	h.mustContain("late arrival")
}

func TestPreviewSaysSoWhileStepsLoadAndWhenTheyFail(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.wide()
	h.selectTodo("t2")
	delete(h.model.todos.known, "t2")
	h.mustContain("loading…")
	h.send(peekMsg{id: "t2", err: fmt.Errorf("boom")})
	h.mustContain("could not be read")
}

func TestPreviewClipsALongChecklist(t *testing.T) {
	f := sampleAPI()
	var many []api.ChecklistItem
	for i := range 40 {
		many = append(many, api.ChecklistItem{ID: fmt.Sprintf("m%d", i), TodoID: "t2", Title: fmt.Sprintf("step number %d", i)})
	}
	f.steps["t2"] = many
	h := newHarness(t, f)
	h.wide()
	h.selectTodo("t2")
	h.mustContain("step number 0")
	h.mustContain("more")
	h.mustNotContain("step number 39")
	for i, line := range strings.Split(h.view(), "\n") {
		if n := visibleWidth(line); n > 140 {
			t.Errorf("line %d is %d cells wide", i, n)
		}
	}
	if got := strings.Count(h.view(), "\n") + 1; got > 40 {
		t.Errorf("the screen is %d rows tall, want at most 40", got)
	}
}

func TestRowsShowHowManyStepsAreDone(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.wide()
	h.selectTodo("t1")
	if done, total := h.model.todos.stepCount("t1"); done != 1 || total != 2 {
		t.Errorf("t1 count = %d/%d, want 1/2", done, total)
	}
	// A todo with no steps shows no count.
	if done, total := h.model.todos.stepCount("t3"); total != 0 || done != 0 {
		t.Errorf("t3 count = %d/%d, want none", done, total)
	}
}

func TestPreviewFollowsEditsMadeInTheOpenTodo(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.wide()
	h.selectTodo("t1")
	h.press("enter", "tab", "space")
	h.press("esc", "esc")
	h.mustContain("2/2")
}

func TestNoStepsAreReadWithoutAPreviewOnScreen(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	f.stepReads = nil
	h.model.todos.known = nil
	h.selectTodo("t1")
	h.mustNotContain("read the meter")
	if len(f.stepReads) != 0 {
		t.Errorf("steps were read with no preview on screen: %v", f.stepReads)
	}
}

func TestLocalModeCarriesStepsWithTheList(t *testing.T) {
	h := offlineHarness(t, config.Config{Local: true, BaseURL: "http://127.0.0.1:1"})
	h.wide()
	h.press("n")
	h.typeText("Pack the car")
	h.press("enter")
	h.press("g", "enter", "tab", "c")
	h.typeText("tyres")
	h.press("enter")
	h.press("c")
	h.typeText("roof box")
	h.press("enter")
	h.press("space")
	h.press("esc", "esc")
	h.mustContain("1/2")
	h.mustContain("tyres")
	h.mustContain("roof box")

	// A restart reads the steps with the list, with no request per row.
	again := restart(t, h.model.cfg)
	again.wide()
	again.mustContain("1/2")
	again.mustContain("roof box")
}

func TestStepsAreReadAgainWhenTheCursorReturnsAfterAWhile(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	h.wide()
	h.selectTodo("t2")
	h.selectTodo("t1")
	f.stepReads = nil

	// Soon after: what the list carried is still good.
	h.selectTodo("t2")
	h.selectTodo("t1")
	if len(f.stepReads) != 0 {
		t.Fatalf("steps were read again straight away: %v", f.stepReads)
	}

	// Later: another device may have ticked a step.
	k := h.model.todos.known["t1"]
	k.at = k.at.Add(-2 * peekMaxAge)
	h.model.todos.known["t1"] = k
	done := time.Now()
	f.steps["t1"][0].CompletedAt = &done
	h.selectTodo("t2")
	h.selectTodo("t1")
	if len(f.stepReads) != 1 || f.stepReads[0] != "t1" {
		t.Fatalf("steps read = %v, want one read of t1", f.stepReads)
	}
	h.mustContain("2/2")
}
