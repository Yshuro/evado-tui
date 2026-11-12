package tui

import (
	"testing"

	"github.com/nerdlabz/evado-tui/internal/config"
)

func TestDemoShowsItsLifetimeAndCannotSignIn(t *testing.T) {
	h := offlineHarness(t, config.Config{Local: true, Demo: true})
	h.mustSay("DEMO · changes discarded on exit")
	h.press("4")
	h.model.acct.selectRow("computer")
	h.press("c")
	if h.model.login != nil || h.model.session != nil {
		t.Fatal("demo offered account sign-in")
	}
	h.mustSay("demo stays offline")
}
