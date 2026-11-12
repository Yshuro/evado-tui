package local

import (
	"context"
	"testing"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestSmartNameAndPlainOverrideReachCapture(t *testing.T) {
	svc, url := newParsingService(t)
	svc.kept = "Tomorrow"
	b := signedIn(t, svc.service, url)
	c := b.Client()
	ctx := context.Background()
	title := "Visit Tomorrow Land at Disney World"
	p, err := c.Parse(ctx, title)
	if err != nil || p.KeptAsName != "Tomorrow" || p.DoAt != nil {
		t.Fatalf("preview = %+v %v", p, err)
	}
	made, err := c.CreateTodos(ctx, api.NewTodo{Title: title})
	if err != nil || len(made) != 1 || made[0].Title != title || made[0].DoAt != nil {
		t.Fatalf("capture = %+v %v", made, err)
	}
	plain := false
	p, err = c.ParseWithSmart(ctx, title, &plain)
	if err != nil || p.KeptAsName != "" || p.DoAt == nil {
		t.Fatalf("plain preview = %+v %v", p, err)
	}
	made, err = c.CreateTodos(ctx, api.NewTodo{Title: title, Smart: &plain})
	if err != nil || len(made) != 1 || made[0].DoAt == nil {
		t.Fatalf("plain capture = %+v %v", made, err)
	}
	if len(svc.smartRequests) != 4 {
		t.Fatalf("parse requests = %d, want four", len(svc.smartRequests))
	}
	for i, smart := range svc.smartRequests {
		if i < 2 && smart != nil {
			t.Fatal("normal reading overrode the account")
		}
		if i >= 2 && (smart == nil || *smart) {
			t.Fatal("plain override was lost")
		}
	}
	before := svc.asked.Load()
	made, err = c.CreateTodos(ctx, api.NewTodo{Title: title, Parse: &plain})
	if err != nil || len(made) != 1 || made[0].Title != title || made[0].DoAt != nil {
		t.Fatalf("literal capture = %+v %v", made, err)
	}
	if svc.asked.Load() != before {
		t.Fatal("literal capture asked the service")
	}
	svc.down.Store(true)
	_, _ = b.Sync(ctx)
	before = svc.asked.Load()
	if _, err := c.Parse(ctx, "Call mum tomorrow"); err != nil {
		t.Fatal(err)
	}
	if svc.asked.Load() != before {
		t.Fatal("offline preview asked the service")
	}
}

func TestSmartParsingSettingIsKeptWithTheAccount(t *testing.T) {
	svc, url := newService(t)
	svc.user.SmartParsingAvailable = true
	b := signedIn(t, svc, url)
	c := b.Client()
	ctx := context.Background()
	u, _, err := c.Me(ctx)
	if err != nil || !u.SmartParsingAvailable || u.SmartParsing {
		t.Fatalf("me = %+v %v", u, err)
	}
	for _, enabled := range []bool{true, false} {
		if _, err := c.UpdateProfile(ctx, api.ProfilePatch{SmartParsing: &enabled}); err != nil {
			t.Fatal(err)
		}
		u, _, err = c.Me(ctx)
		if err != nil || u.SmartParsing != enabled || !u.SmartParsingAvailable {
			t.Fatalf("saved account = %+v %v", u, err)
		}
	}
	svc.down.Store(true)
	enabled := true
	if _, err := c.UpdateProfile(ctx, api.ProfilePatch{SmartParsing: &enabled}); err == nil {
		t.Fatal("offline account change succeeded")
	}
	u, _, _ = c.Me(ctx)
	if u.SmartParsing {
		t.Fatal("failed account change was stored locally")
	}
}
