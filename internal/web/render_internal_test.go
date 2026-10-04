package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// failingPage writes the start of a page and then fails, the way a component
// does when its view model is broken partway through.
var failingPage = templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
	_, _ = io.WriteString(w, "<p>half")
	return errors.New("view model is nil")
})

// renderLogged renders c for r with a logger in r's context that writes JSON
// records into the returned buffer, and returns the decoded records along with
// the response.
func renderLogged(t *testing.T, r *http.Request, c templ.Component) ([]map[string]any, *httptest.ResponseRecorder) {
	t.Helper()
	var buf bytes.Buffer
	l := slog.New(slog.NewJSONHandler(&buf, nil))
	r = r.WithContext(withRequestLogger(r.Context(), l))
	w := httptest.NewRecorder()
	render(w, r, http.StatusOK, c)

	var records []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records, w
}

// A page that fails to draw is logged at Error with the request's method and
// path and the failure, and the part already written is left as it is.
func TestAPageThatFailsToDrawIsLoggedAtError(t *testing.T) {
	t.Parallel()

	records, w := renderLogged(t, httptest.NewRequest(http.MethodGet, "/goals/7", nil), failingPage)

	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1: %v", len(records), records)
	}
	rec := records[0]
	if rec["level"] != "ERROR" || rec["method"] != "GET" || rec["path"] != "/goals/7" || rec["err"] != "view model is nil" {
		t.Errorf("log record %v, want ERROR with method GET, path /goals/7, err %q", rec, "view model is nil")
	}
	if w.Code != http.StatusOK || w.Body.String() != "<p>half" {
		t.Errorf("response %d %q, want 200 %q", w.Code, w.Body.String(), "<p>half")
	}
}

// A page that fails to draw after the client went away is logged at Warn, not
// Error: the failure is the disconnect, not the page.
func TestAPageCutShortByADisconnectIsLoggedAtWarn(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequest(http.MethodPost, "/checkins", nil).WithContext(ctx)
	records, _ := renderLogged(t, r, failingPage)

	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1: %v", len(records), records)
	}
	rec := records[0]
	if rec["level"] != "WARN" || rec["method"] != "POST" || rec["path"] != "/checkins" || rec["err"] != "view model is nil" {
		t.Errorf("log record %v, want WARN with method POST, path /checkins, err %q", rec, "view model is nil")
	}
}

// A page that draws without error logs nothing.
func TestAPageThatDrawsLogsNothing(t *testing.T) {
	t.Parallel()

	page := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "<p>whole</p>")
		return err
	})
	records, w := renderLogged(t, httptest.NewRequest(http.MethodGet, "/goals", nil), page)

	if len(records) != 0 {
		t.Errorf("got log records %v, want none", records)
	}
	if w.Body.String() != "<p>whole</p>" {
		t.Errorf("body %q, want %q", w.Body.String(), "<p>whole</p>")
	}
}

// A request that didn't come through ServeHTTP carries no logger, so render
// falls back to slog.Default() rather than panicking, and the page still draws.
func TestRenderWithoutALoggerInTheRequestStillDraws(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	render(w, httptest.NewRequest(http.MethodGet, "/goals/7", nil), http.StatusOK, failingPage)

	if w.Code != http.StatusOK || w.Body.String() != "<p>half" {
		t.Errorf("response %d %q, want 200 %q", w.Code, w.Body.String(), "<p>half")
	}
	if requestLogger(t.Context()) != slog.Default() {
		t.Error("requestLogger without a recorded logger is not slog.Default()")
	}
}

// A request served by ServeHTTP carries the Server's logger, so a page it
// fails to draw is logged where WithLogger sends the Server's records.
func TestServeHTTPCarriesTheServersLogger(t *testing.T) {
	t.Parallel()

	l := slog.New(slog.NewJSONHandler(io.Discard, nil))
	s := NewServer(nil, WithLogger(l))
	var got *slog.Logger
	s.mux.HandleFunc("GET /probe/logger", func(_ http.ResponseWriter, r *http.Request) {
		got = requestLogger(r.Context())
	})

	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe/logger", nil))

	if got != l {
		t.Errorf("request logger %p, want the Server's %p", got, l)
	}
}
