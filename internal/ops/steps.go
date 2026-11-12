package ops

import (
	"context"
	"strings"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// StepList is a todo with its checklist, in order.
type StepList struct {
	Todo  api.Todo            `json:"todo"`
	Steps []api.ChecklistItem `json:"steps"`
}

// StepChange is the outcome of an operation on one step. Changed is false
// when the step was already in the state asked for, so repeating a call is
// safe.
type StepChange struct {
	Todo    api.Todo          `json:"todo"`
	Step    api.ChecklistItem `json:"step"`
	Changed bool              `json:"changed"`
}

// Steps lists a todo's checklist.
func (o *Ops) Steps(ctx context.Context, ref string) (StepList, error) {
	t, err := o.Resolve(ctx, ref)
	if err != nil {
		return StepList{}, err
	}
	steps, err := o.C.ListChecklist(ctx, t.ID)
	if err != nil {
		return StepList{}, err
	}
	if steps == nil {
		steps = []api.ChecklistItem{}
	}
	return StepList{Todo: t, Steps: steps}, nil
}

// AddStep adds a checklist step to a todo.
func (o *Ops) AddStep(ctx context.Context, ref, title string) (api.ChecklistItem, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return api.ChecklistItem{}, usagef("a step needs a title")
	}
	t, err := o.Resolve(ctx, ref)
	if err != nil {
		return api.ChecklistItem{}, err
	}
	return o.C.AddChecklistItem(ctx, t.ID, title)
}

// resolveStep finds the step a reference points at among the steps of one
// todo. The service changes a step by its id alone and never checks which
// todo it is on, so the only protection against acting on another todo's
// step is to look for it in this todo's checklist and use the id found
// there. A reference is a full id or the start of one; an exact id wins.
func (o *Ops) resolveStep(ctx context.Context, todoRef, stepRef string) (api.Todo, api.ChecklistItem, error) {
	stepRef = strings.TrimSpace(stepRef)
	if stepRef == "" {
		return api.Todo{}, api.ChecklistItem{}, usagef("give a step id")
	}
	list, err := o.Steps(ctx, todoRef)
	if err != nil {
		return api.Todo{}, api.ChecklistItem{}, err
	}
	var hits []api.ChecklistItem
	for _, s := range list.Steps {
		if s.ID == stepRef {
			return list.Todo, s, nil
		}
		if strings.HasPrefix(s.ID, stepRef) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 0:
		return api.Todo{}, api.ChecklistItem{}, notFoundf("%q has no step with an id starting with %q (evado steps list %s)", list.Todo.Title, stepRef, list.Todo.ID)
	case 1:
		return list.Todo, hits[0], nil
	}
	cands := make([]string, 0, len(hits))
	for _, s := range hits {
		cands = append(cands, s.ID+"  "+s.Title)
	}
	return api.Todo{}, api.ChecklistItem{}, &Error{
		Code:       codeAmbiguous,
		Message:    sprintf("%q starts %d steps; give more of the id", stepRef, len(hits)),
		Candidates: cands,
	}
}

// SetStepDone ticks or unticks a step. It sets the state asked for rather
// than flipping it, so a repeated call changes nothing.
func (o *Ops) SetStepDone(ctx context.Context, todoRef, stepRef string, done bool) (StepChange, error) {
	t, s, err := o.resolveStep(ctx, todoRef, stepRef)
	if err != nil {
		return StepChange{}, err
	}
	if s.Completed() == done {
		return StepChange{Todo: t, Step: s}, nil
	}
	var at *time.Time
	if done {
		now := time.Now().UTC()
		at = &now
	}
	updated, err := o.C.UpdateChecklistItem(ctx, s.ID, nil, at)
	if err != nil {
		return StepChange{}, err
	}
	return StepChange{Todo: t, Step: fillStep(updated, s, done), Changed: true}, nil
}

// RenameStep changes a step's title.
func (o *Ops) RenameStep(ctx context.Context, todoRef, stepRef, title string) (StepChange, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return StepChange{}, usagef("a step needs a title")
	}
	t, s, err := o.resolveStep(ctx, todoRef, stepRef)
	if err != nil {
		return StepChange{}, err
	}
	updated, err := o.C.UpdateChecklistItem(ctx, s.ID, &title, nil)
	if err != nil {
		return StepChange{}, err
	}
	if updated.ID == "" {
		s.Title = title
		updated = s
	}
	return StepChange{Todo: t, Step: updated, Changed: s.Title != title}, nil
}

// DeleteStep removes a step. Without confirm it changes nothing and reports
// what would have gone, as Delete does for a todo.
func (o *Ops) DeleteStep(ctx context.Context, todoRef, stepRef string, confirm bool) (StepChange, error) {
	t, s, err := o.resolveStep(ctx, todoRef, stepRef)
	if err != nil {
		return StepChange{}, err
	}
	ch := StepChange{Todo: t, Step: s}
	if !confirm {
		return ch, &Error{
			Code:    codeConfirmation,
			Message: sprintf("this would delete the step %q (%s) of %q for every device; repeat with confirmation to do it", s.Title, s.ID, t.Title),
		}
	}
	if err := o.C.DeleteChecklistItem(ctx, s.ID); err != nil {
		return StepChange{}, err
	}
	ch.Changed = true
	return ch, nil
}

// fillStep keeps the step we already had, with the state asked for, when
// the API's answer is empty.
func fillStep(got, had api.ChecklistItem, done bool) api.ChecklistItem {
	if got.ID != "" {
		return got
	}
	if !done {
		had.CompletedAt = nil
		return had
	}
	now := time.Now().UTC()
	had.CompletedAt = &now
	return had
}
