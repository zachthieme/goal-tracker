package web_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// rootElement is the page's opening <html> tag, where the theme pin goes.
func rootElement(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, "<html")
	if start < 0 {
		t.Fatalf("page has no <html> element:\n%s", page)
	}
	return page[start : start+strings.Index(page[start:], ">")+1]
}

// getWithTheme fetches rawURL with the theme cookie set to value.
func getWithTheme(t *testing.T, client *http.Client, rawURL, value string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "gt_theme", Value: value})
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", rawURL, resp.StatusCode)
	}
	return readBody(t, resp)
}

// The page is pinned to the chosen theme from the first byte, signed in or
// not. With no choice, or one the server doesn't recognise, it isn't pinned
// and follows the system.
func TestThemeCookiePinsEveryPage(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct {
		cookie, want string
	}{
		{"light", `data-theme="light"`},
		{"dark", `data-theme="dark"`},
	} {
		if root := rootElement(t, getWithTheme(t, http.DefaultClient, ts.URL+"/signin", tc.cookie)); !strings.Contains(root, tc.want) {
			t.Errorf("sign-in page with %q chosen: root %s, want %s", tc.cookie, root, tc.want)
		}
		if root := rootElement(t, getWithTheme(t, client, ts.URL+"/home", tc.cookie)); !strings.Contains(root, tc.want) {
			t.Errorf("Home with %q chosen: root %s, want %s", tc.cookie, root, tc.want)
		}
	}

	for _, cookie := range []string{"system", "purple", ""} {
		if root := rootElement(t, getWithTheme(t, client, ts.URL+"/home", cookie)); strings.Contains(root, "data-theme") {
			t.Errorf("Home with %q in the cookie is pinned: %s", cookie, root)
		}
	}
	if root := rootElement(t, getBody(t, http.DefaultClient, ts.URL+"/signin")); strings.Contains(root, "data-theme") {
		t.Errorf("sign-in page with no cookie is pinned: %s", root)
	}
}

// noRedirects stops client following redirects, so a test sees where a
// response sends the person.
func noRedirects(client *http.Client) *http.Client {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// chooseTheme posts a theme choice from the page at back and returns the
// response, unfollowed.
func chooseTheme(t *testing.T, client *http.Client, baseURL, choice, back string) *http.Response {
	t.Helper()
	resp := postForm(t, noRedirects(client), baseURL+"/theme", url.Values{"theme": {choice}, "return": {back}})
	_ = readBody(t, resp)
	return resp
}

// themeCookieIn is the theme cookie a response sets, nil when it sets none.
func themeCookieIn(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "gt_theme" {
			return c
		}
	}
	return nil
}

// Choosing Light or Dark remembers it in a cookie that lasts a year, renewed on
// each choice, and pins every page to it. Choosing System clears the cookie,
// and the pages follow the system again.
func TestChoosingAThemePinsItAndSystemClearsIt(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, choice := range []string{"dark", "light"} {
		resp := chooseTheme(t, client, ts.URL, choice, "/home")
		c := themeCookieIn(resp)
		if c == nil || c.Value != choice {
			t.Fatalf("choosing %s: cookie %v, want %s", choice, c, choice)
		}
		if year := int((365 * 24 * time.Hour).Seconds()); c.MaxAge != year || c.Path != "/" {
			t.Errorf("choosing %s: cookie lasts %ds on path %q, want a year (%ds) on /", choice, c.MaxAge, c.Path, year)
		}
		want := `data-theme="` + choice + `"`
		for _, path := range []string{"/home", "/goals"} {
			if root := rootElement(t, getBody(t, client, ts.URL+path)); !strings.Contains(root, want) {
				t.Errorf("%s after choosing %s: root %s, want %s", path, choice, root, want)
			}
		}
	}

	resp := chooseTheme(t, client, ts.URL, "system", "/home")
	if c := themeCookieIn(resp); c == nil || c.MaxAge >= 0 {
		t.Errorf("choosing system does not clear the cookie: %v", c)
	}
	if root := rootElement(t, getBody(t, client, ts.URL+"/home")); strings.Contains(root, "data-theme") {
		t.Errorf("Home after choosing system is still pinned: %s", root)
	}
}

// After choosing, the person lands back on the page they chose from. A return
// address that isn't a path on this site lands on Home instead.
func TestChoosingAThemeReturnsToThePage(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct {
		back, want string
	}{
		{"/goals?owner=me", "/goals?owner=me"},
		{"/signin", "/signin"},
		{"", "/home"},
		{"goals", "/home"},
		{"https://evil.example/", "/home"},
		{"//evil.example/goals", "/home"},
		{`/\evil.example/goals`, "/home"},
		{"javascript:alert(1)", "/home"},
	} {
		resp := chooseTheme(t, client, ts.URL, "dark", tc.back)
		if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != tc.want {
			t.Errorf("choosing from %q: status %d to %q, want 303 to %q", tc.back, resp.StatusCode, loc, tc.want)
		}
	}
}

// The choice belongs to the browser, not the session: it can be made before
// signing in, and it survives signing out and in again.
func TestThemeChoiceOutlivesTheSession(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	if resp := chooseTheme(t, client, ts.URL, "dark", "/signin"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("choosing before sign-in: status %d, want 303", resp.StatusCode)
	}
	if root := rootElement(t, getBody(t, client, ts.URL+"/signin")); !strings.Contains(root, `data-theme="dark"`) {
		t.Errorf("sign-in page after choosing dark: root %s", root)
	}
	_ = readBody(t, postForm(t, client, ts.URL+"/signin", url.Values{"email": {"sam@example.com"}}))
	if root := rootElement(t, getBody(t, client, ts.URL+"/home")); !strings.Contains(root, `data-theme="dark"`) {
		t.Errorf("Home after signing in: root %s", root)
	}
	_ = readBody(t, postForm(t, client, ts.URL+"/signout", nil))
	if root := rootElement(t, getBody(t, client, ts.URL+"/signin")); !strings.Contains(root, `data-theme="dark"`) {
		t.Errorf("sign-in page after signing out: root %s", root)
	}
	_ = readBody(t, postForm(t, client, ts.URL+"/signin", url.Values{"email": {"sam@example.com"}}))
	if root := rootElement(t, getBody(t, client, ts.URL+"/home")); !strings.Contains(root, `data-theme="dark"`) {
		t.Errorf("Home after signing in again: root %s", root)
	}
}
