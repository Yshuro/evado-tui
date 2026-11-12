package api

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"time"
)

// ExportTodos needs todos.read rather than the account scope of Export.
func (c *Client) ExportTodos(ctx context.Context) (Export, error) {
	path := "/me/export/todos"
	if c.BaseURL() == LocalBaseURL {
		path = "/local/export"
	}
	var raw json.RawMessage
	_, err := c.do(ctx, http.MethodGet, path, nil, &raw)
	if err != nil {
		return Export{}, err
	}
	if _, err := validateExportJSON(raw, false); err != nil {
		return Export{}, err
	}
	return decodeExport(raw)
}

// EncodeTodosExport writes only portable todo data, including when its input
// came from the full account export.
func EncodeTodosExport(w io.Writer, ex Export) error {
	type list struct {
		ID         string    `json:"id"`
		Name       string    `json:"name"`
		IsPersonal bool      `json:"isPersonal"`
		Shared     bool      `json:"shared"`
		CreatedAt  time.Time `json:"createdAt"`
	}
	out := struct {
		Format     string                       `json:"format"`
		Version    int                          `json:"version"`
		ExportedAt time.Time                    `json:"exportedAt"`
		Lists      []list                       `json:"lists"`
		Todos      []map[string]json.RawMessage `json:"todos"`
	}{Format: ExportFormat, Version: ExportVersion, ExportedAt: ex.ExportedAt, Lists: []list{}, Todos: []map[string]json.RawMessage{}}
	if out.ExportedAt.IsZero() {
		out.ExportedAt = time.Now().UTC()
	}
	for _, in := range ex.Lists {
		out.Lists = append(out.Lists, list{ID: in.ID, Name: in.Name, IsPersonal: in.IsPersonal, Shared: in.Shared || len(in.Members) > 1, CreatedAt: in.CreatedAt})
	}
	for _, in := range ex.Todos {
		if in.Tombstoned() {
			continue
		}
		if in.Checklist == nil {
			in.Checklist = []ChecklistItem{}
		} else {
			// The service imports steps in array order rather than by position.
			in.Checklist = slices.Clone(in.Checklist)
			slices.SortStableFunc(in.Checklist, func(a, b ChecklistItem) int {
				return cmp.Compare(a.Position, b.Position)
			})
		}
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		var todo map[string]json.RawMessage
		if err := json.Unmarshal(raw, &todo); err != nil {
			return err
		}
		delete(todo, "assigneeId")
		delete(todo, "deletedAt")
		out.Todos = append(out.Todos, todo)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
