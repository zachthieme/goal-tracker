package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed static
var staticFS embed.FS

// stylesheetURL is the stylesheet's address, fingerprinted by its content so a
// browser may cache it for a long time yet fetch a new one after every change.
var stylesheetURL = "/static/app.css?v=" + fingerprint("static/app.css")

// htmxURL is htmx's address, fingerprinted like the stylesheet's. The fonts
// are named by package and never edited, so they need no fingerprint.
var htmxURL = "/static/htmx.min.js?v=" + fingerprint("static/htmx.min.js")

func fingerprint(name string) string {
	b, err := staticFS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}

// staticFileTypes is the Content-Type of each kind of file under static/, set
// here rather than looked up in the host's MIME table, which a minimal
// container may lack. A file of any other kind is not served.
var staticFileTypes = map[string]string{
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".woff2": "font/woff2",
	".md":    "text/plain; charset=utf-8",
}

// handleStaticFile serves a file from the embedded static/ directory: the
// stylesheet, htmx, and the fonts with their licences. A directory or a
// missing file is a 404, never a listing or a redirect. The stylesheet's and
// htmx's links carry a content fingerprint and the fonts never change, so a
// year-long cache never serves a stale one.
func (s *Server) handleStaticFile(w http.ResponseWriter, r *http.Request) {
	name := "static/" + strings.TrimPrefix(r.URL.Path, "/static/")
	contentType, ok := staticFileTypes[path.Ext(name)]
	if !ok {
		http.NotFound(w, r)
		return
	}
	b, err := staticFS.ReadFile(name) // fails on a directory as on a missing file
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}
