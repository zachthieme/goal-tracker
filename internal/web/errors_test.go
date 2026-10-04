package web_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// loggedServer serves h's Service with a logger that writes JSON records into
// the returned buffer, one per line.
func loggedServer(t *testing.T, h *testsupport.Harness) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ts := httptest.NewServer(web.NewServer(h.Service, web.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil)))))
	t.Cleanup(ts.Close)
	return ts, &buf
}

// logRecords decodes the JSON records written to buf.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
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
	return records
}

// An unexpected server error answers 500 just as before, and its cause reaches
// the server log with the request's method and path, so an operator can see why.
func TestAnUnexpectedServerErrorIsLoggedWithItsCause(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts, buf := loggedServer(t, h)
	if err := h.DB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	resp := postForm(t, http.DefaultClient, ts.URL+"/signin", url.Values{"email": {"sam@example.com"}})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	if body != "sign-in failed\n" {
		t.Errorf("body %q, want %q", body, "sign-in failed\n")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type %q, want text/plain; charset=utf-8", ct)
	}

	records := logRecords(t, buf)
	if len(records) != 1 {
		t.Fatalf("logged %d records, want 1:\n%s", len(records), buf)
	}
	rec := records[0]
	if rec["level"] != "ERROR" || rec["method"] != http.MethodPost || rec["path"] != "/signin" {
		t.Errorf("record %v, want an ERROR for POST /signin", rec)
	}
	if cause, _ := rec["err"].(string); !strings.Contains(cause, "database is closed") {
		t.Errorf("record err %q, want the closed database's error", cause)
	}
}

// A refusal the app means to give, like a Departed person's sign-in, is not a
// server error, so it leaves the server log alone.
func TestARefusalIsNotLoggedAsAServerError(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts, buf := loggedServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	resp := postForm(t, http.DefaultClient, ts.URL+"/signin", url.Values{"email": {"sam@example.com"}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d, want 403", resp.StatusCode)
	}
	if buf.Len() != 0 {
		t.Errorf("a 403 was logged:\n%s", buf)
	}
}

// A nil logger is ignored, so the Server keeps logging to slog.Default()
// rather than panicking on its first 500.
//
//nolint:paralleltest // sets the process-wide slog default
func TestANilLoggerLogsToTheDefault(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	h := testsupport.New(t)
	ts := httptest.NewServer(web.NewServer(h.Service, web.WithLogger(nil)))
	t.Cleanup(ts.Close)
	if err := h.DB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	buf.Reset()

	resp := postForm(t, http.DefaultClient, ts.URL+"/signin", url.Values{"email": {"sam@example.com"}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	records := logRecords(t, &buf)
	if len(records) != 1 {
		t.Fatalf("logged %d records, want 1:\n%s", len(records), &buf)
	}
	if rec := records[0]; rec["level"] != "ERROR" || rec["method"] != http.MethodPost || rec["path"] != "/signin" {
		t.Errorf("record %v, want an ERROR for POST /signin", rec)
	}
}
