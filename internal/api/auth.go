package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// Error codes the refresh endpoint answers with. Either one means the
// refresh token will never work again.
const (
	CodeInvalidGrant       = "invalid_grant"
	CodeRefreshTokenReused = "refresh_token_reused"
	CodeSignupRequired     = "signup_required"
)

// TokenPair is what signing in with an emailed code, and every refresh
// after it, hands out: a short-lived access token and the single-use
// refresh token that replaces it.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	// ExpiresIn is the access token's lifetime in seconds.
	ExpiresIn int    `json:"expires_in"`
	Scope     string `json:"scope"`
	User      User   `json:"user"`
}

// Verified is the answer to a correct emailed code.
type Verified struct {
	TokenPair
	Lists []List `json:"lists"`
}

// SignupRequired names the website where this server creates accounts.
type SignupRequired struct {
	Err       *Error
	SignupURL string
}

func (e *SignupRequired) Error() string { return e.Err.Error() }
func (e *SignupRequired) Unwrap() error { return e.Err }

func signupRequired(err error) error {
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeSignupRequired {
		return err
	}
	var body struct {
		SignupURL string `json:"signupUrl"`
	}
	_ = json.Unmarshal(e.body, &body)
	return &SignupRequired{Err: e, SignupURL: body.SignupURL}
}

// The sign-in endpoints take no bearer token, so they are called on a
// client made without one.

// RequestCode asks the server to email a six-digit sign-in code. The
// server allows a handful per email each hour and answers 429 with a
// Retry-After beyond that.
func (c *Client) RequestCode(ctx context.Context, email string) error {
	body := struct {
		Email string `json:"email"`
	}{Email: email}
	_, err := c.do(ctx, http.MethodPost, "/auth/login", body, nil)
	return signupRequired(err)
}

// VerifyCode trades an emailed code for the account and a token pair. The
// account must already exist. A wrong or expired code is a 400.
func (c *Client) VerifyCode(ctx context.Context, email, code string) (Verified, error) {
	body := struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}{Email: email, Code: code}
	var out Verified
	_, err := c.do(ctx, http.MethodPost, "/auth/verify", body, &out)
	return out, signupRequired(err)
}

// RefreshTokens spends a refresh token on a fresh pair. The old refresh
// token is consumed: sending it again revokes the whole family.
func (c *Client) RefreshTokens(ctx context.Context, refreshToken string) (TokenPair, error) {
	body := struct {
		RefreshToken string `json:"refresh_token"`
	}{RefreshToken: refreshToken}
	var out TokenPair
	_, err := c.do(ctx, http.MethodPost, "/auth/refresh", body, &out)
	return out, err
}

// EndSession revokes every token descended from the same sign-in as
// refreshToken.
func (c *Client) EndSession(ctx context.Context, refreshToken string) error {
	body := struct {
		RefreshToken string `json:"refresh_token"`
	}{RefreshToken: refreshToken}
	_, err := c.do(ctx, http.MethodPost, "/auth/token/logout", body, nil)
	return err
}

// ExchangeCode finishes a browser authorization bound to this redirect and
// PKCE verifier. It never needs a client secret for the registered native app.
func (c *Client) ExchangeCode(ctx context.Context, client, redirect, code, verifier string) (Verified, error) {
	body := struct {
		Grant    string `json:"grant_type"`
		Client   string `json:"client_id"`
		Redirect string `json:"redirect_uri"`
		Code     string `json:"code"`
		Verifier string `json:"code_verifier"`
	}{"authorization_code", client, redirect, code, verifier}
	var out Verified
	_, err := c.do(ctx, http.MethodPost, "/oauth/token", body, &out)
	return out, err
}
