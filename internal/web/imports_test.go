package web_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const webImportCSV = `Title,Owner,So What,Kind,Delivery Date,Parents
Parent goal,boss@example.com,It matters.,Ongoing,,
Child goal,ic@example.com,Supports the parent.,Dated,2026-06-30,Parent goal
`

// An Admin uploads a clean spreadsheet: a dry run shows the rows and saves
// nothing, then a commit imports the Goals.
func TestAdminDryRunsThenCommitsImportOverHTTP(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")

	// The import page offers the upload form to an Admin.
	page := getBody(t, admin, ts.URL+"/imports")
	if !strings.Contains(page, `data-testid="import-form"`) {
		t.Fatalf("Admin import page missing the upload form; body:\n%s", page)
	}

	// Dry run: the report shows, but nothing is saved.
	body := postImport(t, admin, ts.URL+"/imports", "goals.csv", webImportCSV, "dry-run")
	if !strings.Contains(body, `data-testid="import-report"`) {
		t.Fatalf("dry run did not render a report; body:\n%s", body)
	}
	if !strings.Contains(body, "nothing saved") {
		t.Errorf("dry run summary should say nothing saved; body:\n%s", body)
	}
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Fatalf("dry run saved %d Goals, want 0", len(goals))
	}

	// Commit: the Goals are imported.
	body = postImport(t, admin, ts.URL+"/imports", "goals.csv", webImportCSV, "commit")
	if !strings.Contains(body, "Imported 2 Goals") {
		t.Errorf("commit summary missing; body:\n%s", body)
	}
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 2 {
		t.Fatalf("commit saved %d Goals, want 2", len(goals))
	}
}

// A dry run over HTTP shows the offending row's errors and saves nothing.
func TestAdminSeesRowErrorsOnDryRunOverHTTP(t *testing.T) {
	const badCSV = `Title,Owner,So What,Kind,Delivery Date
Fine,owner@example.com,It matters.,Ongoing,
,owner@example.com,No title here.,Sideways,
`
	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")

	body := postImport(t, admin, ts.URL+"/imports", "goals.csv", badCSV, "dry-run")
	if !strings.Contains(body, `data-testid="import-row-errors"`) {
		t.Fatalf("dry run did not surface row errors; body:\n%s", body)
	}
	if !strings.Contains(body, "Title") || !strings.Contains(body, "Kind") {
		t.Errorf("expected Title and Kind errors; body:\n%s", body)
	}
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Errorf("dry run with errors saved %d Goals, want 0", len(goals))
	}
}

// A non-Admin sees only a note that imports are Admin-only, and the API refuses
// their upload.
func TestNonAdminCannotImportOverHTTP(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	sam := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, sam, ts.URL+"/imports")
	if !strings.Contains(page, `data-testid="import-forbidden"`) {
		t.Errorf("non-Admin should see the Admin-only note; body:\n%s", page)
	}
	if strings.Contains(page, `data-testid="import-form"`) {
		t.Errorf("non-Admin should not see the upload form; body:\n%s", page)
	}

	resp := postImportRaw(t, sam, ts.URL+"/imports", "goals.csv", webImportCSV, "commit")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin import: status %d, want 403", resp.StatusCode)
	}
}

// postImport uploads a spreadsheet and returns the rendered body, failing on a
// non-2xx status.
func postImport(t *testing.T, client *http.Client, rawURL, filename, content, action string) string {
	t.Helper()
	resp := postImportRaw(t, client, rawURL, filename, content, action)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: status %d", rawURL, resp.StatusCode)
	}
	return readBody(t, resp)
}

// postImportRaw uploads a spreadsheet as multipart form data with the given
// action, returning the raw response.
func postImportRaw(t *testing.T, client *http.Client, rawURL, filename, content, action string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.WriteField("action", action); err != nil {
		t.Fatalf("write action field: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	resp, err := client.Post(rawURL, mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	return resp
}

// The import form is a card whose primary button is Dry run, beside Commit. The
// report opens with its summary as an alert — red when rows have errors — then
// a table of the rows with their errors.
func TestImportReportIsAnAlertThenATable(t *testing.T) {
	const badCSV = `Title,Owner,So What,Kind,Delivery Date
Fine,owner@example.com,It matters.,Ongoing,
,owner@example.com,No title here.,Sideways,
`
	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")

	form := pageElement(t, getBody(t, admin, ts.URL+"/imports"), "form", "import-form")
	if !strings.Contains(openTag(form), `class="card`) {
		t.Errorf("the import form isn't a card: %s", openTag(form))
	}
	if !strings.Contains(form, `class="btn primary" name="action" value="dry-run"`) || !strings.Contains(form, `value="commit">Commit</button>`) {
		t.Errorf("the form lacks a primary Dry run and a Commit:\n%s", form)
	}

	body := postImport(t, admin, ts.URL+"/imports", "goals.csv", badCSV, "dry-run")
	report := between(t, body, `data-testid="import-report"`, "")
	summary := pageElement(t, report, "p", "import-summary")
	if !strings.Contains(openTag(summary), `class="alert r"`) {
		t.Errorf("a report with errors doesn't open with a red alert: %s", summary)
	}
	if !strings.Contains(report, "<table") || !strings.Contains(report, `<tr data-testid="import-row"`) {
		t.Errorf("the report's rows aren't a table:\n%s", report)
	}
	if strings.Index(report, "import-summary") > strings.Index(report, "<table") {
		t.Errorf("the summary doesn't come before the rows:\n%s", report)
	}
}
