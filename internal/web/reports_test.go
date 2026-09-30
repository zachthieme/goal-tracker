package web_test

import (
	"context"
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
	if credit := pageElement(t, narrative, "span", "highlight-credit"); !strings.Contains(credit, "— alice@example.com, ") || !strings.Contains(credit, "Launch in EU") {
		t.Errorf("Highlight credit %q, want — owner, Goal", credit)
	}
}
