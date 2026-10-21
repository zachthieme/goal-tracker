package web_test

import (
	"net/http"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A load balancer probing /healthz, signed out, hears ok while the database
// answers.
func TestHealthzAnswersOKSignedOut(t *testing.T) {
	t.Parallel()

	ts := newServer(t, testsupport.New(t))

	resp, err := noRedirects(http.DefaultClient).Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", resp.StatusCode)
	}
	if body != "ok" {
		t.Errorf("body %q, want %q", body, "ok")
	}
}

// Once the database is gone, /healthz answers 503, so the probe takes this
// instance out of service.
func TestHealthzAnswersUnavailableWhenTheDatabaseIsClosed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	if err := h.DB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	resp, err := noRedirects(http.DefaultClient).Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", resp.StatusCode)
	}
}
