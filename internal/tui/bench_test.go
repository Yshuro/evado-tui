package tui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/config"
)

// benchLoads are the todo counts the interface can hold: through the
// service it first loads 200 and L raises that to at most 2,000, and the
// todos kept on this computer are all loaded, however many there are.
var benchLoads = []int{200, 1000, 2000, 5000}

// benchModel is a model with n todos loaded and no server behind it. The
// todos are spread over every section and some are done, as in a real
// account, and their titles share words so a search narrows gradually.
func benchModel(b testing.TB, n int) *Model {
	b.Helper()
	m := New(context.Background(), config.Config{Token: "evt_bench", BaseURL: "http://127.0.0.1:1", NoCache: true})
	m.ready = true
	m.todos.setViewport(100, 40)
	now := m.now
	notes := "Remember to check the figures before sending them on to the rest of the team."
	todos := make([]api.Todo, n)
	for i := range todos {
		t := api.Todo{
			ID:        fmt.Sprintf("%08x-0000-4000-8000-000000000000", i),
			ListID:    "l1",
			Title:     fmt.Sprintf("Item %d in the big account", i),
			CreatedAt: now.Add(-time.Duration(i) * time.Minute),
			UpdatedAt: now,
		}
		if i%2 == 0 {
			t.Notes = &notes
		}
		switch i % 7 {
		case 1, 2, 3, 4:
			day := now.AddDate(0, 0, i%9-2).Format("2006-01-02")
			t.ScheduledDay = &day
		case 5:
			due := now.Add(time.Duration(i%48-24) * time.Hour)
			t.DueAt = &due
		case 6:
			t.IsCompleted = true
			done := now.Add(-time.Duration(i%100) * time.Hour)
			t.CompletedAt = &done
		}
		todos[i] = t
	}
	m.todos.setItems(todos)
	return m
}

func eachLoad(b *testing.B, fn func(b *testing.B, n int)) {
	for _, n := range benchLoads {
		b.Run(fmt.Sprint(n), func(b *testing.B) { fn(b, n) })
	}
}

// BenchmarkRebuild is what every change to the loaded todos costs: a
// refresh, a completion, a feed merge. The board is what it does on top
// when the board is the view on screen.
func BenchmarkRebuild(b *testing.B) {
	for _, board := range []bool{false, true} {
		name := "list"
		if board {
			name = "board"
		}
		b.Run(name, func(b *testing.B) {
			eachLoad(b, func(b *testing.B, n int) {
				m := benchModel(b, n)
				m.todos.filter.board = board
				m.todos.rebuild("")
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					m.todos.rebuild("")
				}
			})
		})
	}
}

// BenchmarkSearchKeystroke is one key typed into the search box, which
// narrows the loaded todos live. The query alternates between a word and
// the word with a digit added, so every other key narrows and the others
// widen again, as typing and deleting do.
func BenchmarkSearchKeystroke(b *testing.B) {
	eachLoad(b, func(b *testing.B, n int) {
		m := benchModel(b, n)
		queries := [2]string{"item big", "item big 1"}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.todos.setLive(queries[i&1])
		}
	})
}

// BenchmarkDraw is one frame of the Todos tab with the whole account
// loaded, which is what every key press and tick costs on screen.
func BenchmarkDraw(b *testing.B) {
	eachLoad(b, func(b *testing.B, n int) {
		m := benchModel(b, n)
		m.width, m.height = 100, 40
		m.todos.computeStats()
		_ = m.View()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = m.View()
		}
	})
}
