package web_test

import (
	"net/http"
	"net/url"
	"strconv"
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

// Every page links the stylesheet and wears the dark top bar, which marks the
// page you are on — and only that one — as current.
func TestTopBarMarksTheCurrentPage(t *testing.T) {
	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct{ path, current string }{
		{"/reports", "nav-reports"},
		{"/home", "nav-home"},
		{"/dimensions", "nav-dimensions"},
		{"/goals", ""},
	} {
		page := getBody(t, client, ts.URL+tc.path)
		if !strings.Contains(page, `<link rel="stylesheet" href="/static/app.css`) {
			t.Errorf("%s does not link the stylesheet; body:\n%s", tc.path, page)
		}
		if !strings.Contains(page, `<nav class="nav">`) {
			t.Errorf("%s has no top bar; body:\n%s", tc.path, page)
		}
		if !strings.Contains(page, `<main class="page">`) {
			t.Errorf("%s does not wrap its content in the page; body:\n%s", tc.path, page)
		}
		for _, item := range []string{"nav-home", "nav-dimensions", "nav-reports"} {
			link := pageElement(t, page, "a", item)
			marked := strings.Contains(link, `aria-current="page"`) && strings.Contains(link, `class="navitem on"`)
			if item == tc.current && !marked {
				t.Errorf("on %s, %s is not marked current: %s", tc.path, item, link)
			}
			if item != tc.current && strings.Contains(link, `aria-current`) {
				t.Errorf("on %s, %s is marked current: %s", tc.path, item, link)
			}
		}
		if me := pageElement(t, page, "span", "current-user"); !strings.Contains(me, "sam@example.com") {
			t.Errorf("%s top bar does not show who is signed in: %s", tc.path, me)
		}
	}
}

// A page below a nav item's section — a Report — still marks that item.
func TestTopBarMarksTheSectionOfANestedPage(t *testing.T) {
	h := testsupport.New(t)
	boss := h.SignIn("boss@example.com")
	root := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports", url.Values{
		"name": {"EU MBR"},
		"root": {strconv.FormatInt(root.ID, 10)},
	})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save report: status %d; body:\n%s", resp.StatusCode, page)
	}
	if resp.Request.URL.Path == "/reports" {
		t.Fatalf("saving a Report did not land on the Report's own page")
	}
	link := pageElement(t, page, "a", "nav-reports")
	if !strings.Contains(link, `aria-current="page"`) {
		t.Errorf("a Report page does not mark Reports current: %s", link)
	}
}
