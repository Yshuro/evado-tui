package local

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestExportImportRoundTripAndRepeatedImport(t *testing.T) {
	ctx := context.Background()
	source := open(t, "", "")
	c := source.Client()
	list, err := c.CreateList(ctx, "Household")
	if err != nil {
		t.Fatal(err)
	}
	notes := "Check the readings"
	made, err := c.CreateTodos(ctx, api.NewTodo{Title: "Check the meter tomorrow 9am", Notes: &notes, ListID: list.ID})
	if err != nil {
		t.Fatal(err)
	}
	steps := addSteps(t, c, made[0].ID, "Find the key", "Take a photo")
	completed := time.Now()
	if _, err := c.UpdateChecklistItem(ctx, steps[0].ID, nil, &completed); err != nil {
		t.Fatal(err)
	}
	done := add(t, c, "Pay rent")
	if _, _, err := c.CompleteTodo(ctx, done.ID); err != nil {
		t.Fatal(err)
	}
	ex := source.Export()
	raw, _ := json.Marshal(ex)
	decoded, err := api.ReadExport(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	target := open(t, "", "")
	res, err := target.Import(decoded)
	if err != nil || res.ListsAdded != 1 || res.TodosAdded != 2 || len(res.Refused) != 0 {
		t.Fatalf("import = %+v, %v", res, err)
	}
	got := target.Export()
	if len(got.Lists) != 2 || len(got.Todos) != 2 || got.Todos[0].Title != ex.Todos[0].Title || len(got.Todos[0].Checklist) != 2 || !got.Todos[1].IsCompleted {
		t.Fatalf("round trip changed content: %+v", got)
	}
	wantTodos, _ := json.Marshal(got.Todos)
	writes := target.writes
	res, err = target.Import(decoded)
	if err != nil || res.ListsAdded != 0 || res.TodosAdded != 0 || res.TodosUpdated != 0 || res.TodosSkipped != 2 || len(res.Refused) != 0 || target.writes != writes {
		t.Fatalf("second import changed something: %+v, %v", res, err)
	}
	reopened, err := Open(target.path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	keptTodos, _ := json.Marshal(reopened.Export().Todos)
	if string(keptTodos) != string(wantTodos) {
		t.Fatalf("import was not kept on disk: %v", err)
	}
}

func TestImportKeepsNewerEditsAndLocalDeletions(t *testing.T) {
	ctx := context.Background()
	svc, b, c, first := offlineWith(t, "Original")
	svc.down.Store(false)
	second := add(t, c, "Remove this")
	mustSync(t, b)
	svc.down.Store(true)
	ex := b.Export()
	if _, _, err := c.UpdateTodo(ctx, first.ID, api.TodoPatch{Title: api.Val("Newer here")}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteTodo(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	res, err := b.Import(ex)
	if err != nil || res.TodosSkipped != 2 || res.TodosUpdated != 0 {
		t.Fatalf("import = %+v, %v", res, err)
	}
	if got := titles(t, c); len(got) != 1 || got[0] != "Newer here" {
		t.Fatalf("import overwrote local changes: %v", got)
	}
	ex.Todos[0].Title = "Newer export"
	ex.Todos[0].UpdatedAt = get(t, c, first.ID).UpdatedAt.Add(time.Hour)
	res, err = b.Import(ex)
	if err != nil || res.TodosSkipped != 2 || res.TodosUpdated != 0 || get(t, c, first.ID).Title != "Newer here" {
		t.Fatalf("newer export overwrote a todo: %+v, %v", res, err)
	}
}

func TestImportRefusesInvalidEntriesAndReadonlyUpdates(t *testing.T) {
	b := open(t, "", "")
	todo := add(t, b.Client(), "Keep")
	ex := b.Export()
	ex.Todos[0].Title = "Cannot edit"
	ex.Todos[0].UpdatedAt = time.Now().Add(time.Hour)
	b.mu.Lock()
	b.st.Lists[0].ReadOnly = true
	if err := b.persist(); err != nil {
		t.Fatal(err)
	}
	b.mu.Unlock()
	res, err := b.Import(ex)
	if err != nil || res.TodosSkipped != 1 || len(res.Refused) != 0 || get(t, b.Client(), todo.ID).Title != "Keep" {
		t.Fatalf("read-only update = %+v, %v", res, err)
	}
	target := open(t, "", "")
	ex.Todos[0].Notes = strp(strings.Repeat("😀", 5001))
	res, err = target.Import(ex)
	if err != nil || res.TodosAdded != 0 || len(res.Refused) != 1 {
		t.Fatalf("invalid todo = %+v, %v", res, err)
	}
}

func TestImportRefusesUnknownExportShapeBeforeWriting(t *testing.T) {
	b := open(t, "", "")
	add(t, b.Client(), "Keep")
	raw, _ := json.Marshal(b.Export())
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	fields["version"] = 2
	raw, _ = json.Marshal(fields)
	if _, err := api.ReadExport(strings.NewReader(string(raw))); err == nil {
		t.Fatal("unknown version accepted")
	}
	delete(fields, "version")
	delete(fields, "todos")
	raw, _ = json.Marshal(fields)
	if _, err := api.ReadExport(strings.NewReader(string(raw))); err == nil {
		t.Fatal("incomplete shape accepted")
	}
	if got := titles(t, b.Client()); len(got) != 1 || got[0] != "Keep" {
		t.Fatal("bad file changed data")
	}
}

func TestImportedOfflineTodosAndChecklistsSyncOnce(t *testing.T) {
	ctx := context.Background()
	source := open(t, "", "")
	list, err := source.Client().CreateList(ctx, "Household")
	if err != nil {
		t.Fatal(err)
	}
	made, err := source.Client().CreateTodos(ctx, api.NewTodo{Title: "Take a reading", ListID: list.ID})
	if err != nil {
		t.Fatal(err)
	}
	addSteps(t, source.Client(), made[0].ID, "Take a photo")
	svc, url := newImportService(t)
	target := syncedAt(t, filepath.Join(t.TempDir(), "todos.json"), url)
	c := target.Client()
	add(t, c, "Already here")
	mustSync(t, target)
	svc.down.Store(true)
	res, err := target.Import(source.Export())
	if err != nil || res.TodosAdded != 1 {
		t.Fatalf("import = %+v, %v", res, err)
	}
	svc.down.Store(false)
	result := mustSync(t, target)
	if len(result.Refused) != 0 || target.Status().Pending != 0 {
		t.Fatalf("import did not settle: %+v", result)
	}
	got := get(t, c, made[0].ID)
	if got.Title != "Take a reading" || len(checklist(t, c, got.ID)) != 1 || svc.todos[got.ID] == nil {
		t.Fatal("imported todo or checklist did not reach the account")
	}
	if again := mustSync(t, target); again.Pushed != 0 {
		t.Fatal("settled import was pushed again")
	}
}

func TestImportPreviewPrivacyRemindersAndListMapping(t *testing.T) {
	source := open(t, "", "")
	ctx := context.Background()
	l, err := source.Client().CreateList(ctx, "Work")
	if err != nil {
		t.Fatal(err)
	}
	made, err := source.Client().CreateTodos(ctx, api.NewTodo{Title: "Tomorrow Land", ListID: l.ID})
	if err != nil {
		t.Fatal(err)
	}
	ex := source.Export()
	ex.User.ID = "another-account"
	ex.Lists[1].OwnerID = "another-account"
	ex.Lists[1].Members = []api.Member{{UserID: "foreign", Email: "private@example.com"}}
	ex.Todos[0].Title = "Tomorrow Land"
	ex.Todos[0].AssigneeID = strp("foreign")
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	ex.Todos[0].RemindAt = &past
	target := open(t, "", "")
	own, err := target.Client().CreateList(ctx, "Work")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(target.st)
	writes := target.writes
	res, err := target.Import(ex, api.ImportOptions{DryRun: true})
	after, _ := json.Marshal(target.st)
	if err != nil || !res.DryRun || res.Added != 1 || res.ListsAdded != 0 || string(before) != string(after) || writes != target.writes {
		t.Fatalf("preview = %+v %v", res, err)
	}
	res, err = target.Import(ex)
	if err != nil || res.Added != 1 || res.Dropped["assignees"] != 1 {
		t.Fatalf("import = %+v %v", res, err)
	}
	got := get(t, target.Client(), made[0].ID)
	if got.ListID != own.ID || got.AssigneeID != nil || got.RemindAt != nil || got.Title != "Tomorrow Land" {
		t.Fatalf("mapped = %+v", got)
	}
	ex.Todos[0].ID = NewID()
	ex.Todos[0].RemindedAt = &past
	ex.Todos[0].RemindAt = &future
	if _, err := target.Import(ex); err != nil {
		t.Fatal(err)
	}
	if got := get(t, target.Client(), ex.Todos[0].ID); got.RemindAt == nil || got.RemindedAt != nil {
		t.Fatal("future reminder lost")
	}
	ex.Todos[0].ID = NewID()
	ex.Todos[0].IsCompleted = true
	if _, err := target.Import(ex); err != nil {
		t.Fatal(err)
	}
	if get(t, target.Client(), ex.Todos[0].ID).RemindAt != nil {
		t.Fatal("completed todo retained reminder")
	}
}

func TestImportOverrideAndChecklistCollision(t *testing.T) {
	source := open(t, "", "")
	todo := add(t, source.Client(), "Imported")
	addSteps(t, source.Client(), todo.ID, "Step")
	ex := source.Export()
	target := open(t, "", "")
	existing := add(t, target.Client(), "Existing")
	oldSteps := addSteps(t, target.Client(), existing.ID, "Already here")
	ex.Todos[0].Checklist[0].ID = oldSteps[0].ID
	res, err := target.Import(ex, api.ImportOptions{ListID: target.personal()})
	if err != nil || res.Added != 1 {
		t.Fatalf("override = %+v %v", res, err)
	}
	importedSteps := checklist(t, target.Client(), todo.ID)
	if len(importedSteps) != 1 || importedSteps[0].ID == oldSteps[0].ID || importedSteps[0].TodoID != todo.ID {
		t.Fatalf("colliding step = %+v", importedSteps)
	}
	if _, err := target.Import(ex, api.ImportOptions{ListID: "missing"}); err == nil {
		t.Fatal("missing override accepted")
	}
	target.st.Lists[0].ReadOnly = true
	if _, err := target.Import(ex, api.ImportOptions{ListID: target.personal()}); err == nil {
		t.Fatal("readonly override accepted")
	}
}

func TestImportPrivateListAndMalformedExistingTodo(t *testing.T) {
	source := open(t, "", "")
	l, err := source.Client().CreateList(context.Background(), "Shared source")
	if err != nil {
		t.Fatal(err)
	}
	made, err := source.Client().CreateTodos(context.Background(), api.NewTodo{Title: "Imported", ListID: l.ID})
	if err != nil {
		t.Fatal(err)
	}
	ex := source.Export()
	ex.Lists[1].OwnerID = "foreign"
	ex.Lists[1].ReadOnly = true
	ex.Lists[1].Members = []api.Member{{UserID: "foreign", Email: "private@example.com"}}
	target := open(t, "", "")
	target.st.UserID = "target-account"
	res, err := target.Import(ex)
	if err != nil || res.Added != 1 {
		t.Fatalf("foreign import = %+v %v", res, err)
	}
	imported := target.list(get(t, target.Client(), made[0].ID).ListID)
	if imported.OwnerID != target.st.User.ID || imported.ReadOnly || imported.Role != "owner" || len(imported.Members) != 1 || imported.Members[0].UserID != target.st.User.ID {
		t.Fatalf("sharing imported: %+v", imported)
	}
	ex.Todos[0].Title = ""
	ex.Todos[0].ListID = "unknown"
	ex.Todos[0].UpdatedAt = time.Now().Add(time.Hour)
	res, err = target.Import(ex)
	if err != nil || res.Skipped != 1 || res.RefusedCount != 0 || get(t, target.Client(), made[0].ID).Title != "Imported" {
		t.Fatalf("existing malformed copy = %+v %v", res, err)
	}
}

func TestImportWholeFileLimitWritesNothing(t *testing.T) {
	b := open(t, "", "")
	before, _ := json.Marshal(b.st)
	ex := api.Export{Todos: make([]api.SyncTodo, api.MaxImportTodos+1)}
	if _, err := b.Import(ex); err == nil {
		t.Fatal("oversized file accepted")
	}
	after, _ := json.Marshal(b.st)
	if string(before) != string(after) {
		t.Fatal("oversized file changed store")
	}
}
