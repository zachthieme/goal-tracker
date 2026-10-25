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

	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/goals/42", nil))

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
// sends, still gets its access line, logged under the route it was aimed at, so
// an operator can find the refusals on a route by filtering on it.
func TestACrossSiteRefusalIsLoggedUnderItsRoute(t *testing.T) {
	t.Parallel()

	srv, buf := requestLogServer(t, testsupport.New(t))
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/goals", strings.NewReader("title=x"))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	srv.ServeHTTP(httptest.NewRecorder(), r)

	lines := accessLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("logged %d access lines, want 1:\n%s", len(lines), buf)
	}
	if line := lines[0]; line["route"] != "POST /goals" || line["status"] != float64(http.StatusForbidden) || line["method"] != http.MethodPost {
		t.Errorf("access line %v, want POST /goals answered 403", line)
	}
}

// A body over the cap is refused before routing too, and logged under the
// route it was aimed at.
func TestAnOversizeRefusalIsLoggedUnderItsRoute(t *testing.T) {
	t.Parallel()

	srv, buf := requestLogServer(t, testsupport.New(t))
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/goals", strings.NewReader("title="+strings.Repeat("x", 1<<20)))
	srv.ServeHTTP(httptest.NewRecorder(), r)

	lines := accessLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("logged %d access lines, want 1:\n%s", len(lines), buf)
	}
	if line := lines[0]; line["route"] != "POST /goals" || line["status"] != float64(http.StatusRequestEntityTooLarge) {
		t.Errorf("access line %v, want POST /goals answered 413", line)
	}
}

// A path no route serves is answered 404 by the catch-all, and logged as
// unmatched, not under the index page's GET /.
func TestAnUnknownPathIsLoggedAsUnmatched(t *testing.T) {
	t.Parallel()

	srv, buf := requestLogServer(t, testsupport.New(t))
	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/no-such-page", nil))

	lines := accessLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("logged %d access lines, want 1:\n%s", len(lines), buf)
	}
	if line := lines[0]; line["route"] != "unmatched" || line["status"] != float64(http.StatusNotFound) {
		t.Errorf("access line %v, want unmatched answered 404", line)
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
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/signin", strings.NewReader(form.Encode()))
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

// A proxy in front that already gave the request an ID, of 1 to 64 letters,
// digits, underscores and hyphens, has it reused: echoed back and logged.
func TestAValidIncomingRequestIDIsReused(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"a", "req_42-AbC", strings.Repeat("z", 64)} {
		srv, buf := requestLogServer(t, testsupport.New(t))
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
		r.Header.Set("X-Request-ID", id)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)

		if got := w.Header().Get("X-Request-ID"); got != id {
			t.Errorf("incoming ID %q: response X-Request-ID %q, want it echoed", id, got)
		}
		if lines := accessLines(t, buf); len(lines) != 1 || lines[0]["request_id"] != id {
			t.Errorf("incoming ID %q: access lines %v, want one logged under it", id, lines)
		}
	}
}

// An incoming ID that could flood or forge the log, too long or carrying a
// newline or other characters, is replaced with one the app makes.
func TestAnInvalidIncomingRequestIDIsReplaced(t *testing.T) {
	t.Parallel()

	for _, id := range []string{strings.Repeat("z", 65), strings.Repeat("a", 200), "abc\ndef", "a b", "id;drop", "é"} {
		srv, buf := requestLogServer(t, testsupport.New(t))
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
		r.Header.Set("X-Request-ID", id)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)

		got := w.Header().Get("X-Request-ID")
		if got == "" || got == id {
			t.Errorf("incoming ID %q: response X-Request-ID %q, want a new one", id, got)
		}
		if lines := accessLines(t, buf); len(lines) != 1 || lines[0]["request_id"] != got {
			t.Errorf("incoming ID %q: access lines %v, want one logged under %q", id, lines, got)
		}
	}
}

// A handler that panics, as /healthz does on a Server with no Service, is
// answered 500 rather than a dropped connection, its panic is logged at ERROR
// with its stack under the request's ID, and it still gets its access line.
func TestAPanickingHandlerIsAnswered500AndLoggedUnderItsRequestsID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	srv := web.NewServer(nil, web.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("answered %d, want 500", w.Code)
	}
	id := w.Header().Get("X-Request-ID")
	errs := logRecords(t, &buf)
	lines := accessLines(t, &buf)
	if len(errs) != 1 || len(lines) != 1 {
		t.Fatalf("logged %d records and %d access lines, want 1 of each:\n%s", len(errs), len(lines), &buf)
	}
	if e := errs[0]; e["level"] != "ERROR" || e["request_id"] != id || e["panic"] == nil || e["stack"] == nil {
		t.Errorf("error record %v, want an ERROR with request_id %q, the panic and its stack", e, id)
	}
	if line := lines[0]; line["request_id"] != id || line["status"] != float64(http.StatusInternalServerError) || line["route"] != "GET /healthz" {
		t.Errorf("access line %v, want GET /healthz under request_id %q answered 500", line, id)
	}
}

// A handler that has already answered 200 when it panics keeps its answer: the
// access line records the 200, and the panic is still logged at ERROR.
func TestAHandlerThatPanicsAfterAnsweringKeepsItsStatus(t *testing.T) {
	t.Parallel()

	srv, buf := requestLogServer(t, testsupport.New(t))
	srv.HandleForPanicTest("GET /panics-after-ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("boom")
	})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/panics-after-ok", nil))

	if w.Code != http.StatusOK || w.Body.String() != "partial" {
		t.Errorf("answered %d %q, want the handler's own 200 %q", w.Code, w.Body, "partial")
	}
	id := w.Header().Get("X-Request-ID")
	errs := logRecords(t, buf)
	lines := accessLines(t, buf)
	if len(errs) != 1 || len(lines) != 1 {
		t.Fatalf("logged %d records and %d access lines, want 1 of each:\n%s", len(errs), len(lines), buf)
	}
	if e := errs[0]; e["level"] != "ERROR" || e["request_id"] != id || e["panic"] != "boom" {
		t.Errorf("error record %v, want an ERROR with request_id %q and panic boom", e, id)
	}
	if line := lines[0]; line["request_id"] != id || line["status"] != float64(http.StatusOK) || line["route"] != "GET /panics-after-ok" {
		t.Errorf("access line %v, want GET /panics-after-ok under request_id %q answered 200", line, id)
	}
}

// A handler that aborts its response with http.ErrAbortHandler gets its access
// line, and the abort reaches net/http, which drops the connection without
// logging it; the app logs no ERROR for it either.
func TestAnAbortedHandlerIsLoggedAndStillAborts(t *testing.T) {
	t.Parallel()

	srv, buf := requestLogServer(t, testsupport.New(t))
	srv.HandleForPanicTest("GET /aborts", func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})
	func() {
		defer func() {
			if p := recover(); p != http.ErrAbortHandler { //nolint:errorlint // the sentinel itself must be passed on, unwrapped, as net/http compares it with ==
				t.Errorf("ServeHTTP panicked with %v, want http.ErrAbortHandler passed on", p)
			}
		}()
		srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/aborts", nil))
	}()

	if errs := logRecords(t, buf); len(errs) != 0 {
		t.Errorf("logged %v, want no records besides the access line", errs)
	}
	if lines := accessLines(t, buf); len(lines) != 1 || lines[0]["route"] != "GET /aborts" {
		t.Errorf("access lines %v, want one for GET /aborts", lines)
	}
}
