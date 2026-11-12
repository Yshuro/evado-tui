package tui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

func TestPrefsKeepTheViewButNotTheMoment(t *testing.T) {
	t.Setenv("EVADO_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	cfg := config.Config{Token: "evt_kept"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	f := todoFilter{
		mode: viewStatus, status: api.StatusUpcoming, listID: "l2", board: true,
		assigned: "me", query: "milk",
	}
	if err := (todoPrefs{filter: f, hideSide: true}).save(&cfg); err != nil {
		t.Fatal(err)
	}
	disk, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	got := prefsOf(disk)
	want := f
	want.assigned, want.query = "", ""
	if got.filter != want || !got.hideSide {
		t.Errorf("after a restart the view is %+v (sidebar hidden %v), want %+v and hidden", got.filter, got.hideSide, want)
	}
	if disk.Token != "evt_kept" {
		t.Errorf("saving the view touched the token: %q", disk.Token)
	}
}

func TestPrefsWithAnUnknownModeOpenOnEverything(t *testing.T) {
	got := prefsOf(config.Config{Mode: "Someday only", Status: "someday"})
	if !got.filter.is(defaultFilter()) {
		t.Errorf("an unknown mode opened %+v, want the Everything view", got.filter)
	}
}

// restarted is a new run of the program that reads back what the last one
// left in the config file, as a person's next start would.
func restarted(t *testing.T, run config.Config) *Model {
	t.Helper()
	disk, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	disk.Token, disk.BaseURL, disk.NoCache = run.Token, run.BaseURL, true
	return New(context.Background(), disk)
}

func TestAllListsIsRememberedForTheNextRun(t *testing.T) {
	h := newHarnessWith(t, sampleAPI(), func(c *config.Config) { c.ListID = "l2" })
	// The last run left Household behind, so it has to be forgotten on disk.
	if err := h.model.cfg.SavePrefs(); err != nil {
		t.Fatal(err)
	}
	if h.model.todos.filter.listID != "l2" {
		t.Fatalf("listID = %q, want the test to start on Household", h.model.todos.filter.listID)
	}
	h.press("F", "right", "right", "enter") // Household, Work, All lists
	if h.model.todos.filter.listID != "" {
		t.Fatalf("listID = %q, want every list", h.model.todos.filter.listID)
	}
	if again := restarted(t, h.model.cfg); again.todos.filter.listID != "" {
		t.Errorf("the next run opened on list %q, want All lists", again.todos.filter.listID)
	}
}

func TestListChosenFromTheListsTabIsRememberedForTheNextRun(t *testing.T) {
	h := newHarness(t, sampleAPI())
	if err := h.model.cfg.SavePrefs(); err != nil { // nothing remembered yet
		t.Fatal(err)
	}
	h.press("2", "j", "t") // Personal, then Household
	if h.model.tab != tabTodos || h.model.todos.filter.listID != "l2" {
		t.Fatalf("tab %v list %q, want Todos on Household", h.model.tab, h.model.todos.filter.listID)
	}
	if again := restarted(t, h.model.cfg); again.todos.filter.listID != "l2" {
		t.Errorf("the next run opened on list %q, want Household", again.todos.filter.listID)
	}
}
