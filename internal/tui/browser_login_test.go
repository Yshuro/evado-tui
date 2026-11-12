package tui

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestBrowserSignInKeepsTheSessionAndEnablesAccountActions(t *testing.T) {
	a, _, server := newAuthAccount(t)
	h := guestWithATodo(t, server)
	old := openWebAddress
	t.Cleanup(func() { openWebAddress = old })
	openWebAddress = func(address string) error {
		u, _ := url.Parse(address)
		callback, _ := url.Parse(u.Query().Get("redirect_uri"))
		callback.RawQuery = url.Values{"state": {u.Query().Get("state")}, "code": {"browser-code"}}.Encode()
		res, err := http.Get(callback.String())
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, res.Body)
		return res.Body.Close()
	}
	h.press("enter")
	if h.model.login != nil || h.model.session == nil || !api.HasScope(h.model.session.Scope(), api.ScopeAccount) {
		t.Fatal("browser did not start an account-scoped session")
	}
	if h.model.lacksAccountScope() || h.model.exportsFromComputer() {
		t.Fatal("account actions still disabled")
	}
	for _, row := range h.model.acct.menu() {
		if (row.key == "export" || row.key == "delete") && row.sessionOnly {
			t.Fatalf("%s is still web-only", row.key)
		}
	}
	if sent, _, _ := a.count(); sent != 0 {
		t.Fatal("browser path sent a terminal sign-in code")
	}
	if !strings.Contains(h.model.session.Email(), "me@example.com") {
		t.Fatal("wrong user saved")
	}
}

func TestBrowserShortcutKeepsEmailFallback(t *testing.T) {
	l := newLoginView(api.DefaultBaseURL, "")
	_, done := l.update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !done || l.want != wantBrowser {
		t.Fatal("browser shortcut missing")
	}
	l.fill("me@example.com")
	_, done = l.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !done || l.want != wantCode {
		t.Fatal("email fallback missing")
	}
}

// The screen opens on the ways to start, with the browser first and lit, and
// asks for an email only once that is chosen. Leaving the field goes back to
// the same list, so the browser is never out of reach.
func TestTheSignInScreenListsTheWaysToStart(t *testing.T) {
	l := newLoginView(api.DefaultBaseURL, "")
	l.allowLocal = true
	key := func(k tea.KeyType) bool { _, done := l.update(tea.KeyMsg{Type: k}); return done }
	got := l.view(100, 40)
	for _, want := range []string{"Sign in with your browser", "Use an email code or an API token", "Use without an account"} {
		if !strings.Contains(got, want) {
			t.Errorf("the list lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "you@example.com") || strings.Contains(got, "Server") || strings.Contains(got, api.DefaultBaseURL) {
		t.Errorf("the list should show neither a field to type in nor the ordinary server:\n%s", got)
	}
	// Pointed at another server, as with -server, the screen says so.
	other := newLoginView(api.DefaultBaseURL, "https://staging.example/api/v1")
	if got := other.view(100, 40); !strings.Contains(got, "Server") || !strings.Contains(got, "https://staging.example/api/v1") {
		t.Errorf("another server should be shown:\n%s", got)
	}

	if key(tea.KeyDown) || key(tea.KeyEnter) || !l.entering() {
		t.Fatal("choosing the email row should open its field, not finish the screen")
	}
	for _, r := range "me@example.com" {
		l.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := l.view(100, 40); !strings.Contains(got, "send code") || strings.Contains(got, "Use without an account") {
		t.Errorf("the email step should offer to send a code and nothing else:\n%s", got)
	}
	if key(tea.KeyEsc) || l.entering() || l.tokenInput.Value() != "me@example.com" {
		t.Fatal("esc should go back to the list and keep the address")
	}
	key(tea.KeyUp)
	if !key(tea.KeyEnter) || l.want != wantBrowser {
		t.Fatal("the first row should start browser sign-in")
	}
}

// A sign-in the person denied, or whose state was changed on the way, says
// so. Neither is a server that could not be reached.
func TestAFailedBrowserSignInSaysWhy(t *testing.T) {
	for name, tc := range map[string]struct {
		query func(state string) url.Values
		want  string
	}{
		"denied":      {func(state string) url.Values { return url.Values{"state": {state}, "error": {"access_denied"}} }, "was denied"},
		"wrong state": {func(string) url.Values { return url.Values{"state": {"other"}, "code": {"browser-code"}} }, "state did not match"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, server := newAuthAccount(t)
			h := guestWithATodo(t, server)
			old := openWebAddress
			t.Cleanup(func() { openWebAddress = old })
			openWebAddress = func(address string) error {
				u, _ := url.Parse(address)
				callback, _ := url.Parse(u.Query().Get("redirect_uri"))
				callback.RawQuery = tc.query(u.Query().Get("state")).Encode()
				res, err := http.Get(callback.String())
				if err != nil {
					return err
				}
				_, _ = io.Copy(io.Discard, res.Body)
				return res.Body.Close()
			}
			h.press("enter")
			if h.model.session != nil || h.model.login == nil {
				t.Fatal("a failed browser sign-in signed in")
			}
			h.mustSay(tc.want)
			h.mustNotContain("Could not reach the server")
		})
	}
}

func TestBrowserCancelDropsLateReplies(t *testing.T) {
	_, _, server := newAuthAccount(t)
	h := guestWithATodo(t, server)
	_ = h.model.beginBrowser()
	flow, seq := h.model.login.browser, h.model.login.seq
	if flow == nil {
		t.Fatal("no callback listener")
	}
	h.press("esc")
	if _, err := flow.Wait(); err == nil {
		t.Fatal("escape did not cancel listener")
	}
	late := browserVerifiedMsg{codeVerifiedMsg{seq: seq, server: server, res: api.Verified{TokenPair: api.TokenPair{AccessToken: "access", RefreshToken: "refresh", User: api.User{ID: "u1"}}}}}
	h.run(h.model.onBrowserVerified(late))
	if h.model.session != nil || h.model.login == nil || h.model.login.waiting {
		t.Fatal("late reply signed in after cancellation")
	}
	h.mustSay("Browser sign-in cancelled")
}
