package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/ops"
)

func TestCLIExportPortableJSONToStdoutAndOwnerOnlyFile(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/export/todos" || r.Method != http.MethodGet {
			t.Errorf("unexpected export request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
			return
		}
		calls++
		_ = api.EncodeTodosExport(w, api.Export{ExportedAt: time.Now(), Todos: []api.SyncTodo{{Todo: api.Todo{ID: "t1", Title: "Pay rent"}}}})
	}))
	defer srv.Close()
	o := ops.New(api.New("evt_test", api.WithBaseURL(srv.URL)))
	var out bytes.Buffer
	if err := runCommand(context.Background(), o, []string{"export"}, &out, nil, false); err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &data); err != nil || string(data["format"]) != `"evado-export"` || data["user"] != nil {
		t.Fatalf("export=%s err=%v", out.Bytes(), err)
	}
	file := filepath.Join(t.TempDir(), "export.json")
	if err := runCommand(context.Background(), o, []string{"export", file}, &out, nil, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat export file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("export file mode = %v, want owner only", info.Mode().Perm())
	}
	if calls != 2 {
		t.Errorf("export requests = %d, want 2", calls)
	}
}

func TestCLIServerImportFlagsAndIncompleteReportArePreserved(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/import" || r.URL.Query().Get("dryRun") != "true" || r.URL.Query().Get("listId") != "destination" || r.URL.Query().Get("format") != "evado" {
			t.Errorf("import request %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(api.ImportResult{DryRun: true, Added: 200, Skipped: 2, Complete: false, Remaining: 3, Dropped: map[string]int{"assignee": 1}})
	}))
	defer srv.Close()
	var out bytes.Buffer
	err := runCommand(context.Background(), ops.New(api.New("evt_test", api.WithBaseURL(srv.URL))), []string{"import", "-dry-run", "-list", "destination", "-json", "-"}, &out, strings.NewReader(`{"todos":[],"lists":[]}`), false)
	if info := describe(err); info.Code != "partial_import" || info.ExitCode != 10 || calls != 1 {
		t.Fatalf("error=%+v calls=%d", info, calls)
	}
	var report api.ImportResult
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Added != 200 || report.Complete || report.Remaining != 3 {
		t.Fatalf("report=%s err=%v", out.Bytes(), err)
	}
}
