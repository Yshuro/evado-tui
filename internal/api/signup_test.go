package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestSignupRequiredFromBothCodeEndpoints(t *testing.T) {
	c, _ := newServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"signup_required","error":"Create an account first","signupUrl":"https://staging.example.com/login?email=new%40example.com"}`))
	}))
	for _, call := range []func() error{
		func() error { return c.RequestCode(context.Background(), "new@example.com") },
		func() error { _, err := c.VerifyCode(context.Background(), "new@example.com", "123456"); return err },
	} {
		err := call()
		var signup *SignupRequired
		if !errors.As(err, &signup) || signup.SignupURL != "https://staging.example.com/login?email=new%40example.com" || CodeOf(err) != CodeSignupRequired {
			t.Fatalf("signup error = %v", err)
		}
	}
}
