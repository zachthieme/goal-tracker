package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// stylesheetURL is the stylesheet's address, fingerprinted by its content so a
// browser may cache it for a long time yet fetch a new one after every change.
var stylesheetURL = "/static/app.css?v=" + fingerprint("static/app.css")

func fingerprint(name string) string {
	b, err := staticFS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}

// handleStylesheet serves the shared stylesheet. Its link carries a content
// fingerprint, so a year-long cache never serves a stale one.
func (s *Server) handleStylesheet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	http.ServeFileFS(w, r, staticFS, "static/app.css")
}
