package main

import (
	"strings"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/testapi"
)

func TestRepeatsInDateFlagsAreRefusedBeforeAnyWrite(t *testing.T) {
	for _, flag := range []string{"-day", "-at", "-due", "-remind"} {
		f := testapi.New()
		rule, day := "1:week:5", "2026-10-09"
		f.Parsed = api.ParsePreview{ScheduledDay: &day, RecurrenceRule: &rule}
		_, err := run(t, f, "", "edit", f.Todos[0].ID, flag, "every friday")
		if err == nil || !strings.Contains(err.Error(), "-repeat") || len(f.Patches) != 0 {
			t.Fatalf("%s: %v, patches %+v", flag, err, f.Patches)
		}
	}
}

func TestShowPrintsADateWithoutATime(t *testing.T) {
	f := testapi.New()
	at := time.Date(2026, 10, 9, 9, 0, 0, 0, time.Local)
	day := "2026-10-09"
	f.Todos[0].DoAt, f.Todos[0].ScheduledDay, f.Todos[0].HasTime = &at, &day, false
	out, err := run(t, f, "", "show", f.Todos[0].ID)
	if err != nil || strings.Contains(out, "09:00") || !strings.Contains(out, day) {
		t.Fatalf("show = %q %v", out, err)
	}
}
