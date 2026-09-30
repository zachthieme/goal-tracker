package web_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

func TestGoalsRequiresSignIn(t *testing.T) {
	h := testsupport.New(t)
	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)

	// A client that does not follow redirects, so we can see the redirect itself.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/goals")
	if err != nil {
		t.Fatalf("GET /goals: %v", err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status %d, want 303 redirect to sign-in", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/signin" {
		t.Errorf("redirect to %q, want /signin", loc)
	}
}

// Signing in lands on Home, and so does visiting / or the sign-in page while
// signed in.
func TestSignedInPersonLandsOnHome(t *testing.T) {
	h := testsupport.New(t)
	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/signin", url.Values{"email": {"sam@example.com"}})
	if err != nil {
		t.Fatalf("POST /signin: %v", err)
	}
	_ = resp.Body.Close()
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/home" {
		t.Errorf("sign-in: status %d to %q, want 303 to /home", resp.StatusCode, loc)
	}
	for _, path := range []string{"/", "/signin"} {
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/home" {
			t.Errorf("GET %s signed in: status %d to %q, want 303 to /home", path, resp.StatusCode, loc)
		}
	}
}
