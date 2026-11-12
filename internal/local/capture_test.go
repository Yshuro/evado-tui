package local

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

// parsingService is the test service with a /parse that reads every title
// as "Read by the service", on 3 March 2031 at 14:15 UTC, and counts how
// often it was asked. delay holds each answer back.
type parsingService struct {
	*service
	asked atomic.Int32
	delay time.Duration
	// assumed is the assumedTime the service reports, if any.
	assumed       map[string]any
	kept          string
	smartRequests []*bool
}

var serviceDay = time.Date(2031, 3, 3, 14, 15, 0, 0, time.UTC)

func newParsingService(t *testing.T) (*parsingService, string) {
	svc, _ := newService(t)
	p := &parsingService{service: svc}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/parse" || svc.down.Load() {
			svc.serve(w, r)
			return
		}
		p.asked.Add(1)
		var body struct {
			Title string
			Smart *bool
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if p.kept != "" {
			p.smartRequests = append(p.smartRequests, body.Smart)
			if body.Smart == nil || *body.Smart {
				_ = json.NewEncoder(w).Encode(map[string]any{"parsed": map[string]any{"title": body.Title, "keptAsName": p.kept}})
				return
			}
		}
		select {
		case <-time.After(p.delay):
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"parsed": map[string]any{
			"title": "Read by the service: " + body.Title, "doAt": serviceDay, "hasTime": true,
			"recurrenceRule": nil, "matchedText": "whatever", "assumedTime": p.assumed,
		}})
	}))
	t.Cleanup(srv.Close)
	return p, srv.URL
}

// Signed in, a captured todo takes the service's reading of its dates, and
// the preview shows that same reading.
func TestCaptureSignedInUsesTheServicesReading(t *testing.T) {
	ctx := context.Background()
	svc, url := newParsingService(t)
	c := signedIn(t, svc.service, url).Client()

	made, err := c.CreateTodos(ctx, api.NewTodo{Title: "Call mum tomorrow\nPay rent friday"})
	if err != nil || len(made) != 2 {
		t.Fatalf("create = %v %v", made, err)
	}
	for i, line := range []string{"Call mum tomorrow", "Pay rent friday"} {
		got := made[i]
		if got.Title != "Read by the service: "+line || got.DoAt == nil || !got.DoAt.Equal(serviceDay) || !got.HasTime {
			t.Errorf("todo %d = %q at %v time=%v, want the service's reading", i, got.Title, got.DoAt, got.HasTime)
		}
	}
	if n := svc.asked.Load(); n != 2 {
		t.Errorf("the service was asked %d times, want once a line", n)
	}

	p, err := c.Parse(ctx, "Call mum tomorrow")
	if err != nil || p.DoAt == nil || !p.DoAt.Equal(serviceDay) {
		t.Errorf("preview = %+v %v, want the service's reading", p, err)
	}
}

// A service that does not answer in time costs the timeout and no more,
// and the reader here takes over.
func TestCaptureFallsBackWhenTheServiceIsSlow(t *testing.T) {
	defer func(d time.Duration) { serviceReadTimeout = d }(serviceReadTimeout)
	serviceReadTimeout = 50 * time.Millisecond
	svc, url := newParsingService(t)
	svc.delay = 5 * time.Second
	c := signedIn(t, svc.service, url).Client()

	start := time.Now()
	got := add(t, c, "Call mum tomorrow at 5pm")
	if took := time.Since(start); took > time.Second {
		t.Errorf("capture took %v, want it bounded by the timeout", took)
	}
	if got.Title != "Call mum" || got.DoAt == nil || got.DoAt.Equal(serviceDay) || !got.HasTime {
		t.Errorf("created %q at %v, want the local reading", got.Title, got.DoAt)
	}
	if svc.asked.Load() == 0 {
		t.Error("the service was not asked")
	}
}

// A service that refuses to read falls back too.
func TestCaptureFallsBackWhenTheServiceRefuses(t *testing.T) {
	svc, url := newService(t) // answers /parse with not found
	c := signedIn(t, svc, url).Client()
	got := add(t, c, "Call mum the day after tomorrow")
	want := time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")
	if got.Title != "Call mum" || got.ScheduledDay == nil || *got.ScheduledDay != want {
		t.Errorf("created %q on %v, want %q on %s", got.Title, got.ScheduledDay, "Call mum", want)
	}
}

// Once a sync has found no network, capture does not wait on the service.
func TestCaptureOfflineDoesNotAskTheService(t *testing.T) {
	svc, url := newParsingService(t)
	b := signedIn(t, svc.service, url)
	svc.down.Store(true)
	if _, err := b.Sync(context.Background()); !Offline(err) {
		t.Fatalf("sync with the network gone: %v, want offline", err)
	}
	got := add(t, b.Client(), "Call mum tomorrow")
	if got.Title != "Call mum" {
		t.Errorf("created %q, want the local reading", got.Title)
	}
	if n := svc.asked.Load(); n != 0 {
		t.Errorf("the service was asked %d times while offline", n)
	}
}

// With no account the service is never asked.
func TestCaptureWithNoAccountReadsHere(t *testing.T) {
	svc, url := newParsingService(t)
	b, err := Open(t.TempDir()+"/todos.json", "", url)
	if err != nil {
		t.Fatal(err)
	}
	if got := add(t, b.Client(), "Call mum tomorrow"); got.Title != "Call mum" {
		t.Errorf("created %q, want the local reading", got.Title)
	}
	if svc.asked.Load() != 0 {
		t.Error("the service was asked with no account")
	}
}

func lines(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "Task %d\n", i+1)
	}
	return b.String()
}

// More lines than a request may hold are refused whole, as the service does,
// rather than the first twenty being kept and the rest dropped quietly.
func TestCaptureOfTooManyLinesIsRefusedAndMakesNothing(t *testing.T) {
	ctx := context.Background()
	b := open(t, "", "")
	c := b.Client()

	made, err := c.CreateTodos(ctx, api.NewTodo{Title: lines(20)})
	if err != nil || len(made) != 20 {
		t.Fatalf("20 lines made %d todos, err %v; want all 20", len(made), err)
	}

	_, err = c.CreateTodos(ctx, api.NewTodo{Title: lines(21)})
	if api.CodeOf(err) != api.CodeTooManyLines {
		t.Fatalf("21 lines: err = %v, want %s", err, api.CodeTooManyLines)
	}
	if all, _ := c.ListTodos(ctx, api.TodoFilter{}); len(all) != 20 {
		t.Errorf("%d todos after the refusal, want the 20 from before", len(all))
	}

	// The answer says how many there were and how many are allowed.
	req, _ := http.NewRequest(http.MethodPost, address+"/todos", strings.NewReader(`{"title":`+strconv.Quote(lines(25))+`}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := b.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Code         string
		Limit, Lines int
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest || body.Code != api.CodeTooManyLines || body.Limit != 20 || body.Lines != 25 {
		t.Errorf("answer = %d %+v, want 400 too_many_lines with limit 20 and lines 25", res.StatusCode, body)
	}

	// A todo with its own id is one todo whatever it holds.
	id := NewID()
	if made, err := c.CreateTodos(ctx, api.NewTodo{ID: id, Title: lines(30)}); err != nil || len(made) != 1 {
		t.Errorf("a todo with an id made %d, err %v; want 1", len(made), err)
	}
}
