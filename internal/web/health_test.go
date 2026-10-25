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

	resp, err := ctxGet(t, noRedirects(http.DefaultClient), ts.URL+"/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
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

	resp, err := ctxGet(t, noRedirects(http.DefaultClient), ts.URL+"/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
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
	if err := h.DB.QueryRowContext(t.Context(), `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatalf("find database file: %v", err)
	}
	h.DB.SetMaxIdleConns(0)
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("remove %s: %v", f, err)
		}
	}

	resp, err := ctxGet(t, noRedirects(http.DefaultClient), ts.URL+"/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", resp.StatusCode)
	}
}

// A pooled connection keeps reading the file it opened after that file is
// deleted, so /healthz checks the path on disk too: once the database file is
// removed it answers 503 even while a connection from an earlier check is
// still open.
func TestHealthzAnswersUnavailableWhenTheDatabaseFileIsRemovedUnderAPooledConnection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	var path string
	if err := h.DB.QueryRowContext(t.Context(), `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatalf("find database file: %v", err)
	}
	if got := healthzStatus(t, ts.URL); got != http.StatusOK {
		t.Fatalf("before removing the file: status %d, want 200", got)
	}
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("remove %s: %v", f, err)
		}
	}

	for i := range 3 {
		if got := healthzStatus(t, ts.URL); got != http.StatusServiceUnavailable {
			t.Errorf("check %d after removing the file: status %d, want 503", i+1, got)
		}
	}
}

// A database restored over the old one, with every table a fresh connection
// reads, still answers 503 while a pooled connection reads the old file: the
// path no longer names the file the check found there.
func TestHealthzAnswersUnavailableWhenTheDatabaseFileIsReplacedUnderAPooledConnection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	var path string
	if err := h.DB.QueryRowContext(t.Context(), `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatalf("find database file: %v", err)
	}
	if got := healthzStatus(t, ts.URL); got != http.StatusOK {
		t.Fatalf("before replacing the file: status %d, want 200", got)
	}
	for _, f := range []string{path + "-wal", path + "-shm"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("remove %s: %v", f, err)
		}
	}
	if err := os.Rename(testsupport.MigratedDatabaseFile(t), path); err != nil {
		t.Fatalf("replace database file: %v", err)
	}

	if got := healthzStatus(t, ts.URL); got != http.StatusServiceUnavailable {
		t.Errorf("after replacing the file: status %d, want 503", got)
	}
}

// healthzStatus GETs /healthz from the server at baseURL and returns the status
// it answers with.
func healthzStatus(t *testing.T, baseURL string) int {
	t.Helper()
	resp, err := ctxGet(t, noRedirects(http.DefaultClient), baseURL+"/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	return resp.StatusCode
}
