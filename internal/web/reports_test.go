package web_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A signed-in person saves a Report Definition of a root Goal to a depth and is
// shown a live draft of the selected Goals, with each Goal's title, Owner,
// Health, and due date (CONTEXT.md: Report Definition). Every Goal here was
// created within the default baseline, so each gets an exception block.
func TestSaveReportDefinitionAndSeeDraftOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")

	a := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	b := h.ActiveChildOf(boss, a, "Launch in EU", "Expand the market.")
	c := h.ActiveChildOf(boss, a, "Cut churn", "Keep customers.")
	h.Checkin(boss, b.ID, domain.HealthYellow, "slipping", "add staff", h.Clock.Now().AddDate(0, 1, 0))

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	// The create form offers the Goals as roots.
	form := getBody(t, client, ts.URL+"/reports")
	if !strings.Contains(form, "Grow revenue") {
		t.Fatalf("reports page does not offer Goals as roots; body:\n%s", form)
	}

	resp := postForm(t, client, ts.URL+"/reports", url.Values{
		"name":         {"EU MBR"},
		"introduction": {"Quarterly business review."},
		"root":         {strconv.FormatInt(a.ID, 10)},
		"depth":        {"1"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save report: status %d", resp.StatusCode)
	}
	draft := readBody(t, resp)
	for _, want := range []string{
		"EU MBR",
		"Quarterly business review.",
		"Grow revenue", "Launch in EU", "Cut churn", // selected Goals
		"boss@example.com",  // Owner
		domain.HealthYellow, // B's Health
	} {
		if !strings.Contains(draft, want) {
			t.Errorf("live draft missing %q; body:\n%s", want, draft)
		}
	}
	// Each selected Goal appears once, as a block or a line.
	if got := strings.Count(draft, `data-testid="report-exception"`) + strings.Count(draft, `data-testid="selected-goal"`); got != 3 {
		t.Errorf("draft shows %d selected Goals, want 3; body:\n%s", got, draft)
	}

	// The saved definition shows up on the reports list.
	list := getBody(t, client, ts.URL+"/reports")
	if !strings.Contains(list, "EU MBR") {
		t.Errorf("reports list missing the saved definition; body:\n%s", list)
	}
	_ = c
}

// A Report Definition that selects nothing (no roots, no filters) is rejected.
func TestSaveReportDefinitionRejectsEmptySelection(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports", url.Values{"name": {"Selects nothing"}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("empty selection: status %d, want 422", resp.StatusCode)
	}
}

// The draft Report gives an exception the full block and an unchanged Green one
// line, read against the default baseline or one the reader picks. The Report's
// content is asserted on its view model in the domain tests; this checks the
// page wires the baseline through and renders both kinds.
func TestSmokeReportDraftShowsExceptionBlocksAgainstBaseline(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{red.ID, green.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)

	page := getBody(t, client, reportURL)
	if !strings.Contains(page, `value="2026-01-12"`) {
		t.Errorf("page does not offer the default baseline, 30 days ago; body:\n%s", page)
	}
	block := pageElement(t, page, "article", "report-exception")
	for _, want := range []string{"Launch in EU", domain.HealthRed, "Blocked on legal.", "Hire counsel."} {
		if !strings.Contains(block, want) {
			t.Errorf("exception block missing %q; block:\n%s", want, block)
		}
	}
	if got := strings.Count(page, `data-testid="report-exception"`); got != 1 {
		t.Errorf("%d exception blocks, want 1; body:\n%s", got, page)
	}
	if line := pageElement(t, page, "li", "selected-goal"); !strings.Contains(line, "Cut churn") {
		t.Errorf("unchanged Green is not one line; line:\n%s", line)
	}

	// A baseline before both Goals were created makes both New.
	page = getBody(t, client, reportURL+"?baseline=2026-01-01")
	if got := strings.Count(page, `data-testid="report-exception"`); got != 2 {
		t.Errorf("with an earlier baseline, %d exception blocks, want 2; body:\n%s", got, page)
	}

	resp, err := client.Get(reportURL + "?baseline=not-a-date")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unreadable baseline: status %d, want 400", resp.StatusCode)
	}
}

// From the draft a reader publishes the Report and lands on the frozen
// publication, which later Check-ins never change. The Definition's page lists
// its publications, and the next draft reads its changes against the previous
// publication unless the reader picks a date, which publishing carries through
// (CONTEXT.md: Report Definition). The snapshot's content is asserted on its
// view model in the domain tests; this checks the pages wire it through.
func TestSmokePublishReportOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthYellow, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	other := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "WBR", RootIDs: []int64{g.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)

	draft := getBody(t, client, reportURL)
	if !strings.Contains(draft, `data-testid="no-publications"`) {
		t.Errorf("an unpublished definition does not say so; body:\n%s", draft)
	}
	resp := postForm(t, client, reportURL+"/publications", url.Values{"baseline": {""}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	pubURL := resp.Request.URL.String()
	if !strings.HasPrefix(pubURL, reportURL+"/publications/") {
		t.Fatalf("publish redirected to %s, want a publication of the definition", pubURL)
	}
	published := readBody(t, resp)
	if line := pageElement(t, published, "p", "report-published"); !strings.Contains(line, "boss@example.com") {
		t.Errorf("publication does not say who published it; line:\n%s", line)
	}
	for _, want := range []string{"MBR", "Vendor is late."} {
		if !strings.Contains(published, want) {
			t.Errorf("publication missing %q; body:\n%s", want, published)
		}
	}

	h.Clock.Advance(24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthRed, "Vendor is gone.", "Find a new vendor.", h.Clock.Now().AddDate(0, 0, 14))
	published = getBody(t, client, pubURL)
	if !strings.Contains(published, "Vendor is late.") || strings.Contains(published, "Vendor is gone.") {
		t.Errorf("publication changed after a later Check-in; body:\n%s", published)
	}

	draft = getBody(t, client, reportURL)
	if item := pageElement(t, draft, "li", "report-publication"); !strings.Contains(item, strings.TrimPrefix(pubURL, ts.URL)) {
		t.Errorf("definition page does not link its publication; item:\n%s", item)
	}
	if !strings.Contains(draft, `data-testid="report-since-publication"`) {
		t.Errorf("the next draft does not say it reads against the previous publication; body:\n%s", draft)
	}

	// A date the reader picks is carried into the publication.
	draft = getBody(t, client, reportURL+"?baseline=2026-01-01")
	if form := pageElement(t, draft, "form", "report-publish"); !strings.Contains(form, `value="2026-01-01"`) {
		t.Errorf("publish form does not carry the chosen baseline; form:\n%s", form)
	}
	resp = postForm(t, client, reportURL+"/publications", url.Values{"baseline": {"2026-01-01"}})
	if line := pageElement(t, readBody(t, resp), "p", "report-published"); !strings.Contains(line, "Changes since 2026-01-01") {
		t.Errorf("publication does not read against the chosen baseline; line:\n%s", line)
	}

	// A publication is only found under its own definition.
	wrong := ts.URL + "/reports/" + strconv.FormatInt(other.ID, 10) + strings.TrimPrefix(pubURL, reportURL)
	resp, err := client.Get(wrong)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("publication under another definition: status %d, want 404", resp.StatusCode)
	}
}
