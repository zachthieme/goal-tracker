package web_test

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
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

// When the database file is swapped out from under a running server, as when
// its volume unmounts or the file is deleted, /healthz answers 503: the check
// reads a real table, so an empty database SQLite makes in its place fails it.
func TestHealthzAnswersUnavailableWhenTheDatabaseFileIsReplaced(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	var path string
	if err := h.DB.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatalf("find database file: %v", err)
	}
	h.DB.SetMaxIdleConns(0)
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("remove %s: %v", f, err)
		}
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
