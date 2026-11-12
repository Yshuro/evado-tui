package config

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

var (
	mine  = AccountViews("https://evado.example/api/v1", "u1")
	yours = AccountViews("https://evado.example/api/v1", "u2")
)

func everything() SavedView {
	return SavedView{
		Name: "Work today", Status: "today", ListID: "l1", ListName: "Work",
		Assigned: "u2", AssignedName: "@sam", Query: "report", Undated: true, Board: true,
	}
}

func TestSavedViewsSurviveARoundTrip(t *testing.T) {
	withTempConfig(t)
	var want Config
	want.SetViews(mine, []SavedView{everything(), {Name: "Plain"}})
	want.SetViews(yours, []SavedView{{Name: "Theirs", Status: "overdue"}})
	want.SetViews(ViewScope{}, []SavedView{{Name: "Guest"}})
	if err := want.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
	if v := got.ViewsFor(mine); len(v) != 2 || v[0] != everything() {
		t.Errorf("views = %+v, want every field kept", v)
	}
}

func TestViewsAreKeptPerServerAndAccount(t *testing.T) {
	var c Config
	c.SetViews(mine, []SavedView{{Name: "A"}})
	c.SetViews(yours, []SavedView{{Name: "B"}})
	c.SetViews(AccountViews("https://elsewhere.example/api/v1", "u1"), []SavedView{{Name: "C"}})
	c.SetViews(ViewScope{}, []SavedView{{Name: "D"}})

	for scope, want := range map[ViewScope]string{
		mine: "A", yours: "B", {}: "D",
		AccountViews("https://elsewhere.example/api/v1", "u1"): "C",
	} {
		if got := c.ViewsFor(scope); len(got) != 1 || got[0].Name != want {
			t.Errorf("%+v holds %+v, want %q", scope, got, want)
		}
	}
	if got := c.ViewsFor(AccountViews("https://evado.example/api/v1", "u3")); got != nil {
		t.Errorf("an account that saved nothing holds %+v", got)
	}
}

func TestTheServerIsTheSameWhateverWayItIsWritten(t *testing.T) {
	if AccountViews("https://evado.example/api/v1/", "u1") != mine {
		t.Error("a trailing slash made another server")
	}
	if AccountViews("", "u1") != AccountViews("https://evado.app/api/v1", "u1") {
		t.Error("no address should mean the production server")
	}
	if AccountViews("https://api.evado.example", "u1") == (ViewScope{}) {
		t.Error("an account's views must not be the guest's")
	}
}

func TestSetViewsLeavesOtherScopesAndCopiesWhatItIsGiven(t *testing.T) {
	var c Config
	c.SetViews(mine, []SavedView{{Name: "A"}, {Name: "B"}})
	c.SetViews(yours, []SavedView{{Name: "X"}})
	views := c.ViewsFor(mine)
	views[0].Name = "changed in place"
	if got := c.ViewsFor(mine)[0].Name; got != "A" {
		t.Errorf("editing what ViewsFor returned changed the config: %q", got)
	}
	given := []SavedView{{Name: "C"}}
	c.SetViews(mine, given)
	given[0].Name = "changed after"
	if got := c.ViewsFor(mine); len(got) != 1 || got[0].Name != "C" {
		t.Errorf("views = %+v", got)
	}
	if got := c.ViewsFor(yours); len(got) != 1 || got[0].Name != "X" {
		t.Errorf("another scope changed: %+v", got)
	}
}

func TestAScopeWithNoViewsLeavesNothingInTheFile(t *testing.T) {
	var c Config
	c.SetViews(mine, []SavedView{{Name: "A"}})
	c.SetViews(mine, nil)
	if len(c.SavedViews) != 0 {
		t.Errorf("sets = %+v, want none", c.SavedViews)
	}
	raw, err := json.Marshal(c)
	if err != nil || strings.Contains(string(raw), "savedViews") {
		t.Errorf("file = %s %v, want no savedViews key", raw, err)
	}
}

func TestNamesAreTheSameWhateverTheCaseOrSpacing(t *testing.T) {
	if !SameName("Work today", " work TODAY ") {
		t.Error("names that differ only in case and space are one name")
	}
	if SameName("Work", "Work today") {
		t.Error("different names matched")
	}
}

func TestSavingPrefsKeepsViewsAndNeverWritesTheRunToken(t *testing.T) {
	withTempConfig(t)
	stored := Config{Token: "evt_stored", Theme: "dark", PollSeconds: 300}
	if err := stored.Save(); err != nil {
		t.Fatal(err)
	}
	run := stored
	run.Token = "evt_this_run_only"
	run.SetViews(mine, []SavedView{everything()})
	if err := run.SavePrefs(); err != nil {
		t.Fatal(err)
	}
	disk, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if disk.Token != "evt_stored" {
		t.Errorf("token = %q, want the stored one", disk.Token)
	}
	if got := disk.ViewsFor(mine); len(got) != 1 || got[0] != everything() {
		t.Errorf("views = %+v", got)
	}
	if disk.Theme != "dark" || disk.PollSeconds != 300 {
		t.Errorf("settings changed: %+v", disk)
	}
}

func TestSavingPrefsWithNoStoredTokenStoresNone(t *testing.T) {
	withTempConfig(t)
	run := Config{Token: "evt_this_run_only"}
	run.SetViews(mine, []SavedView{{Name: "A"}})
	if err := run.SavePrefs(); err != nil {
		t.Fatal(err)
	}
	disk, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if disk.Token != "" {
		t.Errorf("a per-run token was stored: %q", disk.Token)
	}
	if len(disk.ViewsFor(mine)) != 1 {
		t.Error("the view was not kept")
	}
}

func TestSavingPrefsRefusesAnUnreadableFile(t *testing.T) {
	withTempConfig(t)
	file, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	const broken = `{"token": "evt_keep", "savedViews": [`
	if err := os.WriteFile(file, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	run := Config{Token: "evt_run"}
	run.SetViews(mine, []SavedView{{Name: "A"}})
	if err := run.SavePrefs(); err == nil {
		t.Fatal("saving over a file that cannot be read should fail")
	}
	if b, _ := os.ReadFile(file); string(b) != broken {
		t.Errorf("the file was changed to %q", b)
	}
}

func TestSigningOutKeepsSavedViews(t *testing.T) {
	c := Config{Token: "evt_x", Account: "me", Theme: "dark"}
	c.SetViews(mine, []SavedView{{Name: "A"}})
	out := c.SignedOut()
	if out.Token != "" || out.Account != "" {
		t.Errorf("sign-out kept the account: %+v", out)
	}
	if got := out.ViewsFor(mine); len(got) != 1 {
		t.Errorf("sign-out dropped the views: %+v", got)
	}
}
