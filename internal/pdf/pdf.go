// Package pdf renders HTML pages to PDF files with a headless Chromium: how a
// published Report downloads as a PDF.
package pdf

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// chromeNames are the Chromium binaries Find looks for on PATH, in order.
var chromeNames = []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"}

// Find is the Chromium to render with. A named one (GOAL_TRACKER_CHROME's
// value, a path or a name on PATH) must exist and be executable, or Find
// returns an error naming it. Unnamed, it is the first of chromium,
// chromium-browser, google-chrome and google-chrome-stable on PATH, or "" when
// there's none.
func Find(named string) (string, error) {
	if named != "" {
		path, err := exec.LookPath(named)
		if err != nil {
			return "", fmt.Errorf("%s can't run: %w", named, err)
		}
		return path, nil
	}
	for _, name := range chromeNames {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", nil
}

// Chromium renders pages to PDF with the headless Chromium at Path.
type Chromium struct {
	Path string
}

// Render prints the page html to a PDF, as Chromium's print to PDF does, its
// assets, such as /static/fonts/, loaded from the app at baseURL. Chromium
// loads the page from a server of its own on the loopback interface, at a
// path no one can guess, which hands every other path to the app: so the
// page's fonts load from its own origin, as a font must, and the app needn't
// serve the page to a browser without a session. It fails rather than giving a
// broken PDF, and when ctx ends, and leaves no Chromium process running. It
// leaves no file behind when TMPDIR is short enough for Chromium's socket in a
// render's own directory, as CheckTMPDIR, which startup runs, tells: under a
// longer TMPDIR, a render killed for time may leave org.chromium.Chromium.*
// there.
func (c Chromium) Render(ctx context.Context, html []byte, baseURL string) ([]byte, error) {
	app, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("base URL %q: %w", baseURL, err)
	}
	// Named short, as it's Chromium's TMPDIR, and a socket's path can't be long.
	dir, err := os.MkdirTemp("", renderDir)
	if err != nil {
		return nil, err
	}
	// Every process the render starts runs with render in its environment, so
	// dir goes only once they're all gone: none is left running, and none is
	// left writing to a dir that's gone, which crashes it.
	render := "GOAL_TRACKER_PDF_RENDER=" + dir
	defer func() {
		awaitExit(render, 2*time.Second)
		_ = os.RemoveAll(dir)
	}()
	env := append(os.Environ(), render)
	// Chromium's scratch files go in dir as its TMPDIR, so a render killed for
	// time leaves nothing behind: its socket as the profile's one browser, in
	// a directory it removes only as it exits, and the shared memory it keeps
	// on disk without /dev/shm, which a helper killed mid-way leaves. Only
	// where TMPDIR is too long for Chromium's socket in dir does it keep them
	// in TMPDIR.
	if len(filepath.Join(dir, chromiumSocket)) <= maxSocketPath {
		env = append(env, "TMPDIR="+dir)
	}

	pageURL, stop, err := servePage(ctx, html, app)
	if err != nil {
		return nil, err
	}
	defer stop()

	out := filepath.Join(dir, "page.pdf")
	cmd := exec.CommandContext(ctx, c.Path,
		"--headless",
		// The app runs Chromium as a non-root user in a container, where
		// Chromium's sandbox can't start without a seccomp profile that allows
		// it, and setting one up is the operator's work.
		"--no-sandbox",
		// The print page needs no script, and without JavaScript no markup
		// that ever reached it unescaped can run.
		"--disable-javascript",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--no-pdf-header-footer",
		// Its profile in dir too; incognito, as with a profile of its own
		// Chromium otherwise never prints.
		"--incognito",
		"--user-data-dir="+filepath.Join(dir, "profile"),
		"--print-to-pdf="+out,
		pageURL,
	)
	cmd.Env = env
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	cmd.WaitDelay = 5 * time.Second // Chromium's helpers may hold its output open after it's killed
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, fmt.Errorf("%s: %w%s", c.Path, err, lastLine(output.String()))
	}
	b, err := os.ReadFile(out)
	if err != nil || !bytes.HasPrefix(b, []byte("%PDF-")) {
		return nil, fmt.Errorf("%s wrote no PDF%s", c.Path, lastLine(output.String()))
	}
	return b, nil
}

// chromiumSocket is the socket Chromium makes in its TMPDIR, as the profile's
// one browser: its path, like any socket's on Linux, can be no longer than
// maxSocketPath, or Chromium aborts.
const (
	chromiumSocket = "org.chromium.Chromium.XXXXXX/SingletonSocket"
	maxSocketPath  = 107
)

// CheckTMPDIR is whether renders can run under TMPDIR, and leave nothing
// behind when killed for time: nothing to say when it has room for Chromium's
// socket in a render's own directory; a warning, naming TMPDIR and its length,
// when it has room for the socket only in TMPDIR itself, where a render killed
// for time may leave it; and an error, naming TMPDIR and the limit, when it
// hasn't room even there, and Chromium can't render at all.
func CheckTMPDIR() (warning string, err error) {
	tmp := os.TempDir()
	if len(filepath.Join(tmp, longestRenderDir, chromiumSocket)) <= maxSocketPath {
		return "", nil
	}
	if limit := maxSocketPath - len("/"+chromiumSocket); len(filepath.Join(tmp, chromiumSocket)) > maxSocketPath {
		return "", fmt.Errorf("TMPDIR %s is %d bytes, too long for Chromium's socket, which needs it at most %d bytes: set TMPDIR to a short directory, such as /tmp", tmp, len(tmp), limit)
	}
	return fmt.Sprintf("TMPDIR %s is %d bytes, too long for Chromium's socket in a render's own directory: a render killed for time may leave org.chromium.Chromium.* in TMPDIR. Set TMPDIR to a short directory, such as /tmp", tmp, len(tmp)), nil
}

// renderDir is the name of a render's own directory in TMPDIR, as
// os.MkdirTemp's pattern; longestRenderDir is the longest it makes of it, with
// a uint32 in decimal after.
const (
	renderDir        = "pdf-"
	longestRenderDir = renderDir + "4294967295"
)

// awaitExit waits, for at most limit, until no process runs with env, a
// variable such as NAME=value, among its environment.
func awaitExit(env string, limit time.Duration) {
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if !runningWith(env) {
			return
		}
	}
}

// runningWith is whether a process runs with env among its environment, as
// /proc tells: where there's no /proc, none does.
func runningWith(env string) bool {
	environs, _ := filepath.Glob("/proc/[0-9]*/environ")
	for _, environ := range environs {
		b, err := os.ReadFile(environ)
		if err != nil {
			continue
		}
		for kv := range bytes.SplitSeq(b, []byte{0}) {
			if string(kv) == env {
				return true
			}
		}
	}
	return false
}

// servePage serves html at a URL on the loopback interface, and every other
// path from the app, until stop is called.
func servePage(ctx context.Context, html []byte, app *url.URL) (pageURL string, stop func(), err error) {
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	pagePath := "/" + hex.EncodeToString(secret)

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(app) },
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != pagePath {
				proxy.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(html)
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String() + pagePath, func() { _ = srv.Close() }, nil
}

// lastLine is the last line Chromium printed, to say why a render failed, or
// "" when it printed nothing.
func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return ": " + last
	}
	return ""
}
