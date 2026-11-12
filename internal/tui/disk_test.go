package tui

import (
	"context"
	"slices"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
	"github.com/nerdlabz/evado-tui/internal/local"
)

// A todo added from the command line while the TUI runs shows up on the
// next tick, and what the TUI does afterwards keeps it.
func TestAChangeFromTheCommandLineShowsWithoutARestart(t *testing.T) {
	h := offlineHarness(t, config.Config{Local: true})
	h.press("n")
	h.typeText("Call Sam")
	h.press("enter")
	h.mustContain("Call Sam")

	path, err := local.Path()
	if err != nil {
		t.Fatal(err)
	}
	cli, err := local.Open(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := cli.Client().CreateTodos(ctx, api.NewTodo{Title: "Water plants"}); err != nil {
		t.Fatal(err)
	}
	h.mustNotContain("Water plants")
	h.run(h.model.watchDisk())
	h.mustContain("Water plants")

	h.press("n")
	h.typeText("Buy stamps")
	h.press("enter")
	after, err := local.Open(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	todos, err := after.Client().ListTodos(ctx, api.TodoFilter{Status: api.StatusAll})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, td := range todos {
		got = append(got, td.Title)
	}
	slices.Sort(got)
	if want := []string{"Buy stamps", "Call Sam", "Water plants"}; !slices.Equal(got, want) {
		t.Errorf("on disk: %v, want %v", got, want)
	}
}
