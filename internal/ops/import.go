package ops

import (
	"bytes"
	"context"
	"io"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func (o *Ops) Import(ctx context.Context, r io.Reader, opts ...api.ImportOptions) (api.ImportResult, error) {
	raw, err := api.ReadExportJSON(r)
	if err != nil {
		return api.ImportResult{}, &Error{Code: "invalid_export", Message: err.Error(), Exit: ExitUsage}
	}
	if o.C.BaseURL() != api.LocalBaseURL {
		return o.C.Import(ctx, raw, opts...)
	}
	ex, err := api.ReadExport(bytes.NewReader(raw))
	if err != nil {
		return api.ImportResult{}, &Error{Code: "invalid_export", Message: err.Error(), Exit: ExitUsage}
	}
	return o.C.ImportLocal(ctx, ex, opts...)
}
