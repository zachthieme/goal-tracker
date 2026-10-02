package web_test

import (
	"bytes"
	"context"
	"html"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
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

// An Admin commits a spreadsheet whose columns name a Field and an Extendable
// Dimension: the Field is set and the unknown value joins the list. The form
// says how such columns are read (#77).
func TestAdminImportsFieldAndExtendableColumnsOverHTTP(t *testing.T) {
	const csv = `Title,Owner,So What,Kind,Budget,Customer
Grow revenue,ceo@example.com,Revenue is flat.,Ongoing,1200,acme; Newco
`
	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")
	adminAcct := h.SignIn("admin@example.com")
	h.CreateField(adminAcct, "Budget", domain.FieldNumber, "$")
	customer := h.CreateExtendableDimension(adminAcct, "Customer", "Acme")
	h.SetDimensionSelection(adminAcct, customer, domain.SelectionSeveral)

	hint := pageElement(t, getBody(t, admin, ts.URL+"/imports"), "p", "import-columns")
	for _, want := range []string{"Dimension", "Field", "semicolons", "Extendable"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the column hint doesn't mention %s: %s", want, hint)
		}
	}

	body := postImport(t, admin, ts.URL+"/imports", "goals.csv", csv, "commit")
	if !strings.Contains(body, "Imported 1 Goals") {
		t.Fatalf("commit summary missing; body:\n%s", body)
	}
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil || len(goals) != 1 {
		t.Fatalf("ListGoals = %d Goals, %v; want 1", len(goals), err)
	}
	fields, _ := h.Service.GoalFields(context.Background(), goals[0].ID)
	if len(fields) != 1 || fields[0].Value != "1200" {
		t.Errorf("Goal Fields = %+v, want Budget 1200", fields)
	}
	values, _ := h.Service.GoalValues(context.Background(), goals[0].ID)
	if len(values) != 2 || values[0].Value != "Acme" || values[1].Value != "Newco" {
		t.Errorf("Goal values = %+v, want Acme and Newco", values)
	}
}

// An Admin downloads the Goal table, changes a Field cell and a Title, and
// commits it: the Goal is updated rather than created, and the report says
// once that the changed Title was ignored (#81).
func TestAdminReimportsADownloadOverHTTP(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")
	adminAcct := h.SignIn("admin@example.com")
	budget := h.CreateField(adminAcct, "Budget", domain.FieldNumber, "$")
	alpha := h.CreateGoal(adminAcct, "Alpha", "It matters.")
	bravo := h.CreateGoal(adminAcct, "Bravo", "It matters.")
	h.SetGoalField(adminAcct, alpha, budget, "10")

	csv := getBody(t, admin, ts.URL+"/goals/download?layout=table")
	edited := strings.NewReplacer(",Alpha,", ",Alpha renamed,", ",10\n", ",20\n", ",Bravo,", ",Bravo renamed,").Replace(csv)
	body := postImport(t, admin, ts.URL+"/imports", "goals.csv", edited, "commit")

	if summary := pageElement(t, body, "p", "import-summary"); !strings.Contains(summary, "2 updated") {
		t.Errorf("summary doesn't count the updates: %s", summary)
	}
	if n := strings.Count(body, `data-testid="import-ignored"`); n != 1 {
		t.Fatalf("the ignored note shows %d times, want once; body:\n%s", n, body)
	}
	if note := pageElement(t, body, "p", "import-ignored"); !strings.Contains(note, "Title") {
		t.Errorf("the ignored note doesn't name Title: %s", note)
	}
	goals, _ := h.Service.ListGoals(context.Background())
	if len(goals) != 2 {
		t.Fatalf("re-import left %d Goals, want 2", len(goals))
	}
	for _, g := range goals {
		if g.ID == bravo.ID && g.Title != "Bravo" {
			t.Errorf("Bravo was renamed to %q", g.Title)
		}
	}
	fields, _ := h.Service.GoalFields(context.Background(), alpha.ID)
	if len(fields) != 1 || fields[0].Value != "20" {
		t.Errorf("Alpha Fields = %+v, want Budget 20", fields)
	}
}

// A CSV saved by Excel, which starts with a UTF-8 byte-order mark, imports
// over HTTP as one without; and a cell that would give a one-value Dimension a
// value containing a semicolon is reported on its row, saying why (#101).
func TestAdminImportsAnExcelCSVAndSeesSemicolonValuesRefusedOverHTTP(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	h.CreateDimension(admin, "Pillar", "Growth", "Reliability")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "admin@example.com")

	body := postImport(t, client, ts.URL+"/imports", "goals.csv", "\xEF\xBB\xBF"+webImportCSV, "commit")
	if !strings.Contains(body, "Imported 2 Goals") {
		t.Errorf("committing a CSV with a byte-order mark didn't import it; body:\n%s", body)
	}

	const semicolonCSV = "Title,Owner,So What,Kind,Pillar\nGrow,owner@example.com,It matters.,Ongoing,R&D; Ops\n"
	body = postImport(t, client, ts.URL+"/imports", "goals.csv", semicolonCSV, "dry-run")
	if errs := pageElement(t, body, "ul", "import-row-errors"); !strings.Contains(html.UnescapeString(errs), "a value can't contain a semicolon, because the import format uses it to separate values") {
		t.Errorf("dry run doesn't say why R&D; Ops is refused; body:\n%s", body)
	}
}
