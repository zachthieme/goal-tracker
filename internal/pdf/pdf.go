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
// broken PDF, and when ctx ends, and leaves no file behind.
func (c Chromium) Render(ctx context.Context, html []byte, baseURL string) ([]byte, error) {
	app, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("base URL %q: %w", baseURL, err)
	}
	dir, err := os.MkdirTemp("", "goal-tracker-pdf-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	pageURL, stop, err := servePage(html, app)
	if err != nil {
		return nil, err
	}
	defer stop()

	out := filepath.Join(dir, "page.pdf")
	cmd := exec.CommandContext(ctx, c.Path,
		"--headless",
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--no-pdf-header-footer",
		// Its profile in dir, so a render killed for time leaves nothing
		// behind; incognito, as with a profile of its own Chromium otherwise
		// never prints.
		"--incognito",
		"--user-data-dir="+filepath.Join(dir, "profile"),
		"--print-to-pdf="+out,
		pageURL,
	)
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

// servePage serves html at a URL on the loopback interface, and every other
// path from the app, until stop is called.
func servePage(html []byte, app *url.URL) (pageURL string, stop func(), err error) {
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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
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
