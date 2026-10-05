package web_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

const (
	oidcClientID     = "goal-tracker"
	oidcClientSecret = "test-secret"
)

// fakeProvider is an in-process OpenID Connect provider: discovery, JWKS, an
// authorize endpoint that signs the person in at once, and a token endpoint
// that hands back an ID token it signs. claims are the person it signs in as;
// tamper, when set, edits each ID token's claims before signing, and authorize,
// when set, edits the callback query the authorize endpoint redirects with.
type fakeProvider struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	claims    map[string]any
	tamper    func(claims map[string]any)
	authorize func(callback url.Values)
	// endSession advertises an end_session_endpoint in discovery, for
	// RP-initiated logout.
	endSession bool

	mu    sync.Mutex
	codes map[string]pendingCode
	// idToken is the last ID token the token endpoint issued.
	idToken string
}

// pendingCode is what the authorize endpoint saw, for the token endpoint to
// check and answer with.
type pendingCode struct {
	nonce, challenge, redirectURI string
}

func newFakeProvider(t *testing.T, claims map[string]any) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{t: t, key: key, claims: claims, codes: map[string]pendingCode{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("GET /authorize", p.authorizeEndpoint)
	mux.HandleFunc("POST /token", p.token)
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeProvider) issuer() string { return p.srv.URL }

func (p *fakeProvider) discovery(w http.ResponseWriter, _ *http.Request) {
	doc := map[string]any{
		"issuer":                                p.issuer(),
		"authorization_endpoint":                p.issuer() + "/authorize",
		"token_endpoint":                        p.issuer() + "/token",
		"jwks_uri":                              p.issuer() + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	if p.endSession {
		doc["end_session_endpoint"] = p.issuer() + "/end-session"
	}
	writeJSON(w, doc)
}

func (p *fakeProvider) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test",
		"n": base64.RawURLEncoding.EncodeToString(p.key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.key.E)).Bytes()),
	}}})
}

// authorizeEndpoint signs the person in without asking and redirects back with
// a code, after checking the request is an authorization code request with
// PKCE, a state and a nonce, for openid email profile.
func (p *fakeProvider) authorizeEndpoint(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for key, want := range map[string]string{"client_id": oidcClientID, "response_type": "code", "code_challenge_method": "S256"} {
		if q.Get(key) != want {
			p.t.Errorf("authorize: %s = %q, want %q", key, q.Get(key), want)
		}
	}
	for _, scope := range []string{"openid", "email", "profile"} {
		if !strings.Contains(" "+q.Get("scope")+" ", " "+scope+" ") {
			p.t.Errorf("authorize: scope %q lacks %s", q.Get("scope"), scope)
		}
	}
	for _, key := range []string{"state", "nonce", "code_challenge", "redirect_uri"} {
		if q.Get(key) == "" {
			p.t.Errorf("authorize: no %s", key)
		}
	}
	code := rand.Text()
	p.mu.Lock()
	p.codes[code] = pendingCode{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri")}
	p.mu.Unlock()

	callback := url.Values{"code": {code}, "state": {q.Get("state")}}
	if p.authorize != nil {
		p.authorize(callback)
	}
	http.Redirect(w, r, q.Get("redirect_uri")+"?"+callback.Encode(), http.StatusFound)
}

// token exchanges a code for an ID token, checking the client's secret and the
// PKCE verifier.
func (p *fakeProvider) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.FormValue("client_id"), r.FormValue("client_secret")
	}
	if id != oidcClientID || secret != oidcClientSecret {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	p.mu.Lock()
	pending, ok := p.codes[r.FormValue("code")]
	delete(p.codes, r.FormValue("code"))
	p.mu.Unlock()
	verifier := sha256.Sum256([]byte(r.FormValue("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(verifier[:]) != pending.challenge || r.FormValue("redirect_uri") != pending.redirectURI {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}

	now := time.Now()
	claims := map[string]any{
		"iss": p.issuer(), "aud": oidcClientID, "sub": "subject-1",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": pending.nonce,
	}
	for k, v := range p.claims {
		claims[k] = v
	}
	if p.tamper != nil {
		p.tamper(claims)
	}
	idToken := p.sign(claims)
	p.mu.Lock()
	p.idToken = idToken
	p.mu.Unlock()
	writeJSON(w, map[string]any{
		"access_token": "access", "token_type": "Bearer", "expires_in": 3600,
		"id_token": idToken,
	})
}

// sign is claims as an RS256 JWT under the provider's key.
func (p *fakeProvider) sign(claims map[string]any) string {
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			p.t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test"}) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, digest[:])
	if err != nil {
		p.t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newOIDCServer is the app with OIDC sign-in through p.
func newOIDCServer(t *testing.T, h *testsupport.Harness, p *fakeProvider) *httptest.Server {
	t.Helper()
	var app http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { app.ServeHTTP(w, r) }))
	t.Cleanup(ts.Close)
	o, err := web.NewOIDC(t.Context(), web.OIDCConfig{
		Issuer:       p.issuer(),
		ClientID:     oidcClientID,
		ClientSecret: oidcClientSecret,
		RedirectURL:  ts.URL + "/auth/callback",
	})
	if err != nil {
		t.Fatalf("NewOIDC: %v", err)
	}
	app = web.NewServer(h.Service, web.WithOIDC(o))
	return ts
}

// signInThroughProvider opens the sign-in page with a fresh browser, presses
// "Sign in with your organization" and follows the redirects through the
// provider and back. It returns the browser and the page it ends on.
func signInThroughProvider(t *testing.T, appURL string) (*http.Client, *http.Response, string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	page := getBody(t, client, appURL+"/signin")
	button := pageElement(t, page, "a", "signin-org")
	if !strings.Contains(button, "Sign in with your organization") {
		t.Fatalf("the sign-in button reads %s", button)
	}
	resp, err := client.Get(appURL + html.UnescapeString(attr(openTag(button), "href")))
	if err != nil {
		t.Fatalf("sign in through the provider: %v", err)
	}
	return client, resp, readBody(t, resp)
}

// A new person signs in through the org's provider: their Account is created,
// named by the provider's name claim, and they land on Home.
func TestANewPersonSignsInThroughTheProvider(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	p := newFakeProvider(t, map[string]any{"email": "noor.haddad@example.com", "email_verified": true, "name": "Noor Haddad"})
	ts := newOIDCServer(t, h, p)

	_, resp, page := signInThroughProvider(t, ts.URL)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/home" {
		t.Fatalf("signing in ended at %s with status %d, want Home:\n%s", resp.Request.URL, resp.StatusCode, page)
	}
	if !strings.Contains(page, "Noor Haddad") {
		t.Errorf("Home doesn't show the provider's Name:\n%s", page)
	}
	if acc := h.SignIn("noor.haddad@example.com"); acc.Name != "Noor Haddad" {
		t.Errorf("Account Name = %q, want Noor Haddad", acc.Name)
	}
}

// A returning person signs in to their existing Account even when the provider
// spells their email in another case, and a changed name claim becomes their
// Name (CONTEXT.md: Account, Name).
func TestAReturningPersonIsMatchedByEmailAndRenamed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	noor := h.SignIn("noor.haddad@example.com")
	if err := h.Service.SetName(t.Context(), noor.ID, "Noor H."); err != nil {
		t.Fatal(err)
	}
	p := newFakeProvider(t, map[string]any{"email": "Noor.Haddad@Example.com", "email_verified": true, "name": "Noor Haddad"})
	ts := newOIDCServer(t, h, p)

	_, resp, page := signInThroughProvider(t, ts.URL)
	if resp.Request.URL.Path != "/home" {
		t.Fatalf("signing in ended at %s, want Home:\n%s", resp.Request.URL, page)
	}
	got, err := h.Service.Account(t.Context(), noor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Noor Haddad" {
		t.Errorf("Name = %q, want the provider's Noor Haddad", got.Name)
	}
	if again := h.SignIn("noor.haddad@example.com"); again.ID != noor.ID {
		t.Errorf("signing in as Noor.Haddad@Example.com made Account %d, want Noor's %d", again.ID, noor.ID)
	}
}

// A provider that sends no name claim leaves the person's Name as it was.
func TestNoNameClaimLeavesTheNameAlone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	noor := h.SignIn("noor.haddad@example.com")
	if err := h.Service.SetName(t.Context(), noor.ID, "Noor Haddad"); err != nil {
		t.Fatal(err)
	}
	p := newFakeProvider(t, map[string]any{"email": "noor.haddad@example.com", "email_verified": true})
	ts := newOIDCServer(t, h, p)

	if _, resp, page := signInThroughProvider(t, ts.URL); resp.Request.URL.Path != "/home" {
		t.Fatalf("signing in ended at %s, want Home:\n%s", resp.Request.URL, page)
	}
	if got, _ := h.Service.Account(t.Context(), noor.ID); got.Name != "Noor Haddad" {
		t.Errorf("Name = %q, want it left as Noor Haddad", got.Name)
	}
}

// Every way the provider's answer can fail to prove who someone is lands on the
// sign-in page with a plain sentence and a Try again button, and signs no one
// in: never a 500 or a bare text page.
func TestTheProvidersAnswerIsRefusedUnlessItProvesAVerifiedEmail(t *testing.T) {
	t.Parallel()

	noor := map[string]any{"email": "noor.haddad@example.com", "email_verified": true, "name": "Noor Haddad"}
	for _, tc := range []struct {
		name      string
		tamper    func(claims map[string]any)
		authorize func(callback url.Values)
		departed  bool
		says      string
	}{
		{name: "wrong state", authorize: func(cb url.Values) { cb.Set("state", "forged") }, says: "Try again"},
		{name: "denied consent", authorize: func(cb url.Values) {
			cb.Del("code")
			cb.Set("error", "access_denied")
		}, says: "didn't sign you in"},
		{name: "wrong nonce", tamper: func(c map[string]any) { c["nonce"] = "forged" }, says: "didn't work"},
		{name: "wrong audience", tamper: func(c map[string]any) { c["aud"] = "another-app" }, says: "didn't work"},
		{name: "expired token", tamper: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, says: "didn't work"},
		{name: "unverified email", tamper: func(c map[string]any) { c["email_verified"] = false }, says: "hasn't verified your email"},
		{name: "no email", tamper: func(c map[string]any) { delete(c, "email") }, says: "didn't share your email"},
		{name: "departed", departed: true, says: "noor.haddad@example.com has been marked departed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t, "boss@example.com")
			if tc.departed {
				boss := h.SignIn("boss@example.com")
				gone := h.SignIn("noor.haddad@example.com")
				if err := h.Service.MarkDeparted(t.Context(), boss.ID, gone.ID); err != nil {
					t.Fatal(err)
				}
			}
			p := newFakeProvider(t, noor)
			p.tamper, p.authorize = tc.tamper, tc.authorize
			ts := newOIDCServer(t, h, p)

			client, resp, page := signInThroughProvider(t, ts.URL)
			if resp.StatusCode >= 500 || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
				t.Fatalf("status %d, %s, want the sign-in page:\n%s", resp.StatusCode, resp.Header.Get("Content-Type"), page)
			}
			if msg := html.UnescapeString(pageElement(t, page, "p", "signin-error")); !strings.Contains(msg, tc.says) {
				t.Errorf("the sign-in page says %s, want it to say %q", msg, tc.says)
			}
			if button := pageElement(t, page, "a", "signin-org"); !strings.Contains(button, "Try again") {
				t.Errorf("the sign-in page's button is %s, want Try again", button)
			}
			home, err := noRedirects(client).Get(ts.URL + "/home")
			if err != nil {
				t.Fatal(err)
			}
			_ = readBody(t, home)
			if home.StatusCode != http.StatusSeeOther {
				t.Errorf("after a refused sign-in, /home answers %d, want a redirect to sign-in", home.StatusCode)
			}
		})
	}
}

// With the org's provider on, the development form is off: posting an email to
// /signin is refused and signs no one in, and the page offers only the
// organization's sign-in.
func TestTheDevelopmentFormIsOffWithOIDC(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newOIDCServer(t, h, newFakeProvider(t, nil))

	page := getBody(t, http.DefaultClient, ts.URL+"/signin")
	if strings.Contains(page, `name="email"`) {
		t.Errorf("the sign-in page still has an email field:\n%s", page)
	}

	resp := postForm(t, noRedirects(http.DefaultClient), ts.URL+"/signin", url.Values{"email": {"sam@example.com"}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /signin with OIDC on: status %d, want 404 or 405", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "gt_session" && c.Value != "" {
			t.Errorf("POST /signin with OIDC on issued a session cookie")
		}
	}
}

// An issuer whose discovery fails stops NewOIDC with the issuer in the error,
// so startup says which provider it couldn't reach.
func TestNewOIDCNamesAnIssuerItCantDiscover(t *testing.T) {
	t.Parallel()

	nothing := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(nothing.Close)

	_, err := web.NewOIDC(t.Context(), web.OIDCConfig{Issuer: nothing.URL, ClientID: oidcClientID, ClientSecret: oidcClientSecret, RedirectURL: "http://localhost:8080/auth/callback"})
	if err == nil {
		t.Fatal("NewOIDC discovered a provider that serves nothing")
	}
	if !strings.Contains(err.Error(), nothing.URL) {
		t.Errorf("error %q doesn't name the issuer %s", err, nothing.URL)
	}
}

// signOut posts the top bar's Sign out as client and returns where the app
// sends the browser.
func signOut(t *testing.T, client *http.Client, appURL string) *url.URL {
	t.Helper()
	resp := postForm(t, noRedirects(client), appURL+"/signout", nil)
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign out: status %d, want 303", resp.StatusCode)
	}
	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("sign out: %v", err)
	}
	return loc
}

// Signing out of the app also signs the person out of the org's provider when
// it offers RP-initiated logout, so the next sign-in asks who they are instead
// of slipping straight back in as the same person. The provider is handed the
// ID token as a hint and sends the browser back to the app's sign-in page.
func TestSigningOutAlsoSignsOutOfTheProvider(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	p := newFakeProvider(t, map[string]any{"email": "cpo@example.com", "email_verified": true, "name": "Marcus Bell"})
	p.endSession = true
	ts := newOIDCServer(t, h, p)
	client, resp, page := signInThroughProvider(t, ts.URL)
	if resp.Request.URL.Path != "/home" {
		t.Fatalf("signing in ended at %s, want Home:\n%s", resp.Request.URL, page)
	}

	loc := signOut(t, client, ts.URL)
	if got, want := loc.Scheme+"://"+loc.Host+loc.Path, p.issuer()+"/end-session"; got != want {
		t.Fatalf("sign out sends the browser to %s, want the provider's %s", got, want)
	}
	q := loc.Query()
	p.mu.Lock()
	issued := p.idToken
	p.mu.Unlock()
	if q.Get("id_token_hint") != issued {
		t.Errorf("id_token_hint = %q, want the ID token the provider issued", q.Get("id_token_hint"))
	}
	if got, want := q.Get("post_logout_redirect_uri"), ts.URL+"/signin"; got != want {
		t.Errorf("post_logout_redirect_uri = %q, want %q", got, want)
	}
	if q.Get("client_id") != oidcClientID {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), oidcClientID)
	}
	home, err := noRedirects(client).Get(ts.URL + "/home")
	if err != nil {
		t.Fatal(err)
	}
	_ = readBody(t, home)
	if home.StatusCode != http.StatusSeeOther {
		t.Errorf("after signing out, /home answers %d, want a redirect to sign-in", home.StatusCode)
	}
}

// A provider without RP-initiated logout leaves sign-out local: the app's
// session ends and the browser goes to the sign-in page, as without OIDC.
func TestSigningOutStaysLocalWithoutProviderLogout(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	p := newFakeProvider(t, map[string]any{"email": "cpo@example.com", "email_verified": true})
	ts := newOIDCServer(t, h, p)
	client, _, _ := signInThroughProvider(t, ts.URL)

	if loc := signOut(t, client, ts.URL); loc.String() != "/signin" && loc.String() != ts.URL+"/signin" {
		t.Errorf("sign out sends the browser to %s, want /signin", loc)
	}
}
