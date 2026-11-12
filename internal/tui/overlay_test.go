package tui

import (
	"strings"
	"testing"
)

// A dialog drawn over blank screen rows must keep its left edge straight.
// The screen is padded with empty lines, which have no cells to the left of
// the dialog, and the rows used to slide to column 0.
func TestOverlayKeepsAlignmentOverBlankRows(t *testing.T) {
	base := "aaaaaaaaaaaaaaaaaaaa\n\n\n"
	rows := strings.Split(overlay(base, "XXXX\nXXXX\nXXXX", 20, 4), "\n")
	for i, row := range rows[:3] {
		if got := strings.Index(row, "XXXX"); got != 8 {
			t.Errorf("row %d: dialog starts at cell %d, want 8 (%q)", i, got, row)
		}
	}
}
