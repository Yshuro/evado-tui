package local

import (
	"context"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestListCarriesEachTodosSteps(t *testing.T) {
	ctx := context.Background()
	c := open(t, "", "").Client()
	with, without := add(t, c, "with steps"), add(t, c, "without")
	if _, err := c.AddChecklistItem(ctx, with.ID, "first"); err != nil {
		t.Fatal(err)
	}

	_, steps, err := c.ListTodosWithSteps(ctx, api.TodoFilter{Status: api.StatusOpen})
	if err != nil {
		t.Fatal(err)
	}
	if got := steps[with.ID]; len(got) != 1 || got[0].Title != "first" {
		t.Errorf("steps of the first todo = %+v, want its one step", got)
	}
	// No steps is said, not left out, or the preview would wait for them.
	if got, ok := steps[without.ID]; !ok || len(got) != 0 {
		t.Errorf("steps of the second todo = %+v, present %v, want an empty list", got, ok)
	}
}
