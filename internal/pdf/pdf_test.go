package pdf_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/pdf"
)

// chromeForTest is the Chromium to render with: GOAL_TRACKER_CHROME's, else
// one on PATH, as the app finds it, else the newest Playwright installed under
// PLAYWRIGHT_BROWSERS_PATH or ~/.cache/ms-playwright, as the vetinari image
// has. Without one the test is skipped, saying so.
func chromeForTest(t *testing.T) pdf.Chromium {
	t.Helper()
	if path, err := pdf.Find(os.Getenv("GOAL_TRACKER_CHROME")); err == nil && path != "" {
		return pdf.Chromium{Path: path}
	}
	roots := []string{os.Getenv("PLAYWRIGHT_BROWSERS_PATH")}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".cache", "ms-playwright"))
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		found, _ := filepath.Glob(filepath.Join(root, "chromium-*", "chrome-linux64", "chrome"))
		slices.Sort(found)
		if len(found) > 0 {
			return pdf.Chromium{Path: found[len(found)-1]}
		}
	}
	t.Skip("no Chromium to render with: set GOAL_TRACKER_CHROME, put chromium on PATH, or install Playwright's")
	return pdf.Chromium{}
}

// appForTest serves what a rendered page loads from the app: the print page's
// serif under /static/fonts/.
func appForTest(t *testing.T) *httptest.Server {
	t.Helper()
	font, err := os.ReadFile("../web/static/fonts/source-serif-4-latin-wght-normal.woff2")
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/static/fonts/source-serif-4-latin-wght-normal.woff2" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "font/woff2")
		_, _ = w.Write(font)
	}))
	t.Cleanup(app.Close)
	return app
}

const servedFontPage = `<!DOCTYPE html>
<html lang="en">
	<head>
		<meta charset="utf-8"/>
		<style>
			@font-face { font-family: "Source Serif 4"; font-weight: 200 900; font-display: swap; src: url(/static/fonts/source-serif-4-latin-wght-normal.woff2) format("woff2"); }
			body { font-family: "Source Serif 4", Georgia, serif; }
		</style>
	</head>
	<body><h1>EU MBR</h1><p>Vendor is late.</p></body>
</html>`

// Chromium renders HTML to a PDF, its fonts loaded from the app at the base
// URL, not fallen back to the machine's.
func TestChromiumRendersHTMLToAPDFWithTheAppsFonts(t *testing.T) {
	t.Parallel()

	chrome := chromeForTest(t)
	app := appForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	b, err := chrome.Render(ctx, []byte(servedFontPage), app.URL)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatalf("Render gave %d bytes not starting %%PDF-: %q", len(b), b[:min(len(b), 32)])
	}
	if !bytes.Contains(b, []byte("SourceSerif4")) {
		t.Error("the PDF doesn't embed the app's Source Serif 4: the page's font didn't load from the base URL")
	}
}

// A render that runs out of time fails, rather than giving a PDF.
func TestChromiumFailsARenderOutOfTime(t *testing.T) {
	t.Parallel()

	chrome := chromeForTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if b, err := chrome.Render(ctx, []byte(servedFontPage), appForTest(t).URL); err == nil {
		t.Errorf("Render with its time up gave %d bytes and no error", len(b))
	}
}

// A Chromium that writes no PDF fails the render, naming the binary, rather
// than giving a broken PDF.
func TestARenderWithoutAPDFFails(t *testing.T) {
	t.Parallel()

	fake := filepath.Join(t.TempDir(), "chromium")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'no display' >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	b, err := pdf.Chromium{Path: fake}.Render(t.Context(), []byte("<p>hi</p>"), "http://127.0.0.1:1")
	if err == nil {
		t.Fatalf("Render gave %d bytes and no error", len(b))
	}
	if !strings.Contains(err.Error(), fake) {
		t.Errorf("error %q doesn't name the Chromium %s", err, fake)
	}
}

// Find takes the named Chromium as it is, when it's an executable file.
func TestFindTakesTheNamedChromium(t *testing.T) {
	t.Parallel()

	named := executable(t, t.TempDir(), "my-chrome")
	got, err := pdf.Find(named)
	if err != nil || got != named {
		t.Errorf("Find(%q) = %q, %v; want it back", named, got, err)
	}
}

// A named Chromium that isn't there, or can't run, is an error naming it.
func TestFindRefusesANamedChromiumThatCantRun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(dir, "missing"), "not executable": plain, "a directory": dir} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := pdf.Find(path)
			if err == nil {
				t.Fatalf("Find(%q) = %q, want an error", path, got)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q doesn't name %s", err, path)
			}
		})
	}
}

// Unnamed, Find looks on PATH for chromium, chromium-browser, google-chrome
// and google-chrome-stable, in that order, and finds nothing where there's
// none.
//
//nolint:paralleltest // sets PATH
func TestFindLooksOnPATH(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if got, err := pdf.Find(""); got != "" || err != nil {
		t.Errorf("Find(\"\") with no Chromium on PATH = %q, %v; want nothing", got, err)
	}

	stable := executable(t, dir, "google-chrome-stable")
	if got, err := pdf.Find(""); got != stable || err != nil {
		t.Errorf("Find(\"\") = %q, %v; want %s", got, err, stable)
	}
	executable(t, dir, "google-chrome")
	browser := executable(t, dir, "chromium-browser")
	if got, err := pdf.Find(""); got != browser || err != nil {
		t.Errorf("Find(\"\") = %q, %v; want %s, ahead of google-chrome", got, err, browser)
	}
	chromium := executable(t, dir, "chromium")
	if got, err := pdf.Find(""); got != chromium || err != nil {
		t.Errorf("Find(\"\") = %q, %v; want %s, first of all", got, err, chromium)
	}
}

// executable writes an executable file name in dir and returns its path.
func executable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
