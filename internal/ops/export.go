package ops

import (
	"context"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func (o *Ops) Export(ctx context.Context) (api.Export, error) {
	return o.C.ExportTodos(ctx)
}
