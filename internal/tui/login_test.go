package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

// authServer is Evado's sign-in surface in front of an account: codes by
// email, single-use rotating refresh tokens, and revocation. Pasted tokens
// work too, so both ways in can be compared.
type authServer struct {
	mu sync.Mutex
	// sent is every address a code went to, and limit makes asking for a
	// code a 429.
	sent  []string
	limit bool
	// access and refresh are the live tokens; spent refresh tokens are
	// remembered, so sending one again revokes everything.
	access, refresh, spent map[string]bool
	revoked                bool
	n                      int
	refreshes, logouts     int
	failLogout             bool
	// tokenGood says whether the pasted token evt_good is accepted.
	tokenGood bool
	// failSync makes /sync a server error, which is how a guest's
	// migration into the account fails.
	failSync bool
	// bearers is every Authorization header the account saw.
	bearers []string
}

// withAuth serves the sign-in endpoints and checks the bearer of every
// other request, handing those it accepts to inner.
func withAuth(a *authServer, inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail := func(status int, code string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "code": code})
		}
		var body struct {
			Email        string `json:"email"`
			Code         string `json:"code"`
			RefreshToken string `json:"refresh_token"`
		}
		if strings.HasPrefix(r.URL.Path, "/auth/") || r.URL.Path == "/oauth/token" {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		issue := func() map[string]any {
			a.n++
			acc, ref := fmt.Sprintf("acc%d", a.n), fmt.Sprintf("ref%d", a.n)
			a.access[acc], a.refresh[ref] = true, true
			return map[string]any{
				"access_token": acc, "refresh_token": ref, "token_type": "Bearer", "expires_in": 900,
				"user": api.User{ID: "u1", Email: "me@example.com", TZ: "UTC"},
			}
		}
		switch r.URL.Path {
		case "/oauth/token":
			if body.Code != "browser-code" {
				fail(http.StatusBadRequest, "invalid_grant")
				return
			}
			out := issue()
			out["scope"] = "todos:read todos:write lists:read lists:write profile:read profile:write account"
			_ = json.NewEncoder(w).Encode(out)
		case "/auth/login":
			if body.Email != "me@example.com" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "signup_required", "signupUrl": "https://evado.example/login?email=" + body.Email})
				return
			}
			if a.limit {
				w.Header().Set("Retry-After", "1800")
				fail(http.StatusTooManyRequests, "rate_limited")
				return
			}
			a.sent = append(a.sent, body.Email)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case "/auth/verify":
			if body.Email != "me@example.com" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "signup_required", "signupUrl": "https://evado.example/login?email=" + body.Email})
				return
			}
			if body.Code != "123456" || body.Email != "me@example.com" {
				fail(http.StatusBadRequest, "invalid_request")
				return
			}
			out := issue()
			out["lists"] = []api.List{{ID: "p1", Name: "Personal", IsPersonal: true, Role: "owner"}}
			_ = json.NewEncoder(w).Encode(out)
		case "/auth/refresh":
			a.refreshes++
			switch {
			case a.spent[body.RefreshToken]:
				a.revoked = true
				fail(http.StatusUnauthorized, "refresh_token_reused")
				return
			case a.revoked || !a.refresh[body.RefreshToken]:
				fail(http.StatusBadRequest, "invalid_grant")
				return
			}
			delete(a.refresh, body.RefreshToken)
			a.spent[body.RefreshToken] = true
			_ = json.NewEncoder(w).Encode(issue())
		case "/auth/token/logout":
			a.logouts++
			if a.failLogout {
				fail(http.StatusServiceUnavailable, "unavailable")
				return
			}
			a.revoked = true
		default:
			bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			a.bearers = append(a.bearers, bearer)
			ok := (bearer == "evt_good" && a.tokenGood) || (a.access[bearer] && !a.revoked)
			if !ok {
				fail(http.StatusUnauthorized, "unauthorized")
				return
			}
			a.mu.Unlock()
			inner.ServeHTTP(w, r)
			a.mu.Lock()
		}
	})
}

// expireAccess makes the server turn down every access token it gave out,
// as it does once they are fifteen minutes old.
func (a *authServer) expireAccess() {
	a.mu.Lock()
	a.access = map[string]bool{}
	a.mu.Unlock()
}

func (a *authServer) count() (sent, refreshes, logouts int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.sent), a.refreshes, a.logouts
}

// newAuthAccount is an account that can be signed into by code, with the
// sync surface the todos kept here talk to.
func newAuthAccount(t *testing.T) (*authServer, *account, string) {
	t.Helper()
	a := &authServer{access: map[string]bool{}, refresh: map[string]bool{}, spent: map[string]bool{}, tokenGood: true}
	acct := &account{todos: map[string]map[string]json.RawMessage{}, clock: time.Now().Add(time.Hour)}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		lists := []api.List{{ID: "p1", Name: "Personal", IsPersonal: true, Role: "owner"}}
		switch r.URL.Path {
		case "/me":
			write(map[string]any{"user": api.User{ID: "u1", Email: "me@example.com", TZ: "UTC"}, "lists": lists})
		case "/sync":
			a.mu.Lock()
			failing := a.failSync
			a.mu.Unlock()
			if failing {
				w.WriteHeader(http.StatusInternalServerError)
				write(map[string]any{"error": "down", "code": "internal"})
				return
			}
			var body struct {
				Changes []map[string]json.RawMessage `json:"changes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			acct.clock = acct.clock.Add(time.Second)
			for _, ch := range body.Changes {
				var id string
				_ = json.Unmarshal(ch["id"], &id)
				if acct.todos[id] == nil {
					acct.todos[id] = map[string]json.RawMessage{}
				}
				for k, v := range ch {
					acct.todos[id][k] = v
				}
			}
			out := append([]api.Todo{}, acct.existed...)
			for id, ch := range acct.todos {
				todo := api.Todo{ID: id, ListID: "p1", UpdatedAt: acct.clock}
				_ = json.Unmarshal(ch["title"], &todo.Title)
				out = append(out, todo)
			}
			write(map[string]any{"todos": out, "lists": lists, "cursor": "1", "hasMore": false})
		default:
			w.WriteHeader(http.StatusNotFound)
			write(map[string]any{"error": "not found", "code": "not_found"})
		}
	})
	srv := httptest.NewServer(withAuth(a, inner))
	t.Cleanup(srv.Close)
	return a, acct, srv.URL
}

// sessionFile is where the harness's session is kept.
func sessionFile(t *testing.T) string {
	t.Helper()
	p, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(filepath.Dir(p), "session.json")
}

// askForCode types an address on the sign-in screen and sends for a code.
func (h *harness) askForCode(email string) {
	h.t.Helper()
	if h.model.layer != layerLogin {
		h.t.Fatalf("layer = %v, want the sign-in screen", h.model.layer)
	}
	h.typeText(email)
	h.mustContain("send code")
	h.press("enter")
}

// guestWithATodo is a first run that chose to go without an account and
// has made a todo, then opened the sign-in screen from Connection.
func guestWithATodo(t *testing.T, url string) *harness {
	t.Helper()
	h := offlineHarness(t, config.Config{Local: true, BaseURL: url})
	h.press("n")
	h.typeText("Made before signing in")
	h.press("enter")
	h.press("4")
	h.model.acct.selectRow("computer")
	h.press("c")
	return h
}

func TestSigningInWithAnEmailedCode(t *testing.T) {
	a, acct, url := newAuthAccount(t)
	h := guestWithATodo(t, url)
	h.mustSay("Sign in or create an account on the web")

	h.askForCode("me@example.com")
	if sent, _, _ := a.count(); sent != 1 {
		t.Fatalf("codes sent = %d, want 1", sent)
	}
	h.mustSay("We sent a 6-digit code to me@example.com")
	h.mustSay("Enter the code below to sign in")

	h.typeText("123 456") // as copied from the email, with a space
	h.press("enter")
	if h.model.layer != layerNone {
		t.Fatalf("layer = %v, want the app; login err %q", h.model.layer, loginErr(h))
	}
	if h.model.session == nil || h.model.session.UserID() != "u1" {
		t.Fatal("the app is not running on the new session")
	}
	// The guest's todo went through the migration into the account.
	if len(acct.todos) != 1 {
		t.Errorf("the account holds %d todos, want the one made here", len(acct.todos))
	}
	if st := h.model.backend.Status(); !st.SignedIn || st.Pending != 0 {
		t.Errorf("status = %+v, want signed in with nothing left to send", st)
	}
	h.press("4")
	h.model.acct.selectRow("computer")
	h.mustContain("me@example.com, by emailed code")

	// The pair is kept on its own, owner-only, never in the config.
	raw, err := os.ReadFile(sessionFile(t))
	if err != nil {
		t.Fatalf("session not saved: %v", err)
	}
	if !strings.Contains(string(raw), `"userId": "u1"`) || !strings.Contains(string(raw), url) {
		t.Errorf("session file does not say whose it is:\n%s", raw)
	}
	if fi, _ := os.Stat(sessionFile(t)); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("session file mode = %o, want 600", fi.Mode().Perm())
	}
	if err := h.model.cfg.SavePrefs(); err != nil {
		t.Fatal(err)
	}
	cfgPath, _ := config.Path()
	cfgRaw, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(cfgRaw), "acc") || strings.Contains(string(cfgRaw), "ref") {
		t.Errorf("tokens reached the config file:\n%s", cfgRaw)
	}
	if disk, _ := config.Load(); disk.Token != "" || disk.Local {
		t.Errorf("saved config = %+v, want no token and no longer local", disk)
	}
}

// mustSay is mustContain for prose, which the screen may wrap anywhere.
func (h *harness) mustSay(s string) {
	h.t.Helper()
	flat := strings.Join(strings.FieldsFunc(stripANSI(h.view()), func(r rune) bool {
		return r == ' ' || r == '\n' || r == '│'
	}), " ")
	if !strings.Contains(flat, s) {
		h.t.Errorf("view does not say %q\n---\n%s", s, h.view())
	}
}

func loginErr(h *harness) string {
	if h.model.login == nil {
		return ""
	}
	return h.model.login.err
}

func TestAWrongOrExpiredCodeKeepsTheForm(t *testing.T) {
	a, _, url := newAuthAccount(t)
	h := offlineHarness(t, config.Config{BaseURL: url})
	h.askForCode("me@example.com")

	h.typeText("654321")
	h.press("enter")
	if h.model.layer != layerLogin || !h.model.login.asking() {
		t.Fatal("a wrong code left the code screen")
	}
	h.mustSay("wrong or has expired")
	if got := h.model.login.codeInput.Value(); got != "654321" {
		t.Errorf("code = %q, want what was typed kept to fix", got)
	}
	// Fixing the typo and trying again works.
	for range 6 {
		h.press("bs")
	}
	h.typeText("123456")
	h.press("enter")
	if h.model.layer != layerNone || h.model.session == nil {
		t.Fatalf("layer = %v err = %q, want signed in", h.model.layer, loginErr(h))
	}
	if sent, _, _ := a.count(); sent != 1 {
		t.Errorf("codes sent = %d, want the first one reused", sent)
	}
}

func TestTheEmailCanBeCorrectedAndACodeResent(t *testing.T) {
	a, _, url := newAuthAccount(t)
	h := offlineHarness(t, config.Config{BaseURL: url})
	h.askForCode("me@exmaple.com")
	h.mustContain("me@exmaple.com")

	// An address with no account stays in the field, to correct rather
	// than retype.
	if h.model.layer != layerLogin || h.model.login.asking() || !h.model.login.entering() {
		t.Fatal("a refused address should leave the email field open")
	}
	if got := h.model.login.tokenInput.Value(); got != "me@exmaple.com" {
		t.Errorf("email = %q, want it kept", got)
	}
	for range len("exmaple.com") {
		h.press("bs")
	}
	h.typeText("example.com")
	h.press("enter")
	h.mustSay("We sent a 6-digit code to me@example.com")

	// Resend is a button on the code screen.
	h.press("tab", "enter")
	h.mustSay("We sent a new code to me@example.com")
	a.mu.Lock()
	sent := append([]string{}, a.sent...)
	a.mu.Unlock()
	if want := []string{"me@example.com", "me@example.com"}; strings.Join(sent, ",") != strings.Join(want, ",") {
		t.Errorf("codes went to %v, want %v", sent, want)
	}
}

func TestAResendWaitsAsLongAsTheServerSays(t *testing.T) {
	a, _, url := newAuthAccount(t)
	h := offlineHarness(t, config.Config{BaseURL: url})
	h.askForCode("me@example.com")

	a.mu.Lock()
	a.limit = true
	a.mu.Unlock()
	h.press("tab", "enter") // resend
	h.mustSay("ask again in 30 minutes")
	if !h.model.login.asking() {
		t.Fatal("a rate-limited resend left the code screen")
	}
	// Asking again before then does not reach the server.
	a.mu.Lock()
	a.limit = false
	a.mu.Unlock()
	h.press("enter")
	if sent, _, _ := a.count(); sent != 1 {
		t.Errorf("codes sent = %d, want the resend held back", sent)
	}
	h.mustSay("another code in 30 minutes")

	// The code already sent still works meanwhile.
	h.press("shift+tab")
	h.typeText("123456")
	h.press("enter")
	if h.model.session == nil {
		t.Errorf("err = %q, want signed in with the first code", loginErr(h))
	}
}

func TestASessionSurvivesARestartAndRenewsItself(t *testing.T) {
	a, _, url := newAuthAccount(t)
	h := offlineHarness(t, config.Config{BaseURL: url})
	h.askForCode("me@example.com")
	h.typeText("123456")
	h.press("enter")
	if h.model.session == nil {
		t.Fatalf("not signed in: %q", loginErr(h))
	}
	before, _ := os.ReadFile(sessionFile(t))

	disk, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	again := restart(t, disk)
	if again.model.layer == layerLogin {
		t.Fatal("a restart asked to sign in again")
	}
	if again.model.session == nil || !again.model.backend.Status().SignedIn {
		t.Fatal("the restart did not pick the session up")
	}
	// Fifteen minutes on, the server turns the access token down; the
	// session renews and the sync goes through without a word.
	a.expireAccess()
	again.run(again.model.syncNow())
	if again.model.layer == layerLogin {
		t.Fatal("an expired access token sent the user to sign in")
	}
	if _, refreshes, _ := a.count(); refreshes != 1 {
		t.Errorf("refreshes = %d, want 1", refreshes)
	}
	if st := again.model.backend.Status(); st.Err != nil {
		t.Errorf("sync failed: %v", st.Err)
	}
	after, _ := os.ReadFile(sessionFile(t))
	if string(before) == string(after) {
		t.Error("the rotated pair was not saved")
	}
}

func TestARevokedSessionReturnsToSignIn(t *testing.T) {
	a, _, url := newAuthAccount(t)
	h := offlineHarness(t, config.Config{BaseURL: url})
	h.askForCode("me@example.com")
	h.typeText("123456")
	h.press("enter")
	h.press("n")
	h.typeText("Kept through it all")
	h.press("enter")

	// Signed out from another device.
	a.mu.Lock()
	a.revoked = true
	a.mu.Unlock()
	h.run(h.model.syncNow())
	if h.model.layer != layerLogin {
		t.Fatalf("layer = %v, want the sign-in screen", h.model.layer)
	}
	h.mustContain("Your session has ended")
	if got := h.model.login.tokenInput.Value(); got != "me@example.com" {
		t.Errorf("email = %q, want it filled in to sign in again", got)
	}
	if _, err := os.Stat(sessionFile(t)); !os.IsNotExist(err) {
		t.Errorf("the dead session is still on disk: %v", err)
	}
	// The todos here stay, to go up once signed in again. The first esc
	// leaves the email field for the list, the second the screen.
	h.press("esc", "esc")
	h.press("1")
	h.mustContain("Kept through it all")
}

func TestSignOutEndsTheSessionAndSaysWhenTheServerCouldNotBeTold(t *testing.T) {
	a, _, url := newAuthAccount(t)
	h := offlineHarness(t, config.Config{BaseURL: url})
	h.askForCode("me@example.com")
	h.typeText("123456")
	h.press("enter")
	a.mu.Lock()
	a.failLogout = true
	a.mu.Unlock()

	h.press("4")
	h.model.acct.selectRow("signout")
	h.press("enter", "enter")
	if h.model.layer != layerLogin || h.model.session != nil {
		t.Fatalf("layer = %v, want signed out to the sign-in screen", h.model.layer)
	}
	if _, _, logouts := a.count(); logouts != 1 {
		t.Errorf("revocations asked = %d, want 1", logouts)
	}
	if !strings.Contains(h.model.toastText(), "could not be told to end the session") {
		t.Errorf("toast = %q, want the failed revocation reported", h.model.toastText())
	}
	if _, err := os.Stat(sessionFile(t)); !os.IsNotExist(err) {
		t.Errorf("the session is still on disk: %v", err)
	}
	again := restart(t, config.Config{BaseURL: url})
	if again.model.layer != layerLogin {
		t.Error("a restart after signing out came back signed in")
	}
}

func TestAFailedGuestMigrationLeavesThisComputerAsItWas(t *testing.T) {
	a, acct, url := newAuthAccount(t)
	h := guestWithATodo(t, url)
	a.mu.Lock()
	a.failSync = true
	a.mu.Unlock()

	h.askForCode("me@example.com")
	h.typeText("123456")
	h.press("enter")
	if h.model.layer != layerLogin {
		t.Fatalf("layer = %v, want back on the sign-in screen", h.model.layer)
	}
	if got := h.model.login.tokenInput.Value(); got != "me@example.com" {
		t.Errorf("email = %q, want it kept to try again", got)
	}
	if h.model.session != nil || h.model.joining != nil {
		t.Error("a sign-in that did not go through is still in use")
	}
	if _, err := os.Stat(sessionFile(t)); !os.IsNotExist(err) {
		t.Errorf("the session was saved: %v", err)
	}
	if _, _, logouts := a.count(); logouts != 1 {
		t.Errorf("revocations = %d, want the unused session ended", logouts)
	}
	if disk, _ := config.Load(); !disk.Local || disk.Token != "" {
		t.Errorf("saved config = %+v, want it untouched", disk)
	}
	if len(acct.todos) != 0 || h.model.backend.Status().SignedIn {
		t.Error("the todos here were tied to the account anyway")
	}
	h.press("esc", "esc")
	h.press("1")
	h.mustContain("Made before signing in")
}

func TestATokenSignInNeverRefreshes(t *testing.T) {
	a, _, url := newAuthAccount(t)
	h := offlineHarness(t, config.Config{BaseURL: url})
	h.typeText("evt_good")
	h.mustNotContain("send code")
	h.press("enter")
	if h.model.layer != layerNone || h.model.session != nil {
		t.Fatalf("layer = %v, want signed in with the token alone", h.model.layer)
	}
	// The token is revoked; a 401 goes back to sign-in without trying to
	// refresh anything.
	a.mu.Lock()
	a.tokenGood = false
	a.mu.Unlock()
	h.run(h.model.syncNow())
	if h.model.layer != layerLogin {
		t.Fatalf("layer = %v, want the sign-in screen", h.model.layer)
	}
	h.mustContain("That token was rejected")
	if _, refreshes, _ := a.count(); refreshes != 0 {
		t.Errorf("refreshes = %d, want none for a token", refreshes)
	}
	if _, err := os.Stat(sessionFile(t)); !os.IsNotExist(err) {
		t.Error("a token sign-in wrote a session")
	}
}

func TestEmailSignInWithNothingKeptHere(t *testing.T) {
	a := &authServer{access: map[string]bool{}, refresh: map[string]bool{}, spent: map[string]bool{}}
	srv := httptest.NewServer(withAuth(a, sampleAPI().handler(t)))
	t.Cleanup(srv.Close)
	t.Setenv("EVADO_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	m := New(context.Background(), config.Config{BaseURL: srv.URL, NoCache: true, Status: api.StatusAll})
	h := &harness{t: t, model: m}
	h.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.run(m.Init())
	h.askForCode("me@example.com")
	h.typeText("123456")
	h.press("enter")
	if h.model.layer != layerNone {
		t.Fatalf("layer = %v err = %q, want the app", h.model.layer, loginErr(h))
	}
	h.mustContain("Pay the cable bill")
	a.mu.Lock()
	last := a.bearers[len(a.bearers)-1]
	a.mu.Unlock()
	if !strings.HasPrefix(last, "acc") {
		t.Errorf("requests carried %q, want the session's access token", last)
	}
	if _, err := os.Stat(sessionFile(t)); err != nil {
		t.Errorf("session not saved: %v", err)
	}
}
