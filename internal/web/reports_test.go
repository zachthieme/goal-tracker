package web_test

import (
	"context"
	"fmt"
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
	if line := pageElement(t, page, "tr", "selected-goal"); !strings.Contains(line, "Cut churn") {
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
	if line := pageElement(t, readBody(t, resp), "p", "report-published"); !strings.Contains(line, "changes since 2026-01-01") {
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

// A published Report downloads as Markdown from its publication page, with the
// same content as the snapshot, and a later Check-in never changes it. The
// Markdown itself is asserted in the export package's tests; this checks the
// page wires it through.
func TestSmokeExportPublicationAsMarkdownOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthRed, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", RootIDs: []int64{g.ID}})
	other := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "WBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	h.Checkin(boss, g.ID, domain.HealthRed, "Vendor is gone.", "Find a new vendor.", h.Clock.Now().AddDate(0, 0, 14))

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubPath := "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(pub.ID, 10)

	page := getBody(t, client, ts.URL+pubPath)
	if link := pageElement(t, page, "a", "export-markdown"); !strings.Contains(link, pubPath+"/markdown") {
		t.Errorf("publication page does not link its Markdown export; link:\n%s", link)
	}

	resp, err := client.Get(ts.URL + pubPath + "/markdown")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("markdown export: status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("Content-Type %q, want text/markdown", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".md") {
		t.Errorf("Content-Disposition %q, want a .md attachment", cd)
	}
	md := readBody(t, resp)
	if !strings.HasPrefix(md, "# EU MBR\n") {
		t.Errorf("export does not open with the Report's name:\n%s", md)
	}
	for _, want := range []string{"### Launch in EU", "Health: **Red**", "Vendor is late."} {
		if !strings.Contains(md, want) {
			t.Errorf("export missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Vendor is gone.") {
		t.Errorf("export shows a Check-in made after publishing:\n%s", md)
	}

	// A publication exports only under its own definition.
	resp, err = client.Get(ts.URL + "/reports/" + strconv.FormatInt(other.ID, 10) + "/publications/" + strconv.FormatInt(pub.ID, 10) + "/markdown")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("export under another definition: status %d, want 404", resp.StatusCode)
	}
}

// A published Report has a print-friendly page to print to PDF from the
// browser: the snapshot's content without the app's navigation.
func TestSmokePrintPublicationOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthRed, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", Introduction: "Where the EU launch stands.", RootIDs: []int64{g.ID}})
	other := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "WBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubPath := "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(pub.ID, 10)

	page := getBody(t, client, ts.URL+pubPath)
	if link := pageElement(t, page, "a", "export-print"); !strings.Contains(link, pubPath+"/print") {
		t.Errorf("publication page does not link its print page; link:\n%s", link)
	}

	printed := getBody(t, client, ts.URL+pubPath+"/print")
	if line := pageElement(t, printed, "p", "report-published"); !strings.Contains(line, "boss@example.com") {
		t.Errorf("print page does not say who published it; line:\n%s", line)
	}
	block := pageElement(t, printed, "article", "report-exception")
	for _, want := range []string{"Launch in EU", domain.HealthRed, "Vendor is late.", "Chase the vendor."} {
		if !strings.Contains(block, want) {
			t.Errorf("print page's exception block missing %q; block:\n%s", want, block)
		}
	}
	if !strings.Contains(printed, "Where the EU launch stands.") {
		t.Errorf("print page missing the introduction; body:\n%s", printed)
	}
	for _, chrome := range []string{`data-testid="nav-reports"`, `action="/signout"`} {
		if strings.Contains(printed, chrome) {
			t.Errorf("print page carries the app's navigation (%s); body:\n%s", chrome, printed)
		}
	}

	resp, err := client.Get(ts.URL + "/reports/" + strconv.FormatInt(other.ID, 10) + "/publications/" + strconv.FormatInt(pub.ID, 10) + "/print")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("print page under another definition: status %d, want 404", resp.StatusCode)
	}
}

// While preparing a publication the author sees each Highlight in scope on the
// draft page, picks which go into Insights, Accomplishments, and Misses, and
// adds their own text. The draft and then the publication show the narrative,
// each Highlight crediting the Goal's Owner. The narrative's content is
// asserted on its view model in the domain tests; this checks the pages wire
// it through.
func TestSmokeCurateNarrativeOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	g := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(alice, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.Clock.Advance(time.Hour)
	h.CheckinWithHighlight(alice, g.ID, domain.HighlightMiss, "Lost the second customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	draft, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	var signed, lost int64
	for _, nh := range draft.Highlights {
		switch nh.Highlight.Note {
		case "Signed the first EU customer.":
			signed = nh.Highlight.ID
		case "Lost the second customer.":
			lost = nh.Highlight.ID
		}
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)

	page := getBody(t, client, reportURL)
	form := pageElement(t, page, "form", "narrative-curation")
	for _, want := range []string{"Signed the first EU customer.", "Lost the second customer.", "alice@example.com", "Launch in EU"} {
		if !strings.Contains(form, want) {
			t.Errorf("curation form missing %q; form:\n%s", want, form)
		}
	}

	resp := postForm(t, client, reportURL+"/narrative", url.Values{
		"pick-" + strconv.FormatInt(signed, 10):  {domain.HighlightAccomplishment},
		"pick-" + strconv.FormatInt(lost, 10):    {""},
		"text-" + domain.HighlightAccomplishment: {"EU is open for business."},
	})
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/reports/"+strconv.FormatInt(def.ID, 10) {
		t.Fatalf("curate: status %d at %s, want the draft page", resp.StatusCode, resp.Request.URL)
	}
	page = readBody(t, resp)
	narrative := pageElement(t, page, "section", "report-narrative")
	for _, want := range []string{"Accomplishments", "EU is open for business.", "Signed the first EU customer.", "alice@example.com"} {
		if !strings.Contains(narrative, want) {
			t.Errorf("draft narrative missing %q; narrative:\n%s", want, narrative)
		}
	}
	if strings.Contains(narrative, "Lost the second customer.") {
		t.Errorf("draft narrative shows a Highlight the author left out; narrative:\n%s", narrative)
	}
	if form := pageElement(t, page, "form", "narrative-curation"); !strings.Contains(form, "EU is open for business.") {
		t.Errorf("curation form does not keep the author's text; form:\n%s", form)
	}

	resp = postForm(t, client, reportURL+"/publications", url.Values{"baseline": {""}})
	published := readBody(t, resp)
	narrative = pageElement(t, published, "section", "report-narrative")
	for _, want := range []string{"EU is open for business.", "Signed the first EU customer.", "alice@example.com"} {
		if !strings.Contains(narrative, want) {
			t.Errorf("published narrative missing %q; narrative:\n%s", want, narrative)
		}
	}
	if strings.Contains(published, `data-testid="narrative-curation"`) {
		t.Errorf("publication offers to curate its frozen narrative; body:\n%s", published)
	}

	resp = postForm(t, client, reportURL+"/narrative", url.Values{"pick-9999": {domain.HighlightInsight}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("picking an unknown Highlight: status %d, want 422", resp.StatusCode)
	}
}

// The publication opens with a Health summary: how many of the Report's
// selected Goals are Red, Yellow, Green, and Stale, so an exec sees the shape
// of the org before reading a block.
func TestPublicationSummarisesHealthOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	yellow := h.ActiveGoal(boss, "Hire a CFO", "We need one.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	stale := h.ActiveGoal(boss, "Open Tokyo", "Expand east.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, yellow.ID, domain.HealthYellow, "Slow.", "Use a recruiter.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{red.ID, yellow.ID, green.ID, stale.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	summary := pageElement(t, page, "section", "health-summary")
	for tile, want := range map[string]string{"red": "1", "yellow": "1", "green": "1", "stale": "1"} {
		if count := pageElement(t, summary, "span", "health-count-"+tile); !strings.HasSuffix(count, ">"+want) {
			t.Errorf("%s tile: %q, want a count of %s", tile, count, want)
		}
	}
}

// The publication splits the selected Goals in two: exceptions as cards under
// Needs attention, each with its Health badge and its So What, Status, and
// Path to Green — marked Overdue once its target date has passed — and the
// rest as a table of Health, Goal, Owner, and Due under On track.
func TestPublicationSplitsNeedsAttentionFromOnTrackOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 0, 1))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(3 * 24 * time.Hour)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{red.ID, green.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	attention := pageElement(t, page, "section", "needs-attention")
	card := pageElement(t, attention, "article", "report-exception")
	if !strings.Contains(card, `class="card`) || !strings.Contains(card, `class="badge r"`) {
		t.Errorf("exception is not a card with a Red badge; card:\n%s", card)
	}
	if status := pageElement(t, card, "div", "report-status-box"); !strings.Contains(status, "Hire counsel.") ||
		!strings.Contains(status, `data-testid="path-overdue"`) {
		t.Errorf("status box does not mark the missed Path to Green Overdue; box:\n%s", status)
	}

	onTrack := pageElement(t, page, "table", "on-track")
	row := pageElement(t, onTrack, "tr", "selected-goal")
	for _, want := range []string{"Cut churn", "boss@example.com", domain.HealthGreen} {
		if !strings.Contains(row, want) {
			t.Errorf("On track row missing %q; row:\n%s", want, row)
		}
	}
}

// A publication's narrative reads in the serif face, each section under a
// label heading and each Highlight credited "— owner, Goal".
func TestPublicationNarrativeReadsAsProseOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	g := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(alice, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	draft, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil || len(draft.Highlights) != 1 {
		t.Fatalf("DraftReport: %d Highlights, %v", len(draft.Highlights), err)
	}
	if err := h.Service.CurateNarrative(context.Background(), def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: draft.Highlights[0].Highlight.ID, Section: domain.HighlightAccomplishment}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	narrative := pageElement(t, page, "section", "report-narrative")
	if !strings.Contains(narrative, `class="card rp-narrative"`) {
		t.Errorf("narrative is not set in the serif face; narrative:\n%s", narrative)
	}
	if heading := pageElement(t, narrative, "h3", "narrative-section"); !strings.Contains(heading, `class="label"`) || !strings.HasSuffix(heading, ">Accomplishments") {
		t.Errorf("narrative section heading %q, want an Accomplishments label", heading)
	}
	if credit := between(t, narrative, `data-testid="highlight-credit"`, "</a>"); !strings.Contains(credit, "— "+shownAs("alice@example.com", "alice")+", ") || !strings.Contains(credit, "Launch in EU") {
		t.Errorf("Highlight credit %q, want — owner, Goal", credit)
	}
}

// The draft page reads top to bottom as the author works: pick the baseline,
// curate the narrative, check the preview, and only then Publish — the primary
// button, at the bottom, saying what it does. Publications sit in a sidebar.
func TestDraftPagePublishesOnlyAfterThePreviewOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	last := -1
	for _, part := range []string{`data-testid="report-baseline"`, `data-testid="report-curation"`, `data-testid="report-draft"`, `data-testid="report-publish"`} {
		at := strings.Index(page, part)
		if at <= last {
			t.Errorf("%s is out of order (at %d, after %d); body:\n%s", part, at, last, page)
		}
		last = at
	}
	publish := pageElement(t, page, "form", "report-publish")
	if !strings.Contains(publish, `class="btn primary"`) || !strings.Contains(publish, "Publishes a frozen copy readers can comment on.") {
		t.Errorf("Publish is not the primary button with its summary; form:\n%s", publish)
	}
	if !strings.Contains(page, `<aside data-testid="report-publications"`) {
		t.Errorf("publications are not in a sidebar; body:\n%s", page)
	}
}

// The reports page lists saved definitions as cards and keeps the form behind
// a New report button. Its Root Goals picker is a scrolling list with the
// top-level Goals first, and Depth says what its numbers mean.
func TestReportsListFormOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	child := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	root := h.MarkTopLevel(boss, h.ActiveGoal(boss, "Grow revenue", "The org needs to grow."))
	h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{root.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports")

	if def := pageElement(t, page, "li", "report-definition"); !strings.Contains(def, `class="card`) || !strings.Contains(def, "MBR") {
		t.Errorf("saved definition is not a card; item:\n%s", def)
	}
	create := pageElement(t, page, "details", "create-report")
	if !strings.Contains(create, ">New report</summary>") || strings.HasPrefix(create, `<details data-testid="create-report" open`) {
		t.Errorf("the definition form is not behind a collapsed New report button; details:\n%s", create)
	}
	roots := pageElement(t, create, "fieldset", "report-roots")
	if !strings.Contains(roots, `class="rp-picker"`) {
		t.Errorf("root picker does not scroll; fieldset:\n%s", roots)
	}
	if strings.Index(roots, root.Title) > strings.Index(roots, child.Title) {
		t.Errorf("top-level Goal is not listed first; fieldset:\n%s", roots)
	}
	if !strings.Contains(create, "0 = just the roots, 1 = roots and their direct contributors, …") {
		t.Errorf("Depth is not explained; details:\n%s", create)
	}
}

// The print page sets the Report in the design system's serif, falling back to
// Georgia, and marks Health with a shape as well as its name so it survives
// black-and-white printing: ■ Red, ▲ Yellow, ● Green.
func TestPrintPageMarksHealthWithShapesOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{red.ID, green.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	printed := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")

	if !strings.Contains(printed, "--print-font: 'Source Serif 4', Georgia, serif") || !strings.Contains(printed, "font-family: var(--print-font)") {
		t.Errorf("print page is not set in Source Serif 4 with a Georgia fallback; body:\n%s", printed)
	}
	if block := pageElement(t, printed, "article", "report-exception"); !strings.Contains(block, "■</span>Red") {
		t.Errorf("Red is not marked ■; block:\n%s", block)
	}
	if row := pageElement(t, printed, "tr", "selected-goal"); !strings.Contains(row, "●</span>Green") {
		t.Errorf("Green is not marked ●; row:\n%s", row)
	}
}

// At phone width the draft page's columns stack into one grid track, and each
// column shrinks to the viewport rather than to its widest content — the On
// track table scrolls inside its card instead of pushing the baseline line and
// the Narrative card off the right edge (#43).
func TestDraftPageColumnsShrinkToPhoneWidthOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	if !strings.Contains(page, `<div class="grid-main-aside">`) {
		t.Fatalf("draft page has no main and aside columns; body:\n%s", page)
	}
	if !strings.Contains(page, ".grid-main-aside>*{min-width:0}") {
		t.Errorf("draft page columns keep their content's minimum width, so a wide table scrolls the page sideways; body:\n%s", page)
	}
}

// The Print view has no hover, so it introduces each person as Name (email) at
// their first mention and by Name after that (CONTEXT.md: Name).
func TestPrintPageIntroducesEachPersonOnceOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	first := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	second := h.ActiveGoal(ada, "Cut churn", "Keep customers.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{first.ID, second.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	printed := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")

	if line := pageElement(t, printed, "p", "report-published"); !strings.Contains(line, ">boss (boss@example.com)<") {
		t.Errorf("print page does not introduce the publisher; line:\n%s", line)
	}
	if n := strings.Count(printed, ">Ada Okafor (ada.okafor@example.com)<"); n != 1 {
		t.Errorf("print page introduces Ada %d times, want once; body:\n%s", n, printed)
	}
	if n := strings.Count(printed, ">Ada Okafor<"); n != 1 {
		t.Errorf("print page names Ada alone %d times, want once after her introduction; body:\n%s", n, printed)
	}
}

// A publication shows each Owner by the Name they had when it was published,
// whatever their Name is now; one published before Accounts had Names still
// shows its people by email.
func TestPublicationShowsNamesAsPublishedOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	g := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	renamed := h.PublishReport(boss, def)
	old := h.PublishReport(boss, def)
	if _, err := h.DB.Exec(`UPDATE accounts SET name = 'Ada Mensah' WHERE id = ?`, ada.ID); err != nil {
		t.Fatalf("rename Ada: %v", err)
	}
	before := fmt.Sprintf(`{"Definition":{"ID":%d,"Name":"MBR"},"Lines":[{"Goal":{"ID":%d,"Title":"Launch in EU",`+
		`"Owner":{"ID":%d,"Email":"ada.okafor@example.com","IsAdmin":false,"Departed":false}},"Health":""}]}`, def.ID, g.ID, ada.ID)
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, old.ID); err != nil {
		t.Fatalf("write a pre-Names snapshot: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubURL := func(p domain.Publication) string {
		return ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(p.ID, 10)
	}

	if page := getBody(t, client, pubURL(renamed)); !strings.Contains(page, shownAs("ada.okafor@example.com", "Ada Okafor")) || strings.Contains(page, "Ada Mensah") {
		t.Errorf("publication does not show Ada as published; body:\n%s", page)
	}
	if row := pageElement(t, getBody(t, client, pubURL(old)), "td", "selected-owner"); !strings.Contains(row, shownAs("ada.okafor@example.com", "ada.okafor@example.com")) {
		t.Errorf("pre-Names Owner reads %s, want the email", row)
	}
	if printed := getBody(t, client, pubURL(old)+"/print"); !strings.Contains(printed, ">ada.okafor@example.com<") || strings.Contains(printed, "(ada.okafor@example.com)") {
		t.Errorf("pre-Names print page does not show Ada by email alone; body:\n%s", printed)
	}
}

// A publication's byline shows the publisher by the Name they had when it was
// published, on the publication, its print page, and its Markdown export, so
// the byline and the Goals they own read the same Name.
func TestPublicationBylineKeepsThePublishersNameOverHTTP(t *testing.T) {
	h := testsupport.New(t, "ceo@example.com")
	ceo := h.SignInNamed("ceo@example.com", "Dana Whitfield")
	g := h.ActiveGoal(ceo, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(ceo, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(ceo, def)
	if _, err := h.DB.Exec(`UPDATE accounts SET name = 'Dana Renamed' WHERE id = ?`, ceo.ID); err != nil {
		t.Fatalf("rename the publisher: %v", err)
	}

	ts := newServer(t, h)
	// Read by someone else, so the signed-in header doesn't show the live Name.
	client := signInClient(t, ts.URL, "ada.okafor@example.com")
	pubURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(pub.ID, 10)

	page := getBody(t, client, pubURL)
	if line := pageElement(t, page, "p", "report-published"); !strings.Contains(line, shownAs("ceo@example.com", "Dana Whitfield")) {
		t.Errorf("publication byline does not show Dana Whitfield as published; line:\n%s", line)
	}
	printed := getBody(t, client, pubURL+"/print")
	if line := pageElement(t, printed, "p", "report-published"); !strings.Contains(line, ">Dana Whitfield (ceo@example.com)<") {
		t.Errorf("print byline does not introduce Dana Whitfield as published; line:\n%s", line)
	}
	md := getBody(t, client, pubURL+"/markdown")
	if !strings.Contains(md, " by Dana Whitfield (ceo@example.com). ") {
		t.Errorf("Markdown byline does not introduce Dana Whitfield as published; body:\n%s", md)
	}
	for name, body := range map[string]string{"publication": page, "print page": printed, "Markdown": md} {
		if strings.Contains(body, "Dana Renamed") {
			t.Errorf("%s shows the Name given after publishing; body:\n%s", name, body)
		}
	}
}

// The New Report Definition form sits a gap below its button through the
// page's own class rather than a style attribute.
func TestNewReportFormSpacingComesFromAClassOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports")
	assertStyledBy(t, tagAround(t, page, `action="/reports"`), page, "rp-new", "margin-top:12px")
}

// A Retired Dimension drops out of the Report Definition form's filters, yet a
// Report Definition saved with a filter on its value still drafts the same
// Goals. Restoring the Dimension returns it to the form (CONTEXT.md: Retired;
// ADR 0005).
func TestRetiredDimensionLeavesReportFormButSavedFilterKeepsWorkingOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.CreateDimension(boss, "Team", "Core")
	growth, trust := pillar.Values[0], pillar.Values[1]
	grower := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	truster := h.ActiveGoal(boss, "Earn trust", "Customers need to trust us.")
	h.AssignGoalValue(grower, growth)
	h.AssignGoalValue(truster, trust)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Growth MBR", DimensionValueIDs: []int64{growth.ID}})
	ctx := context.Background()
	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	filters := between(t, getBody(t, client, ts.URL+"/reports"), `<fieldset data-testid="report-dimension-filters"`, `<div><button type="submit"`)
	if strings.Contains(filters, "Pillar") || strings.Contains(filters, "Growth") {
		t.Errorf("the Retired Pillar is offered as a Report filter:\n%s", filters)
	}
	if !strings.Contains(filters, "Team") {
		t.Errorf("the live Team isn't offered as a Report filter:\n%s", filters)
	}

	draft := getBody(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	if !strings.Contains(draft, grower.Title) || strings.Contains(draft, truster.Title) {
		t.Errorf("the saved Growth filter no longer drafts just %q; body:\n%s", grower.Title, draft)
	}

	if err := h.Service.RestoreDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RestoreDimension: %v", err)
	}
	filters = between(t, getBody(t, client, ts.URL+"/reports"), `<fieldset data-testid="report-dimension-filters"`, `<div><button type="submit"`)
	if !strings.Contains(filters, "<legend>Pillar</legend>") || !strings.Contains(filters, fmt.Sprintf(`value="%d"`, growth.ID)) {
		t.Errorf("the restored Pillar isn't offered as a Report filter:\n%s", filters)
	}
}

// The Report Definition form offers the Fields still offered (none Retired) to
// show beside each Goal. The chosen ones then appear beside each Goal that has
// a value, with its unit, on the exception card and the On track row alike; a
// long text reads as a labelled value there, not in the narrative. A number
// Field is never totalled across Goals (ADR 0005). A definition that chose no
// Fields shows none (ticket #78).
func TestReportDefinitionShowsChosenFieldsOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	notes := h.CreateField(boss, "Notes", domain.FieldLongText, "")
	sponsor := h.CreateField(boss, "Sponsor", domain.FieldShortText, "")
	legacy := h.CreateField(boss, "Legacy code", domain.FieldShortText, "")
	if err := h.Service.RetireField(context.Background(), boss.ID, legacy.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.SetGoalField(boss, red, budget, "120")
	h.SetGoalField(boss, red, notes, "Counsel hired in March.")
	h.SetGoalField(boss, red, sponsor, "Dana")
	h.SetGoalField(boss, green, budget, "40")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	choice := between(t, getBody(t, client, ts.URL+"/reports"), `<fieldset data-testid="report-field-choice"`, `</fieldset>`)
	for _, f := range []domain.Field{budget, notes, sponsor} {
		if !strings.Contains(choice, f.Name) || !strings.Contains(choice, fmt.Sprintf(`name="field" value="%d"`, f.ID)) {
			t.Errorf("the form doesn't offer %s to show; fieldset:\n%s", f.Name, choice)
		}
	}
	if strings.Contains(choice, legacy.Name) {
		t.Errorf("the form offers the Retired %s; fieldset:\n%s", legacy.Name, choice)
	}

	resp := postForm(t, client, ts.URL+"/reports", url.Values{
		"name":  {"EU MBR"},
		"root":  {strconv.FormatInt(red.ID, 10), strconv.FormatInt(green.ID, 10)},
		"depth": {"0"},
		"field": {strconv.FormatInt(budget.ID, 10), strconv.FormatInt(notes.ID, 10)},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save report: status %d", resp.StatusCode)
	}
	draft := readBody(t, resp)
	card := pageElement(t, pageElement(t, draft, "article", "report-exception"), "dl", "report-fields")
	for _, want := range []string{"Budget", "120", "$", "Notes", "Counsel hired in March."} {
		if !strings.Contains(card, want) {
			t.Errorf("exception card's Fields missing %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "Dana") {
		t.Errorf("exception card shows the unchosen Sponsor:\n%s", card)
	}
	row := pageElement(t, pageElement(t, draft, "table", "on-track"), "tr", "selected-goal")
	fields := pageElement(t, row, "dl", "report-fields")
	if !strings.Contains(fields, "Budget") || !strings.Contains(fields, "40") || !strings.Contains(fields, "$") {
		t.Errorf("On track row doesn't show Budget 40 $:\n%s", row)
	}
	if strings.Contains(fields, "Notes") {
		t.Errorf("On track row shows Notes, which the Goal has no value in:\n%s", row)
	}
	if strings.Contains(draft, "160") {
		t.Errorf("the draft totals Budget across Goals; body:\n%s", draft)
	}

	plain := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Plain MBR", RootIDs: []int64{red.ID, green.ID}})
	page := getBody(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, plain.ID))
	if strings.Contains(page, `data-testid="report-fields"`) || strings.Contains(page, "Counsel hired in March.") {
		t.Errorf("a definition with no Fields chosen shows Fields; body:\n%s", page)
	}
}

// A publication's page, its Print view and its Markdown all show the chosen
// Fields as they were published: editing the value, renaming the Field or
// retiring it afterwards changes none of them (ticket #78).
func TestPublicationKeepsTheChosenFieldsAsPublishedOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.SetGoalField(boss, red, budget, "120")
	h.SetGoalField(boss, green, budget, "40")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", RootIDs: []int64{red.ID, green.ID}, FieldIDs: []int64{budget.ID}})
	pub := h.PublishReport(boss, def)

	h.SetGoalField(boss, red, budget, "8125")
	h.SetGoalField(boss, green, budget, "")
	// The tool has no Field rename yet; rename it in place, as one would.
	if _, err := h.DB.ExecContext(ctx, `UPDATE fields SET name = 'Spend' WHERE id = ?`, budget.ID); err != nil {
		t.Fatalf("rename field: %v", err)
	}
	if err := h.Service.RetireField(ctx, boss.ID, budget.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubPath := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(pub.ID, 10)
	for name, page := range map[string]string{
		"publication page": getBody(t, client, pubPath),
		"Print view":       getBody(t, client, pubPath+"/print"),
	} {
		card := pageElement(t, pageElement(t, page, "article", "report-exception"), "dl", "report-fields")
		if !strings.Contains(card, "Budget") || !strings.Contains(card, "120") || !strings.Contains(card, "$") {
			t.Errorf("%s's exception card doesn't show Budget 120 $ as published:\n%s", name, card)
		}
		row := pageElement(t, pageElement(t, page, "table", "on-track"), "tr", "selected-goal")
		if fields := pageElement(t, row, "dl", "report-fields"); !strings.Contains(fields, "Budget") || !strings.Contains(fields, "40") {
			t.Errorf("%s's On track row doesn't show Budget 40 $ as published:\n%s", name, row)
		}
		if strings.Contains(page, "8125") || strings.Contains(page, "Spend") || strings.Contains(page, "160") {
			t.Errorf("%s shows the Field as edited since, or a total; body:\n%s", name, page)
		}
	}
	md := getBody(t, client, pubPath+"/markdown")
	for _, want := range []string{"**Budget:** 120 $", "— Budget: 40 $\n"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "8125") || strings.Contains(md, "Spend") {
		t.Errorf("Markdown shows the Field as edited since:\n%s", md)
	}
}

// An author pulls one of a Check-in's two Highlights into the narrative and
// leaves the other: the draft's pick list shows each separately, and the
// published Report and its Markdown export show only the one pulled
// (CONTEXT.md: Highlight).
func TestPullOneHighlightOfACheckinOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	g := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlights(alice, g.ID,
		domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Signed the first EU customer."},
		domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Hired the EU lead."},
	)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	draft, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	var signed, hired int64
	for _, nh := range draft.Highlights {
		switch nh.Highlight.Note {
		case "Signed the first EU customer.":
			signed = nh.Highlight.ID
		case "Hired the EU lead.":
			hired = nh.Highlight.ID
		}
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)

	form := pageElement(t, getBody(t, client, reportURL), "form", "narrative-curation")
	if n := strings.Count(form, `data-testid="curation-highlight"`); n != 2 {
		t.Errorf("pick list has %d entries, want one per Highlight; form:\n%s", n, form)
	}

	postForm(t, client, reportURL+"/narrative", url.Values{
		"pick-" + strconv.FormatInt(signed, 10): {domain.HighlightAccomplishment},
		"pick-" + strconv.FormatInt(hired, 10):  {""},
	})
	resp := postForm(t, client, reportURL+"/publications", url.Values{"baseline": {""}})
	published := readBody(t, resp)
	narrative := pageElement(t, published, "section", "report-narrative")
	if !strings.Contains(narrative, "Signed the first EU customer.") || strings.Contains(narrative, "Hired the EU lead.") {
		t.Errorf("published narrative should show only the Highlight pulled; narrative:\n%s", narrative)
	}

	mdResp, err := client.Get(ts.URL + resp.Request.URL.Path + "/markdown")
	if err != nil {
		t.Fatalf("GET markdown: %v", err)
	}
	md := readBody(t, mdResp)
	if !strings.Contains(md, "Signed the first EU customer.") || strings.Contains(md, "Hired the EU lead.") {
		t.Errorf("Markdown export should show only the Highlight pulled:\n%s", md)
	}
}

// A Report Definition saved from the form filtering on one value of a
// several-values Dimension selects a Goal that carries that value among others,
// and leaves out a Goal that carries only other values (ticket #69).
func TestReportFilterSelectsAGoalCarryingTheValueAmongSeveralOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	customer := h.CreateSeveralValuesDimension(boss, "Customer", "Acme", "Globex", "Initech")
	acme, globex, initech := customer.Values[0], customer.Values[1], customer.Values[2]
	both := h.ActiveGoal(boss, "Renew contracts", "Revenue depends on renewals.")
	h.AssignGoalValue(both, acme)
	h.AssignGoalValue(both, globex)
	other := h.ActiveGoal(boss, "Pilot Initech", "A new customer.")
	h.AssignGoalValue(other, initech)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports", url.Values{
		"name":   {"Globex MBR"},
		"filter": {strconv.FormatInt(globex.ID, 10)},
	})
	draft := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save report: status %d: %s", resp.StatusCode, draft)
	}
	if !strings.Contains(draft, both.Title) {
		t.Errorf("the Globex filter doesn't select %q, which carries Acme and Globex; body:\n%s", both.Title, draft)
	}
	if strings.Contains(draft, other.Title) {
		t.Errorf("the Globex filter selects %q, which carries only Initech; body:\n%s", other.Title, draft)
	}
}
