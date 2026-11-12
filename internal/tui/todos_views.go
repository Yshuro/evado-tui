package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

// Saved views: a filter combination the user gave a name, to come back to
// in one step. A view is only what to ask for and how to lay it out, so
// opening one is choosing its filter, and the rules here are about the
// ways that can go wrong. A view that names a list or a person who is gone
// is never opened with that part quietly left out, because that would show
// more than the view was made to show. It opens in a repair state that says
// what is missing, and the filter changes only when the user picks what
// replaces it or drops it.

// maxViewName is the longest name a view can have, so it fits the sidebar
// and the palette.
const maxViewName = 40

// issueKind is what about a saved view can no longer be found.
type issueKind int

const (
	issueList issueKind = iota
	issuePerson
	issueStatus
)

// repairIssue is one part of a saved view that cannot be used as saved.
type repairIssue struct {
	kind issueKind
	// what says it in words, for the screen that asks for the repair.
	what string
}

// repairState is a saved view that was asked for and not opened.
type repairState struct {
	view   config.SavedView
	issues []repairIssue
}

// The choices that stand for leaving a part of a view out, in the repair
// form. They are choices like any other: nothing picks them for the user.
const (
	dropList   = "Drop the list, show every list"
	dropPerson = "Drop the person, show everyone's"
	dropStatus = "Drop the status, show everything"
)

// -- the views on screen ----------------------------------------------------

// savedViews are the views of whoever is signed in, or of the guest, in the
// order they were made. There are none while it is not yet known who that
// is, since another account's views must never stand in for them.
func (v *todosView) savedViews() []config.SavedView {
	scope, ok := v.m.viewScope()
	if !ok {
		return nil
	}
	return viewsOf(v.m.cfg, scope)
}

// viewNamed finds a view of the current scope by name.
func (v *todosView) viewNamed(name string) (config.SavedView, bool) {
	for _, sv := range v.savedViews() {
		if config.SameName(sv.Name, name) {
			return sv, true
		}
	}
	return config.SavedView{}, false
}

// activeView is the saved view the list is showing, which is the one last
// opened for as long as the filter is still the one it saved. Changing a
// filter by hand makes it the plain filter again, so the title never
// claims a view the list is no longer showing.
func (v *todosView) activeView() (config.SavedView, bool) {
	if v.opened == "" || v.repair != nil {
		return config.SavedView{}, false
	}
	sv, ok := v.viewNamed(v.opened)
	if !ok || !filterOf(sv).is(v.filter) {
		return config.SavedView{}, false
	}
	return sv, true
}

// dropViewState forgets which view is open, for when the account changes
// under it.
func (v *todosView) dropViewState() {
	v.opened, v.repair = "", nil
	v.openSeq++
}

// filterOf is the filter a saved view stands for.
func filterOf(sv config.SavedView) todoFilter {
	f := todoFilter{
		mode: viewEverything, status: api.StatusToday,
		listID: sv.ListID, assigned: sv.Assigned, query: sv.Query,
		undated: sv.Undated, board: sv.Board,
	}
	if sv.Status != "" {
		f.mode, f.status = viewStatus, sv.Status
	}
	return f
}

// savedOf is the view a filter would be saved as. Names are kept next to
// the ids so a list or a person who is gone later can still be named.
func (m *Model) savedOf(name string, f todoFilter) config.SavedView {
	sv := config.SavedView{
		Name: name, ListID: f.listID, Assigned: f.assigned, Query: f.query,
		Undated: f.undated, Board: f.board,
	}
	if f.mode == viewStatus {
		sv.Status = f.status
	}
	if f.listID != "" {
		sv.ListName = m.listName(f.listID)
	}
	if specificPerson(f.assigned) {
		sv.AssignedName = m.personName(f.assigned)
	}
	return sv
}

// specificPerson reports whether an assignee filter names one user rather
// than "me", "anyone" or nobody in particular.
func specificPerson(assigned string) bool {
	return assigned != "" && assigned != "me" && assigned != "anyone"
}

// knownStatuses are the statuses the server filters on.
var knownStatuses = []string{
	api.StatusOpen, api.StatusToday, api.StatusDue, api.StatusUpcoming,
	api.StatusOverdue, api.StatusCompleted, api.StatusAll,
}

// -- people -----------------------------------------------------------------

// person is someone a todo can be assigned to, as the assignee filter
// lists them.
type person struct {
	id, label string
}

// personName is what to call a user: "you", the name their list members
// carry, or a short id when none is loaded.
func (m *Model) personName(id string) string {
	if id == m.user.ID && id != "" {
		return "you"
	}
	for _, list := range m.lists {
		for _, mem := range m.acct.memberCache[list.ID] {
			if mem.UserID == id {
				return mem.Name()
			}
		}
	}
	return shortID(id)
}

// people are the members of the given lists other than the user, each once,
// in the order the lists and their members are kept. A label two people
// share gets their short id too, so the picker never offers two of the
// same.
func (m *Model) people(listIDs []string) []person {
	var out []person
	seen := map[string]bool{m.user.ID: true}
	count := map[string]int{}
	for _, id := range listIDs {
		for _, mem := range m.acct.memberCache[id] {
			if seen[mem.UserID] {
				continue
			}
			seen[mem.UserID] = true
			out = append(out, person{id: mem.UserID, label: mem.Name()})
			count[mem.Name()]++
		}
	}
	for i, p := range out {
		if count[p.label] > 1 {
			out[i].label = p.label + " · " + shortID(p.id)
		}
	}
	return out
}

// scopeLists are the lists a person filter can be about: the one on
// screen, or all of them.
func (m *Model) scopeLists(listID string) []string {
	if _, ok := m.listByID(listID); ok && listID != "" {
		return []string{listID}
	}
	ids := make([]string, len(m.lists))
	for i, l := range m.lists {
		ids[i] = l.ID
	}
	return ids
}

// -- saving -----------------------------------------------------------------

// viewSaveErr says a saved view could not be written, in the way every
// such failure is reported: the change holds for this run, and only the
// next start is affected.
func viewSaveErr(err error) string {
	return "kept for now, but the settings file could not be written, so the next start will not have it: " + errText(err)
}

// store writes the views of the current scope, and the view prefs with
// them, to the settings file. The token is not among what is written.
func (v *todosView) store(views []config.SavedView) error {
	scope, ok := v.m.viewScope()
	if !ok {
		return fmt.Errorf("no account is open yet")
	}
	return v.keepViews(scope, views)
}

// validName checks a name for a view, which is the same for saving and
// renaming. except is the view being renamed, whose own name is free.
func (v *todosView) validName(name, except string) string {
	switch {
	case name == "":
		return "a view needs a name"
	case utf8.RuneCountInString(name) > maxViewName:
		return fmt.Sprintf("a view's name is at most %d characters", maxViewName)
	}
	for _, sv := range v.savedViews() {
		if config.SameName(sv.Name, name) && !config.SameName(sv.Name, except) {
			return fmt.Sprintf("there is already a view called %q", sv.Name)
		}
	}
	return ""
}

// openSaveView asks for a name for the filters on screen.
func (v *todosView) openSaveView(m *Model, name, problem string) tea.Cmd {
	if _, ok := m.viewScope(); !ok {
		return m.warnf("still signing in; try again in a moment")
	}
	if v.repair != nil {
		return m.warnf("%s needs repair first; its filters are not on screen", v.repair.view.Name)
	}
	f := newForm(formSaveView, "Save this view", m.width)
	f.note("what", "Saving: "+v.summary(m, v.filter))
	if problem != "" {
		f.alert(problem)
	}
	f.field("name", "name", name, "e.g. Work today")
	return m.openForm(f)
}

// saveView keeps the filters on screen under a name.
func (v *todosView) saveView(m *Model, name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if problem := v.validName(name, ""); problem != "" {
		return tea.Batch(m.warnf("%s", problem), v.openSaveView(m, name, problem))
	}
	views := append(v.savedViews(), m.savedOf(name, v.filter))
	v.opened = name
	if err := v.store(views); err != nil {
		return m.warnf("saved %q, %s", name, viewSaveErr(err))
	}
	return m.okf("saved view %q: %s", name, v.summary(m, v.filter))
}

// updateView overwrites a view with the filters on screen.
func (v *todosView) updateView(m *Model, name string) tea.Cmd {
	views := v.savedViews()
	i := viewIndex(views, name)
	if i < 0 {
		return m.warnf("there is no view called %q", name)
	}
	if v.repair != nil {
		return m.warnf("%s needs repair first; its filters are not on screen", v.repair.view.Name)
	}
	views[i] = m.savedOf(views[i].Name, v.filter)
	v.opened = views[i].Name
	if err := v.store(views); err != nil {
		return m.warnf("updated %q, %s", views[i].Name, viewSaveErr(err))
	}
	return m.okf("updated view %q: %s", views[i].Name, v.summary(m, v.filter))
}

// openRenameView asks for a view's new name.
func (v *todosView) openRenameView(m *Model, name, problem string) tea.Cmd {
	f := newForm(formRenameView, "Rename view", m.width)
	if problem != "" {
		f.alert(problem)
	}
	f.field("name", "name", name, "")
	f.orig = map[string]string{"name": name}
	return m.openForm(f)
}

// renameView gives a view a new name, leaving everything else about it.
func (v *todosView) renameView(m *Model, old, name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if problem := v.validName(name, old); problem != "" {
		return tea.Batch(m.warnf("%s", problem), v.openRenameView(m, old, problem))
	}
	views := v.savedViews()
	i := viewIndex(views, old)
	if i < 0 {
		return m.warnf("there is no view called %q", old)
	}
	views[i].Name = name
	if v.repair != nil && config.SameName(v.repair.view.Name, old) {
		v.repair.view.Name = name
	}
	if config.SameName(v.opened, old) {
		v.opened = name
	}
	if err := v.store(views); err != nil {
		return m.warnf("renamed to %q, %s", name, viewSaveErr(err))
	}
	return m.okf("renamed %q to %q", old, name)
}

// askRemoveView confirms before a view is removed. Only the view goes: its
// todos, lists and every other view stay as they are.
func (v *todosView) askRemoveView(m *Model, name string) tea.Cmd {
	return m.ask("remove-view", "Remove this view?",
		fmt.Sprintf("%q will be removed from the views saved on this computer. Your todos and lists are not touched.", name),
		false, func() tea.Cmd { return v.removeView(m, name) })
}

// removeView drops a view's entry from the preferences. It settles the
// confirmation's busy state itself, since nothing is waiting on a server.
func (v *todosView) removeView(m *Model, name string) tea.Cmd {
	views := v.savedViews()
	i := viewIndex(views, name)
	if i < 0 {
		return settled(m.warnf("there is no view called %q", name))
	}
	removed := views[i].Name
	views = slices.Delete(views, i, i+1)
	if config.SameName(v.opened, removed) {
		v.opened = ""
	}
	if v.repair != nil && config.SameName(v.repair.view.Name, removed) {
		v.repair = nil
	}
	if err := v.store(views); err != nil {
		return settled(m.warnf("removed %q, %s", removed, viewSaveErr(err)))
	}
	return settled(m.okf("removed view %q", removed))
}

// settledMsg ends the busy state a confirmation started and shows the
// toast that came with it.
type settledMsg struct{ toast tea.Cmd }

func settled(toast tea.Cmd) tea.Cmd {
	return func() tea.Msg { return settledMsg{toast: toast} }
}

func viewIndex(views []config.SavedView, name string) int {
	return slices.IndexFunc(views, func(sv config.SavedView) bool { return config.SameName(sv.Name, name) })
}

// -- the menu ---------------------------------------------------------------

// The things the views menu does to the view it is pointed at.
const (
	menuOpen   = "Open"
	menuRename = "Rename"
	menuUpdate = "Update to the filters on screen"
	menuRemove = "Remove"
)

// openViewMenu lists the saved views and what can be done to one.
func (v *todosView) openViewMenu(m *Model) tea.Cmd {
	views := v.savedViews()
	if len(views) == 0 {
		return m.warnf("no saved views yet: press s to save the filters on screen")
	}
	names := make([]string, len(views))
	for i, sv := range views {
		names[i] = sv.Name
	}
	f := newForm(formViewMenu, "Saved views", m.width)
	f.selectValue("view", "view", names, v.cursorView())
	f.selectField("do", "do", []string{menuOpen, menuRename, menuUpdate, menuRemove})
	return m.openForm(f)
}

// cursorView is the view the menu starts on: the one the sidebar is on,
// else the one open.
func (v *todosView) cursorView() string {
	if v.m.focus == focusSidebar {
		items := v.sideItems()
		if v.side >= 0 && v.side < len(items) && items[v.side].isSaved {
			return items[v.side].label
		}
	}
	return v.opened
}

// applyViewMenu does what the menu was asked.
func (v *todosView) applyViewMenu(m *Model, name, do string) tea.Cmd {
	sv, ok := v.viewNamed(name)
	if !ok {
		return m.warnf("there is no view called %q", name)
	}
	switch do {
	case menuRename:
		return v.openRenameView(m, sv.Name, "")
	case menuUpdate:
		return v.updateView(m, sv.Name)
	case menuRemove:
		return v.askRemoveView(m, sv.Name)
	}
	return v.openSaved(m, sv.Name)
}

// -- opening ----------------------------------------------------------------

// issuesOf are the parts of a view that can be told missing without asking
// anyone: a list that is not among the account's, a status the server does
// not filter on.
func (v *todosView) issuesOf(sv config.SavedView) []repairIssue {
	m := v.m
	var out []repairIssue
	if sv.ListID != "" {
		if _, ok := m.listByID(sv.ListID); !ok {
			out = append(out, repairIssue{issueList, fmt.Sprintf("the list %s is no longer available", quoted(sv.ListName, sv.ListID))})
		}
	}
	if sv.Status != "" && !slices.Contains(knownStatuses, sv.Status) {
		out = append(out, repairIssue{issueStatus, fmt.Sprintf("the status %q is not one this app knows", sv.Status)})
	}
	return out
}

// quoted is a name in quotes, or the id when it was never known.
func quoted(name, id string) string {
	if name == "" {
		name = shortID(id)
	}
	return "“" + name + "”"
}

// openSaved shows a saved view, or puts it in the repair state when a part
// of it is missing. Who the people of a list are is asked of the server
// first, since a former member cannot be told from the cached names.
func (v *todosView) openSaved(m *Model, name string) tea.Cmd {
	sv, ok := v.viewNamed(name)
	if !ok {
		return m.warnf("there is no view called %q", name)
	}
	if m.lists == nil {
		return m.warnf("your lists are still loading; try again in a moment")
	}
	issues := v.issuesOf(sv)
	if !specificPerson(sv.Assigned) {
		return v.settleOpen(m, sv, issues)
	}
	v.openSeq++
	m.busy = true
	return tea.Batch(m.setBusy(""), fetchMembersFor(m.ctx, m.client, m.scopeLists(sv.ListID), v.openSeq, sv.Name))
}

// membersForMsg is the members of some lists, asked for to settle whether a
// person a view names still belongs to one of them, or to fill the person
// picker, which is what an empty name says. failed says some list could not
// be read, so who is missing is not known.
type membersForMsg struct {
	seq    int
	name   string
	byList map[string][]api.Member
	failed bool
}

// fetchMembersFor reads the members of each list.
func fetchMembersFor(ctx context.Context, c *api.Client, listIDs []string, seq int, name string) tea.Cmd {
	return func() tea.Msg {
		out := membersForMsg{seq: seq, name: name, byList: map[string][]api.Member{}}
		for _, id := range listIDs {
			ms, err := c.ListMembers(ctx, id)
			if err != nil {
				out.failed = true
				continue
			}
			out.byList[id] = ms
		}
		return out
	}
}

// onMembers carries on opening a view once its people are known.
func (v *todosView) onMembers(msg membersForMsg) tea.Cmd {
	m := v.m
	// A newer open has replaced this one, or the account changed under it.
	if msg.seq != v.openSeq {
		return nil
	}
	m.done()
	sv, ok := v.viewNamed(msg.name)
	if !ok {
		return nil
	}
	for id, ms := range msg.byList {
		m.acct.memberCache[id] = ms
	}
	issues := v.issuesOf(sv)
	if !msg.failed && !v.isMember(sv.Assigned, msg.byList) {
		scope := "any of your lists"
		if _, ok := m.listByID(sv.ListID); ok && sv.ListID != "" {
			scope = m.listName(sv.ListID)
		}
		issues = append(issues, repairIssue{issuePerson, fmt.Sprintf("%s is no longer a member of %s", v.personLabel(sv), scope)})
	}
	return v.settleOpen(m, sv, issues)
}

// isMember reports whether a user belongs to any of the lists.
func (v *todosView) isMember(id string, byList map[string][]api.Member) bool {
	for _, ms := range byList {
		for _, mem := range ms {
			if mem.UserID == id {
				return true
			}
		}
	}
	return false
}

// personLabel is what a view called its person when it was saved.
func (v *todosView) personLabel(sv config.SavedView) string {
	if sv.AssignedName != "" {
		return sv.AssignedName
	}
	return shortID(sv.Assigned)
}

// settleOpen shows the view, or the repair state when it has issues.
func (v *todosView) settleOpen(m *Model, sv config.SavedView, issues []repairIssue) tea.Cmd {
	v.opened = sv.Name
	m.focus = focusList
	if len(issues) > 0 {
		v.repair = &repairState{view: sv, issues: issues}
		return nil
	}
	v.repair = nil
	f := filterOf(sv)
	v.live = ""
	cmd := v.choose(m, f)
	if cmd == nil {
		// Already on screen, but opening a view is asking again: "today"
		// may be another day than when it was last loaded.
		m.busy = true
		cmd = tea.Batch(m.setBusy(""), v.load())
	}
	return tea.Batch(cmd, m.okf("opened %q: %s", sv.Name, v.summary(m, f)))
}

// enterRepair puts the view that is on screen into the repair state, for
// when what it names disappears while it is open.
func (v *todosView) enterRepair(sv config.SavedView, issues []repairIssue) {
	v.opened = sv.Name
	v.repair = &repairState{view: sv, issues: issues}
}

// summary is a filter in words: the same parts the title shows, without
// the name of a view.
func (v *todosView) summary(m *Model, f todoFilter) string {
	return strings.Join(m.filterParts(f, "", ""), " · ")
}

// -- repairing --------------------------------------------------------------

// openRepair asks what should replace each missing part of the view, or
// whether to drop it.
func (v *todosView) openRepair(m *Model) tea.Cmd {
	r := v.repair
	if r == nil {
		return nil
	}
	f := newForm(formRepairView, "Repair “"+r.view.Name+"”", m.width)
	for _, is := range r.issues {
		f.alert(capitalize(is.what) + ".")
	}
	f.note("hint", "Pick what replaces it, or drop that part. Nothing changes until you save.")
	for _, is := range r.issues {
		switch is.kind {
		case issueList:
			opts := make([]string, 0, len(m.lists)+1)
			for _, l := range m.lists {
				opts = append(opts, l.Name)
			}
			f.selectField("list", "list", append(opts, dropList))
		case issuePerson:
			opts := []string{}
			for _, p := range m.people(m.scopeLists(r.view.ListID)) {
				opts = append(opts, p.label)
			}
			f.selectField("person", "person", append(opts, dropPerson))
		case issueStatus:
			opts := []string{}
			for _, c := range filterCycle[1:] {
				opts = append(opts, c.label)
			}
			f.selectField("status", "status", append(opts, dropStatus))
		}
	}
	f.orig = map[string]string{"name": r.view.Name}
	return m.openForm(f)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// applyRepair rewrites the view with what was picked, keeps it, and opens
// it again, which checks the new parts the same way as the old ones.
func (v *todosView) applyRepair(m *Model, name string, values map[string]string) tea.Cmd {
	views := v.savedViews()
	i := viewIndex(views, name)
	if i < 0 {
		return m.warnf("there is no view called %q", name)
	}
	sv := views[i]
	// The people offered were those of the lists the view had, so that is
	// where a pick is looked up, even if the list is being replaced too.
	offered := m.people(m.scopeLists(sv.ListID))
	if pick, ok := values["person"]; ok {
		sv.Assigned, sv.AssignedName = "", ""
		if pick != dropPerson {
			at := slices.IndexFunc(offered, func(p person) bool { return p.label == pick })
			if at < 0 {
				return m.warnf("%s is no longer listed; open the repair again", pick)
			}
			sv.Assigned, sv.AssignedName = offered[at].id, offered[at].label
		}
	}
	if pick, ok := values["list"]; ok {
		sv.ListID, sv.ListName = "", ""
		if pick != dropList {
			for _, l := range m.lists {
				if l.Name == pick {
					sv.ListID, sv.ListName = l.ID, l.Name
					break
				}
			}
		}
	}
	if pick, ok := values["status"]; ok {
		sv.Status = ""
		if pick != dropStatus {
			for _, c := range filterCycle[1:] {
				if c.label == pick {
					sv.Status = c.status
					break
				}
			}
		}
	}
	views[i] = sv
	v.repair = nil
	var cmds []tea.Cmd
	if err := v.store(views); err != nil {
		cmds = append(cmds, m.warnf("repaired %q, %s", sv.Name, viewSaveErr(err)))
	}
	return tea.Batch(append(cmds, v.openSaved(m, sv.Name))...)
}

// -- the person filter ------------------------------------------------------

// Choices in the person picker that are not a member.
const (
	pickAnyone     = "Everyone, no filter"
	pickMe         = "Me"
	pickUnassigned = "Nobody, unassigned todos"
)

// openPersonFilter asks whose todos to show. The members of the lists in
// scope are read first, so a person who joined a moment ago is there.
func (v *todosView) openPersonFilter(m *Model) tea.Cmd {
	ids := m.scopeLists(v.filter.listID)
	if len(ids) == 0 {
		return m.warnf("no lists to pick a person from")
	}
	v.openSeq++
	m.busy = true
	return tea.Batch(m.setBusy(""), fetchMembersFor(m.ctx, m.client, ids, v.openSeq, ""))
}

// onPeople shows the person picker once the members are in.
func (v *todosView) onPeople(msg membersForMsg) tea.Cmd {
	m := v.m
	if msg.seq != v.openSeq {
		return nil
	}
	m.done()
	for id, ms := range msg.byList {
		m.acct.memberCache[id] = ms
	}
	opts := []string{pickAnyone, pickMe, pickUnassigned}
	current := pickAnyone
	switch v.filter.assigned {
	case "me":
		current = pickMe
	case "anyone":
		current = pickUnassigned
	}
	for _, p := range m.people(m.scopeLists(v.filter.listID)) {
		opts = append(opts, p.label)
		if p.id == v.filter.assigned {
			current = p.label
		}
	}
	f := newForm(formPersonFilter, "Show todos assigned to", m.width)
	if msg.failed {
		f.note("hint", "Some lists could not be read, so the people may be incomplete.")
	}
	f.selectValue("person", "assigned to", opts, current)
	return m.openForm(f)
}

// applyPersonFilter narrows the todos to what was picked.
func (v *todosView) applyPersonFilter(m *Model, pick string) tea.Cmd {
	f := v.filter
	switch pick {
	case pickAnyone:
		f.assigned = ""
	case pickMe:
		f.assigned = "me"
	case pickUnassigned:
		f.assigned = "anyone"
	default:
		found := false
		for _, p := range m.people(m.scopeLists(f.listID)) {
			if p.label == pick {
				f.assigned, found = p.id, true
				break
			}
		}
		if !found {
			return m.warnf("that person is no longer in the list")
		}
	}
	return v.choose(m, f)
}

// toggleUndated narrows to the todos with no date, or back. The server
// answers it with any status, but no dated status can hold an undated
// todo, so turning it on leaves those for Everything rather than asking
// for something that is empty by construction.
func (v *todosView) toggleUndated(m *Model) tea.Cmd {
	f := v.filter
	f.undated = !f.undated
	if f.undated && !undatedFits(f) {
		f.mode = viewEverything
	}
	return v.choose(m, f)
}
