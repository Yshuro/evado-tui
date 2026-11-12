package tui

import (
	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

// The Todos tab remembers where it was left between runs. This file is the
// only place that reads those preferences from the config or writes them
// back. Changing the filter saves nothing by itself: the actions that pick
// a view to come back to call remember, and drawing the view, moving the
// cursor or loading todos never do.

// todoPrefs is what the Todos tab keeps between runs: of the filter, its
// status, list and layout, since the assignee and the search are for the
// moment, and whether the sidebar is folded away.
type todoPrefs struct {
	filter   todoFilter
	hideSide bool
}

// prefsOf reads the preferences out of a config. A mode label that no
// longer matches anything leaves the default Everything view, which cannot
// hide a capture.
func prefsOf(cfg config.Config) todoPrefs {
	p := todoPrefs{filter: defaultFilter(), hideSide: cfg.HideSidebar}
	for _, f := range filterCycle {
		if f.label == cfg.Mode {
			p.filter.mode, p.filter.status = f.mode, f.status
			if f.mode == viewEverything {
				p.filter.status = api.StatusToday
			}
		}
	}
	p.filter.listID = cfg.ListID
	p.filter.board = cfg.Board
	return p
}

// save writes the preferences into cfg and into the file on disk, leaving
// everything else in the file alone. Mode and status are written together:
// a status left behind by an earlier view would otherwise reopen as the
// wrong one.
func (p todoPrefs) save(cfg *config.Config) error {
	cfg.Mode = p.filter.scopeLabel()
	cfg.Status = ""
	if p.filter.mode == viewStatus {
		cfg.Status = p.filter.status
	}
	cfg.ListID = p.filter.listID
	cfg.Board = p.filter.board
	cfg.HideSidebar = p.hideSide
	return cfg.SavePrefs()
}

// restore opens the view on the preferences in cfg.
func (v *todosView) restore(cfg config.Config) {
	p := prefsOf(cfg)
	v.filter, v.hideSide = p.filter, p.hideSide
}

// remember saves where the user is looking, so the next run opens on the
// same view and list. A save that fails is not worth interrupting anyone
// for: the view on screen has changed all the same, and only the next
// start is affected.
func (v *todosView) remember() {
	_ = todoPrefs{filter: v.filter, hideSide: v.hideSide}.save(&v.m.cfg)
}

// Saved views are preferences too, kept next to the view the tab reopens
// on. They belong to a server and an account, or to no account, and
// nothing here ever reads or writes the token.

// viewScope is whose saved views the tab shows. Signed in, it is the
// account on the server in use, which is only known once the account has
// been read; until then there are no views to show, because another
// account's must not stand in for them. With no account it is the guest's,
// which is a set of its own.
func (m *Model) viewScope() (config.ViewScope, bool) {
	if m.cfg.Token == "" && m.session == nil && m.joining == nil {
		return config.ViewScope{}, true
	}
	// A sign-in under way may be another account, and the one on record
	// is still the last.
	if m.user.ID == "" || m.joining != nil || m.signingIn {
		return config.ViewScope{}, false
	}
	return config.AccountViews(m.serverURL(), m.user.ID), true
}

// viewsOf reads the saved views of one scope out of a config.
func viewsOf(cfg config.Config, scope config.ViewScope) []config.SavedView {
	return cfg.ViewsFor(scope)
}

// keepViews writes the saved views of a scope. The view on screen is
// written with them, as any other save of the preferences does, and every
// other scope's views are left as they are. Unlike remembering the view,
// the caller is told when it fails, since saving was what was asked for.
func (v *todosView) keepViews(scope config.ViewScope, views []config.SavedView) error {
	cfg := &v.m.cfg
	cfg.SetViews(scope, views)
	return todoPrefs{filter: v.filter, hideSide: v.hideSide}.save(cfg)
}
