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

// The sign-in page is a single narrow card: the brand, the development sign-in
// note, and the email field with its button.
func TestSignInPageIsACentredCard(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	page := getBody(t, http.DefaultClient, ts.URL+"/signin")

	at := strings.Index(page, `<form data-testid="signin-card"`)
	if at < 0 {
		t.Fatalf("the sign-in page has no card:\n%s", page)
	}
	card := page[at:strings.Index(page, "</form>")]
	if !strings.Contains(openTag(card), `class="card`) {
		t.Errorf("the sign-in form isn't a card: %s", openTag(card))
	}
	for _, want := range []string{"Goal Tracker", "Development sign-in", `type="email" name="email"`, `<button type="submit" class="btn primary`} {
		if !strings.Contains(card, want) {
			t.Errorf("the sign-in card lacks %s:\n%s", want, card)
		}
	}
}
