package tui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nerdlabz/evado-tui/internal/api"
	"github.com/nerdlabz/evado-tui/internal/ops"
)

type importMsg struct {
	result  api.ImportResult
	err     error
	path    string
	data    []byte
	preview bool
}

func importFile(m *Model, path string, data []byte, preview bool) tea.Cmd {
	return func() tea.Msg {
		if data == nil {
			f, err := os.Open(path)
			if err != nil {
				return importMsg{err: err, path: path, preview: preview}
			}
			defer func() { _ = f.Close() }()
			data, err = io.ReadAll(io.LimitReader(f, api.MaxImportBytes+1))
			if err != nil {
				return importMsg{err: err, path: path, preview: preview}
			}
		}
		opts := api.ImportOptions{DryRun: preview}
		var result api.ImportResult
		var err error
		if m.backend != nil && !m.backend.Status().SignedIn {
			var ex api.Export
			ex, err = api.ReadExport(bytes.NewReader(data))
			if err == nil {
				result, err = m.backend.Import(ex, opts)
			}
		} else {
			result, err = ops.New(directClient(m.remote())).Import(m.ctx, bytes.NewReader(data), opts)
		}
		return importMsg{result: result, err: err, path: path, data: data, preview: preview}
	}
}

func importSummary(r api.ImportResult) string {
	phase := "Import"
	if r.DryRun {
		phase = "Preview"
	}
	summary := fmt.Sprintf("%s: %d todos added, %d skipped, %d deleted skipped; %d lists added, %d used; %d refused.", phase, max(r.Added, r.TodosAdded), max(r.Skipped, r.TodosSkipped), r.SkippedDeleted, max(r.ListsAdded, len(r.Lists.Created)), max(r.ListsSkipped, len(r.Lists.Used)), max(r.RefusedCount, len(r.Refused)))
	if r.TodosUpdated > 0 {
		summary += fmt.Sprintf(" %d todos updated.", r.TodosUpdated)
	}
	if len(r.Dropped) > 0 {
		fields := []string{}
		for name, n := range r.Dropped {
			if n > 0 {
				fields = append(fields, fmt.Sprintf("%s (%d)", name, n))
			}
		}
		sort.Strings(fields)
		if len(fields) > 0 {
			summary += " Dropped fields: " + strings.Join(fields, ", ") + "."
		}
	}
	if r.ApproximateDates > 0 {
		summary += fmt.Sprintf(" %d approximate dates.", r.ApproximateDates)
	}
	if !r.Complete {
		summary += fmt.Sprintf(" Incomplete, %d remaining.", r.Remaining)
	}
	return summary
}

func (m *Model) restoreImportForm(path string) tea.Cmd {
	f := newForm(formImport, "Import todos", m.width)
	f.note("hint", "Preview first. Existing todos and pending deletions are kept.")
	f.field("path", "export file", path, "path to an Evado JSON export")
	return m.openForm(f)
}

func importRefusalName(r api.ImportRefusal) string {
	if r.Kind != "" || r.ID != "" {
		return strings.TrimSpace(r.Kind + " " + r.ID)
	}
	return fmt.Sprintf("item %d", r.Item)
}
