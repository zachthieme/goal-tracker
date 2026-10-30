package web_test

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// assertNotFoundPage checks a 404 came back as the Not found page inside the
// site's chrome, with a link back to back (#120).
func assertNotFoundPage(t *testing.T, client *http.Client, rawURL, back string) {
	t.Helper()
	assertNotFoundPageFor(t, client, http.MethodGet, rawURL, back)
}

// assertNotFoundPageFor is assertNotFoundPage for a request with any method,
// and returns the page.
func assertNotFoundPageFor(t *testing.T, client *http.Client, method, rawURL, back string) string {
	t.Helper()
	return assertNotFoundPageWithForm(t, client, method, rawURL, nil, back)
}

// assertNotFoundPageWithForm is assertNotFoundPageFor for a request carrying
// form, as a submit sends it; a nil form sends no body.
func assertNotFoundPageWithForm(t *testing.T, client *http.Client, method, rawURL string, form url.Values, back string) string {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(t.Context(), method, rawURL, body)
	if err != nil {
		t.Fatalf("new %s request: %v", method, err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("%s %s: status %d, want 404", method, rawURL, resp.StatusCode)
	}
	if !strings.Contains(page, "<title>Not found · Goal Tracker</title>") || !strings.Contains(page, `class="brand"`) {
		t.Fatalf("%s %s: not a Not found page inside the site's chrome; body:\n%s", method, rawURL, page)
	}
	element := pageElement(t, page, "p", "not-found")
	sentence := html.UnescapeString(element[strings.Index(element, ">")+1:])
	if sentence != "There's nothing here. It may have been removed, or the link may be wrong." {
		t.Errorf("%s %s: Not found page says %q", method, rawURL, sentence)
	}
	if !strings.Contains(page, `<a href="`+back+`">`) {
		t.Errorf("%s %s: Not found page has no link to %s; body:\n%s", method, rawURL, back, page)
	}
	return page
}

// A signed-in person following a link to a Goal that doesn't exist gets the
// Not found page with a way back to Home.
func TestMissingGoalShowsNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	pat := signInClient(t, ts.URL, "pat@example.com")

	assertNotFoundPage(t, pat, ts.URL+"/goals/999999", "/home")
}

// The Graph signals, Freshness signals and Pending links pages are retired:
// Follow-ups shows the signals and Home the link requests, so a signed-in
// person following an old link to one gets the Not found page (#677).
func TestRetiredSignalsAndPendingLinksPagesAreNotFound(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	pat := signInClient(t, ts.URL, "pat@example.com")

	for _, path := range []string{"/signals", "/freshness", "/links"} {
		assertNotFoundPage(t, pat, ts.URL+path, "/home")
	}
}

// A Goal address that isn't a number names nothing either.
func TestMalformedGoalAddressShowsNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	pat := signInClient(t, ts.URL, "pat@example.com")

	assertNotFoundPage(t, pat, ts.URL+"/goals/abc", "/home")
}

// The Check-in page of a Goal that doesn't exist is the Not found page.
func TestCheckinOnMissingGoalShowsNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	pat := signInClient(t, ts.URL, "pat@example.com")

	assertNotFoundPage(t, pat, ts.URL+"/goals/999999/checkin", "/home")
}

// A Report Definition that doesn't exist is the Not found page.
func TestMissingReportShowsNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	pat := signInClient(t, ts.URL, "pat@example.com")

	assertNotFoundPage(t, pat, ts.URL+"/reports/999999", "/home")
}

// A publication that doesn't exist, or one asked for under a Report Definition
// it wasn't published from, is the Not found page.
func TestMissingPublicationShowsNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com")
	g := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	def := h.SaveReportDefinition(pat, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	other := h.SaveReportDefinition(pat, domain.SaveReportDefinitionInput{Name: "WBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(pat, def)
	client := signInClient(t, ts.URL, "pat@example.com")

	assertNotFoundPage(t, client, fmt.Sprintf("%s/reports/%d/publications/999999", ts.URL, def.ID), "/home")
	assertNotFoundPage(t, client, fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, other.ID, pub.ID), "/home")
}

// The Definition log about a Dimension or Field that doesn't exist is the Not
// found page.
func TestDefinitionLogAboutMissingThingShowsNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	pat := signInClient(t, ts.URL, "pat@example.com")

	assertNotFoundPage(t, pat, ts.URL+"/definition-log?about=dimension:999999", "/home")
	assertNotFoundPage(t, pat, ts.URL+"/definition-log?about=field:999999", "/home")
}

// An address no route matches is the Not found page: back to Home for someone
// signed in, and to sign in for a visitor who isn't.
func TestUnknownAddressShowsNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	pat := signInClient(t, ts.URL, "pat@example.com")

	assertNotFoundPage(t, pat, ts.URL+"/no-such-page", "/home")
	assertNotFoundPage(t, http.DefaultClient, ts.URL+"/no-such-page", "/signin")
}

// The Not found page is plain text centred on the canvas with its link home,
// no card around it, signed in or not (DESIGN.md § Not found page).
func TestNotFoundPageIsPlainCentredText(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	card := regexp.MustCompile(`class="(?:[^"]* )?card(?: [^"]*)?"`)

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if rule := cssRule(t, css, ".message-page"); !strings.Contains(rule, "text-align:center") || !strings.Contains(rule, "margin:64px auto 0") {
		t.Errorf(".message-page{%s} doesn't centre the message", rule)
	}

	for who, client := range map[string]*http.Client{"signed in": signInClient(t, ts.URL, "pat@example.com"), "signed out": http.DefaultClient} {
		resp, err := ctxGet(t, client, ts.URL+"/no-such-page")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		page := readBody(t, resp)
		if tag := tagAround(t, page, `class="message-page nf-message"`); !strings.HasPrefix(tag, "<div") {
			t.Errorf("%s, the Not found message isn't its own block: %s", who, tag)
		}
		if !strings.Contains(between(t, page, `class="message-page nf-message"`, "</div>"), `data-testid="not-found"`) {
			t.Errorf("%s, the Not found sentence isn't in the centred message:\n%s", who, page)
		}
		if c := card.FindString(page); c != "" {
			t.Errorf("%s, the Not found page has a card, %s:\n%s", who, c, page)
		}
	}
}

// sidebarCounts returns the sidebar's Home and Follow-ups items on page.
func sidebarCounts(t *testing.T, page string) (home, followUps string) {
	t.Helper()
	return pageElement(t, page, "a", "nav-home"), pageElement(t, page, "a", "nav-follow-ups")
}

// An unknown address's Not found page shows the sidebar's counts, the same as
// a missing Goal's does for the same person (#127).
func TestUnknownAddressShowsSidebarCounts(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // due a Check-in, and Unlinked
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")

	wantHome, wantFollowUps := sidebarCounts(t, assertNotFoundPageFor(t, client, http.MethodGet, ts.URL+"/goals/999999", "/home"))
	if !strings.Contains(wantHome, "count") || !strings.Contains(wantFollowUps, "count") {
		t.Fatalf("a missing Goal's sidebar lacks the counts this test compares against: %s %s", wantHome, wantFollowUps)
	}
	home, followUps := sidebarCounts(t, assertNotFoundPageFor(t, client, http.MethodGet, ts.URL+"/no-such-page", "/home"))
	if home != wantHome || followUps != wantFollowUps {
		t.Errorf("unknown address's sidebar = %s %s, want %s %s", home, followUps, wantHome, wantFollowUps)
	}
}

// Asking an unknown address with any method, not just GET, gets the Not found
// page: with the sidebar's counts for someone signed in, and the way to sign in
// for a visitor who isn't (#127).
func TestUnknownAddressShowsNotFoundPageForEveryMethod(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // due a Check-in, and Unlinked
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")
	wantHome, wantFollowUps := sidebarCounts(t, assertNotFoundPageFor(t, client, http.MethodGet, ts.URL+"/goals/999999", "/home"))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		home, followUps := sidebarCounts(t, assertNotFoundPageFor(t, client, method, ts.URL+"/no-such-page", "/home"))
		if home != wantHome || followUps != wantFollowUps {
			t.Errorf("%s /no-such-page: sidebar = %s %s, want %s %s", method, home, followUps, wantHome, wantFollowUps)
		}
		assertNotFoundPageFor(t, http.DefaultClient, method, ts.URL+"/no-such-page", "/signin")
	}
}

// A path some route serves, asked with a method none serves it with, still
// answers 405 naming the methods it takes, rather than the Not found page. So
// does the bare root, which takes GET, while an unknown path still answers
// with the Not found page (#127, #544).
func TestKnownPathWithWrongMethodIsNotAllowed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/follow-ups", "/"} {
		resp, err := ctxPost(t, client, ts.URL+path, "application/x-www-form-urlencoded", nil)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		_ = readBody(t, resp)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET, HEAD" {
			t.Errorf("POST %s: status %d, Allow %q; want 405, %q", path, resp.StatusCode, resp.Header.Get("Allow"), "GET, HEAD")
		}
	}

	assertNotFoundPageFor(t, client, http.MethodPost, ts.URL+"/no-such-page", "/home")
}

// Each form the Goal page submits, sent as a valid submit would but to a Goal
// that doesn't exist, gets the Not found page with a way back to Home.
func TestGoalFormsOnMissingGoalShowNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	kim := h.SignIn("kim@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	headcount := h.CreateField(boss, "Headcount", domain.FieldNumber, "people")
	parent := h.ActiveGoal(kim, "Grow revenue", "The company needs it.")
	client := signInClient(t, ts.URL, "boss@example.com")
	missing := fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID+1000)
	due := h.Clock.Now().AddDate(0, 3, 0).Format("2006-01-02")

	for path, form := range map[string]url.Values{
		"/delegates":        {"email": {kim.Email}},
		"/delegates/remove": {"email": {kim.Email}},
		"/milestones":       {"name": {"Beta"}, "target_date": {due}},
		"/links":            {"parent_id": {fmt.Sprint(parent.ID)}, "note": {"It feeds revenue."}},
		"/handoff":          {"to_email": {kim.Email}},
		"/reassign":         {"email": {kim.Email}},
		"/fields":           {"field_id": {fmt.Sprint(headcount.ID)}, "value": {"12"}},
		"/dimensions":       {"value_id": {fmt.Sprint(pillar.Values[0].ID)}},
	} {
		assertNotFoundPageWithForm(t, client, http.MethodPost, missing+path, form, "/home")
	}
}
