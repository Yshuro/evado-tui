package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	LocalBaseURL   = "http://evado.local/api/v1"
	MaxImportBytes = 32 << 20
	MaxImportTodos = 25000
	ExportFormat   = "evado-export"
	ExportVersion  = 1
)

type ImportOptions struct {
	DryRun bool
	ListID string
}

type ImportRefusal struct {
	Item   int    `json:"item,omitempty"`
	Kind   string `json:"kind,omitempty"`
	ID     string `json:"id,omitempty"`
	Reason string `json:"reason"`
}

type ImportLists struct {
	Created []string `json:"created"`
	Used    []string `json:"used"`
}

type ImportResult struct {
	DryRun           bool            `json:"dryRun"`
	Format           string          `json:"format"`
	Added            int             `json:"added"`
	Skipped          int             `json:"skipped"`
	SkippedDeleted   int             `json:"skippedDeleted"`
	Complete         bool            `json:"complete"`
	Remaining        int             `json:"remaining"`
	RefusedCount     int             `json:"refusedCount"`
	Lists            ImportLists     `json:"lists"`
	Dropped          map[string]int  `json:"dropped"`
	ApproximateDates int             `json:"approximateDates"`
	ListsAdded       int             `json:"listsAdded"`
	ListsSkipped     int             `json:"listsSkipped"`
	TodosAdded       int             `json:"todosAdded"`
	TodosUpdated     int             `json:"todosUpdated"`
	TodosSkipped     int             `json:"todosSkipped"`
	Refused          []ImportRefusal `json:"refused"`
}

// ReadExportJSON checks the envelope without decoding individual todos, so
// the server can report bad items while importing the rest of the file.
func ReadExportJSON(r io.Reader) (json.RawMessage, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxImportBytes {
		return nil, fmt.Errorf("export is larger than %d MiB", MaxImportBytes>>20)
	}
	return validateExportJSON(raw, true)
}

func validateExportJSON(raw json.RawMessage, importing bool) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("cannot read export JSON: %w", err)
	}
	if fields == nil {
		return nil, fmt.Errorf("unsupported export shape: expected an object")
	}
	if v, ok := fields["format"]; ok {
		var format string
		if json.Unmarshal(v, &format) != nil || format != ExportFormat {
			return nil, fmt.Errorf("unsupported export format: expected %s", ExportFormat)
		}
	}
	if v, ok := fields["version"]; ok {
		var version int
		if json.Unmarshal(v, &version) != nil || version != ExportVersion {
			return nil, fmt.Errorf("unsupported export version: this client reads version %d", ExportVersion)
		}
	}
	for _, name := range []string{"lists", "todos"} {
		var items []json.RawMessage
		if fields[name] == nil || string(fields[name]) == "null" || json.Unmarshal(fields[name], &items) != nil {
			return nil, fmt.Errorf("unsupported export shape: %s must be an array", name)
		}
		if importing && name == "todos" && len(items) > MaxImportTodos {
			return nil, fmt.Errorf("export has more than %d todos", MaxImportTodos)
		}
	}
	return raw, nil
}

func ReadExport(r io.Reader) (Export, error) {
	raw, err := ReadExportJSON(r)
	if err != nil {
		return Export{}, err
	}
	return decodeExport(raw)
}

func decodeExport(raw json.RawMessage) (Export, error) {
	// Account records have no role in restoring todos.
	var envelope struct {
		ExportedAt json.RawMessage   `json:"exportedAt"`
		Lists      []json.RawMessage `json:"lists"`
		Todos      []json.RawMessage `json:"todos"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Export{}, fmt.Errorf("unsupported export shape: %w", err)
	}
	ex := Export{Format: ExportFormat, Version: ExportVersion, Lists: []ListSnapshot{}, Todos: []SyncTodo{}}
	for _, rawList := range envelope.Lists {
		var in struct {
			ID         string          `json:"id"`
			Name       string          `json:"name"`
			IsPersonal bool            `json:"isPersonal"`
			Shared     bool            `json:"shared"`
			CreatedAt  json.RawMessage `json:"createdAt"`
		}
		if err := json.Unmarshal(rawList, &in); err != nil {
			return Export{}, fmt.Errorf("invalid list: %w", err)
		}
		list := ListSnapshot{List: List{ID: in.ID, Name: in.Name, IsPersonal: in.IsPersonal}, Shared: in.Shared}
		if len(in.CreatedAt) > 0 {
			if err := json.Unmarshal(in.CreatedAt, &list.CreatedAt); err != nil {
				return Export{}, fmt.Errorf("invalid list date: %w", err)
			}
		}
		ex.Lists = append(ex.Lists, list)
	}
	for _, rawTodo := range envelope.Todos {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawTodo, &fields); err != nil || fields == nil {
			return Export{}, fmt.Errorf("invalid todo: expected an object")
		}
		assignee, hasAssignee := fields["assigneeId"]
		delete(fields, "assigneeId")
		sanitized, err := json.Marshal(fields)
		if err != nil {
			return Export{}, err
		}
		var todo SyncTodo
		if err := json.Unmarshal(sanitized, &todo); err != nil {
			return Export{}, fmt.Errorf("invalid todo: %w", err)
		}
		if hasAssignee && string(assignee) != "null" {
			// The marker lets local import count dropped assignments without retaining
			// another person's account id or interpreting its type.
			marker := ""
			todo.AssigneeID = &marker
		}
		ex.Todos = append(ex.Todos, todo)
	}

	if len(envelope.ExportedAt) > 0 {
		if err := json.Unmarshal(envelope.ExportedAt, &ex.ExportedAt); err != nil {
			return Export{}, fmt.Errorf("invalid export date: %w", err)
		}
	}
	return ex, nil
}

func importPath(path string, opts []ImportOptions, server bool) string {
	q := url.Values{}
	if server {
		q.Set("format", "evado")
	}
	if len(opts) > 0 {
		if opts[0].DryRun {
			q.Set("dryRun", "true")
		}
		if opts[0].ListID != "" {
			q.Set("listId", opts[0].ListID)
		}
	}
	if len(q) > 0 {
		return path + "?" + q.Encode()
	}
	return path
}

func (c *Client) ImportLocal(ctx context.Context, ex Export, opts ...ImportOptions) (ImportResult, error) {
	if c.BaseURL() != LocalBaseURL {
		return ImportResult{}, &Error{StatusCode: http.StatusBadRequest, Code: "needs_local", Message: "local import requires -local"}
	}
	var out ImportResult
	_, err := c.do(ctx, http.MethodPost, importPath("/local/import", opts, false), ex, &out)
	return out, err
}

func (c *Client) Import(ctx context.Context, raw json.RawMessage, opts ...ImportOptions) (ImportResult, error) {
	var out ImportResult
	_, err := c.do(ctx, http.MethodPost, importPath("/import", opts, true), raw, &out)
	return out, err
}
