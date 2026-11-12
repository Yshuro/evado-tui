package tui

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

func TestUnknownEmailReturnsToSignupWithoutSendingACode(t *testing.T) {
	a, _, url := newAuthAccount(t)
	h := offlineHarness(t, config.Config{BaseURL: url})
	h.askForCode("new@example.com")
	if sent, _, _ := a.count(); sent != 0 {
		t.Fatal("unknown address got a code")
	}
	if h.model.login.asking() || h.model.session != nil {
		t.Fatal("signup entered the code step or saved tokens")
	}
	h.mustSay("Create one in your browser, then sign in here")
	h.mustContain("open the sign-up page")
	if !strings.Contains(h.model.login.view(100, 40), "\x1b]8;;https://evado.example/login") {
		t.Fatal("signup link is not a terminal hyperlink")
	}
	old := openWebAddress
	t.Cleanup(func() { openWebAddress = old })
	opened := ""
	openWebAddress = func(address string) error { opened = address; return errors.New("no browser") }
	h.press("ctrl+o")
	if opened != h.model.login.signupURL {
		t.Fatal("wrong signup address opened")
	}
	h.mustSay("Could not open the browser")
}

func TestSignupRequiredDuringVerificationKeepsTheEmail(t *testing.T) {
	l := newLoginView(api.DefaultBaseURL, "")
	l.email = "new@example.com"
	l.onVerifyFailed(&api.SignupRequired{Err: &api.Error{StatusCode: http.StatusForbidden, Code: api.CodeSignupRequired}, SignupURL: "https://evado.app/login?email=new%40example.com"})
	if l.asking() || l.tokenInput.Value() != "new@example.com" || l.signupURL == "" {
		t.Fatalf("signup view = %+v", l)
	}
	if webAddress("javascript:alert(1)") != "" || webAddress("https://evado.app/\x1b]8;;bad") != "" {
		t.Fatal("unsafe signup link accepted")
	}
}
