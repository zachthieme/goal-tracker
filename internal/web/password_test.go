package web_test

import (
	"fmt"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// samsPassword is a password long enough to be accepted.
const samsPassword = "correct horse battery"

// passwordSignInCookie signs emailAddr in with password and returns the
// session cookie, failing the test unless the sign-in sends them to /home.
func passwordSignInCookie(t *testing.T, base, emailAddr, password string) *http.Cookie {
	t.Helper()
	resp := postForm(t, noRedirects(http.DefaultClient), base+"/signin", url.Values{"email": {emailAddr}, "password": {password}})
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/home" {
		t.Fatalf("password sign-in as %s: status %d to %q, want 303 to /home:\n%s", emailAddr, resp.StatusCode, resp.Header.Get("Location"), body)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "gt_session" && c.Value != "" {
			return c
		}
	}
	t.Fatalf("password sign-in as %s issued no session cookie", emailAddr)
	return nil
}

// Opening a set-password link shows the person's email and two password
// fields; filling them in signs the person in, on /home, and signs out every
// browser they were signed in on before.
func TestSettingAPasswordSignsThePersonInAndEndsTheirOtherSessions(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newPasswordServer(t, h)
	givePassword(t, h, "sam@example.com", samsPassword)
	before := passwordSignInCookie(t, ts.URL, "sam@example.com", samsPassword)
	token := passwordLinkToken(t, h, "sam@example.com")

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	page := getBody(t, client, ts.URL+"/set-password?token="+url.QueryEscape(token))
	form := pageElement(t, page, "form", "set-password-card")
	for _, want := range []string{"sam@example.com", `type="password" name="password"`, `type="password" name="confirm"`, `method="post"`} {
		if !strings.Contains(form, want) {
			t.Errorf("the set-password form lacks %s:\n%s", want, form)
		}
	}

	resp := postForm(t, client, ts.URL+"/set-password", url.Values{"token": {token}, "password": {"a brand new password"}, "confirm": {"a brand new password"}})
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/home" {
		t.Fatalf("after setting a password: status %d at %s, want 200 at /home:\n%s", resp.StatusCode, resp.Request.URL.Path, body)
	}
	if !signedOut(t, ts.URL, before) {
		t.Error("a session from before the password was set still reaches /home")
	}
	passwordSignInCookie(t, ts.URL, "sam@example.com", "a brand new password")
}

// postPasswordSignIn posts the password sign-in form without following its
// redirect, and returns the response's status and body.
func postPasswordSignIn(t *testing.T, base, emailAddr, password string) (int, string) {
	t.Helper()
	resp := postForm(t, noRedirects(http.DefaultClient), base+"/signin", url.Values{"email": {emailAddr}, "password": {password}})
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, readBody(t, resp)
}

// passwordSignInFailure is what every failed password sign-in says.
const passwordSignInFailure = "That email and password didn't work. After 5 failed tries an account is locked for 15 minutes."

// The password sign-in page has the email and password fields, a Sign in
// button, and where to turn for a forgotten password, and no development
// sign-in.
func TestThePasswordSignInPageAsksForEmailAndPassword(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newPasswordServer(t, h)

	card := pageElement(t, getBody(t, http.DefaultClient, ts.URL+"/signin"), "form", "signin-card")
	for _, want := range []string{`type="email" name="email"`, `type="password" name="password"`, ">Sign in</button>", "Forgot your password? Ask an Admin for a set-password link."} {
		if !strings.Contains(card, want) {
			t.Errorf("the password sign-in card lacks %s:\n%s", want, card)
		}
	}
	if strings.Contains(card, "Development sign-in") {
		t.Errorf("the password sign-in card offers development sign-in:\n%s", card)
	}
}

// Password fields look like the email field beside them: the stylesheet gives
// input[type=password] the rule every text input shares, 40px tall, and on a
// coarse pointer the same 44px floor.
func TestPasswordFieldsAreStyledLikeTheOtherFields(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	password, ok := ruleFor(css, "input[type=password]")
	if !ok {
		t.Fatalf("no rule styles input[type=password]")
	}
	if !strings.Contains(password, "height:40px") {
		t.Errorf("input[type=password] is not 40px tall: %s", password)
	}
	if email, _ := ruleFor(css, "input[type=email]"); password != email {
		t.Errorf("input[type=password] declares %q, input[type=email] %q", password, email)
	}

	coarse := mediaBlock(t, css, "@media (pointer:coarse)")
	if decls, ok := ruleFor(coarse, "input[type=password]"); !ok || !strings.Contains(decls, "min-height:44px") {
		t.Errorf("on a coarse pointer input[type=password] is not raised to 44px: %s", coarse)
	}
}

// Every failed password sign-in shows the form again with status 401 and the
// same words, whether the email is unknown, its Account has no password, the
// password is wrong, or the Account is locked, and creates no Account.
func TestEveryFailedPasswordSignInLooksTheSame(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newPasswordServer(t, h)
	h.SignIn("kim@example.com") // an Account with no password
	givePassword(t, h, "sam@example.com", samsPassword)
	givePassword(t, h, "lee@example.com", samsPassword)
	for range 5 {
		if _, err := h.Service.PasswordSignIn(t.Context(), "lee@example.com", "not the password"); err == nil {
			t.Fatal("a wrong password signed lee in")
		}
	}

	messages := map[string]bool{}
	for name, tc := range map[string]struct{ email, password string }{
		"unknown email":  {"nobody@example.com", samsPassword},
		"no password":    {"kim@example.com", samsPassword},
		"wrong password": {"sam@example.com", "not the password"},
		"locked":         {"lee@example.com", samsPassword},
	} {
		status, body := postPasswordSignIn(t, ts.URL, tc.email, tc.password)
		if status != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, status)
		}
		msg := pageElement(t, body, "p", "signin-error")
		messages[msg] = true
		if !strings.Contains(html.UnescapeString(msg), passwordSignInFailure) {
			t.Errorf("%s: the form says %s", name, msg)
		}
		if strings.Contains(body, tc.password) {
			t.Errorf("%s: the page repeats the password", name)
		}
		pageElement(t, body, "form", "signin-card")
	}
	if len(messages) != 1 {
		t.Errorf("the failures say different things: %v", messages)
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "nobody@example.com"); err == nil {
		t.Error("signing in with an unknown email created an Account")
	}
}

// A Departed person with the right password is told they've departed, with
// status 403, and gets no session (CONTEXT.md: Departed).
func TestADepartedPersonsRightPasswordIsRefusedWith403(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newPasswordServer(t, h)
	boss := h.SignIn("boss@example.com")
	givePassword(t, h, "sam@example.com", samsPassword)
	sam, err := h.Service.AccountByEmail(t.Context(), "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatal(err)
	}

	resp := postForm(t, noRedirects(http.DefaultClient), ts.URL+"/signin", url.Values{"email": {"sam@example.com"}, "password": {samsPassword}})
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d, want 403", resp.StatusCode)
	}
	if msg := pageElement(t, body, "p", "signin-error"); !strings.Contains(msg, "sam@example.com has been marked departed") {
		t.Errorf("the form says %s", msg)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "gt_session" && c.Value != "" {
			t.Errorf("a Departed person's sign-in issued a session cookie: %v", c)
		}
	}
}

// After 50 failed sign-ins from one IP within 15 minutes, a sign-in from it
// fails even with the right password, alike to any other failure, until 15
// minutes have passed.
func TestFiftyFailuresFromOneIPHoldItsSignInsFor15Minutes(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newPasswordServer(t, h)
	givePassword(t, h, "sam@example.com", samsPassword)
	for i := range 50 {
		// Each from an unknown email, so no Account's own lockout is involved.
		if status, _ := postPasswordSignIn(t, ts.URL, fmt.Sprintf("nobody%d@example.com", i), samsPassword); status != http.StatusUnauthorized {
			t.Fatalf("failure %d: status %d, want 401", i+1, status)
		}
	}

	status, body := postPasswordSignIn(t, ts.URL, "sam@example.com", samsPassword)
	if status != http.StatusUnauthorized {
		t.Errorf("the right password after 50 failures: status %d, want 401", status)
	}
	if msg := html.UnescapeString(pageElement(t, body, "p", "signin-error")); !strings.Contains(msg, passwordSignInFailure) {
		t.Errorf("the held sign-in says %s", msg)
	}

	h.Clock.Advance(15 * time.Minute)
	passwordSignInCookie(t, ts.URL, "sam@example.com", samsPassword)
}

// postPasswordSignInFrom posts the password sign-in form to srv as a request
// arriving from remoteAddr, carrying an X-Forwarded-For header for each of xff,
// and returns the response's status.
func postPasswordSignInFrom(t *testing.T, srv http.Handler, remoteAddr, emailAddr, password string, xff ...string) int {
	t.Helper()
	form := url.Values{"email": {emailAddr}, "password": {password}}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = remoteAddr
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w.Code
}

// failFiftyPasswordSignInsFrom fails 50 password sign-ins to srv from
// remoteAddr, each carrying the X-Forwarded-For headers xff, enough to hold the
// client they're counted against.
func failFiftyPasswordSignInsFrom(t *testing.T, srv http.Handler, remoteAddr string, xff ...string) {
	t.Helper()
	for i := range 50 {
		// Each from an unknown email, so no Account's own lockout is involved.
		if status := postPasswordSignInFrom(t, srv, remoteAddr, fmt.Sprintf("nobody%d@example.com", i), samsPassword, xff...); status != http.StatusUnauthorized {
			t.Fatalf("failure %d: status %d, want 401", i+1, status)
		}
	}
}

// Behind a trusted reverse proxy, each client is throttled by its own address
// in X-Forwarded-For: 50 failures from one hold its sign-ins and not those of
// another client behind the same proxy.
func TestClientsBehindATrustedProxyAreThrottledSeparately(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	srv := web.NewServer(h.Service, web.WithPasswordSignIn(), web.WithTrustedProxies([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}))
	givePassword(t, h, "sam@example.com", samsPassword)
	failFiftyPasswordSignInsFrom(t, srv, "10.0.0.2:40000", "203.0.113.7")

	if status := postPasswordSignInFrom(t, srv, "10.0.0.2:40001", "sam@example.com", samsPassword, "203.0.113.7"); status != http.StatusUnauthorized {
		t.Errorf("the failing client's right password: status %d, want 401", status)
	}
	if status := postPasswordSignInFrom(t, srv, "10.0.0.2:40002", "sam@example.com", samsPassword, "198.51.100.9"); status != http.StatusSeeOther {
		t.Errorf("another client behind the proxy: status %d, want 303", status)
	}
}

// A request that doesn't come from a trusted proxy is throttled by the address
// it came from, whatever X-Forwarded-For it carries: a client can't forge one
// to escape its hold.
func TestAForgedForwardedForFromAnUntrustedAddressIsIgnored(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	srv := web.NewServer(h.Service, web.WithPasswordSignIn(), web.WithTrustedProxies([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}))
	givePassword(t, h, "sam@example.com", samsPassword)
	failFiftyPasswordSignInsFrom(t, srv, "192.0.2.4:40000", "203.0.113.7")

	if status := postPasswordSignInFrom(t, srv, "192.0.2.4:40001", "sam@example.com", samsPassword, "198.51.100.9"); status != http.StatusUnauthorized {
		t.Errorf("the held address's right password with a new X-Forwarded-For: status %d, want 401", status)
	}
	if status := postPasswordSignInFrom(t, srv, "10.0.0.2:40000", "sam@example.com", samsPassword, "203.0.113.7"); status != http.StatusSeeOther {
		t.Errorf("the address it forged, through the trusted proxy: status %d, want 303", status)
	}
}

// Without trusted proxies, X-Forwarded-For is ignored from every address: the
// sign-ins are throttled by the address they came from.
func TestWithoutTrustedProxiesForwardedForIsIgnored(t *testing.T) {
	t.Parallel()

	for name, opts := range map[string][]web.Option{
		"unset": {web.WithPasswordSignIn()},
		"empty": {web.WithPasswordSignIn(), web.WithTrustedProxies(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			srv := web.NewServer(h.Service, opts...)
			givePassword(t, h, "sam@example.com", samsPassword)
			failFiftyPasswordSignInsFrom(t, srv, "10.0.0.2:40000", "203.0.113.7")

			if status := postPasswordSignInFrom(t, srv, "10.0.0.2:40001", "sam@example.com", samsPassword, "198.51.100.9"); status != http.StatusUnauthorized {
				t.Errorf("the held address's right password with a new X-Forwarded-For: status %d, want 401", status)
			}
		})
	}
}

// Through trusted proxies, the client is the right-most address in
// X-Forwarded-For that isn't one of them, so addresses a client writes to the
// left of it don't change whom it counts against; with no such address, or an
// entry that isn't an address in the way, it's the proxy that sent it.
func TestTheClientIsTheRightMostUntrustedForwardedForAddress(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		// failXFF are the X-Forwarded-For headers 50 failures carry; heldXFF
		// those of a sign-in that must then be held, as from the same client.
		failXFF, heldXFF []string
	}{
		{"behind two trusted proxies", []string{"203.0.113.7, 10.1.1.1"}, []string{"203.0.113.7,10.9.9.9"}},
		{"with addresses the client wrote", []string{"1.1.1.1, 203.0.113.7"}, []string{"2.2.2.2, 203.0.113.7"}},
		{"split over two headers", []string{"1.1.1.1", "203.0.113.7"}, []string{"203.0.113.7"}},
		{"with only trusted proxies", []string{"10.1.1.1"}, []string{"10.9.9.9"}},
		{"with no header", nil, nil},
		{"with an entry that isn't an address", []string{"203.0.113.7, unknown"}, []string{"198.51.100.9, unknown"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			srv := web.NewServer(h.Service, web.WithPasswordSignIn(), web.WithTrustedProxies([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}))
			givePassword(t, h, "sam@example.com", samsPassword)
			failFiftyPasswordSignInsFrom(t, srv, "10.0.0.2:40000", c.failXFF...)

			if status := postPasswordSignInFrom(t, srv, "10.0.0.2:40001", "sam@example.com", samsPassword, c.heldXFF...); status != http.StatusUnauthorized {
				t.Errorf("X-Forwarded-For %q after 50 failures with %q: status %d, want 401", c.heldXFF, c.failXFF, status)
			}
		})
	}
}

// Password sign-in, the set-password form and a used link's notice are each a
// single centred column on the canvas, the app's mark above it, and none of
// them a card (DESIGN.md § Sign-in and Set password).
func TestSignedOutPasswordPagesAreAColumnUnderTheMark(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newPasswordServer(t, h)
	token := passwordLinkToken(t, h, "sam@example.com")
	card := regexp.MustCompile(`class="(?:[^"]* )?card(?: [^"]*)?"`)

	for _, tc := range []struct{ name, path, tag, testID string }{
		{"password sign-in", "/signin", "form", "signin-card"},
		{"set-password form", "/set-password?token=" + url.QueryEscape(token), "form", "set-password-card"},
		{"set-password notice", "/set-password?token=not-a-token", "div", "set-password-card"},
	} {
		resp, err := ctxGet(t, http.DefaultClient, ts.URL+tc.path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		page := readBody(t, resp)
		pageElement(t, page, tc.tag, tc.testID)
		if above := between(t, page, "<main", `data-testid="`+tc.testID+`"`); !strings.Contains(above, `class="brand-mark si-mark"`) {
			t.Errorf("the %s page has no app's mark above its column:\n%s", tc.name, above)
		}
		if c := card.FindString(page); c != "" {
			t.Errorf("the %s page has a card, %s:\n%s", tc.name, c, page)
		}
	}
}

// A set-password link that's unknown, expired, or used opens a page saying so,
// with status 410, and posting to it sets nothing.
func TestABadSetPasswordLinkIsGone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newPasswordServer(t, h)
	used := passwordLinkToken(t, h, "sam@example.com")
	if _, err := h.Service.SetPassword(t.Context(), used, samsPassword); err != nil {
		t.Fatal(err)
	}
	expired := passwordLinkToken(t, h, "kim@example.com")
	h.Clock.Advance(72 * time.Hour)

	for name, token := range map[string]string{"unknown": "not-a-token", "missing": "", "used": used, "expired": expired} {
		resp, err := ctxGet(t, http.DefaultClient, ts.URL+"/set-password?token="+url.QueryEscape(token))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusGone {
			t.Errorf("GET with a %s link: status %d, want 410", name, resp.StatusCode)
		}
		if msg := html.UnescapeString(pageElement(t, body, "p", "set-password-error")); !strings.Contains(msg, "This link has expired or was already used. Ask an Admin for a new one.") {
			t.Errorf("GET with a %s link says %s", name, msg)
		}
		if strings.Contains(body, `type="password"`) {
			t.Errorf("GET with a %s link shows the password fields", name)
		}

		resp = postForm(t, noRedirects(http.DefaultClient), ts.URL+"/set-password", url.Values{"token": {token}, "password": {"a brand new password"}, "confirm": {"a brand new password"}})
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusGone {
			t.Errorf("POST with a %s link: status %d, want 410", name, resp.StatusCode)
		}
	}
}

// The set-password page sends no Referer at all, rather than the app-wide
// same-origin, so its address, token and all, reaches nothing it loads, the
// app's own files included (#415).
func TestTheSetPasswordPageSendsNoReferer(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newPasswordServer(t, h)
	resp, err := ctxGet(t, http.DefaultClient, ts.URL+"/set-password?token="+url.QueryEscape(passwordLinkToken(t, h, "sam@example.com")))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /set-password: status %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Values("Referrer-Policy"); len(got) != 1 || got[0] != "no-referrer" {
		t.Errorf("the set-password page's Referrer-Policy is %q, want just no-referrer", got)
	}
}

// Fields that don't match, or a password too short or too long, show the
// form again with the reason, and the link still works.
func TestARefusedPasswordShowsTheFormAgainWithTheReason(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newPasswordServer(t, h)
	token := passwordLinkToken(t, h, "sam@example.com")

	for name, tc := range map[string]struct{ password, confirm, reason string }{
		"mismatched": {"a brand new password", "a brand new passw0rd", "The two passwords don't match."},
		"too short":  {"short", "short", "A password needs at least 12 characters."},
		"too long":   {strings.Repeat("a", 1025), strings.Repeat("a", 1025), "A password can be at most 1024 bytes."},
	} {
		resp := postForm(t, noRedirects(http.DefaultClient), ts.URL+"/set-password", url.Values{"token": {token}, "password": {tc.password}, "confirm": {tc.confirm}})
		defer func() { _ = resp.Body.Close() }()
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", name, resp.StatusCode)
		}
		form := pageElement(t, body, "form", "set-password-card")
		if msg := html.UnescapeString(pageElement(t, form, "p", "set-password-error")); !strings.Contains(msg, tc.reason) {
			t.Errorf("%s: the form says %s, want %q", name, msg, tc.reason)
		}
		if !strings.Contains(form, "sam@example.com") || !strings.Contains(form, `type="password" name="password"`) {
			t.Errorf("%s: the form isn't shown again for sam:\n%s", name, form)
		}
		if strings.Contains(body, tc.password) {
			t.Errorf("%s: the page repeats the password", name)
		}
	}
	if _, err := h.Service.PasswordLinkAccount(t.Context(), token); err != nil {
		t.Errorf("a refused password used the link up: %v", err)
	}
}
