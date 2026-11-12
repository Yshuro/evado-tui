package tui

import "github.com/nerdlabz/evado-tui/internal/api"

// viewMode is what the todo list is currently showing.
type viewMode int

const (
	// viewEverything shows every todo, bucketed into dated sections with the
	// inbox always present. This is the default because it is the only view
	// that cannot hide something the user just captured.
	viewEverything viewMode = iota
	// viewStatus narrows to one server-side status filter.
	viewStatus
)

// todoFilter is one combination of what the Todos tab shows and how it is
// laid out. The view owns the one on screen and asks the server for the
// todos it describes, and the preferences keep part of it between runs.
// It is a plain value, so a combination can be kept, compared and put
// back whole rather than field by field.
type todoFilter struct {
	// mode and status describe what is being shown: either everything, or
	// one server-side status filter.
	mode   viewMode
	status string
	// listID narrows to one list; empty means every list.
	listID string
	// assigned narrows to "me", "anyone" or one user id; empty means
	// whoever the todo is assigned to.
	assigned string
	// query is the search the server was asked for. What is being typed
	// into the search box is not part of it until it is committed.
	query string
	// undated narrows to the todos with no date. The server ANDs it with
	// the status, which undatedFits keeps from being empty by construction.
	undated bool
	// board lays the todos out as a column per day instead of one list.
	board bool
}

// defaultFilter is what a first run shows.
func defaultFilter() todoFilter {
	return todoFilter{mode: viewEverything, status: api.StatusToday}
}

// unfiltered reports whether the filter lets the whole account through.
// The layout does not count: the board shows the same todos as the list.
func (f todoFilter) unfiltered() bool {
	return f.mode == viewEverything && f.query == "" && f.assigned == "" && f.listID == "" && !f.undated
}

// request is what to ask the server for, up to limit todos.
func (f todoFilter) request(limit int) api.TodoFilter {
	r := api.TodoFilter{
		Query:    f.query,
		Limit:    limit,
		Assigned: f.assigned,
		ListID:   f.listID,
		Undated:  f.undated,
	}
	if f.mode == viewStatus {
		r.Status = f.status
	} else {
		// One request for everything, bucketed on this side. That is what
		// lets the inbox and the dated sections share a single view.
		r.Status = api.StatusAll
	}
	return r
}

// is reports whether f and g show the same todos laid out the same way.
func (f todoFilter) is(g todoFilter) bool {
	return f.request(0) == g.request(0) && f.board == g.board
}

// undatedFits reports whether the undated filter can go with the status.
// The server ANDs them, and a status that is about a date cannot hold a
// todo that has none.
func undatedFits(f todoFilter) bool {
	if f.mode == viewEverything {
		return true
	}
	switch f.status {
	case api.StatusToday, api.StatusDue, api.StatusUpcoming, api.StatusOverdue:
		return false
	}
	return true
}

// shows reports whether the filter is already the given mode and status.
// A status left behind by the Everything view does not count.
func (f todoFilter) shows(mode viewMode, status string) bool {
	return f.mode == mode && (mode == viewEverything || f.status == status)
}

// filterCycle is what f walks through. Everything comes first and last,
// because it is the view that never hides a capture.
var filterCycle = []struct {
	mode   viewMode
	status string
	label  string
}{
	{viewEverything, "", "Everything"},
	{viewStatus, api.StatusOverdue, "Overdue only"},
	{viewStatus, api.StatusToday, "Today only"},
	{viewStatus, api.StatusUpcoming, "Upcoming only"},
	{viewStatus, api.StatusCompleted, "Completed only"},
}

// scopeLabel names what the filter shows, in words a person would use.
func (f todoFilter) scopeLabel() string {
	if f.mode == viewEverything {
		return "Everything"
	}
	for _, c := range filterCycle {
		if c.status == f.status {
			return c.label
		}
	}
	return f.status
}
