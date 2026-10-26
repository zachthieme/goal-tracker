package web

import (
	"net/http"

	"github.com/a-h/templ"
)

// isHTMX reports whether r was sent by htmx. It is the only reader of the
// HX-Request header: a handler that answers htmx differently, such as with
// a fragment in place of a redirect, asks it.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// answerSwap answers with fragment for htmx to swap in place of its target,
// at 200 even when the fragment carries a refusal: htmx swaps no error status
// by default, so a refused form at 422 would never be shown. Callers reach it
// only on an htmx path, but it answers a plain post the same way.
func answerSwap(w http.ResponseWriter, r *http.Request, fragment templ.Component) {
	render(w, r, http.StatusOK, fragment)
}

// answerRedirect sends the reader to target: htmx is told by HX-Redirect, at
// 200, so it loads the whole page there; a plain post follows a 303.
func answerRedirect(w http.ResponseWriter, r *http.Request, target string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// answerPushedPage answers with a whole page, at url. htmx gets it at 200 for
// answerSwap's reason, with HX-Push-Url so the address bar names the page it
// swapped in; a plain post gets it at status.
func answerPushedPage(w http.ResponseWriter, r *http.Request, status int, page templ.Component, url string) {
	if isHTMX(r) {
		w.Header().Set("HX-Push-Url", url)
		answerSwap(w, r, page)
		return
	}
	render(w, r, status, page)
}

// answerUnswapped answers with fragment for htmx to show without swapping its
// target (HX-Reswap: none), such as a save status beside a form whose typing
// must stay, at 200 for answerSwap's reason. It answers a plain post the same.
func answerUnswapped(w http.ResponseWriter, r *http.Request, fragment templ.Component) {
	w.Header().Set("HX-Reswap", "none")
	answerSwap(w, r, fragment)
}
