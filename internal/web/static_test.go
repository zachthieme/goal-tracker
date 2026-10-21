package web_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The shared stylesheet is served as CSS, cacheable, without signing in.
func TestStylesheetIsServedAsCSS(t *testing.T) {
	t.Parallel()

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
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000" {
		t.Errorf("Cache-Control %q, want public, max-age=31536000", cc)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, ".navitem.on{") {
		t.Errorf("stylesheet missing the nav rules; body:\n%s", body)
	}
}

// htmx is served from the app, the exact release the pages were written
// against, so a browser that can't reach a CDN still gets the in-page
// interactions.
func TestHtmxIsServedFromTheApp(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	resp, err := http.Get(ts.URL + "/static/htmx.min.js")
	if err != nil {
		t.Fatalf("GET /static/htmx.min.js: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type %q, want text/javascript; charset=utf-8", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000" {
		t.Errorf("Cache-Control %q, want public, max-age=31536000", cc)
	}
	sum := sha256.Sum256([]byte(readBody(t, resp)))
	if got, want := hex.EncodeToString(sum[:]), "491955cd1810747d7d7b9ccb936400afb760e06d25d53e4572b64b6563b2784e"; got != want {
		t.Errorf("htmx.min.js SHA-256 %s, want %s (htmx 2.0.3)", got, want)
	}
}

// Every font face the pages use is served from the app as a woff2 font.
func TestFontsAreServedFromTheApp(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	for _, name := range []string{
		"inter-latin-wght-normal.woff2",
		"inter-latin-ext-wght-normal.woff2",
		"source-serif-4-latin-wght-normal.woff2",
		"source-serif-4-latin-ext-wght-normal.woff2",
		"source-serif-4-latin-wght-italic.woff2",
		"source-serif-4-latin-ext-wght-italic.woff2",
	} {
		resp, err := http.Get(ts.URL + "/static/fonts/" + name)
		if err != nil {
			t.Fatalf("GET %s: %v", name, err)
		}
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d, want 200", name, resp.StatusCode)
			continue
		}
		if ct := resp.Header.Get("Content-Type"); ct != "font/woff2" {
			t.Errorf("%s: Content-Type %q, want font/woff2", name, ct)
		}
		if !strings.HasPrefix(body, "wOF2") {
			t.Errorf("%s is not a woff2 file", name)
		}
	}
}

// The flat frame sets everything in Inter, so the faces it retired, Plus
// Jakarta Sans and JetBrains Mono, are no longer served.
func TestRetiredFontsAreGone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	for _, name := range []string{
		"plus-jakarta-sans-latin-wght-normal.woff2",
		"plus-jakarta-sans-latin-ext-wght-normal.woff2",
		"jetbrains-mono-latin-500-normal.woff2",
		"jetbrains-mono-latin-ext-500-normal.woff2",
	} {
		resp, err := http.Get(ts.URL + "/static/fonts/" + name)
		if err != nil {
			t.Fatalf("GET %s: %v", name, err)
		}
		readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", name, resp.StatusCode)
		}
	}
}

// Under /static/ the app serves files only: a directory, with or without its
// trailing slash, or a missing file is not found — never a listing of what is
// there, nor a redirect to one.
func TestStaticServesFilesOnly(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	client := noRedirects(http.DefaultClient)

	for _, p := range []string{"/static/", "/static/fonts/", "/static/fonts", "/static/nope.js"} {
		resp, err := client.Get(ts.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404; Location %q; body:\n%s", p, resp.StatusCode, resp.Header.Get("Location"), body)
		}
		if strings.Contains(body, "app.css") || strings.Contains(body, ".woff2") {
			t.Errorf("%s lists the directory:\n%s", p, body)
		}
	}
}

// No page loads anything from another host: every stylesheet and script a
// signed-in page or a Report's Print view links is the app's own, htmx among
// them with a fingerprint, so a browser that can't reach a CDN gets the whole
// app.
func TestPagesLoadNothingFromAnotherHost(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	tags := regexp.MustCompile(`<(?:link|script)\b[^>]*>`)
	home := getBody(t, client, ts.URL+"/home")
	printed := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")
	for path, page := range map[string]string{"/home": home, "a Print view": printed} {
		for _, tag := range tags.FindAllString(page, -1) {
			if strings.Contains(attr(tag, "href"), "https://") || strings.Contains(attr(tag, "src"), "https://") {
				t.Errorf("%s loads from another host: %s", path, tag)
			}
		}
	}
	htmx := tagAround(t, home, "/static/htmx.min.js")
	if !strings.HasPrefix(attr(htmx, "src"), "/static/htmx.min.js?v=") {
		t.Errorf("htmx's script tag carries no fingerprint: %s", htmx)
	}
}

// The faces the pages name are the app's own: the stylesheet declares Inter
// alone, the Print view declares Source Serif 4, and every font file they
// point at is served.
func TestFontFacesPointAtTheAppsFonts(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	printed := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")
	faces := regexp.MustCompile(`@font-face\s*\{[^}]*\}`)
	family := regexp.MustCompile(`font-family:\s*"([^"]+)"`)
	src := regexp.MustCompile(`url\(([^)]*)\)`)
	for where, want := range map[string]struct {
		text     string
		families []string
	}{
		"app.css":        {css, []string{"Inter"}},
		"the Print view": {printed, []string{"Source Serif 4"}},
	} {
		declared := map[string]int{}
		for _, face := range faces.FindAllString(want.text, -1) {
			m := family.FindStringSubmatch(face)
			if m == nil {
				t.Errorf("%s declares a face with no family: %s", where, face)
				continue
			}
			declared[m[1]]++
			u := src.FindStringSubmatch(face)
			if u == nil || !strings.HasPrefix(u[1], "/static/fonts/") {
				t.Errorf("%s declares %s from somewhere other than the app's fonts: %s", where, m[1], face)
				continue
			}
			resp, err := http.Get(ts.URL + u[1])
			if err != nil {
				t.Fatalf("GET %s: %v", u[1], err)
			}
			readBody(t, resp)
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "font/woff2" {
				t.Errorf("%s's %s points at %s, which serves %d %q", where, m[1], u[1], resp.StatusCode, resp.Header.Get("Content-Type"))
			}
		}
		for _, f := range want.families {
			if declared[f] == 0 {
				t.Errorf("%s declares no %s face", where, f)
			}
		}
		if len(declared) != len(want.families) {
			t.Errorf("%s declares faces %v, want only %v", where, declared, want.families)
		}
	}
}

// Every page links the stylesheet and sits in the sidebar frame, whose nav
// marks the page you are on — and only that one — as current.
func TestSidebarMarksTheCurrentPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct{ path, current string }{
		{"/reports", "nav-reports"},
		{"/home", "nav-home"},
		{"/follow-ups", "nav-follow-ups"},
		{"/dimensions", ""},
		{"/goals", "nav-goals"},
	} {
		page := getBody(t, client, ts.URL+tc.path)
		if !strings.Contains(page, `<link rel="stylesheet" href="/static/app.css`) {
			t.Errorf("%s does not link the stylesheet; body:\n%s", tc.path, page)
		}
		if !strings.Contains(page, `<header class="sidebar">`) {
			t.Errorf("%s has no sidebar; body:\n%s", tc.path, page)
		}
		if !strings.Contains(page, `<main class="page">`) {
			t.Errorf("%s does not wrap its content in the page; body:\n%s", tc.path, page)
		}
		for _, item := range []string{"nav-home", "nav-goals", "nav-follow-ups", "nav-reports"} {
			link := pageElement(t, page, "a", item)
			marked := strings.Contains(link, `aria-current="page"`) && strings.Contains(link, `class="navitem on"`)
			if item == tc.current && !marked {
				t.Errorf("on %s, %s is not marked current: %s", tc.path, item, link)
			}
			if item != tc.current && strings.Contains(link, `aria-current`) {
				t.Errorf("on %s, %s is marked current: %s", tc.path, item, link)
			}
		}
		if me := pageElement(t, page, "p", "current-user"); !strings.Contains(me, "sam@example.com") {
			t.Errorf("%s sidebar does not show who is signed in: %s", tc.path, me)
		}
	}
}

// A page below a nav item's section — a Report — still marks that item.
func TestSidebarMarksTheSectionOfANestedPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	boss := h.SignIn("boss@example.com")
	root := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":   {"EU MBR"},
		"mode":   {domain.ReportModePicked},
		"picked": {strconv.FormatInt(root.ID, 10)},
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

// Every page under Goals — a Goal's page, New goal, a Goal's Check-in page —
// marks Goals current, and only Goals.
func TestSidebarMarksGoalsOnPagesUnderIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Grow revenue", "The org needs to grow.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	id := strconv.FormatInt(goal.ID, 10)
	for _, path := range []string{"/goals/" + id, "/goals/new", "/goals/" + id + "/checkin"} {
		page := getBody(t, client, ts.URL+path)
		if link := pageElement(t, page, "a", "nav-goals"); !strings.Contains(link, `aria-current="page"`) || !strings.Contains(link, `class="navitem on"`) {
			t.Errorf("on %s, Goals is not marked current: %s", path, link)
		}
		for _, item := range []string{"nav-home", "nav-follow-ups", "nav-reports"} {
			if link := pageElement(t, page, "a", item); strings.Contains(link, `aria-current`) {
				t.Errorf("on %s, %s is marked current: %s", path, item, link)
			}
		}
	}
}

// On a touch screen every button and text-like field is at least a 44px
// target, and all of them grow to the same floor so a button beside an input
// still lines up. A mouse keeps the 40px and 32px sizes.
func TestTouchTargetsAreAtLeast44px(t *testing.T) {
	t.Parallel()

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

// On a narrow screen nothing scrolls sideways: not the sidebar's strip, and
// not the page as a whole.
func TestNarrowScreensDoNotScrollSideways(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	for _, sel := range []string{"html", "body"} {
		if decls := cssRule(t, css, "\n"+sel); !strings.Contains(decls, "overflow-x:clip") {
			t.Errorf("%s does not clip horizontal overflow: %s", sel, decls)
		}
	}
	for _, block := range mediaBlocks(css, "@media (max-width:900px)") {
		if decls, ok := ruleFor(block, ".sidebar"); ok && strings.Contains(decls, "overflow-x:auto") {
			t.Errorf("on a narrow screen the sidebar's strip scrolls sideways: %s", decls)
		}
	}
}

// At 900px and below the sidebar is a 48px strip: the app's name, Home's
// count, and a Menu disclosure. The rest of the sidebar — Search, the nav, My
// org and the person's menu — is hidden until Menu opens, and then shows as a
// sheet under the strip, all without script. Above the breakpoint the sidebar
// is a 240px column and Menu is hidden.
func TestNarrowSidebarIsAStripWithAMenu(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	bar := between(t, page, `<header class="sidebar">`, "</header>")
	strip := between(t, bar, `class="brand"`, `<div class="sb-body">`)
	if home := between(t, strip, `<a class="sb-strip-home"`, "</a>"); attr(home, "href") != "/home" || !strings.Contains(home, `<span class="count needs-you">1</span>`) {
		t.Errorf("the strip doesn't carry Home's count to Home: %s", home)
	}
	if menu := between(t, strip, `<details class="sb-menu">`, "</details>"); !strings.Contains(menu, ">Menu</summary>") || strings.Contains(menu, "<script") {
		t.Errorf("the strip has no Menu disclosure: %s", menu)
	}
	sheet := between(t, bar, `<div class="sb-body">`, "")
	for _, item := range []string{`<form class="sb-search"`, `data-testid="nav-home"`, `data-testid="person-menu"`} {
		if !strings.Contains(sheet, item) {
			t.Errorf("Menu's sheet lacks %s: %s", item, sheet)
		}
	}

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if decls := cssRule(t, css, "\n.frame"); !strings.Contains(decls, "grid-template-columns:240px") {
		t.Errorf("above the breakpoint the sidebar is not a 240px column: %s", decls)
	}
	if decls, ok := ruleFor(css, ".sb-strip-home"); !ok || !strings.Contains(decls, "display:none") {
		t.Errorf("above the breakpoint the strip's Home count shows: %s", decls)
	}
	var narrow = map[string]string{}
	for _, block := range mediaBlocks(css, "@media (max-width:900px)") {
		for _, sel := range []string{".sidebar", ".sb-menu", ".sb-body", ".sb-menu[open]~.sb-body"} {
			if decls, ok := ruleFor(block, sel); ok {
				narrow[sel] += decls
			}
		}
	}
	for sel, want := range map[string]string{
		".sidebar":                "height:48px",
		".sb-menu":                "display:block",
		".sb-body":                "display:none",
		".sb-menu[open]~.sb-body": "display:flex",
	} {
		if !strings.Contains(narrow[sel], want) {
			t.Errorf("on a narrow screen %s lacks %s: %s", sel, want, narrow[sel])
		}
	}
}

// The person's menu opens from their Name and initials at the sidebar's foot.
// It says who is signed in, with "(Admin)" for an Admin, showing them by Name
// with their email a click away; a long Name truncates in the sidebar.
func TestPersonMenuShowsWhoIsSignedIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	h.SignInNamed("ada@example.com", "Ada Okafor")
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "ada@example.com"), ts.URL+"/home")

	menu := between(t, page, `<details data-testid="person-menu"`, "</details>")
	summary := between(t, menu, "<summary", "</summary>")
	if !strings.Contains(summary, `<span class="avatar" aria-hidden="true">AO</span>`) || !strings.Contains(summary, "Ada Okafor") {
		t.Errorf("the person menu doesn't open from Ada's initials and Name: %s", summary)
	}
	if me := pageElement(t, menu, "p", "current-user"); !strings.Contains(me, "Signed in as "+shownAs("ada@example.com", "Ada Okafor")+" (Admin)") {
		t.Errorf("the person menu doesn't say who is signed in: %s", me)
	}

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	decls := cssRule(t, css, "\n.sb-person-name")
	for _, want := range []string{"min-width:0", "text-overflow:ellipsis", "white-space:nowrap", "overflow:hidden"} {
		if !strings.Contains(decls, want) {
			t.Errorf("a long Name doesn't truncate in the sidebar (want %s): %s", want, decls)
		}
	}
}

// A person's Name never looks like a link: at rest it is plain text in the
// surrounding ink, with no underline. On hover and keyboard focus it takes the
// faint tint of a quiet control, keeping its colour and the focus ring. The
// help buttons, which share the disclose control, keep their dotted edge.
func TestNamesDoNotLookLikeLinks(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	for _, rule := range strings.Split(css, "}") {
		open := strings.LastIndex(rule, "{")
		if open < 0 {
			continue
		}
		selectors := rule[strings.LastIndex(rule[:open], "{")+1 : open]
		if !strings.Contains(selectors, ".person>.disclose") {
			continue
		}
		if decls := rule[open+1:]; strings.Contains(decls, "underline") || strings.Contains(decls, "border-bottom") {
			t.Errorf("a Name is underlined like a link: %s{%s}", strings.TrimSpace(selectors), decls)
		}
	}
	if decls := cssRule(t, css, "\n.disclose"); !strings.Contains(decls, "color:inherit") {
		t.Errorf("a Name no longer takes the surrounding ink: %s", decls)
	}
	for _, sel := range []string{".person>.disclose:hover", ".person>.disclose:focus-visible"} {
		decls, ok := ruleFor(css, sel)
		if !ok {
			t.Errorf("nothing styles %s", sel)
			continue
		}
		if !strings.Contains(decls, "background:var(--color-primary-subtle)") {
			t.Errorf("%s does not take the quiet controls' tint: %s", sel, decls)
		}
		for _, banned := range []string{"outline:none", "outline:0", "color:"} {
			if strings.Contains(decls, banned) {
				t.Errorf("%s declares %s: %s", sel, banned, decls)
			}
		}
	}
	if decls := cssRule(t, css, "\n.help"); decls != "border-bottom:1px dotted var(--color-ink-muted);cursor:help" {
		t.Errorf("the help buttons' dotted edge changed: %s", decls)
	}
}
