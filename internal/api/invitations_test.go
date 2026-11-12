package api

import (
	"context"
	"net/http"
	"testing"
)

const pendingBody = `{"invitations":[
	{"id":"i2","list":{"id":"l2","name":"Household"},
	 "invitedBy":{"userId":"u2","username":"sam","email":"s***@example.com"},
	 "createdAt":"2026-09-29T10:00:00Z"},
	{"id":"i1","list":{"id":"l1","name":"Work"},
	 "invitedBy":{"userId":"u3","username":null,"email":"j***@example.com"},
	 "createdAt":"2026-09-01T10:00:00Z"}]}`

func TestInvitationsDecode(t *testing.T) {
	var gotMethod, gotPath string
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(pendingBody))
	}))
	got, err := c.Invitations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/invitations" {
		t.Errorf("asked %s %s", gotMethod, gotPath)
	}
	if len(got.Invitations) != 2 || got.Cached {
		t.Fatalf("got %+v, want two live invitations", got)
	}
	first, second := got.Invitations[0], got.Invitations[1]
	if first.ID != "i2" || first.List.Name != "Household" || first.InvitedBy.Username == nil || *first.InvitedBy.Username != "sam" {
		t.Errorf("first = %+v", first)
	}
	if second.InvitedBy.Username != nil || second.InvitedBy.Email != "j***@example.com" {
		t.Errorf("a null inviter username must stay null, with the masked email to fall back on: %+v", second.InvitedBy)
	}
	if first.CreatedAt.IsZero() || !first.CreatedAt.After(second.CreatedAt) {
		t.Errorf("dates = %v, %v", first.CreatedAt, second.CreatedAt)
	}
}

func TestEmptyAndCachedInvitations(t *testing.T) {
	for _, tc := range []struct {
		body   string
		empty  bool
		nilled bool
		cached bool
	}{
		{body: `{"invitations":[]}`, empty: true},
		{body: `{"invitations":[],"cached":true}`, empty: true, cached: true},
		{body: `{}`, nilled: true},
	} {
		c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(tc.body))
		}))
		got, err := c.Invitations(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if tc.empty && (got.Invitations == nil || len(got.Invitations) != 0) {
			t.Errorf("%s: invitations = %#v, want an empty slice", tc.body, got.Invitations)
		}
		if tc.nilled && got.Invitations != nil {
			t.Errorf("%s: invitations = %#v, want nil", tc.body, got.Invitations)
		}
		if got.Cached != tc.cached {
			t.Errorf("%s: cached = %v", tc.body, got.Cached)
		}
	}
}

func TestInvitationActions(t *testing.T) {
	var calls []string
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		switch r.URL.Path {
		case "/invitations/i2/accept":
			_, _ = w.Write([]byte(`{"list":{"id":"l2","name":"Household","isPersonal":false,"role":"member","readOnly":false}}`))
		case "/invitations/i2/restore":
			_, _ = w.Write([]byte(`{"invitation":{"id":"i2","list":{"id":"l2","name":"Household"},"invitedBy":{"userId":"u2","username":"sam","email":"s***@example.com"},"createdAt":"2026-09-29T10:00:00Z"}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	ctx := context.Background()

	joined, err := c.AcceptInvitation(ctx, "i2")
	if err != nil || joined.ID != "l2" || joined.Name != "Household" || joined.Role != "member" {
		t.Errorf("accept = %+v, %v", joined, err)
	}
	if err := c.DeclineInvitation(ctx, "i2"); err != nil {
		t.Errorf("decline: %v", err)
	}
	back, err := c.RestoreInvitation(ctx, "i2")
	if err != nil || back.ID != "i2" || back.List.Name != "Household" {
		t.Errorf("restore = %+v, %v", back, err)
	}
	// An id with a slash in it must not reach another path.
	if err := c.DeclineInvitation(ctx, "a/b"); err != nil {
		t.Errorf("decline escaped: %v", err)
	}
	want := []string{
		"POST /invitations/i2/accept", "POST /invitations/i2/decline", "POST /invitations/i2/restore",
		"POST /invitations/a%2Fb/decline",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}

func TestInvitationActionErrors(t *testing.T) {
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Not found","code":"not_found"}`))
	}))
	ctx := context.Background()
	if _, err := c.AcceptInvitation(ctx, "gone"); !IsNotFound(err) {
		t.Errorf("accept err = %v, want not found", err)
	}
	if err := c.DeclineInvitation(ctx, "gone"); !IsNotFound(err) {
		t.Errorf("decline err = %v, want not found", err)
	}
	if _, err := c.RestoreInvitation(ctx, "gone"); !IsNotFound(err) {
		t.Errorf("restore err = %v, want not found", err)
	}
}
