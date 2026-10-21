package web_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// requestLogServer is a Server over h's Service whose log goes, as JSON
// records, into the returned buffer. Tests call its ServeHTTP directly, so a
// request's access line is in the buffer once the call returns.
func requestLogServer(t *testing.T, h *testsupport.Harness) (*web.Server, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return web.NewServer(h.Service, web.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil)))), &buf
}

// accessLines decodes the access lines in buf: one per request served.
func accessLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, rec := range allLogRecords(t, buf) {
		if rec["msg"] == "request" {
			lines = append(lines, rec)
		}
	}
	return lines
}

// Every request served gets exactly one INFO access line, naming its method,
// the route it matched (the pattern, not the path, so IDs don't fill the
// log), the status it was answered with, the bytes sent and how long it took.
func TestEachRequestGetsOneAccessLine(t *testing.T) {
	t.Parallel()

	srv, buf := requestLogServer(t, testsupport.New(t))

	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/goals/42", nil))

	lines := accessLines(t, buf)
	if len(lines) != 2 {
		t.Fatalf("logged %d access lines for 2 requests, want 2:\n%s", len(lines), buf)
	}
	health, goal := lines[0], lines[1]
	if health["level"] != "INFO" || health["method"] != http.MethodGet || health["route"] != "GET /healthz" {
		t.Errorf("access line %v, want an INFO for GET /healthz", health)
	}
	if health["status"] != float64(http.StatusOK) || health["bytes"] != float64(len("ok")) {
		t.Errorf("access line %v, want status 200 and 2 bytes", health)
	}
	if d, ok := health["duration_ms"].(float64); !ok || d < 0 {
		t.Errorf("access line %v, want a duration_ms", health)
	}
	if goal["route"] != "GET /goals/{id}" || goal["status"] != float64(http.StatusSeeOther) {
		t.Errorf("access line %v, want route GET /goals/{id} answered 303", goal)
	}
}

// A request refused before it reaches a route, like a post another site's page
// sends, still gets its access line, its route logged as unmatched.
func TestARequestThatMatchesNoRouteIsLoggedAsUnmatched(t *testing.T) {
	t.Parallel()

	srv, buf := requestLogServer(t, testsupport.New(t))
	r := httptest.NewRequest(http.MethodPost, "/goals", strings.NewReader("title=x"))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	srv.ServeHTTP(httptest.NewRecorder(), r)

	lines := accessLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("logged %d access lines, want 1:\n%s", len(lines), buf)
	}
	if line := lines[0]; line["route"] != "unmatched" || line["status"] != float64(http.StatusForbidden) || line["method"] != http.MethodPost {
		t.Errorf("access line %v, want POST unmatched answered 403", line)
	}
}

// An error logged while handling a request carries the request's ID, the same
// one its access line and the response's X-Request-ID carry, so an operator
// can tie the failure to the request it failed.
func TestAnErrorLoggedInAHandlerCarriesItsRequestsID(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	srv, buf := requestLogServer(t, h)
	if err := h.DB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	form := url.Values{"email": {"sam@example.com"}}
	r := httptest.NewRequest(http.MethodPost, "/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	errs := logRecords(t, buf)
	lines := accessLines(t, buf)
	if len(errs) != 1 || len(lines) != 1 {
		t.Fatalf("logged %d records and %d access lines, want 1 of each:\n%s", len(errs), len(lines), buf)
	}
	id := w.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("the response carries no X-Request-ID")
	}
	if errs[0]["level"] != "ERROR" || errs[0]["request_id"] != id {
		t.Errorf("error record %v, want an ERROR with request_id %q", errs[0], id)
	}
	if lines[0]["request_id"] != id || lines[0]["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("access line %v, want request_id %q answered 500", lines[0], id)
	}
}
