package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nerdlabz/evado-tui/internal/config"
)

// signOutHarness starts a signed-in session whose settings and cache live in
// a private directory, so the test can look at what sign-out leaves behind.
func signOutHarness(t *testing.T) (*harness, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("EVADO_CONFIG", path)
	saved := config.Config{
		Token: "evt_test", BaseURL: "http://saved.example/api/v1", Account: "me",
		Status: "due", Mode: "Due", ListID: "l1", Theme: "dark", PollSeconds: 300,
		Keys: map[string]string{"J": "j"},
	}
	if err := saved.Save(); err != nil {
		t.Fatal(err)
	}
	// Older versions left a snapshot of the todos next to the settings.
	cachePath := filepath.Join(filepath.Dir(path), "cache.json")
	if err := os.WriteFile(cachePath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, sampleAPI())
	return h, cachePath
}

func TestSignOutClearsThisComputerAndKeepsSettings(t *testing.T) {
	h, cachePath := signOutHarness(t)
	h.press("4")
	h.model.acct.selectRow("signout")
	h.mustContain("Removed: the saved token")

	h.press("enter")
	if h.model.layer != layerConfirm {
		t.Fatalf("layer = %v, sign out must ask first", h.model.layer)
	}
	h.press("enter")

	if h.model.layer != layerLogin {
		t.Fatalf("layer = %v, want the sign-in screen", h.model.layer)
	}
	if h.model.client.HasToken() || h.model.cfg.Token != "" {
		t.Error("the running session still has a token")
	}
	if h.model.user.Email != "" || len(h.model.todos.items) != 0 || len(h.model.lists) != 0 {
		t.Error("the old account's data is still in memory")
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Errorf("cache file still there: %v", err)
	}

	disk, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if disk.Token != "" || disk.Account != "" || disk.Status != "" || disk.Mode != "" || disk.ListID != "" {
		t.Errorf("the account or its view is still on disk: %+v", disk)
	}
	if disk.Theme != "dark" || disk.PollSeconds != 300 || disk.Keys["J"] != "j" || disk.BaseURL != "http://saved.example/api/v1" {
		t.Errorf("settings were lost: %+v", disk)
	}
}

func TestSignOutCanBeCancelled(t *testing.T) {
	h, cachePath := signOutHarness(t)
	h.press("4")
	h.model.acct.selectRow("signout")
	h.press("enter", "esc")

	if h.model.layer != layerNone || !h.model.client.HasToken() {
		t.Errorf("layer=%v: cancelling must leave the session alone", h.model.layer)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("cache file should still exist: %v", err)
	}
	if disk, _ := config.Load(); disk.Token != "evt_test" {
		t.Error("the saved token should still be there")
	}
}

func TestPaletteSignOutSelectsItsOwnRow(t *testing.T) {
	h, _ := signOutHarness(t)
	h.press(":")
	for _, r := range "sign out" {
		h.press(string(r))
	}
	h.press("enter")
	if h.model.layer != layerConfirm || h.model.conf.id != "sign-out" {
		t.Errorf("layer=%v: the palette should open the sign-out confirmation", h.model.layer)
	}
}

func TestAccountMenuSaysWhereEachThingLives(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.press("4")
	for _, heading := range []string{"YOUR ACCOUNT", "WEB APP ONLY", "THIS COMPUTER", "DANGER ZONE"} {
		h.mustContain(heading)
	}
	h.model.acct.selectRow("delete")
	if v := h.view(); !strings.Contains(v, "choose Sign out") {
		t.Error("delete account should point at Sign out for the this-computer case")
	}
}

func TestAccountMenuScrollsOnAShortTerminal(t *testing.T) {
	h := newHarness(t, sampleAPI())
	h.send(tea.WindowSizeMsg{Width: 100, Height: 14})
	h.press("4")
	h.model.acct.selectRow("delete")
	h.mustContain("Delete account")
}

// The help lists o as signing out on the Account tab, so it does that from
// any section, not only with Sign out selected.
func TestOSignsOutFromAnyAccountSection(t *testing.T) {
	for _, row := range []string{"profile", "timezone", "computer", "signout"} {
		h := newHarness(t, sampleAPI())
		h.press("4")
		h.model.acct.selectRow(row)
		h.press("o")
		if h.model.layer != layerConfirm || h.model.conf.id != "sign-out" {
			t.Errorf("%s: o should open the sign-out confirmation", row)
		}
	}
}

// Deleting the account signs this computer out too. Its token and todos
// would otherwise stay for the next run to open on an account that is gone.
func TestDeletingTheAccountSignsThisComputerOut(t *testing.T) {
	h, _ := signOutHarness(t)
	h.send(accountDeletedMsg{frozen: 2})
	h.run(h.cmds...)
	h.cmds = nil

	if h.model.quited || h.model.layer != layerLogin {
		t.Fatalf("quited = %v, layer = %v, want the sign-in screen", h.model.quited, h.model.layer)
	}
	if h.model.client.HasToken() || h.model.cfg.Token != "" {
		t.Error("the running session still has a token")
	}
	if disk, err := config.Load(); err != nil || disk.Token != "" || disk.Account != "" {
		t.Errorf("the account is still on disk: %+v %v", disk, err)
	}
	h.mustSay("Account deleted. 2 shared lists left read-only")
}
