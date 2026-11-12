package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nerdlabz/evado-tui/internal/api"
)

const BrowserClient = "Evado TUI"

// BrowserLogin owns one short-lived loopback callback. Close also cancels
// an exchange in progress; no token is saved by this flow.
type BrowserLogin struct {
	URL    string
	ctx    context.Context
	cancel context.CancelFunc
	result chan browserResult
}

// BrowserError is a browser sign-in that ended without a session for a
// reason of its own, not a failed request: its text is for the person, as
// it is. Without the type it reads as any other error with no answer from
// the server, which is how a lost connection looks.
type BrowserError string

func (e BrowserError) Error() string { return string(e) }

type browserResult struct {
	verified api.Verified
	err      error
}

func randomBrowserKey() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// BeginBrowser binds only IPv4 loopback, on a free port as allowed by the
// registered http://127.0.0.1/callback redirect.
func BeginBrowser(parent context.Context, base string) (*BrowserLogin, error) {
	u, err := url.Parse(Server(base))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, BrowserError("The server needs an http or https address.")
	}
	state, err := randomBrowserKey()
	if err != nil {
		return nil, err
	}
	verifier, err := randomBrowserKey()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, BrowserError("Could not listen for browser sign-in: " + err.Error())
	}
	redirect := "http://" + listener.Addr().String() + "/callback"
	u.Path = strings.TrimSuffix(u.Path, "/api/v1") + "/authorize"
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	digest := sha256.Sum256([]byte(verifier))
	u.RawQuery = url.Values{
		"client": {BrowserClient}, "redirect_uri": {redirect}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"},
	}.Encode()
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	f := &BrowserLogin{URL: u.String(), ctx: ctx, cancel: cancel, result: make(chan browserResult, 1)}
	var claimed atomic.Bool
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 40 * time.Second, MaxHeaderBytes: 8192}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Host != listener.Addr().String() || r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !claimed.CompareAndSwap(false, true) {
			http.Error(w, "This sign-in has already finished.", http.StatusConflict)
			return
		}
		q := r.URL.Query()
		var res browserResult
		switch {
		case len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1:
			res.err = BrowserError("Browser sign-in was refused because its state did not match. Try again.")
		case q.Get("error") != "":
			res.err = BrowserError("Browser sign-in was denied. Try again, or use an email code.")
		case len(q["code"]) != 1 || q.Get("code") == "":
			res.err = BrowserError("The browser returned no sign-in code. Try again.")
		default:
			exchangeCtx, stop := context.WithTimeout(ctx, 30*time.Second)
			defer stop()
			res.verified, res.err = api.New("", api.WithBaseURL(base)).ExchangeCode(exchangeCtx, BrowserClient, redirect, q.Get("code"), verifier)
			if res.err == nil && (res.verified.AccessToken == "" || res.verified.RefreshToken == "" || res.verified.User.ID == "") {
				res.err = BrowserError("The server accepted browser sign-in but sent no session back.")
			}
		}
		if res.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintln(w, "Sign-in did not finish. Return to Evado for details. You can close this tab.")
		} else {
			_, _ = fmt.Fprintln(w, "Approved. Return to Evado to finish signing in. You can close this tab.")
		}
		_ = http.NewResponseController(w).Flush()
		f.result <- res
	})
	go func() { _ = server.Serve(listener) }()
	go func() { <-ctx.Done(); _ = server.Close() }()
	return f, nil
}

func (f *BrowserLogin) Close() { f.cancel() }

func (f *BrowserLogin) Wait() (api.Verified, error) {
	defer f.Close()
	select {
	case res := <-f.result:
		if res.err != nil {
			return api.Verified{}, res.err
		}
		return res.verified, res.err
	case <-f.ctx.Done():
		if errors.Is(f.ctx.Err(), context.DeadlineExceeded) {
			return api.Verified{}, BrowserError("Browser sign-in timed out. If you closed the tab, try again or use an email code.")
		}
		return api.Verified{}, BrowserError("Browser sign-in cancelled.")
	}
}
