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

// On a narrow screen nothing scrolls sideways: not the top bar, which used to
// scroll Sign out out of view, and not the page as a whole.
func TestNarrowScreensDoNotScrollSideways(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	for _, sel := range []string{"html", "body"} {
		if decls := cssRule(t, css, "\n"+sel); !strings.Contains(decls, "overflow-x:clip") {
			t.Errorf("%s does not clip horizontal overflow: %s", sel, decls)
		}
	}
	for _, block := range mediaBlocks(css, "@media (max-width:900px)") {
		if decls, ok := ruleFor(block, ".nav"); ok && strings.Contains(decls, "overflow-x:auto") {
			t.Errorf("on a narrow screen the top bar scrolls sideways: %s", decls)
		}
	}
}

// On a narrow screen the top bar takes two rows: the brand, the person and
// Sign out on the first, the nav items on a row of their own below. Each label
// stays on one line. Above the breakpoint the bar is one 56px row.
func TestNarrowTopBarPutsNavItemsOnASecondRow(t *testing.T) {
	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	nav := page[strings.Index(page, `<nav class="nav">`):strings.Index(page, `</nav>`)]
	items := pageElement(t, nav, "div", "nav-items")
	for _, item := range []string{"nav-home", "nav-risks", "nav-reports"} {
		if !strings.Contains(items, `data-testid="`+item+`"`) {
			t.Errorf("%s is not in the nav items' row: %s", item, items)
		}
	}
	if strings.Contains(items, `class="me"`) {
		t.Errorf("the person and Sign out share the nav items' row: %s", items)
	}

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if decls := cssRule(t, css, "\n.nav"); !strings.Contains(decls, "height:56px") || strings.Contains(decls, "wrap") {
		t.Errorf("above the breakpoint the top bar is no longer one 56px row: %s", decls)
	}
	if decls := cssRule(t, css, "\n.navitem"); !strings.Contains(decls, "white-space:nowrap") {
		t.Errorf("a nav label can wrap to two lines: %s", decls)
	}
	var narrow string
	for _, block := range mediaBlocks(css, "@media (max-width:900px)") {
		if decls, ok := ruleFor(block, ".nav"); ok {
			narrow += decls
		}
		if decls, ok := ruleFor(block, ".navitems"); ok {
			narrow += decls
		}
	}
	for _, want := range []string{"flex-wrap:wrap", "100%", "order:1"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("on a narrow screen the nav items do not take a row of their own below (want %s): %s", want, narrow)
		}
	}
}

// On a narrow screen the top bar shows the person by Name alone: "Signed in
// as" and "(Admin)" are hidden visually but stay on the page for screen readers,
// and a long Name truncates rather than push Sign out off the row.
func TestNarrowTopBarShowsThePersonByName(t *testing.T) {
	h := testsupport.New(t, "ada@example.com")
	h.SignIn("ada@example.com")
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "ada@example.com"), ts.URL+"/home")

	me := pageElement(t, page, "span", "current-user")
	if !strings.Contains(openTag(me), `class="who"`) {
		t.Errorf("the signed-in person is not marked for the narrow layout: %s", openTag(me))
	}
	for _, want := range []string{"Signed in as " + shownAs("ada@example.com", "ada"), "(Admin)"} {
		if !strings.Contains(page, want) {
			t.Errorf("the top bar lost %q:\n%s", want, me)
		}
	}

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	var who, person, row string
	for _, block := range mediaBlocks(css, "@media (max-width:900px)") {
		if decls, ok := ruleFor(block, ".who"); ok {
			who += decls
		}
		if decls, ok := ruleFor(block, ".who .person"); ok {
			person += decls
		}
		if decls, ok := ruleFor(block, ".me"); ok {
			row += decls
		}
	}
	if !strings.Contains(who, "font-size:0") || strings.Contains(who, "display:none") || strings.Contains(who, "visibility:hidden") {
		t.Errorf("on a narrow screen the words around the Name are not hidden visually, or are hidden from screen readers: %s", who)
	}
	for _, want := range []string{"font-size:15px", "text-overflow:ellipsis", "white-space:nowrap", "overflow:hidden"} {
		if !strings.Contains(person, want) {
			t.Errorf("on a narrow screen the Name is not shown truncated (want %s): %s", want, person)
		}
	}
	if !strings.Contains(row, "min-width:0") || !strings.Contains(who, "min-width:0") {
		t.Errorf("on a narrow screen a long Name cannot shrink, so it pushes Sign out off the row: .me %s .who %s", row, who)
	}
}

// mediaBlocks returns the body of every media block the stylesheet opens with
// query, in order.
func mediaBlocks(css, query string) []string {
	var blocks []string
	for {
		at := strings.Index(css, query+"{")
		if at < 0 {
			return blocks
		}
		css = css[at+len(query)+1:]
		depth := 1
		for i := 0; i < len(css); i++ {
			if css[i] == '{' {
				depth++
			} else if css[i] == '}' {
				if depth--; depth == 0 {
					blocks = append(blocks, css[:i])
					css = css[i:]
					break
				}
			}
		}
		if depth != 0 {
			return blocks
		}
	}
}

// mediaBlock returns the body of the stylesheet's first media block opened by
// query.
func mediaBlock(t *testing.T, css, query string) string {
	t.Helper()
	blocks := mediaBlocks(css, query)
	if len(blocks) == 0 {
		t.Fatalf("stylesheet has no closed %s block", query)
	}
	return blocks[0]
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
