package local

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestExportHasTheServicesShapeAndLeavesOutDeletedTodos(t *testing.T) {
	b := open(t, "", "")
	c := b.Client()
	ctx := context.Background()
	made, err := c.CreateTodos(ctx, api.NewTodo{Title: "Send the report"})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := c.CreateTodos(ctx, api.NewTodo{Title: "Throw away"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddChecklistItem(ctx, made[0].ID, "Call the bank"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteTodo(ctx, gone[0].ID); err != nil {
		t.Fatal(err)
	}

	ex := b.Export()
	if ex.User.ID != localUser {
		t.Errorf("user id = %q, want the local one", ex.User.ID)
	}
	if len(ex.Todos) != 1 || ex.Todos[0].Title != "Send the report" {
		t.Fatalf("todos = %+v, want only the live one", ex.Todos)
	}
	if got := ex.Todos[0].Checklist; len(got) != 1 || got[0].Title != "Call the bank" || got[0].TodoID != made[0].ID {
		t.Errorf("checklist = %+v", got)
	}
	if len(ex.Lists) != 1 || ex.Lists[0].ID != ex.Todos[0].ListID {
		t.Errorf("lists = %+v", ex.Lists)
	}
	if len(ex.Lists[0].Members) != 1 || ex.Lists[0].Members[0].UserID != ex.User.ID || ex.Lists[0].Members[0].Role != "owner" {
		t.Fatalf("personal list owner missing: %+v", ex.Lists[0].Members)
	}

	// Empty arrays, not null, so a reader that expects the service's file
	// does not trip over missing parts.
	raw, err := json.Marshal(ex)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"exportedAt", "user", "lists", "todos", "notificationChannels", "apiTokens"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("missing %q", k)
		}
	}
	if string(keys["notificationChannels"]) != "[]" || string(keys["apiTokens"]) != "[]" {
		t.Errorf("channels %s tokens %s, want empty arrays", keys["notificationChannels"], keys["apiTokens"])
	}
}

func TestExportRoundTripsThroughTheFile(t *testing.T) {
	b := open(t, "", "")
	c := b.Client()
	ctx := context.Background()
	made, err := c.CreateTodos(ctx, api.NewTodo{Title: "Water the plants"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddChecklistItem(ctx, made[0].ID, "Fill the can"); err != nil {
		t.Fatal(err)
	}
	want := b.Export()

	path := filepath.Join(t.TempDir(), "nested", "export.json")
	if err := WriteExport(path, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want owner only", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := api.ReadExport(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Todos) != 1 || got.Todos[0].Title != want.Todos[0].Title || len(got.Todos[0].Checklist) != 1 {
		t.Fatalf("read back = %+v", got)
	}
	for _, private := range []string{"user", "members", "ownerId", "assigneeId", "apiTokens", "notificationChannels"} {
		if strings.Contains(string(raw), "\""+private+"\"") {
			t.Errorf("portable export contains %s", private)
		}
	}
}

func TestWriteExportTightensAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteExport(path, api.Export{}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
}

func TestExportCheckedRefusesDamagedStore(t *testing.T) {
	b := open(t, "", "")
	add(t, b.Client(), "Keep")
	if err := os.WriteFile(b.path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ExportChecked(); err == nil {
		t.Fatal("damaged store exported a stale snapshot")
	}
	if _, err := b.Client().ExportTodos(context.Background()); err == nil {
		t.Fatal("export route returned stale data")
	}
}

func TestExportRouteRefusesHeldStore(t *testing.T) {
	b := open(t, "", "")
	add(t, b.Client(), "Keep")
	oldWait := diskWait
	diskWait = 20 * time.Millisecond
	t.Cleanup(func() { diskWait = oldWait })
	f, err := lockFile(b.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := b.Client().ExportTodos(context.Background()); err == nil {
		t.Fatal("locked store exported stale data")
	}
}
