package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A form posted with a body over 1 MiB is refused as too large, before the
// route reads it.
func TestAFormOverOneMiBIsTooLarge(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	sam := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, sam, ts.URL+"/goals", url.Values{"title": {strings.Repeat("x", 1<<20)}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /goals with a body over 1 MiB: status %d, want 413", resp.StatusCode)
	}
}

// An import's upload may be larger than an ordinary form: a spreadsheet just
// under the import's 5 MiB cap is read and dry run.
func TestAnImportUnderFiveMiBIsRead(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")

	// The CSV reader skips blank lines, so they pad the file without adding rows.
	csv := webImportCSV + strings.Repeat("\n", 5<<20-len(webImportCSV)-1024)
	page := parsePage(t, postImport(t, admin, ts.URL+"/imports", "goals.csv", csv, "dry-run"))
	if report := findByTestID(t, page, "import-report"); !strings.Contains(textOf(report), "Child goal") {
		t.Errorf("the dry run's report doesn't show the file's rows:\n%s", textOf(report))
	}
}

// An upload over the import's cap is refused as too large.
func TestAnImportOverItsCapIsTooLarge(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")

	csv := webImportCSV + strings.Repeat("\n", 6<<20)
	resp := postImportRaw(t, admin, ts.URL+"/imports", "goals.csv", csv, "dry-run")
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /imports with a 6 MiB file: status %d, want 413", resp.StatusCode)
	}
}

// A body sent without its length up front, in chunks, is held to the same cap.
func TestAChunkedFormOverOneMiBIsTooLarge(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	sam := signInClient(t, ts.URL, "sam@example.com")

	form := url.Values{"title": {strings.Repeat("x", 1<<20)}}.Encode()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/goals", io.NopCloser(strings.NewReader(form)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := sam.Do(req)
	if err != nil {
		t.Fatalf("POST /goals: %v", err)
	}
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /goals with a chunked body over 1 MiB: status %d, want 413", resp.StatusCode)
	}
}
