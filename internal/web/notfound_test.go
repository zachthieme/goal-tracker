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
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET %s: status %d, want 404", rawURL, resp.StatusCode)
	}
	if !strings.Contains(page, "<title>Not found · Goal Tracker</title>") || !strings.Contains(page, `class="brand"`) {
		t.Fatalf("GET %s: not a Not found page inside the site's chrome; body:\n%s", rawURL, page)
	}
	element := pageElement(t, page, "p", "not-found")
	sentence := html.UnescapeString(element[strings.Index(element, ">")+1:])
	if sentence != "There's nothing here. It may have been removed, or the link may be wrong." {
		t.Errorf("GET %s: Not found page says %q", rawURL, sentence)
	}
	if !strings.Contains(page, `<a href="`+back+`">`) {
		t.Errorf("GET %s: Not found page has no link to %s; body:\n%s", rawURL, back, page)
	}
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
	def := h.SaveReportDefinition(pat, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	other := h.SaveReportDefinition(pat, domain.SaveReportDefinitionInput{Name: "WBR", RootIDs: []int64{g.ID}})
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
