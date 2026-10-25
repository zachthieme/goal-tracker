package web_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// A form posted with a body over 1 MiB is refused as too large, before the
// route reads it.
func TestAFormOverOneMiBIsTooLarge(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	sam := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, sam, ts.URL+"/goals", url.Values{"title": {strings.Repeat("x", 1<<20)}})
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("POST /goals with a chunked body over 1 MiB: status %d, want 413", resp.StatusCode)
	}
}

// importBodyReader supplies n bytes of blank lines and counts how many the
// server took from it.
type importBodyReader struct {
	left  int64
	taken atomic.Int64
}

func (r *importBodyReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.left)
	for i := range p[:n] {
		p[i] = '\n'
	}
	r.left -= n
	r.taken.Add(n)
	return int(n), nil
}

// Only a signed-in person's import gets the import's larger cap: a signed-out
// POST /imports sent in chunks is refused once it passes the 1 MiB every
// other request is held to, so the server never reads the 5 MiB it offers.
// The server is driven directly, so what it took is what it read, not what a
// socket's buffers took in while it answered.
func TestASignedOutChunkedImportIsRefusedBeforeItsBodyIsRead(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	srv := web.NewServer(h.Service)

	body := &importBodyReader{left: 5 << 20}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/imports", body)
	r.ContentLength = -1
	r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	refused := w.Code == http.StatusRequestEntityTooLarge ||
		w.Code == http.StatusSeeOther && w.Header().Get("Location") == "/signin"
	if !refused {
		t.Errorf("signed-out chunked POST /imports: status %d, Location %q; want 413 or a redirect to /signin",
			w.Code, w.Header().Get("Location"))
	}
	if taken := body.taken.Load(); taken >= 2<<20 {
		t.Errorf("signed-out chunked POST /imports: the server took %d bytes of its body, want under 2 MiB", taken)
	}
}

// A signed-out POST /imports that says up front it is over 1 MiB is refused as
// too large, as any other request would be.
func TestASignedOutImportOverOneMiBIsTooLarge(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)

	csv := webImportCSV + strings.Repeat("\n", 2<<20)
	resp := postImportRaw(t, noRedirects(http.DefaultClient), ts.URL+"/imports", "goals.csv", csv, "dry-run")
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("signed-out POST /imports with a 2 MiB file: status %d, want 413", resp.StatusCode)
	}
}

// A signed-in Admin's import sent in chunks is read under the import's cap: a
// small spreadsheet, and one past the 1 MiB an ordinary request is held to.
func TestASignedInChunkedImportIsRead(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")

	for name, padding := range map[string]int{"small": 0, "over 1 MiB": 2 << 20} {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, err := mw.CreateFormFile("file", "goals.csv")
		if err != nil {
			t.Fatal(err)
		}
		// The CSV reader skips blank lines, so they pad the file without adding rows.
		if _, err := part.Write([]byte(webImportCSV + strings.Repeat("\n", padding))); err != nil {
			t.Fatal(err)
		}
		if err := mw.WriteField("action", "dry-run"); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		// Wrapped so the client can't tell its length and sends it in chunks.
		resp, err := ctxPost(t, admin, ts.URL+"/imports", mw.FormDataContentType(), io.NopCloser(&buf))
		if err != nil {
			t.Fatalf("%s: POST /imports: %v", name, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			_ = readBody(t, resp)
			t.Errorf("%s chunked import: status %d, want 200", name, resp.StatusCode)
			continue
		}
		page := parsePage(t, readBody(t, resp))
		if report := findByTestID(t, page, "import-report"); !strings.Contains(textOf(report), "Child goal") {
			t.Errorf("%s chunked import: the dry run's report doesn't show the file's rows:\n%s", name, textOf(report))
		}
	}
}
