package local

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A file written before the service gave out cursors holds a time. The
// service refuses that as a cursor, so it is dropped and the next sync is a
// full pull, which keeps the todos here.
func TestATimeLeftAsACursorIsDroppedOnLoad(t *testing.T) {
	st := fresh(time.Now())
	st.Todos = []Todo{{Todo: wireTodo{}.Todo}}
	st.Cursor = "2026-03-10T15:00:00.123456789Z"
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "todos.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.st.Cursor != "" {
		t.Errorf("cursor = %q, want a time to be dropped", b.st.Cursor)
	}
	if len(b.st.Todos) != 1 {
		t.Errorf("todos = %d, dropping the cursor must keep what is here", len(b.st.Todos))
	}
}

func TestACursorTheServiceGaveIsKeptAsItCame(t *testing.T) {
	ctx := context.Background()
	svc, url := newService(t)
	b := signedIn(t, svc, url)
	if !cursorShape.MatchString(b.st.Cursor) {
		t.Fatalf("cursor = %q, want what the service gave", b.st.Cursor)
	}
	first := b.st.Cursor
	if _, err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if b.st.Cursor == first || !cursorShape.MatchString(b.st.Cursor) {
		t.Errorf("cursor = %q after a second sync, want the newer one the service gave", b.st.Cursor)
	}
}
