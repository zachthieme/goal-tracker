package web_test

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// TestSmokeSignInCreateAndViewGoal is the one end-to-end HTTP smoke test: a
// person signs in, creates a Proposed Goal, and sees it rendered on the Goal
// list and on its own page.
func TestSmokeSignInCreateAndViewGoal(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	// Sign in — the account is created on first sign-in.
	resp := postForm(t, client, ts.URL+"/signin", url.Values{"email": {"boss@example.com"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after sign-in: status %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "boss@example.com") {
		t.Errorf("goals page does not show the signed-in user; body:\n%s", body)
	}

	// Create a Proposed Goal.
	resp = postForm(t, client, ts.URL+"/goals", url.Values{
		"title":   {"Cut checkout latency"},
		"so_what": {"Shoppers abandon slow carts."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after create goal: status %d", resp.StatusCode)
	}
	listBody := readBody(t, resp)
	if !strings.Contains(listBody, "Cut checkout latency") {
		t.Errorf("Goal list does not show the new Goal; body:\n%s", listBody)
	}

	// Find the Goal's own page and confirm it renders the full Goal.
	goalPath := findGoalLink(t, listBody)
	resp, err = client.Get(ts.URL + goalPath)
	if err != nil {
		t.Fatalf("GET %s: %v", goalPath, err)
	}
	goalBody := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("goal page status %d", resp.StatusCode)
	}
	for _, want := range []string{"Cut checkout latency", "Shoppers abandon slow carts.", "Proposed", "boss@example.com"} {
		if !strings.Contains(goalBody, want) {
			t.Errorf("goal page missing %q; body:\n%s", want, goalBody)
		}
	}
}

func postForm(t *testing.T, client *http.Client, rawURL string, form url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(rawURL, form)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// findGoalLink pulls the first /goals/<id> href out of the rendered list.
func findGoalLink(t *testing.T, body string) string {
	t.Helper()
	const marker = `href="/goals/`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no goal link in body:\n%s", body)
	}
	rest := body[i+len(`href="`):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("malformed goal link in body:\n%s", body)
	}
	return rest[:end]
}
