package web_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// samsPassword is a password long enough to be accepted.
const samsPassword = "correct horse battery"

// newPasswordServer serves the app with password sign-in.
func newPasswordServer(t *testing.T, h *testsupport.Harness) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(web.NewServer(h.Service, web.WithPasswordSignIn()))
	t.Cleanup(ts.Close)
	return ts
}

// passwordLinkToken issues a set-password link for emailAddr and returns its
// token, failing the test on error.
func passwordLinkToken(t *testing.T, h *testsupport.Harness, emailAddr string) string {
	t.Helper()
	token, _, err := h.Service.IssuePasswordLink(t.Context(), emailAddr)
	if err != nil {
		t.Fatalf("IssuePasswordLink(%q): %v", emailAddr, err)
	}
	return token
}

// givePassword sets emailAddr's password through a fresh set-password link,
// failing the test on error.
func givePassword(t *testing.T, h *testsupport.Harness, emailAddr, password string) {
	t.Helper()
	if _, err := h.Service.SetPassword(t.Context(), passwordLinkToken(t, h, emailAddr), password); err != nil {
		t.Fatalf("SetPassword(%q): %v", emailAddr, err)
	}
}

// passwordSignInCookie signs emailAddr in with password and returns the
// session cookie, failing the test unless the sign-in sends them to /home.
func passwordSignInCookie(t *testing.T, base, emailAddr, password string) *http.Cookie {
	t.Helper()
	resp := postForm(t, noRedirects(http.DefaultClient), base+"/signin", url.Values{"email": {emailAddr}, "password": {password}})
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
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/home" {
		t.Fatalf("after setting a password: status %d at %s, want 200 at /home:\n%s", resp.StatusCode, resp.Request.URL.Path, body)
	}
	if !signedOut(t, ts.URL, before) {
		t.Error("a session from before the password was set still reaches /home")
	}
	passwordSignInCookie(t, ts.URL, "sam@example.com", "a brand new password")
}
