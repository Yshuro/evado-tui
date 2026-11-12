package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestCatchyGridIsWellFormed(t *testing.T) {
	if len(catchy) != catchyRows*2 {
		t.Fatalf("grid has %d pixel rows, want %d", len(catchy), catchyRows*2)
	}
	for i, row := range catchy {
		if len(row) != len(catchy[0]) {
			t.Errorf("row %d is %d wide, row 0 is %d", i, len(row), len(catchy[0]))
		}
		for _, c := range []byte(row) {
			if _, ok := catchyColors[c]; !ok && c != '.' {
				t.Errorf("row %d has unknown pixel %q", i, c)
			}
		}
	}
}

// TestCatchyFaceFitsTerminalRows guards the bug where the eyes sat across a
// half-block row pair and each showed up twice, once on each terminal line.
// The face is everything below the last coral pixel of the note. It must hold
// exactly two eyes and one smile, and each must sit inside one terminal row.
func TestCatchyFaceFitsTerminalRows(t *testing.T) {
	faceTop := 0
	for i, row := range catchy {
		if strings.ContainsRune(row, 'n') {
			faceTop = i + 1
		}
	}
	seen := make(map[[2]int]bool)
	var features int
	for y := faceTop; y < len(catchy); y++ {
		for x := range catchy[y] {
			if catchy[y][x] != 'd' || seen[[2]int{y, x}] {
				continue
			}
			features++
			rows := make(map[int]bool)
			stack := [][2]int{{y, x}}
			for len(stack) > 0 {
				p := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if p[0] < faceTop || p[0] >= len(catchy) || p[1] < 0 || p[1] >= len(catchy[p[0]]) ||
					catchy[p[0]][p[1]] != 'd' || seen[p] {
					continue
				}
				seen[p] = true
				rows[p[0]/2] = true
				stack = append(stack, [2]int{p[0] - 1, p[1]}, [2]int{p[0] + 1, p[1]},
					[2]int{p[0], p[1] - 1}, [2]int{p[0], p[1] + 1})
			}
			if len(rows) != 1 {
				t.Errorf("feature at pixel row %d col %d spans %d terminal rows", y, x, len(rows))
			}
		}
	}
	if features != 3 {
		t.Errorf("face has %d dark features, want two eyes and one smile", features)
	}
}

func TestRenderCatchySize(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	out := renderCatchy()
	if h := lipgloss.Height(out); h != catchyRows {
		t.Errorf("height %d, want %d", h, catchyRows)
	}
	if w := lipgloss.Width(out); w != len(catchy[0]) {
		t.Errorf("width %d, want %d", w, len(catchy[0]))
	}
	if !strings.ContainsRune(out, '▀') {
		t.Error("expected half blocks in the output")
	}
}

func TestLoginShowsMascotOnlyWhenThereIsRoom(t *testing.T) {
	l := newLoginView("https://evado.example/api/v1", "")
	if tall := l.view(100, 50); !strings.ContainsRune(tall, '▀') {
		t.Error("tall terminal should show the mascot")
	}
	short := l.view(100, 20)
	if strings.ContainsRune(short, '▀') || strings.ContainsRune(short, '█') {
		t.Error("short terminal should drop the mascot")
	}
	if !strings.Contains(short, "token") {
		t.Error("the token field must stay visible on a short terminal")
	}
}
