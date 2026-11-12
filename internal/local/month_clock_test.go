package local

import (
	"testing"
	"time"
)

func TestMonthFollowedByATimeDoesNotStealTheDay(t *testing.T) {
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ text, title, at string }{
		{"the first of november 7:30", "the first of november 7:30", "2026-11-01 07:30"},
		{"Dentist 3 oct 5 o'clock", "Dentist", "2026-10-03 17:00"},
		{"Dentist 3 oct 5pm", "Dentist", "2026-10-03 17:00"},
	} {
		p := Parse(tc.text, time.UTC, now)
		if p.DoAt == nil || p.DoAt.Format("2006-01-02 15:04") != tc.at || p.Title != tc.title {
			t.Errorf("Parse(%q) = %+v, want %q at %s", tc.text, p, tc.title, tc.at)
		}
	}
}
