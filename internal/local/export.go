package local

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// Export returns a snapshot for callers that do not need disk errors.
// Use ExportChecked when a disk read failure must reach the caller.
func (b *Backend) Export() api.Export {
	ex, _ := b.ExportChecked()
	return ex
}

// ExportChecked refuses to export stale data when the store cannot be read.
func (b *Backend) ExportChecked() (api.Export, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.diskErr != nil {
		return api.Export{}, b.diskErr
	}
	ex := api.Export{
		Format:               api.ExportFormat,
		Version:              api.ExportVersion,
		ExportedAt:           b.now().UTC(),
		User:                 b.st.User,
		Lists:                make([]api.ListSnapshot, 0, len(b.st.Lists)),
		Todos:                make([]api.SyncTodo, 0, len(b.st.Todos)),
		NotificationChannels: []api.Channel{},
		APITokens:            []api.APIToken{},
	}
	for _, l := range b.st.Lists {
		members := make([]api.Member, len(l.Members))
		copy(members, l.Members)
		if len(members) == 0 {
			owner := api.Member{UserID: l.OwnerID, Role: "owner"}
			if owner.UserID == "" {
				owner.UserID = b.st.User.ID
			}
			if owner.UserID == b.st.User.ID {
				owner.Email, owner.Username = b.st.User.Email, b.st.User.Username
			}
			members = append(members, owner)
		}
		ex.Lists = append(ex.Lists, api.ListSnapshot{List: l.List, Members: members})
	}
	for _, t := range b.st.Todos {
		if t.DeletedAt != nil {
			continue
		}
		steps := make([]api.ChecklistItem, len(t.Steps))
		copy(steps, t.Steps)
		ex.Todos = append(ex.Todos, api.SyncTodo{Todo: t.Todo, Checklist: steps})
	}
	return ex, nil
}

// WriteExport saves an export with owner-only permissions on Unix, like
// the todos file, and makes the folder if it is missing.
func WriteExport(path string, ex api.Export) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := api.EncodeTodosExport(&buf, ex); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}
