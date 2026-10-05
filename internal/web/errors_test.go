package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
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

// A request the client abandoned, like a checklist post htmx replaces with a
// newer one, fails only because its own context was cancelled. That isn't a
// server fault, so it isn't logged at ERROR, and the 500 it answers is
// unchanged.
func TestARequestTheClientAbandonedIsNotLoggedAsAServerError(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts, buf := loggedServer(t, h)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	form := url.Values{"email": {"sam@example.com"}}
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", w.Code)
	}
	if body := w.Body.String(); body != "sign-in failed\n" {
		t.Errorf("body %q, want %q", body, "sign-in failed\n")
	}
	for _, rec := range logRecords(t, buf) {
		if rec["level"] == "ERROR" {
			t.Errorf("an abandoned request was logged at ERROR: %v", rec)
		}
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

// assertLoggedOnce checks that buf gained exactly one record since it held
// before records: an ERROR for method and path whose err contains cause.
func assertLoggedOnce(t *testing.T, buf *bytes.Buffer, before int, method, path, cause string) {
	t.Helper()
	records := logRecords(t, buf)
	if len(records)-before != 1 {
		t.Fatalf("logged %d new records, want 1:\n%s", len(records)-before, buf)
	}
	rec := records[before]
	if rec["level"] != "ERROR" || rec["method"] != method || rec["path"] != path {
		t.Errorf("record %v, want an ERROR for %s %s", rec, method, path)
	}
	if err, _ := rec["err"].(string); !strings.Contains(err, cause) {
		t.Errorf("record err %q, want it to contain %q", err, cause)
	}
}

// A Nudge that fails for no reason the app gives answers 500 on its refusal
// page, and its cause reaches the server log.
func TestAFailedNudgeIsLoggedWithItsCause(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts, buf := loggedServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)
	pat := signInClient(t, ts.URL, "pat@example.com")
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_nudge BEFORE INSERT ON nudges
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}
	before := len(logRecords(t, buf))

	path := "/goals/" + strconv.FormatInt(silent.ID, 10) + "/nudge"
	resp := postForm(t, pat, ts.URL+path, url.Values{"return": {"/risks"}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	assertLoggedOnce(t, buf, before, http.MethodPost, path, "injected failure")
}

// A No change whose Goal can't be loaded answers 500 inside the page, and the
// load's failure, not the No change's, reaches the server log.
func TestANoChangeWhoseGoalCantLoadIsLoggedWithItsCause(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts, buf := loggedServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")
	if _, err := h.DB.Exec(`ALTER TABLE milestones RENAME TO gone_milestones`); err != nil {
		t.Fatalf("rename milestones: %v", err)
	}
	before := len(logRecords(t, buf))

	path := fmt.Sprintf("/goals/%d/checkins/no-change", goal.ID)
	resp, page := postNoChange(t, client, ts.URL+path, false)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	assertCheckinUnavailable(t, page, "The Goal couldn&#39;t be loaded, so nothing was recorded. Try again.")
	assertLoggedOnce(t, buf, before, http.MethodPost, path, "load milestones: list milestones: SQL logic error: no such table: milestones")
}

// A No change that fails to be recorded answers 500 inside the page, and its
// cause reaches the server log.
func TestANoChangeThatFailsIsLoggedWithItsCause(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts, buf := loggedServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_checkin BEFORE INSERT ON checkins
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}
	before := len(logRecords(t, buf))

	path := fmt.Sprintf("/goals/%d/checkins/no-change", goal.ID)
	resp, page := postNoChange(t, client, ts.URL+path, false)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	assertCheckinUnavailable(t, page, "The No change couldn&#39;t be recorded. Try again.")
	assertLoggedOnce(t, buf, before, http.MethodPost, path, "injected failure")
}

// An import that fails for no reason the app gives answers 500 on the import
// page, and its cause reaches the server log.
func TestAFailedImportIsLoggedWithItsCause(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts, buf := loggedServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")
	if _, err := h.DB.Exec(`ALTER TABLE fields RENAME TO gone_fields`); err != nil {
		t.Fatalf("rename fields: %v", err)
	}
	before := len(logRecords(t, buf))

	resp := postImportRaw(t, admin, ts.URL+"/imports", "goals.csv", webImportCSV, "dry-run")
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	assertLoggedOnce(t, buf, before, http.MethodPost, "/imports", "no such table: fields")
}

// An Undo that fails for no reason the app gives answers 500 on its refusal
// page, and its cause reaches the server log.
func TestAFailedUndoIsLoggedWithItsCause(t *testing.T) {
	t.Parallel()

	for _, tc := range undoCases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, tc.admins...)
			ts, buf := loggedServer(t, h)
			client, landed := tc.act(t, h, ts)
			if _, err := h.DB.Exec(`CREATE TRIGGER fail_undo BEFORE ` + tc.writes + `
				BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
				t.Fatalf("install failing trigger: %v", err)
			}
			before := len(logRecords(t, buf))

			path := undoAction(t, landed)
			resp := postForm(t, client, ts.URL+path, toastFields(t, landed))
			_ = readBody(t, resp)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("status %d, want 500", resp.StatusCode)
			}
			assertLoggedOnce(t, buf, before, http.MethodPost, path, "injected failure")
		})
	}
}

// An Undo refused because there is nothing to undo answers 404, which is not a
// server error, so it leaves the server log alone.
func TestAnUndoWithNothingToUndoIsNotLogged(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts, buf := loggedServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")
	before := len(logRecords(t, buf))

	resp := postForm(t, client, ts.URL+"/link-removals/x/undo", url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
	if records := logRecords(t, buf); len(records) != before {
		t.Errorf("a 404 was logged:\n%s", buf)
	}
}
