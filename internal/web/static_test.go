package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The shared stylesheet is served as CSS, cacheable, without signing in.
func TestStylesheetIsServedAsCSS(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	resp, err := http.Get(ts.URL + "/static/app.css")
	if err != nil {
		t.Fatalf("GET /static/app.css: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type %q, want text/css", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Errorf("Cache-Control %q, want a max-age", cc)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, ".navitem.on{") {
		t.Errorf("stylesheet missing the nav rules; body:\n%s", body)
	}
}
