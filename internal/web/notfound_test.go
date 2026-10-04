package web_test

import (
	"fmt"
	"html"
	"net/http"
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
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatalf("new %s request: %v", method, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawURL, err)
	}
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

// topBarCounts returns the top bar's Home and Risks items on page.
func topBarCounts(t *testing.T, page string) (home, risks string) {
	t.Helper()
	return pageElement(t, page, "a", "nav-home"), pageElement(t, page, "a", "nav-risks")
}

// An unknown address's Not found page shows the top bar's counts, the same as
// a missing Goal's does for the same person (#127).
func TestUnknownAddressShowsTopBarCounts(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // due a Check-in, and Unaligned
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")

	wantHome, wantRisks := topBarCounts(t, assertNotFoundPageFor(t, client, http.MethodGet, ts.URL+"/goals/999999", "/home"))
	if !strings.Contains(wantHome, "count") || !strings.Contains(wantRisks, "count") {
		t.Fatalf("a missing Goal's top bar lacks the counts this test compares against: %s %s", wantHome, wantRisks)
	}
	home, risks := topBarCounts(t, assertNotFoundPageFor(t, client, http.MethodGet, ts.URL+"/no-such-page", "/home"))
	if home != wantHome || risks != wantRisks {
		t.Errorf("unknown address's top bar = %s %s, want %s %s", home, risks, wantHome, wantRisks)
	}
}

// Asking an unknown address with any method, not just GET, gets the Not found
// page: with the top bar's counts for someone signed in, and the way to sign in
// for a visitor who isn't (#127).
func TestUnknownAddressShowsNotFoundPageForEveryMethod(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // due a Check-in, and Unaligned
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")
	wantHome, wantRisks := topBarCounts(t, assertNotFoundPageFor(t, client, http.MethodGet, ts.URL+"/goals/999999", "/home"))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		home, risks := topBarCounts(t, assertNotFoundPageFor(t, client, method, ts.URL+"/no-such-page", "/home"))
		if home != wantHome || risks != wantRisks {
			t.Errorf("%s /no-such-page: top bar = %s %s, want %s %s", method, home, risks, wantHome, wantRisks)
		}
		assertNotFoundPageFor(t, http.DefaultClient, method, ts.URL+"/no-such-page", "/signin")
	}
}

// A path some route serves, asked with a method none serves it with, still
// answers 405 naming the methods it takes, rather than the Not found page. The
// bare root names nothing to anything but GET (#127).
func TestKnownPathWithWrongMethodIsNotAllowed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")

	resp, err := client.Post(ts.URL+"/risks", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatalf("POST /risks: %v", err)
	}
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET, HEAD" {
		t.Errorf("POST /risks: status %d, Allow %q; want 405, %q", resp.StatusCode, resp.Header.Get("Allow"), "GET, HEAD")
	}

	assertNotFoundPageFor(t, client, http.MethodPost, ts.URL+"/", "/home")
}
