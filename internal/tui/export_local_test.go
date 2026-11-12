package tui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

func TestExportWithNoAccountWritesTheTodosKeptHere(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVADO_DATA_DIR", dir)
	h := offlineHarness(t, config.Config{Local: true})
	if _, err := h.model.client.CreateTodos(h.model.ctx, api.NewTodo{Title: "Pay rent"}); err != nil {
		t.Fatal(err)
	}

	h.press("4")
	h.model.acct.selectRow("export")
	h.mustNotContain("account scope")
	h.press("enter")
	if h.model.layer != layerForm || h.model.form.id != formExport {
		t.Fatalf("layer = %v, want the export form", h.model.layer)
	}
	h.press("enter")

	path := filepath.Join(dir, "evado-export.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("nothing written: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want owner only", info.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	var ex api.Export
	if err := json.Unmarshal(raw, &ex); err != nil {
		t.Fatal(err)
	}
	if ex.Format != api.ExportFormat || len(ex.Todos) != 1 || ex.Todos[0].Title != "Pay rent" {
		t.Errorf("export = %+v", ex)
	}
	if got := h.model.toastText(); !strings.Contains(got, path) || !strings.Contains(got, "1 todo") {
		t.Errorf("receipt = %q, want the path and counts", got)
	}
	h.mustContain("LAST EXPORT")
}

func TestAnExportRefusalOffersTheLocalCopy(t *testing.T) {
	a, url := newInviteAccount(t)
	t.Setenv("EVADO_DATA_DIR", t.TempDir())
	h := signedInHarness(t, a, url)
	h.model.tab = tabAccount
	h.model.acct.selectRow("export")
	h.send(exportMsg{err: &api.Error{StatusCode: http.StatusForbidden, Code: api.CodeInsufficientScope}})
	h.run(h.cmds...)
	if h.model.form == nil || !h.model.exportsFromComputer() {
		t.Fatal("local export was not offered")
	}
	h.mustContain("The server refused a todos export")
	h.mustContain("todos kept on this computer instead")
	h.press("enter")
	if !h.model.acct.exportedHere {
		t.Fatal("accepted fallback did not export the local copy")
	}
}

// Full account data stays in the web app while todos use the portable export.
func TestTodosScopeSignInExportsPortableDataAndNamesFullDataWebExport(t *testing.T) {
	a, url := newInviteAccount(t)
	t.Setenv("EVADO_DATA_DIR", t.TempDir())
	h := signedInHarness(t, a, url)
	withScope(t, h, "todos.read todos.write")
	h.press("4")

	h.model.acct.selectRow("export")
	h.mustContain("export your data from the web app")
	h.mustContain("todos.read")
	h.press("enter")
	if h.model.layer != layerForm || h.model.form.id != formExport {
		t.Fatalf("layer = %v, want the local copy's form", h.model.layer)
	}
	h.mustContain("web app")
}

func TestCachedAccountExportsFromServerWithTodosScope(t *testing.T) {
	t.Setenv("EVADO_DATA_DIR", t.TempDir())
	f := sampleAPI()
	h := newHarnessWith(t, f, func(cfg *config.Config) { cfg.NoCache = false })
	withScope(t, h, "todos.read todos.write")
	if h.model.backend == nil || h.model.client.BaseURL() != api.LocalBaseURL {
		t.Fatal("test needs cached account")
	}
	h.press("4")
	h.model.acct.selectRow("export")
	h.press("enter", "enter")
	if f.portableExports != 1 || h.model.acct.exportedHere || f.accountCalls != 0 {
		t.Fatalf("server exports=%d local=%t account=%d", f.portableExports, h.model.acct.exportedHere, f.accountCalls)
	}
}
