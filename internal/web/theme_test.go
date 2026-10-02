package web_test

import (
	"net/http"
	"strings"
	"testing"

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
