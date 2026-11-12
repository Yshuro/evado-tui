package config

import (
	"strings"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// SavedView is a todo filter combination the user named, kept as a
// preference. It holds what to ask the server for and how to lay it out,
// never a todo or a date: a status such as "today" is stored as that word
// and means whatever day it is, in the account's timezone, when the view is
// opened.
type SavedView struct {
	Name string `json:"name"`
	// Status is a server status keyword. Empty means the Everything view,
	// which is not a status.
	Status string `json:"status,omitempty"`
	// ListID narrows to one list, and ListName is what it was called when
	// the view was saved, so that a list which has gone can still be named.
	ListID   string `json:"listId,omitempty"`
	ListName string `json:"listName,omitempty"`
	// Assigned is "me", "anyone" or a user id, and AssignedName the label
	// that user had when the view was saved, for the same reason.
	Assigned     string `json:"assigned,omitempty"`
	AssignedName string `json:"assignedName,omitempty"`
	Query        string `json:"query,omitempty"`
	Undated      bool   `json:"undated,omitempty"`
	Board        bool   `json:"board,omitempty"`
}

// ViewScope says whose saved views are meant: those of one account on one
// server. The zero value is use without an account, which has views of its
// own so that nothing carries over from one person to the next.
type ViewScope struct {
	Server  string
	Account string
}

// AccountViews is the scope of a signed-in account. The server is written
// the way the client writes it, so an address typed with a trailing slash
// is the same server.
func AccountViews(server, userID string) ViewScope {
	return ViewScope{Server: api.New("", api.WithBaseURL(api.MigrateLegacy(server))).BaseURL(), Account: userID}
}

// ViewSet is the saved views of one scope, as they are written to the file.
type ViewSet struct {
	Server  string      `json:"server,omitempty"`
	Account string      `json:"account,omitempty"`
	Views   []SavedView `json:"views"`
}

func (s ViewSet) scope() ViewScope { return ViewScope{s.Server, s.Account} }

// ViewsFor returns the saved views of one scope, in the order they were
// made. The slice is a copy, so editing it changes nothing until SetViews.
func (c Config) ViewsFor(s ViewScope) []SavedView {
	for _, set := range c.SavedViews {
		if set.scope() == s {
			return append([]SavedView(nil), set.Views...)
		}
	}
	return nil
}

// SetViews replaces the saved views of one scope and leaves every other
// scope as it was. A scope left with none is dropped from the file.
func (c *Config) SetViews(s ViewScope, views []SavedView) {
	views = append([]SavedView(nil), views...)
	sets := make([]ViewSet, 0, len(c.SavedViews)+1)
	placed := false
	for _, set := range c.SavedViews {
		if set.scope() != s {
			sets = append(sets, set)
			continue
		}
		if len(views) > 0 && !placed {
			sets = append(sets, ViewSet{Server: s.Server, Account: s.Account, Views: views})
		}
		placed = true
	}
	if !placed && len(views) > 0 {
		sets = append(sets, ViewSet{Server: s.Server, Account: s.Account, Views: views})
	}
	c.SavedViews = sets
}

// SameName reports whether two view names are the same view. Names are
// compared without regard to case or surrounding space, so "Work today" and
// "work today " cannot both exist.
func SameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
