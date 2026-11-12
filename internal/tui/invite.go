package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/local"
)

// Inviting someone to a list takes a username or an email typed in, or a
// person picked from those the account already shares a list with. Nobody
// joins until they accept, so nothing here says that anyone has.

// noPerson is the picker's first choice, which leaves the typed text to
// say who to invite.
const noPerson = "(type below)"

// inviteDraft is an invitation being written. It outlives the form that
// holds it, so a refusal can bring the form back with what was typed.
type inviteDraft struct {
	listID   string
	listName string
	// people are who the picker offers, by the label it shows.
	people map[string]api.Person
	order  []string
	// who and person are what the form held when it was sent.
	who, person string
}

type (
	// peopleMsg is the answer to asking whom the account shares lists with.
	peopleMsg struct {
		listID string
		people []api.Person
		err    error
	}
	// invitedMsg is the answer to sending an invitation.
	invitedMsg struct {
		listID  string
		label   string
		invitee api.Invitee
		err     error
	}
)

func fetchPeople(ctx context.Context, c *api.Client, listID string) tea.Cmd {
	return func() tea.Msg {
		ps, err := c.People(ctx)
		return peopleMsg{listID: listID, people: ps, err: err}
	}
}

func inviteMember(ctx context.Context, c *api.Client, listID string, who api.InviteTarget, label string) tea.Cmd {
	return func() tea.Msg {
		got, err := c.InviteMember(ctx, listID, who)
		return invitedMsg{listID: listID, label: label, invitee: got, err: err}
	}
}

// unreachable reports that an error means Evado could not be reached, as
// opposed to it answering with a refusal.
func unreachable(err error) bool {
	return api.CodeOf(err) == api.CodeOffline || local.Offline(err)
}

// startInvite checks that the list can be invited to, and asks whom the
// account knows so the form can offer them.
func (v *listsView) startInvite(m *Model) tea.Cmd {
	l := v.selected()
	if l == nil {
		return nil
	}
	// The spec lists no inviter role, only read-only lists, so whether a
	// plain member may invite is not something this client can know. The
	// owner-only rule it always had stays until the service is seen to
	// allow more; it enforces its own rules either way.
	switch {
	case l.IsPersonal:
		return m.warnf("your personal list cannot be shared; make a new list to share")
	case l.Role != "owner":
		return m.warnf("only the owner can invite people")
	case l.ReadOnly:
		return m.warnf("this list is frozen after the owner left")
	case m.guest():
		return m.warnf("inviting people needs an Evado account; sign in from Account, Connection")
	case v.notSynced(l.ID):
		return m.warnf("%s", local.NotSyncedMessage)
	}
	m.invite = &inviteDraft{listID: l.ID, listName: l.Name}
	m.busy = true
	return tea.Batch(m.setBusy("finding people"), fetchPeople(m.ctx, m.client, l.ID))
}

// guest reports that there is no Evado account behind the todos here.
func (m *Model) guest() bool {
	return m.backend != nil && !m.backend.Status().SignedIn
}

// onPeople opens the invite form once the people the picker offers are
// known. Failing to load them only costs the picker.
func (m *Model) onPeople(msg peopleMsg) tea.Cmd {
	m.done()
	d := m.invite
	if d == nil || d.listID != msg.listID || m.layer != layerNone {
		return nil
	}
	switch {
	case api.CodeOf(msg.err) == api.CodeUnauthorized:
		return m.failf(msg.err, "")
	case msg.err != nil && unreachable(msg.err):
		m.invite = nil
		return m.warnf("inviting needs a connection; try again when you are online")
	}
	d.people, d.order = map[string]api.Person{}, nil
	for _, p := range msg.people {
		if containsString(p.SharedListIDs, d.listID) {
			continue // already in this list
		}
		label := p.Name()
		if p.Username != nil && p.Email != "" {
			label += " · " + p.Email
		}
		for n := 2; ; n++ {
			if _, taken := d.people[label]; !taken {
				break
			}
			label = fmt.Sprintf("%s (%d)", p.Name(), n)
		}
		d.people[label] = p
		d.order = append(d.order, label)
	}
	alert := ""
	if msg.err != nil {
		alert = "Could not load the people you share lists with: " + errText(msg.err)
	}
	return m.openInviteForm(d, alert)
}

func (m *Model) openInviteForm(d *inviteDraft, alert string) tea.Cmd {
	f := newForm(formInvite, "Invite to "+d.listName, m.width)
	if alert != "" {
		f.alert(alert)
	}
	f.note("hint", "Type a username or the email of their Evado account. They join once they accept. "+
		"For someone without an account, press i to create an invite link instead.")
	if len(d.order) > 0 {
		f.selectValue("person", "person", append([]string{noPerson}, d.order...), d.person)
	}
	f.field("who", "username or email", d.who, "@sam or name@example.com")
	return m.openForm(f)
}

// inviteTarget reads the form into the one identifier the service wants.
// What was typed and what was picked are never both sent.
//
// The last result is what is wrong with the form, or empty.
func inviteTarget(d *inviteDraft, who, person string) (api.InviteTarget, string, string) {
	who = strings.TrimSpace(who)
	picked, isPicked := d.people[person]
	switch {
	case who != "" && isPicked:
		return api.InviteTarget{}, "", "Choose a person or type a username or email, not both."
	case isPicked:
		return api.InviteTarget{UserID: picked.UserID}, picked.Name(), ""
	case who == "":
		return api.InviteTarget{}, "", "Type a username or email, or choose someone you already share a list with."
	case strings.Contains(who, "@") && !strings.HasPrefix(who, "@"):
		return api.InviteTarget{Email: who}, who, ""
	}
	return api.InviteTarget{Username: who}, "@" + strings.TrimPrefix(who, "@"), ""
}

// submitInvite sends the invitation, or brings the form back if what it
// held cannot be sent.
func (m *Model) submitInvite(v map[string]string) tea.Cmd {
	d := m.invite
	if d == nil {
		return m.warnf("select a list first")
	}
	d.who, d.person = v["who"], v["person"]
	target, label, problem := inviteTarget(d, d.who, d.person)
	if problem != "" {
		return m.openInviteForm(d, problem)
	}
	m.busy = true
	return tea.Batch(m.setBusy("inviting"), inviteMember(m.ctx, m.client, d.listID, target, label))
}

// onInvited reports the outcome. A refusal reopens the form with the same
// text in it, so one slip does not mean typing it all again.
func (m *Model) onInvited(msg invitedMsg) tea.Cmd {
	m.done()
	d := m.invite
	if msg.err != nil {
		if api.CodeOf(msg.err) == api.CodeUnauthorized {
			return m.failf(msg.err, "")
		}
		if d == nil || d.listID != msg.listID || m.layer != layerNone {
			return m.warnf("%s", inviteProblem(msg.err))
		}
		return m.openInviteForm(d, inviteProblem(msg.err))
	}
	m.invite = nil
	who := msg.label
	if h := api.Handle(msg.invitee.Username); h != "" {
		who = h
	}
	// The same answer comes back whether this is the first invitation or a
	// repeat, even after a decline, so the words claim no more than that
	// one has been sent.
	return m.okf("invitation sent to %s; they join the list once they accept", who)
}

// inviteProblem says in plain words why an invitation was refused.
func inviteProblem(err error) string {
	var apiErr *api.Error
	switch {
	case unreachable(err):
		return "Inviting needs a connection. Your text is kept; try again when you are online."
	case !errors.As(err, &apiErr):
		return errText(err)
	}
	switch apiErr.Code {
	case api.CodeUserNotFound:
		return "No Evado account matches that. Check the spelling, or press i for an invite link for someone without one."
	case api.CodeAlreadyMember:
		return "They are already in this list."
	case api.CodeInviteSelf:
		return "That is your own account. Invite someone else."
	case api.CodePersonalList:
		return "A personal list cannot be shared. Make a new list to share."
	case api.CodeListReadOnly:
		return "This list is frozen after its owner left, so nobody new can be invited."
	case api.CodeRateLimited:
		out := "You have reached the limit of 30 invitations a day."
		if apiErr.RetryAfter > 0 {
			out += " Try again in " + retryIn(apiErr.RetryAfter) + "."
		}
		return out
	case api.CodeListNotSynced:
		return "This list has not been synced yet. Try again after a sync."
	case api.CodeNotFound:
		return "That list was not found. If you just made it, try again once it has synced."
	case api.CodeForbidden:
		return "You cannot invite to this list: " + apiErr.Message
	}
	return errText(err)
}

// retryIn words a wait for a message: "2 hours", "5 minutes".
func retryIn(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return plural(int(d.Round(time.Hour)/time.Hour), "hour")
	case d >= time.Minute:
		return plural(int(d.Round(time.Minute)/time.Minute), "minute")
	}
	return plural(max(int(d.Seconds()), 1), "second")
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
