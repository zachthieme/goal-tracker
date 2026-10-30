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

// logRecords decodes the JSON records written to buf, leaving out the access
// line every request gets (see accessLines), so they are what handling the
// requests logged.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, rec := range allLogRecords(t, buf) {
		if rec["msg"] != "request" {
			records = append(records, rec)
		}
	}
	return records
}

// allLogRecords decodes every JSON record written to buf.
func allLogRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
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
	defer func() { _ = resp.Body.Close() }()
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

// A request the client abandoned is still logged, at DEBUG, as abandoned with
// its method and path, and without a cause, so an operator debugging can see
// it went.
func TestARequestTheClientAbandonedIsLoggedAtDebug(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	var buf bytes.Buffer
	srv := web.NewServer(h.Service, web.WithLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	form := url.Values{"email": {"sam@example.com"}}
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(httptest.NewRecorder(), r)

	var abandoned []map[string]any
	for _, rec := range logRecords(t, &buf) {
		if rec["msg"] == "request abandoned" {
			abandoned = append(abandoned, rec)
		}
	}
	if len(abandoned) != 1 {
		t.Fatalf("logged %d request abandoned records, want 1:\n%s", len(abandoned), &buf)
	}
	rec := abandoned[0]
	if rec["level"] != "DEBUG" || rec["method"] != http.MethodPost || rec["path"] != "/signin" {
		t.Errorf("record %v, want a DEBUG for POST /signin", rec)
	}
	if _, ok := rec["err"]; ok {
		t.Errorf("record %v has an err, want none", rec)
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
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d, want 403", resp.StatusCode)
	}
	if records := logRecords(t, buf); len(records) != 0 {
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
	defer func() { _ = resp.Body.Close() }()
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
	h.FailWrites(testsupport.Insert, "nudges")
	before := len(logRecords(t, buf))

	path := "/goals/" + strconv.FormatInt(silent.ID, 10) + "/nudge"
	resp := postForm(t, pat, ts.URL+path, url.Values{"return": {"/follow-ups"}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	assertLoggedOnce(t, buf, before, http.MethodPost, path, "injected failure")
}

// A No change whose Goal's Milestones can't be read fails before anything is
// recorded: it answers 500 on the refusal page saying nothing was recorded,
// and the No change's own failure reaches the server log.
func TestANoChangeWhoseMilestonesCantBeReadIsLoggedWithItsCause(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts, buf := loggedServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")
	h.FailReads("milestones")
	before := len(logRecords(t, buf))

	path := fmt.Sprintf("/goals/%d/checkins/no-change", goal.ID)
	resp, page := postNoChange(t, client, ts.URL+path, false)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	assertRefusalPage(t, parsePage(t, page), "Can't check in", "The No change couldn't be recorded. Try again.", fmt.Sprintf("/goals/%d", goal.ID))
	// The handler's failure is logged at ERROR. The sidebar reads the overdue
	// Milestones too, when the refusal page draws its frame after that, so
	// then it says at WARN that it left out its Follow-ups count (#483, #628).
	records := logRecords(t, buf)
	if len(records)-before != 2 {
		t.Fatalf("logged %d new records, want the handler's ERROR and the sidebar's WARN:\n%s", len(records)-before, buf)
	}
	failed, sidebar := records[before], records[before+1]
	if failed["level"] != "ERROR" || failed["method"] != http.MethodPost || failed["path"] != path {
		t.Errorf("first record %v, want an ERROR for %s %s", failed, http.MethodPost, path)
	}
	if err, _ := failed["err"].(string); !strings.Contains(err, "no such table: milestones") {
		t.Errorf("the ERROR's err %q, want the No change's failed Milestones read", err)
	}
	if sidebar["level"] != "WARN" || sidebar["msg"] != "sidebar read failed" || sidebar["method"] != http.MethodPost || sidebar["path"] != path {
		t.Errorf("second record %v, want the sidebar's WARN for %s %s", sidebar, http.MethodPost, path)
	}
	if err, _ := sidebar["err"].(string); !strings.Contains(err, "no such table: milestones") {
		t.Errorf("the sidebar's WARN err %q, want its failed Milestones read", err)
	}
}

// A No change that fails to be recorded answers 500 on the refusal page, and its
// cause reaches the server log.
func TestANoChangeThatFailsIsLoggedWithItsCause(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts, buf := loggedServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")
	h.FailWrites(testsupport.Insert, "checkins")
	before := len(logRecords(t, buf))

	path := fmt.Sprintf("/goals/%d/checkins/no-change", goal.ID)
	resp, page := postNoChange(t, client, ts.URL+path, false)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	assertRefusalPage(t, parsePage(t, page), "Can't check in", "The No change couldn't be recorded. Try again.", fmt.Sprintf("/goals/%d", goal.ID))
	assertLoggedOnce(t, buf, before, http.MethodPost, path, "injected failure")
}

// An import that fails for no reason the app gives answers 500 on the import
// page, and its cause reaches the server log.
func TestAFailedImportIsLoggedWithItsCause(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts, buf := loggedServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")
	h.FailReads("fields")
	before := len(logRecords(t, buf))

	resp := postImportRaw(t, admin, ts.URL+"/imports", "goals.csv", webImportCSV, "dry-run")
	defer func() { _ = resp.Body.Close() }()
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
			h.FailWrites(tc.writes, tc.writesTo)
			before := len(logRecords(t, buf))

			path := undoAction(t, landed)
			resp := postForm(t, client, ts.URL+path, toastFields(t, landed))
			defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
	if records := logRecords(t, buf); len(records) != before {
		t.Errorf("a 404 was logged:\n%s", buf)
	}
}
