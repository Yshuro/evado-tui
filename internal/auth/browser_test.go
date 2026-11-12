package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

func TestBrowserLoginExchangesOnlyItsBoundCodeAndVerifier(t *testing.T) {
	var authorize *url.URL
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/v1/oauth/token" || r.Method != "POST" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected exchange: %s %s", r.Method, r.URL)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		q := authorize.Query()
		digest := sha256.Sum256([]byte(body["code_verifier"]))
		if body["grant_type"] != "authorization_code" || body["client_id"] != BrowserClient || body["redirect_uri"] != q.Get("redirect_uri") || body["code"] != "one-use-code" || len(body["code_verifier"]) != 43 || base64.RawURLEncoding.EncodeToString(digest[:]) != q.Get("code_challenge") {
			t.Errorf("wrong exchange binding: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(api.Verified{TokenPair: api.TokenPair{AccessToken: "access", RefreshToken: "refresh", Scope: "todos:read account", User: api.User{ID: "u1"}}})
	}))
	defer s.Close()
	f, err := BeginBrowser(context.Background(), s.URL+"/api/v1")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	authorize, _ = url.Parse(f.URL)
	q := authorize.Query()
	if authorize.Path != "/authorize" || q.Get("client") != BrowserClient || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) != 43 {
		t.Fatal("bad authorization address")
	}
	callback, _ := url.Parse(q.Get("redirect_uri"))
	if callback.Hostname() != "127.0.0.1" || callback.Port() == "" || callback.Path != "/callback" {
		t.Fatal("listener is not IPv4 loopback on a free port")
	}
	callback.RawQuery = url.Values{"state": {q.Get("state")}, "code": {"one-use-code"}}.Encode()
	res, err := http.Get(callback.String())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), "close this tab") {
		t.Fatalf("callback = %d %s", res.StatusCode, body)
	}
	v, err := f.Wait()
	if err != nil || v.User.ID != "u1" || !strings.Contains(v.Scope, "account") || calls.Load() != 1 {
		t.Fatalf("sign-in = %+v, %v", v, err)
	}
}

func TestBrowserFailuresNeverExchangeACode(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer s.Close()
	for _, reason := range []string{"wrong state", "denied", "missing code", "duplicate state", "cancelled", "closed tab timeout"} {
		t.Run(reason, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if reason == "closed tab timeout" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
			}
			defer cancel()
			f, err := BeginBrowser(ctx, s.URL+"/api/v1")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			authorize, _ := url.Parse(f.URL)
			callback, _ := url.Parse(authorize.Query().Get("redirect_uri"))
			q := url.Values{"state": {authorize.Query().Get("state")}, "code": {"secret-code"}}
			switch reason {
			case "wrong state":
				q.Set("state", "other")
			case "denied":
				q.Set("error", "access_denied")
			case "missing code":
				q.Del("code")
			case "duplicate state":
				q.Add("state", q.Get("state"))
			case "cancelled":
				cancel()
			}
			if reason != "cancelled" && reason != "closed tab timeout" {
				callback.RawQuery = q.Encode()
				res, err := http.Get(callback.String())
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
				if res.StatusCode != 400 {
					t.Fatalf("status = %d", res.StatusCode)
				}
			}
			v, err := f.Wait()
			if err == nil || v.AccessToken != "" || v.RefreshToken != "" {
				t.Fatalf("failure returned tokens: %+v, %v", v, err)
			}
			if reason == "closed tab timeout" && !strings.Contains(err.Error(), "timed out") {
				t.Fatalf("timeout message = %v", err)
			}
			deadline := time.Now().Add(time.Second)
			for {
				conn, err := net.DialTimeout("tcp", callback.Host, 20*time.Millisecond)
				if err != nil {
					break
				}
				_ = conn.Close()
				if time.Now().After(deadline) {
					t.Fatal("callback listener left open")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("failed callback exchanged a code")
	}
}
