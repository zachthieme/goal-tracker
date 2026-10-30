package pdf_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
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

// A render runs no script on the page, whatever reached it: the PDF has the
// page's text as served, not as a script would rewrite it.
func TestARenderRunsNoScriptOnThePage(t *testing.T) {
	t.Parallel()

	chrome := chromeForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	page := `<!DOCTYPE html><html><body><p id="x">STATICTEXT</p>` +
		`<script>document.getElementById("x").textContent = "SCRIPTRAN"</script></body></html>`

	b, err := chrome.Render(ctx, []byte(page), appForTest(t).URL)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := pdfText(t, b); got != "STATICTEXT" {
		t.Errorf("the PDF's text is %q, want the page's STATICTEXT: a script on the page ran", got)
	}
}

// pdfText is the text a PDF drawn in one font shows, as Chromium writes it:
// each text object's glyphs, read through the font's ToUnicode map, one text
// object to a line.
func pdfText(t *testing.T, b []byte) string {
	t.Helper()
	var streams [][]byte
	for _, m := range regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`).FindAllSubmatch(b, -1) {
		r, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			continue
		}
		s, err := io.ReadAll(r)
		if err == nil {
			streams = append(streams, s)
		}
	}
	hex4 := func(h []byte) uint16 { n, _ := strconv.ParseUint(string(h), 16, 16); return uint16(n) }
	runes := map[uint16]rune{}
	bfchar := regexp.MustCompile(`<([0-9A-Fa-f]{4})>\s*<([0-9A-Fa-f]{4})>\s*\n`)
	bfrange := regexp.MustCompile(`<([0-9A-Fa-f]{4})>\s*<([0-9A-Fa-f]{4})>\s*<([0-9A-Fa-f]{4})>`)
	for _, s := range streams {
		for _, block := range regexp.MustCompile(`(?s)beginbfchar(.*?)endbfchar`).FindAllSubmatch(s, -1) {
			for _, m := range bfchar.FindAllSubmatch(block[1], -1) {
				runes[hex4(m[1])] = rune(hex4(m[2]))
			}
		}
		for _, block := range regexp.MustCompile(`(?s)beginbfrange(.*?)endbfrange`).FindAllSubmatch(s, -1) {
			for _, m := range bfrange.FindAllSubmatch(block[1], -1) {
				for g, u := hex4(m[1]), hex4(m[3]); g <= hex4(m[2]); g, u = g+1, u+1 {
					runes[g] = rune(u)
				}
			}
		}
	}
	var lines []string
	glyphs := regexp.MustCompile(`<([0-9A-Fa-f]+)>`)
	for _, s := range streams {
		for _, text := range regexp.MustCompile(`(?s)\bBT\b(.*?)\bET\b`).FindAllSubmatch(s, -1) {
			var line strings.Builder
			for _, m := range glyphs.FindAllSubmatch(text[1], -1) {
				for h := m[1]; len(h) >= 4; h = h[4:] {
					line.WriteRune(runes[hex4(h[:4])])
				}
			}
			lines = append(lines, line.String())
		}
	}
	return strings.Join(lines, "\n")
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

// A render killed for time leaves nothing behind: within a few seconds,
// nothing of Chromium's is in TMPDIR, and no core is dumped in the working
// directory, and that holds for a settle after, so a core dumped late is
// caught.
//
//nolint:paralleltest // sets TMPDIR and the working directory
func TestARenderKilledForTimeLeavesNothingBehind(t *testing.T) {
	chrome := chromeForTest(t)
	app := appForTest(t)
	tmp, work := shortTMPDIR(t), t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Chdir(work)
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()

	if b, err := chrome.Render(ctx, []byte(servedFontPage), app.URL); err == nil {
		t.Fatalf("Render in 150ms gave %d bytes and no error; want it killed for time", len(b))
	}

	const wait, settle = 5 * time.Second, 200 * time.Millisecond
	deadline := time.Now().Add(wait)
	var emptySince time.Time
	for {
		left, _ := os.ReadDir(tmp)
		cores, _ := filepath.Glob(filepath.Join(work, "core*"))
		switch {
		case len(left) != 0 || len(cores) != 0:
			emptySince = time.Time{}
		case emptySince.IsZero():
			emptySince = time.Now()
		case time.Since(emptySince) >= settle:
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited %v after the render for an empty TMPDIR and no core, held for %v; TMPDIR has %v, and the working directory the cores %v", wait, settle, names(left), cores)
		}
		time.Sleep(time.Millisecond)
	}
}

// A TMPDIR too long for Chromium's socket in a render's own directory, though
// not in TMPDIR itself, still renders: Chromium's socket path is at most 107
// bytes, 45 of them its own, under TMPDIR.
//
//nolint:paralleltest // sets TMPDIR and the working directory
func TestARenderUnderALongTMPDIRStillRenders(t *testing.T) {
	chrome := chromeForTest(t)
	app := appForTest(t)
	const long = 58
	short := shortTMPDIR(t)
	tmp := filepath.Join(short, strings.Repeat("t", long-len(short)-1))
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	t.Chdir(t.TempDir()) // where a Chromium that aborts dumps core
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	if _, err := chrome.Render(ctx, []byte(servedFontPage), app.URL); err != nil {
		t.Fatalf("Render under the %d-byte TMPDIR %s: %v", len(tmp), tmp, err)
	}
}

// shortTMPDIR is an empty directory with a path short enough for Chromium's
// socket in a render's own directory in it, as t.TempDir, named for the test,
// isn't: in the TMPDIR the tests run with, as a TMPDIR such as /tmp is.
func shortTMPDIR(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tmp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// names are the entries' names.
func names(entries []os.DirEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// A Chromium that writes no PDF fails the render, naming the binary, rather
// than giving a broken PDF.
func TestARenderWithoutAPDFFails(t *testing.T) {
	t.Parallel()

	fake := filepath.Join(t.TempDir(), "chromium")
	writeScript(t, fake, "#!/bin/sh\necho 'no display' >&2\n")

	b, err := pdf.Chromium{Path: fake}.Render(t.Context(), []byte("<p>hi</p>"), "http://127.0.0.1:1")
	if err == nil {
		t.Fatalf("Render gave %d bytes and no error", len(b))
	}
	if !strings.Contains(err.Error(), fake) {
		t.Errorf("error %q doesn't name the Chromium %s", err, fake)
	}
}

// Render returns only once every process Chromium started is gone, even one
// that let go of Chromium's output, as a helper can: none is left running.
func TestARenderOutlastsChromiumsHelpers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gone := filepath.Join(dir, "gone")
	fake := filepath.Join(dir, "chromium")
	writeScript(t, fake, "#!/bin/sh\n(sleep 0.3; touch '"+gone+"') </dev/null >/dev/null 2>&1 &\n")

	_, _ = pdf.Chromium{Path: fake}.Render(t.Context(), []byte("<p>hi</p>"), "http://127.0.0.1:1")
	if _, err := os.Stat(gone); err != nil {
		t.Error("Render returned with Chromium's helper still running")
	}
}

// Render starts Chromium without its sandbox, which can't start in the app's
// container.
func TestARenderRunsChromiumWithoutItsSandbox(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	fake := filepath.Join(dir, "chromium")
	writeScript(t, fake, "#!/bin/sh\nfor arg; do echo \"$arg\"; done > '"+args+"'\n")

	_, _ = pdf.Chromium{Path: fake}.Render(t.Context(), []byte("<p>hi</p>"), "http://127.0.0.1:1")
	b, err := os.ReadFile(args)
	if err != nil {
		t.Fatalf("fake Chromium never ran: %v", err)
	}
	if got := strings.Split(strings.TrimSpace(string(b)), "\n"); !slices.Contains(got, "--no-sandbox") {
		t.Errorf("Chromium started without --no-sandbox; args: %q", got)
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
	writeScript(t, path, "#!/bin/sh\n")
	return path
}

// writeScript writes an executable script to path holding syscall.ForkLock,
// as a parallel test's fork would otherwise inherit the file still open for
// writing and leave it unrunnable, "text file busy", until that child execs
// (golang/go#22315).
func writeScript(t *testing.T, path, script string) {
	t.Helper()
	var err error
	func() {
		syscall.ForkLock.RLock()
		defer syscall.ForkLock.RUnlock()
		err = os.WriteFile(path, []byte(script), 0o755)
	}()
	if err != nil {
		t.Fatal(err)
	}
}

// A TMPDIR such as /tmp has room for Chromium's socket in a render's own
// directory: the check passes it without a word.
//
//nolint:paralleltest // sets TMPDIR
func TestCheckTMPDIRPassesAShortOne(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp")

	if warning, err := pdf.CheckTMPDIR(); warning != "" || err != nil {
		t.Errorf("CheckTMPDIR() under /tmp = %q, %v; want neither", warning, err)
	}
}

// A 58-byte TMPDIR has room for Chromium's socket in TMPDIR itself, not in a
// render's own directory under it: the check warns that a render killed for
// time may leave Chromium's files in TMPDIR, naming it and its length.
//
//nolint:paralleltest // sets TMPDIR
func TestCheckTMPDIRWarnsOfALongOne(t *testing.T) {
	tmp := "/" + strings.Repeat("t", 57)
	t.Setenv("TMPDIR", tmp)

	warning, err := pdf.CheckTMPDIR()
	if err != nil {
		t.Fatalf("CheckTMPDIR() under the 58-byte TMPDIR: %v; want only a warning", err)
	}
	for _, want := range []string{tmp, "58 bytes", "org.chromium.Chromium.*"} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning %q doesn't name %s", warning, want)
		}
	}
}

// A TMPDIR too long for Chromium's socket even in TMPDIR itself, which with
// the socket's 45 bytes under it can be at most 107, is an error naming it and
// that limit: Chromium can't render under it at all.
//
//nolint:paralleltest // sets TMPDIR
func TestCheckTMPDIRRefusesOneTooLongForChromium(t *testing.T) {
	longest := "/" + strings.Repeat("t", 61)
	t.Setenv("TMPDIR", longest)
	if _, err := pdf.CheckTMPDIR(); err != nil {
		t.Fatalf("CheckTMPDIR() under the 62-byte TMPDIR: %v; want only a warning", err)
	}

	tmp := longest + "t"
	t.Setenv("TMPDIR", tmp)
	warning, err := pdf.CheckTMPDIR()
	if err == nil {
		t.Fatalf("CheckTMPDIR() under the 63-byte TMPDIR = %q and no error", warning)
	}
	for _, want := range []string{tmp, "62 bytes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't name %s", err, want)
		}
	}
}
