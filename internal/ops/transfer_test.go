package ops

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestImportValidatesEnvelopeButLeavesBadItemsForServer(t *testing.T) {
	calls := 0
	raw := `{"format":"evado-export","version":1,"lists":[],"todos":[{"title":7,"unknown":"keep"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if string(body) != raw {
			t.Errorf("body changed: %s", body)
		}
		if r.URL.Query().Get("dryRun") != "true" {
			t.Errorf("missing dry run")
		}
		_, _ = io.WriteString(w, `{"dryRun":true,"format":"evado","complete":false,"remaining":1,"refusedCount":1,"refused":[{"item":1,"reason":"invalid title"}]}`)
	}))
	defer srv.Close()
	o := New(api.New("evt_test", api.WithBaseURL(srv.URL)))
	res, err := o.Import(context.Background(), strings.NewReader(raw), api.ImportOptions{DryRun: true})
	if err != nil || res.Complete || res.Remaining != 1 || res.RefusedCount != 1 || calls != 1 {
		t.Fatalf("result=%+v calls=%d err=%v", res, calls, err)
	}
	_, err = o.Import(context.Background(), strings.NewReader(`{"version":2,"lists":[],"todos":[]}`))
	if ExitCode(err) != ExitUsage || calls != 1 {
		t.Errorf("invalid file made request: calls=%d err=%v", calls, err)
	}
}

func TestExportUsesPortableOperation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/export/todos" {
			t.Errorf("path=%s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"format":"evado-export","version":1,"lists":[],"todos":[]}`)
	}))
	defer srv.Close()
	o := New(api.New("evt_test", api.WithBaseURL(srv.URL)))
	if _, err := o.Export(context.Background()); err != nil {
		t.Fatal(err)
	}
}
