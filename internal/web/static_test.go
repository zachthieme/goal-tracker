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
		{"/dimensions", ""},
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
		for _, item := range []string{"nav-home", "nav-risks", "nav-reports"} {
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

// On a touch screen every button and text-like field is at least a 44px
// target, and all of them grow to the same floor so a button beside an input
// still lines up. A mouse keeps the 40px and 32px sizes.
func TestTouchTargetsAreAtLeast44px(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	coarse := mediaBlock(t, css, "@media (pointer:coarse)")
	floor := ""
	for _, sel := range []string{
		".btn", ".btn.sm",
		"input[type=text]", "input[type=email]", "input[type=date]",
		"input[type=number]", "input[type=search]", "select",
	} {
		decls, ok := ruleFor(coarse, sel)
		if !ok {
			t.Errorf("on a coarse pointer nothing sizes %s", sel)
			continue
		}
		if !strings.Contains(decls, "min-height:44px") {
			t.Errorf("on a coarse pointer %s is not raised to 44px: %s", sel, decls)
		}
		if floor != "" && decls != floor {
			t.Errorf("on a coarse pointer %s declares %q, others %q, so a row no longer shares a height", sel, decls, floor)
		}
		floor = decls
	}
	if _, ok := ruleFor(coarse, "textarea"); ok {
		t.Errorf("the coarse-pointer floor reaches textareas, which are already taller: %s", coarse)
	}

	for sel, want := range map[string]string{".btn": "height:40px", ".btn.sm": "height:32px"} {
		if decls := cssRule(t, css, "\n"+sel); !strings.Contains(decls, want) {
			t.Errorf("desktop %s lost %s: %s", sel, want, decls)
		}
	}
}

// mediaBlock returns the body of the stylesheet's media block opened by query.
func mediaBlock(t *testing.T, css, query string) string {
	t.Helper()
	at := strings.Index(css, query+"{")
	if at < 0 {
		t.Fatalf("stylesheet has no %s block", query)
	}
	start := at + len(query) + 1
	depth := 1
	for i := start; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return css[start:i]
			}
		}
	}
	t.Fatalf("%s block is never closed", query)
	return ""
}

// ruleFor returns the declarations of the rule in block whose selector list
// names sel.
func ruleFor(block, sel string) (string, bool) {
	for _, rule := range strings.Split(block, "}") {
		selectors, decls, ok := strings.Cut(rule, "{")
		if !ok {
			continue
		}
		for _, s := range strings.Split(selectors, ",") {
			if strings.TrimSpace(s) == sel {
				return decls, true
			}
		}
	}
	return "", false
}
