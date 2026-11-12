package tui

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
	"github.com/nerdlabz/evado-tui/internal/local"
)

func TestAccountImportsAnExportAndShowsTheReceipt(t *testing.T) {
	h := offlineHarness(t, config.Config{Local: true})
	source, err := local.Open(filepath.Join(t.TempDir(), "todos.json"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Client().CreateTodos(h.model.ctx, api.NewTodo{Title: "Pay rent"}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "export.json")
	if err := local.WriteExport(file, source.Export()); err != nil {
		t.Fatal(err)
	}
	h.press("4")
	h.model.acct.selectRow("import")
	h.press("enter")
	if h.model.form == nil || h.model.form.id != formImport {
		t.Fatal("no import form")
	}
	h.typeText(file)
	h.press("enter")
	if h.model.layer != layerConfirm {
		t.Fatal("no import preview confirmation")
	}
	h.press("enter")
	if h.model.acct.imported == nil || h.model.acct.imported.TodosAdded != 1 {
		t.Fatal("import receipt missing")
	}
	h.mustSay("1 todos added")
	h.press("1")
	h.mustSay("Pay rent")
}

func TestServerImportPreviewsBeforeApplyingCapturedFile(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	file := filepath.Join(t.TempDir(), "export.json")
	raw := []byte(`{"format":"evado-export","version":1,"lists":[],"todos":[{"id":"todo","title":"Pay rent","checklist":[]}]}`)
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	h.press("4")
	h.model.acct.selectRow("import")
	h.press("enter")
	h.typeText(file)
	h.press("enter")
	if len(f.importCalls) != 1 || !f.importCalls[0] || h.model.layer != layerConfirm {
		t.Fatalf("preview calls=%v layer=%v", f.importCalls, h.model.layer)
	}
	if err := os.WriteFile(file, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.press("enter")
	if len(f.importCalls) != 2 || f.importCalls[1] || !bytes.Equal(f.importBodies[0], f.importBodies[1]) {
		t.Fatalf("apply calls=%v bodies=%s", f.importCalls, f.importBodies)
	}
	h.mustSay("1 todos added")
}

func TestServerImportCancellationRestoresThePathWithoutApplying(t *testing.T) {
	f := sampleAPI()
	h := newHarness(t, f)
	file := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(file, []byte(`{"todos":[],"lists":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.press("4")
	h.model.acct.selectRow("import")
	h.press("enter")
	h.typeText(file)
	h.press("enter", "esc")
	if len(f.importCalls) != 1 || h.model.form == nil || h.model.form.result(false).values["path"] != file {
		t.Fatalf("calls=%v form=%v", f.importCalls, h.model.form)
	}
}

func TestServerImportFailurePreservesThePath(t *testing.T) {
	f := sampleAPI()
	f.refuse = func(r *http.Request) bool { return r.URL.Path == "/import" }
	h := newHarness(t, f)
	file := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(file, []byte(`{"todos":[],"lists":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.press("4")
	h.model.acct.selectRow("import")
	h.press("enter")
	h.typeText(file)
	h.press("enter")
	if h.model.form == nil || h.model.form.result(false).values["path"] != file {
		t.Fatal("error discarded import path")
	}
}

func TestCachedAccountImportsFromServerAfterExportRefusal(t *testing.T) {
	t.Setenv("EVADO_DATA_DIR", t.TempDir())
	f := sampleAPI()
	h := newHarnessWith(t, f, func(cfg *config.Config) { cfg.NoCache = false })
	if h.model.backend == nil || h.model.client.BaseURL() != api.LocalBaseURL || !h.model.backend.Status().SignedIn {
		t.Fatal("test needs a signed-in cached account")
	}
	h.model.acct.exportRefused = true
	file := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(file, []byte(`{"todos":[],"lists":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.press("4")
	h.model.acct.selectRow("import")
	h.press("enter")
	h.typeText(file)
	h.press("enter", "enter")
	if len(f.importCalls) != 2 || !f.importCalls[0] || f.importCalls[1] {
		t.Fatalf("server import calls=%v", f.importCalls)
	}
}

func TestIncompleteServerImportDoesNotRetryAndRestoresThePath(t *testing.T) {
	f := sampleAPI()
	f.importIncomplete = true
	h := newHarness(t, f)
	file := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(file, []byte(`{"todos":[],"lists":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.press("4")
	h.model.acct.selectRow("import")
	h.press("enter")
	h.typeText(file)
	h.press("enter", "enter")
	if len(f.importCalls) != 2 || h.model.form == nil || h.model.form.result(false).values["path"] != file {
		t.Fatalf("calls=%v form=%v", f.importCalls, h.model.form)
	}
	h.mustSay("Rerun this file explicitly")
}
