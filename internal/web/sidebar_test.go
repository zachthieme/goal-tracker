package web_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// sidebar is the signed-in page's sidebar, failing the test when it has none.
func sidebar(t *testing.T, page string) string {
	t.Helper()
	return between(t, page, `<header class="sidebar">`, "</header>")
}

// Every signed-in page sits in the flat frame: a sidebar on the left holding,
// in order, the app's name linking Home, a Search that asks the Goal list, the
// nav with its counts, and the person's menu at its foot, with Theme and Sign
// out in it. No teal bar runs across the top (DESIGN.md § Sidebar).
func TestSidebarFramesEverySignedInPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/home", "/goals", "/follow-ups", "/reports"} {
		page := getBody(t, client, ts.URL+path)
		if strings.Contains(page, `class="nav"`) {
			t.Errorf("%s still has the teal nav bar the sidebar replaced", path)
		}
		bar := sidebar(t, page)
		order := []string{`class="brand"`, `<form class="sb-search"`, `data-testid="nav-home"`, `data-testid="nav-reports"`, `data-testid="person-menu"`}
		for i := 1; i < len(order); i++ {
			if a, b := strings.Index(bar, order[i-1]), strings.Index(bar, order[i]); a < 0 || b < a {
				t.Errorf("on %s the sidebar doesn't hold %s then %s:\n%s", path, order[i-1], order[i], bar)
			}
		}
		if brand := tagAround(t, bar, `class="brand"`); attr(brand, "href") != "/home" {
			t.Errorf("on %s the app's name doesn't lead Home: %s", path, brand)
		}
		search := between(t, bar, `<form class="sb-search"`, "</form>")
		if !strings.Contains(search, `method="get"`) || !strings.Contains(search, `action="/goals"`) || !strings.Contains(search, `type="search" name="q"`) {
			t.Errorf("on %s the Search isn't a GET of q to /goals: %s", path, search)
		}
		if home := pageElement(t, bar, "a", "nav-home"); !strings.Contains(home, `<span class="count needs-you">1</span>`) {
			t.Errorf("on %s Home's count isn't in the sidebar: %s", path, home)
		}
		menu := between(t, bar, `<details data-testid="person-menu"`, "</details>")
		for _, want := range []string{`data-testid="current-user"`, `data-testid="theme-menu"`, `action="/signout"`, "Sign out"} {
			if !strings.Contains(menu, want) {
				t.Errorf("on %s the person menu lacks %s:\n%s", path, want, menu)
			}
		}
		if !strings.Contains(page, `<main class="page">`) {
			t.Errorf("%s does not wrap its content in the page", path)
		}
	}
}

// My org lists the people directly under the viewer by Manager, by Name, each
// linking to their Org on Goals. Someone further down isn't listed, and
// someone with no one under them gets no My org at all.
func TestSidebarMyOrgListsThePeopleDirectlyUnderTheViewer(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	lead := h.SignInNamed("lead@example.com", "Lee Lead")
	ben := h.SignInNamed("ben@example.com", "Ben")
	ana := h.SignInNamed("ana@example.com", "Ana")
	cy := h.SignInNamed("cy@example.com", "Cy")
	h.SetManager(ben, lead)
	h.SetManager(ana, lead)
	h.SetManager(cy, ana)

	org := pageElement(t, sidebar(t, getBody(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/goals")), "section", "sidebar-org")
	if !strings.Contains(org, ">My org</h2>") {
		t.Errorf("My org has no heading:\n%s", org)
	}
	var got []string
	for _, link := range strings.Split(org, `<a data-testid="sidebar-org-person"`)[1:] {
		got = append(got, attr(link, "href")+" "+link[strings.Index(link, ">")+1:strings.Index(link, "</a>")])
	}
	want := []string{fmt.Sprintf("/goals?org=%d Ana", ana.ID), fmt.Sprintf("/goals?org=%d Ben", ben.ID)}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("My org lists %q, want %q", got, want)
	}

	for _, who := range []string{"cy@example.com", "sam@example.com"} {
		h.SignIn(who)
		if page := getBody(t, signInClient(t, ts.URL, who), ts.URL+"/goals"); strings.Contains(page, `data-testid="sidebar-org"`) {
			t.Errorf("%s has no one under them but gets My org:\n%s", who, sidebar(t, page))
		}
	}
}

// sidebarNavCountsIn is the counts shown under root, Home's needs-you pill and
// every nav item's plain count, as their text.
func sidebarNavCountsIn(root *html.Node) []string {
	return textsOf(findAll(root, func(n *html.Node) bool {
		return slices.Contains(strings.Fields(attrOf(n, "class")), "count")
	}))
}

// When a read only the needs-you count uses fails, here the Handoffs, the
// page still answers 200 and leaves out only Home's count: the Follow-ups
// count and My org still show. The failure is logged once, at WARN, with its
// cause.
func TestSidebarLeavesOutOnlyWhatAFailedReadFeeds(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts, buf := loggedServer(t, h)
	lead := h.SignInNamed("lead@example.com", "Lee Lead")
	h.SetManager(h.SignInNamed("ben@example.com", "Ben"), lead)
	h.ActiveGoal(lead, "Cut churn", "Customers leave.")
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "lead@example.com")
	if _, err := h.DB.ExecContext(t.Context(), `DROP TABLE handoffs`); err != nil {
		t.Fatalf("drop handoffs: %v", err)
	}
	buf.Reset()

	resp, err := ctxGet(t, client, ts.URL+"/goals")
	if err != nil {
		t.Fatalf("GET /goals: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /goals: status %d, want 200", resp.StatusCode)
	}
	if got := sidebarNavCountsIn(findByTestID(t, page, "nav-follow-ups")); !slices.Equal(got, []string{"1"}) {
		t.Errorf("the Follow-ups item counts %q, want [1]", got)
	}
	if got := sidebarNavCountsIn(findByTestID(t, page, "nav-home")); len(got) != 0 {
		t.Errorf("Home counts %q though what needs the viewer wasn't read, want no count", got)
	}
	if strip := findAll(page, func(n *html.Node) bool { return attrOf(n, "class") == "sb-strip-home" }); len(strip) != 0 {
		t.Errorf("the phone strip shows Home's count %q though it wasn't read", textsOf(strip))
	}
	if got := textsOf(findAllByTestID(findByTestID(t, page, "sidebar-org"), "sidebar-org-person")); !slices.Equal(got, []string{"Ben"}) {
		t.Errorf("My org lists %q, want [Ben]", got)
	}

	var warns []map[string]any
	for _, rec := range logRecords(t, buf) {
		if rec["level"] == "WARN" {
			warns = append(warns, rec)
		}
	}
	if len(warns) != 1 {
		t.Fatalf("logged %d WARN records, want 1:\n%s", len(warns), buf)
	}
	if cause, _ := warns[0]["err"].(string); !strings.Contains(cause, "handoffs") {
		t.Errorf("WARN record err %q, want the Handoffs read's error", cause)
	}
}
