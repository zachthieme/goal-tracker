package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a-h/templ"
)

// htmxFragment is a fragment or page that writes only its own text.
func htmxFragment(text string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, text)
		return err
	})
}

// htmxHeaders are the htmx response headers the answers set, or don't.
var htmxHeaders = []string{"HX-Redirect", "HX-Push-Url", "HX-Reswap"}

// assertHTMXAnswer checks w answered status and body, with the htmx
// response headers in want and no others.
func assertHTMXAnswer(t *testing.T, w *httptest.ResponseRecorder, status int, body string, want map[string]string) {
	t.Helper()
	if w.Code != status {
		t.Errorf("status = %d, want %d", w.Code, status)
	}
	if got := w.Body.String(); got != body {
		t.Errorf("body = %q, want %q", got, body)
	}
	for _, h := range htmxHeaders {
		if got := w.Header().Get(h); got != want[h] {
			t.Errorf("%s = %q, want %q", h, got, want[h])
		}
	}
}

// htmxRequest is a POST to path, sent by htmx when hx is true.
func htmxRequest(t *testing.T, path string, hx bool) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil)
	if hx {
		r.Header.Set("HX-Request", "true")
	}
	return r
}

// A request is htmx's only when it says HX-Request: true.
func TestIsHTMXOnlyForHXRequestTrue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		header string
		set    bool
		want   bool
	}{
		{header: "true", set: true, want: true},
		{header: "false", set: true, want: false},
		{header: "", set: true, want: false},
		{set: false, want: false},
	} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/goals/7/checkin", nil)
		if tc.set {
			r.Header.Set("HX-Request", tc.header)
		}
		if got := isHTMX(r); got != tc.want {
			t.Errorf("HX-Request %q (set %v): isHTMX = %v, want %v", tc.header, tc.set, got, tc.want)
		}
	}
}

// A fragment for htmx to swap in answers 200, refusal or not, since htmx
// swaps no error status; it answers the same to a plain post.
func TestAnswerSwapIsTheFragmentAt200(t *testing.T) {
	t.Parallel()

	for _, hx := range []bool{true, false} {
		w := httptest.NewRecorder()
		answerSwap(w, htmxRequest(t, "/goals/7/checkin", hx), htmxFragment("<form>refused</form>"))
		assertHTMXAnswer(t, w, http.StatusOK, "<form>refused</form>", nil)
		if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("htmx %v: Content-Type = %q", hx, got)
		}
	}
}

// htmx follows a redirect told by HX-Redirect at 200, so it reloads the whole
// page; a plain post follows a 303 to the same place.
func TestAnswerRedirectIsHXRedirectForHTMXAndA303Otherwise(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	answerRedirect(w, htmxRequest(t, "/goals/7/checkin", true), "/goals/7")
	assertHTMXAnswer(t, w, http.StatusOK, "", map[string]string{"HX-Redirect": "/goals/7"})
	if got := w.Header().Get("Location"); got != "" {
		t.Errorf("htmx: Location = %q, want none", got)
	}

	w = httptest.NewRecorder()
	answerRedirect(w, htmxRequest(t, "/goals/7/checkin", false), "/goals/7")
	if w.Code != http.StatusSeeOther {
		t.Errorf("plain: status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if got := w.Header().Get("Location"); got != "/goals/7" {
		t.Errorf("plain: Location = %q, want /goals/7", got)
	}
	for _, h := range htmxHeaders {
		if got := w.Header().Get(h); got != "" {
			t.Errorf("plain: %s = %q, want none", h, got)
		}
	}
}

// A whole page answering htmx comes at 200 with its URL pushed, so htmx swaps
// it in and the address bar names it; a plain post gets it at the status asked.
func TestAnswerPushedPageIsThePageAt200WithItsURLForHTMX(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	answerPushedPage(w, htmxRequest(t, "/goals/7/checkin/nochange", true), http.StatusUnprocessableEntity, htmxFragment("<main>page</main>"), "/goals/7/checkin")
	assertHTMXAnswer(t, w, http.StatusOK, "<main>page</main>", map[string]string{"HX-Push-Url": "/goals/7/checkin"})

	w = httptest.NewRecorder()
	answerPushedPage(w, htmxRequest(t, "/goals/7/checkin/nochange", false), http.StatusUnprocessableEntity, htmxFragment("<main>page</main>"), "/goals/7/checkin")
	assertHTMXAnswer(t, w, http.StatusUnprocessableEntity, "<main>page</main>", nil)
}

// A fragment htmx shows without swapping its target comes with HX-Reswap:
// none at 200, to htmx and to a plain post alike.
func TestAnswerUnswappedIsReswapNoneAt200(t *testing.T) {
	t.Parallel()

	for _, hx := range []bool{true, false} {
		w := httptest.NewRecorder()
		answerUnswapped(w, htmxRequest(t, "/reports/3/narrative", hx), htmxFragment("<span>Not saved</span>"))
		assertHTMXAnswer(t, w, http.StatusOK, "<span>Not saved</span>", map[string]string{"HX-Reswap": "none"})
	}
}
