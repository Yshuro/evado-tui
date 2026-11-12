package local

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// benchSizes are the stored-todo counts the benchmarks run at. 10,000 is
// past what the interface loads, but the store holds every todo, so it is
// the size that shows what a write or a read of the whole file costs.
var benchSizes = []int{1000, 10000}

var benchEpoch = time.Date(2030, 1, 1, 9, 0, 0, 0, time.UTC)

func benchID(i int) string { return fmt.Sprintf("%08x-0000-4000-8000-000000000000", i) }

// benchState is n synced todos in the personal list. A third have a do
// date, a third a deadline and every fifth is done, so listing has a
// real mix to filter and order.
func benchState(n int) State {
	st := fresh(benchEpoch)
	st.User.TZ = "UTC"
	st.Lists[0].Local = false
	list := st.Lists[0].ID
	notes := "Remember to check the figures before sending them on to the rest of the team."
	for i := 0; i < n; i++ {
		at := benchEpoch.Add(time.Duration(i) * time.Hour)
		t := Todo{Todo: api.Todo{
			ID:        benchID(i),
			ListID:    list,
			Title:     fmt.Sprintf("Todo number %d for the big account", i),
			Notes:     &notes,
			CreatedAt: at,
			UpdatedAt: at,
		}}
		switch i % 3 {
		case 0:
			d := at.Add(24 * time.Hour)
			t.DoAt, t.HasTime = &d, true
		case 1:
			d := at.Add(48 * time.Hour)
			t.DueAt = &d
		}
		t.IsCompleted = i%5 == 0
		st.Todos = append(st.Todos, t)
	}
	return st
}

// benchBackend is a backend on disk holding benchState(n), with no account.
func benchBackend(b *testing.B, n int) *Backend {
	b.Helper()
	path := filepath.Join(b.TempDir(), "todos.json")
	if err := save(path, benchState(n)); err != nil {
		b.Fatal(err)
	}
	be, err := Open(path, "", "")
	if err != nil {
		b.Fatal(err)
	}
	be.now = func() time.Time { return benchEpoch.Add(time.Hour) }
	return be
}

func eachSize(b *testing.B, fn func(b *testing.B, n int)) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) { fn(b, n) })
	}
}

func BenchmarkEditTitle(b *testing.B) {
	eachSize(b, func(b *testing.B, n int) {
		be := benchBackend(b, n)
		c := be.Client()
		id := be.st.Todos[n/2].ID
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			title := fmt.Sprint("Edited ", i)
			if _, _, err := c.UpdateTodo(ctx, id, api.TodoPatch{Title: api.Val(title)}); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()
		if fi, err := os.Stat(be.path); err == nil {
			b.ReportMetric(float64(fi.Size()), "bytes-written/op")
		}
	})
}

// BenchmarkRescheduleMany moves 50 todos to another day, as the interface
// does for marked todos, one request each or as one batch.
func BenchmarkRescheduleMany(b *testing.B) {
	const batch = 50
	patch := api.TodoPatch{ScheduledDay: api.Val("2030-02-01")}
	for _, mode := range []string{"each", "batch"} {
		b.Run(mode, func(b *testing.B) {
			eachSize(b, func(b *testing.B, n int) {
				be := benchBackend(b, n)
				c := be.Client()
				ctx := context.Background()
				ids := make([]string, batch)
				for i := range ids {
					ids[i] = be.st.Todos[i*(n/batch)].ID
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if mode == "batch" {
						for _, o := range be.ApplyMany(ids, Change{Patch: patch}) {
							if o.Err != nil {
								b.Fatal(o.Err)
							}
						}
						continue
					}
					for _, id := range ids {
						if _, _, err := c.UpdateTodo(ctx, id, patch); err != nil {
							b.Fatal(err)
						}
					}
				}
				b.StopTimer()
				if fi, err := os.Stat(be.path); err == nil {
					b.ReportMetric(float64(be.writes)/float64(b.N), "writes/op")
					b.ReportMetric(float64(fi.Size())*float64(be.writes)/float64(b.N), "bytes-written/op")
				}
			})
		})
	}
}

// BenchmarkListHundred asks for a page of 100 todos, which is what the
// interface does on every load, with a broad query and a narrowed one.
func BenchmarkListHundred(b *testing.B) {
	queries := map[string]api.TodoFilter{
		"broad":    {Status: api.StatusAll, Limit: 100},
		"narrowed": {Status: api.StatusOpen, Query: "number 7", Limit: 100},
	}
	for _, name := range []string{"broad", "narrowed"} {
		b.Run(name, func(b *testing.B) {
			eachSize(b, func(b *testing.B, n int) {
				be := benchBackend(b, n)
				c := be.Client()
				ctx := context.Background()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := c.ListTodos(ctx, queries[name]); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// BenchmarkListAll reads every todo, as the interface does when it counts
// the whole account kept on this computer: no page, the steps with each.
func BenchmarkListAll(b *testing.B) {
	eachSize(b, func(b *testing.B, n int) {
		be := benchBackend(b, n)
		c := be.Client()
		ctx := context.Background()
		f := api.TodoFilter{Status: api.StatusAll, Limit: math.MaxInt32}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := c.ListTodosWithSteps(ctx, f); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// benchPage is an answer that deletes k todos of the fixture.
func benchPage(k int) syncAnswer {
	now := benchEpoch.Add(2 * time.Hour)
	var ans syncAnswer
	for i := 0; i < k; i++ {
		ans.Todos = append(ans.Todos, wireTodo{Todo: api.Todo{ID: benchID(i * 3), DeletedAt: &now}})
	}
	ans.Cursor = "1"
	return ans
}

// BenchmarkApplyDeletions folds a page of 200 remote deletions into the
// store.
func BenchmarkApplyDeletions(b *testing.B) {
	eachSize(b, func(b *testing.B, n int) {
		st := benchState(n)
		be := &Backend{now: time.Now}
		ans := benchPage(200)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			be.st = st
			be.st.Todos = append([]Todo(nil), st.Todos...)
			seen := map[string]bool{}
			b.StartTimer()
			be.apply(ans, seen)
		}
	})
}

// benchPending is a backend whose every todo is not yet sent.
func benchPending(n int) *Backend {
	be := &Backend{now: time.Now, st: benchState(n)}
	for i := range be.st.Todos {
		be.st.Todos[i].New = true
	}
	return be
}

func BenchmarkHasPending(b *testing.B) {
	eachSize(b, func(b *testing.B, n int) {
		be := benchPending(n)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if !be.hasPending() {
				b.Fatal("expected pending todos")
			}
		}
	})
}

// BenchmarkChanges builds the push batch from a store where everything
// is pending, which is the first sync of a large local account.
func BenchmarkChanges(b *testing.B) {
	eachSize(b, func(b *testing.B, n int) {
		be := benchPending(n)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if out, _ := be.changes(); len(out) != pushBatch {
				b.Fatalf("got %d changes", len(out))
			}
		}
	})
}
