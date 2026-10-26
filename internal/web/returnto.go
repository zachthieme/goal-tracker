package web

import (
	"net/http"
	"strings"
)

// returnTo is the page a form sends someone back to: its posted return, when
// localPath accepts it, or else fallback, the route's own page. Every redirect
// or Back built from a posted page goes through it, so none leaves the app.
func returnTo(r *http.Request, fallback string) string {
	if back := r.FormValue("return"); localPath(back) {
		return back
	}
	return fallback
}

// localPath reports whether p is a path on this site, safe to redirect to.
// Browsers drop tabs and newlines and read a backslash as a slash, so any of
// them could turn "/…" into "//other-site"; such a path is refused outright.
func localPath(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return false
	}
	return !strings.ContainsFunc(p, func(r rune) bool { return r <= ' ' || r == 0x7f || r == '\\' })
}
