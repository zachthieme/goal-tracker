package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// idTokenCookie keeps the ID token from sign-in for sign-out to hand back to
// the provider; only /signout reads it.
const idTokenCookie = "gt_id_token"

// flowCookie carries one sign-in's state, nonce and PKCE verifier from
// /auth/start to /auth/callback, signed like the session cookie.
const flowCookie = "gt_oidc"

// OIDCConfig is how the app reaches the org's OpenID Connect provider.
// RedirectURL is the app's /auth/callback, as registered with the provider.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// OIDC is the org's OpenID Connect provider, discovered and ready to sign
// people in through.
type OIDC struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	// endSession is the provider's RP-initiated logout endpoint, empty when
	// it has none; signedOutURL is the app's sign-in page, where it sends the
	// browser back to.
	endSession   string
	signedOutURL string
}

// NewOIDC discovers the provider at cfg.Issuer. It fails, naming the issuer,
// when discovery does.
func NewOIDC(ctx context.Context, cfg OIDCConfig) (*OIDC, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery at %s: %w", cfg.Issuer, err)
	}
	var discovered struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&discovered); err != nil {
		return nil, fmt.Errorf("OIDC discovery at %s: %w", cfg.Issuer, err)
	}
	callback, err := url.Parse(cfg.RedirectURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC redirect URL %q: %w", cfg.RedirectURL, err)
	}
	return &OIDC{
		endSession:   discovered.EndSession,
		signedOutURL: callback.ResolveReference(&url.URL{Path: "/signin"}).String(),
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

// WithOIDC signs people in through the org's provider instead of the
// development email form, which is then switched off.
func WithOIDC(o *OIDC) Option {
	return func(s *Server) {
		s.oidc = o
	}
}

// signInFlow is what /auth/callback checks the provider's answer against.
type signInFlow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
}

// handleOIDCStart sends the person to the provider with a fresh state, nonce
// and PKCE verifier, remembered in a signed cookie for the callback.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	flow := signInFlow{State: rand.Text(), Nonce: rand.Text(), Verifier: oauth2.GenerateVerifier()}
	payload, err := json.Marshal(flow)
	if err != nil {
		s.serverError(w, r, "sign-in failed", err)
		return
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookie,
		Value:    encoded + "." + s.sessions.sign(encoded),
		Path:     "/auth/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	url := s.oidc.oauth.AuthCodeURL(flow.State, oidc.Nonce(flow.Nonce), oauth2.S256ChallengeOption(flow.Verifier))
	http.Redirect(w, r, url, http.StatusFound)
}

// handleOIDCCallback finishes a sign-in: it checks the provider's answer
// against the flow cookie, verifies the ID token, and signs the person in by
// their verified email, taking their Name from the token when it carries one.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Path: "/auth/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode})
	claims, err := s.verifyCallback(r)
	if err != nil {
		var failure signInFailure
		if !errors.As(err, &failure) {
			failure = signInFailure{sentence: "Signing in with your organization didn't work. Try again, and ask an Admin if it keeps failing."}
		}
		s.log.Warn("organization sign-in failed", "err", err)
		render(w, r, http.StatusBadRequest, orgSignInPage(failure.sentence))
		return
	}

	acc, err := s.svc.SignIn(r.Context(), claims.Email)
	if errors.Is(err, domain.ErrDeparted) {
		render(w, r, http.StatusForbidden, orgSignInPage(departedSentence(claims.Email)))
		return
	}
	if err != nil {
		s.serverError(w, r, "sign-in failed", err)
		return
	}
	if claims.Name != "" && claims.Name != acc.Name {
		if err := s.svc.SetName(r.Context(), acc.ID, claims.Name); err != nil {
			s.serverError(w, r, "sign-in failed", err)
			return
		}
	}
	s.startSession(w, acc.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     idTokenCookie,
		Value:    claims.raw,
		Path:     "/signout",
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}

// providerSignOutURL is where sign-out sends the browser so the provider ends
// its own session too, handing back the ID token from sign-in as a hint; empty
// when the provider has no logout endpoint, and sign-out stays local.
func (s *Server) providerSignOutURL(r *http.Request) string {
	if s.oidc == nil || s.oidc.endSession == "" {
		return ""
	}
	q := url.Values{"client_id": {s.oidc.oauth.ClientID}}
	// A provider may refuse to redirect back without the hint, so ask for the
	// redirect only with one.
	if c, err := r.Cookie(idTokenCookie); err == nil && c.Value != "" {
		q.Set("id_token_hint", c.Value)
		q.Set("post_logout_redirect_uri", s.oidc.signedOutURL)
	}
	sep := "?"
	if strings.Contains(s.oidc.endSession, "?") {
		sep = "&"
	}
	return s.oidc.endSession + sep + q.Encode()
}

// identityClaims are the ID token claims sign-in reads.
type identityClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	// raw is the ID token itself, kept for sign-out's hint.
	raw string
}

// signInFailure is a sign-in the provider's answer doesn't allow, with the
// sentence the person is shown; its cause goes to the log.
type signInFailure struct {
	sentence string
	cause    error
}

func (f signInFailure) Error() string { return f.cause.Error() }
func (f signInFailure) Unwrap() error { return f.cause }

// verifyCallback is the identity the callback request proves, or why it
// doesn't prove one.
func (s *Server) verifyCallback(r *http.Request) (identityClaims, error) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		return identityClaims{}, signInFailure{
			sentence: "Your organization didn't sign you in. Try again, and ask an Admin if it keeps happening.",
			cause:    fmt.Errorf("provider answered %s: %s", e, q.Get("error_description")),
		}
	}
	flow, ok := s.readFlow(r)
	if !ok || q.Get("state") != flow.State {
		return identityClaims{}, signInFailure{
			sentence: "That sign-in had expired or didn't start here. Try again.",
			cause:    errors.New("state doesn't match the flow cookie"),
		}
	}
	token, err := s.oidc.oauth.Exchange(r.Context(), q.Get("code"), oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return identityClaims{}, fmt.Errorf("exchange code: %w", err)
	}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return identityClaims{}, errors.New("token response has no id_token")
	}
	idToken, err := s.oidc.verifier.Verify(r.Context(), raw)
	if err != nil {
		return identityClaims{}, fmt.Errorf("verify ID token: %w", err)
	}
	if idToken.Nonce != flow.Nonce {
		return identityClaims{}, errors.New("ID token nonce doesn't match the flow cookie")
	}
	var claims identityClaims
	if err := idToken.Claims(&claims); err != nil {
		return identityClaims{}, fmt.Errorf("read ID token claims: %w", err)
	}
	claims.raw = raw
	if claims.Email == "" {
		return identityClaims{}, signInFailure{
			sentence: "Your organization didn't share your email, so you can't be signed in. Ask an Admin to check the sign-in setup.",
			cause:    errors.New("ID token has no email"),
		}
	}
	if !claims.EmailVerified {
		return identityClaims{}, signInFailure{
			sentence: "Your organization hasn't verified your email " + claims.Email + ", so you can't be signed in. Ask an Admin.",
			cause:    fmt.Errorf("email %s isn't verified", claims.Email),
		}
	}
	return claims, nil
}

// readFlow is the sign-in flow the request's flow cookie carries, if its
// signature verifies.
func (s *Server) readFlow(r *http.Request) (signInFlow, bool) {
	c, err := r.Cookie(flowCookie)
	if err != nil {
		return signInFlow{}, false
	}
	encoded, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !s.sessions.verify(encoded, sig) {
		return signInFlow{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return signInFlow{}, false
	}
	var flow signInFlow
	if err := json.Unmarshal(payload, &flow); err != nil {
		return signInFlow{}, false
	}
	return flow, true
}
