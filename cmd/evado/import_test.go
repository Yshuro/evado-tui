package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/local"
	"github.com/nerdlabz/evado-tui/internal/ops"
)

func TestCLIImportsFilesAndStandardInputIdempotently(t *testing.T) {
	ctx := context.Background()
	source, err := local.Open(filepath.Join(t.TempDir(), "todos.json"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Client().CreateTodos(ctx, api.NewTodo{Title: "Pay rent"}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "export.json")
	if err := local.WriteExport(file, source.Export()); err != nil {
		t.Fatal(err)
	}
	target, err := local.Open(filepath.Join(t.TempDir(), "todos.json"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runCommand(ctx, ops.New(target.Client()), []string{"import", file, "-json"}, &out, nil, false); err != nil {
		t.Fatal(err)
	}
	var result api.ImportResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.TodosAdded != 1 {
		t.Fatalf("import output = %s, %v", &out, err)
	}
	raw, _ := json.Marshal(source.Export())
	out.Reset()
	if err := runCommand(ctx, ops.New(target.Client()), []string{"import", "-"}, &out, strings.NewReader(string(raw)), false); err != nil || !strings.Contains(out.String(), "1 skipped") {
		t.Fatalf("second import = %s, %v", &out, err)
	}
}

func TestCLIImportOpensItsFileForServer(t *testing.T) {
	var out bytes.Buffer
	err := runCommand(context.Background(), ops.New(api.New("evt_test")), []string{"import", "does-not-exist.json"}, &out, nil, false)
	if info := describe(err); !strings.Contains(info.Message, "does-not-exist.json") {
		t.Fatalf("remote import = %+v", info)
	}
}
