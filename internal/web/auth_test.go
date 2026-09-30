package web_test

import (
	"net/http"
	"net/http/httptest"
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
