package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// A signed-in person saves a Report Definition of picked Goals and is shown a
// live draft of the selected Goals, with each Goal's title, Owner,
// Health, and due date (CONTEXT.md: Report Definition). Every Goal here was
// created within the default baseline, so each gets an exception block.
func TestSaveReportDefinitionAndSeeDraftOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")

	a := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	b := h.ActiveChildOf(boss, a, "Launch in EU", "Expand the market.")
	c := h.ActiveChildOf(boss, a, "Cut churn", "Keep customers.")
	h.Checkin(boss, b.ID, domain.HealthYellow, "slipping", "add staff", h.Clock.Now().AddDate(0, 1, 0))

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	// The builder offers the Goals to pick.
	form := getBody(t, client, ts.URL+"/reports/new")
	if !strings.Contains(pageElement(t, form, "select", "picked-select"), "Grow revenue") {
		t.Fatalf("the builder does not offer Goals to pick; body:\n%s", form)
	}

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":         {"EU MBR"},
		"introduction": {"Quarterly business review."},
		"mode":         {domain.ReportModePicked},
		"picked":       {strconv.FormatInt(a.ID, 10), strconv.FormatInt(b.ID, 10), strconv.FormatInt(c.ID, 10)},
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
}

// A Report Definition that selects nothing (no rules) is rejected.
func TestSaveReportDefinitionRejectsEmptySelection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{"name": {"Selects nothing"}, "mode": {domain.ReportModeRules}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("empty selection: status %d, want 422", resp.StatusCode)
	}
}

// The draft Report gives an exception the full block and an unchanged Green one
// line, read against the default baseline or one the reader picks. The Report's
// content is asserted on its view model in the domain tests; this checks the
// page wires the baseline through and renders both kinds.
func TestSmokeReportDraftShowsExceptionBlocksAgainstBaseline(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, green.ID}})

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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthYellow, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	other := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "WBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)

	draft := getBody(t, client, reportURL)
	if !strings.Contains(draft, `data-testid="no-publications"`) {
		t.Errorf("an unpublished definition does not say so; body:\n%s", draft)
	}
	resp := postForm(t, client, reportURL+"/publications", url.Values{})
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
	// By default publishing reads against the previous publication, so the
	// publish form carries no baseline.
	form := pageElement(t, draft, "form", "report-publish")
	if !strings.Contains(form, `action="/reports/`+strconv.FormatInt(def.ID, 10)+`/publications"`) || strings.Contains(form, `name="baseline"`) {
		t.Errorf("the default publish form carries a baseline; form:\n%s", form)
	}
	resp = postForm(t, client, reportURL+"/publications", url.Values{})
	if line := pageElement(t, readBody(t, resp), "p", "report-published"); !strings.Contains(line, "changes since the previous publication, 2026-02-11") {
		t.Errorf("publication does not read against the previous publication; line:\n%s", line)
	}

	// A date the reader picks lives in the URL and is carried into the
	// publication on the form's action, not in a hidden input.
	draft = getBody(t, client, reportURL+"?baseline=2026-01-01")
	form = pageElement(t, draft, "form", "report-publish")
	if !strings.Contains(form, `action="/reports/`+strconv.FormatInt(def.ID, 10)+`/publications?baseline=2026-01-01"`) || strings.Contains(form, `name="baseline"`) {
		t.Errorf("publish form does not carry the chosen baseline on its action alone; form:\n%s", form)
	}
	resp = postForm(t, client, reportURL+"/publications?baseline=2026-01-01", url.Values{})
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthRed, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	other := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "WBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthRed, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", Introduction: "Where the EU launch stands.", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	other := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "WBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	g := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(alice, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.Clock.Advance(time.Hour)
	h.CheckinWithHighlight(alice, g.ID, domain.HighlightMiss, "Lost the second customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
		"include-" + strconv.FormatInt(signed, 10): {"on"},
		"pick-" + strconv.FormatInt(signed, 10):    {domain.HighlightAccomplishment},
		"pick-" + strconv.FormatInt(lost, 10):      {domain.HighlightMiss},
		"text-" + domain.HighlightAccomplishment:   {"EU is open for business."},
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

	resp = postForm(t, client, reportURL+"/publications", url.Values{})
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

	resp = postForm(t, client, reportURL+"/narrative", url.Values{"include-9999": {"on"}, "pick-9999": {domain.HighlightInsight}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("picking an unknown Highlight: status %d, want 422", resp.StatusCode)
	}
}

// The author includes a Highlight by ticking its box and picks its section
// with a radio. A section radio without the box leaves the Highlight out; a
// ticked box without a section is refused (ticket #155).
func TestCurateNarrativeIncludesOnlyTickedHighlightsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.Clock.Advance(time.Hour)
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightMiss, "Lost the second customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	signed, lost := draftHighlightID(t, h, def, "Signed the first EU customer."), draftHighlightID(t, h, def, "Lost the second customer.")

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)

	resp := postForm(t, client, reportURL+"/narrative", url.Values{
		"include-" + strconv.FormatInt(signed, 10): {"on"},
		"pick-" + strconv.FormatInt(signed, 10):    {domain.HighlightMiss},
		"pick-" + strconv.FormatInt(lost, 10):      {domain.HighlightMiss},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("curate: status %d, want the draft page", resp.StatusCode)
	}
	narrative := pageElement(t, readBody(t, resp), "section", "report-narrative")
	if heading := pageElement(t, narrative, "h3", "narrative-section"); !strings.HasSuffix(heading, ">Misses") {
		t.Errorf("narrative's only section %q, want Misses", heading)
	}
	if !strings.Contains(narrative, "Signed the first EU customer.") {
		t.Errorf("narrative leaves out the ticked Highlight; narrative:\n%s", narrative)
	}
	if strings.Contains(narrative, "Lost the second customer.") || strings.Contains(narrative, "Accomplishments") {
		t.Errorf("narrative includes a Highlight whose box wasn't ticked; narrative:\n%s", narrative)
	}

	resp = postForm(t, client, reportURL+"/narrative", url.Values{"include-" + strconv.FormatInt(lost, 10): {"on"}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("including a Highlight without a section: status %d, want 422", resp.StatusCode)
	}
}

// On a fresh draft no Highlight is included, and each one's section control
// starts on its own kind. Once the author saves a Highlight as a Miss, its box
// is ticked and Miss is selected. The count says how many of the Highlights
// are in (ticket #155).
func TestCurationFormShowsWhatIsIncludedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.Clock.Advance(time.Hour)
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightInsight, "Buyers want invoices in euros.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	signed := strconv.FormatInt(draftHighlightID(t, h, def, "Signed the first EU customer."), 10)
	euros := strconv.FormatInt(draftHighlightID(t, h, def, "Buyers want invoices in euros."), 10)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)

	page := getBody(t, client, reportURL)
	if count := pageElement(t, page, "span", "curation-count"); !strings.HasSuffix(count, ">0 of 2 in") {
		t.Errorf("fresh draft's count %q, want 0 of 2 in", count)
	}
	// Without JS the radios stay in the form; CSS shows them only while the box
	// is ticked, and shows the Highlight's kind as text while it isn't.
	if rule := cssRule(t, page, ".rp-curate:not(:has(input[name^=include-]:checked)) .rp-sections"); !strings.Contains(rule, "display:none") {
		t.Errorf("an unticked Highlight's section control is not hidden: {%s}", rule)
	}
	if rule := cssRule(t, page, ".rp-curate:has(input[name^=include-]:checked) .rp-kind"); !strings.Contains(rule, "display:none") {
		t.Errorf("a ticked Highlight still shows its kind as text: {%s}", rule)
	}
	form := between(t, page, `<form data-testid="narrative-curation"`, "</form>")
	if kind := between(t, form, `<span class="rp-kind">`, "</span>"); !strings.Contains(kind, domain.HighlightInsight) {
		t.Errorf("unticked Highlight does not show its kind: %q", kind)
	}
	for id, kind := range map[string]string{signed: domain.HighlightAccomplishment, euros: domain.HighlightInsight} {
		if box := formInput(t, form, "include-"+id, "on"); isChecked(box) {
			t.Errorf("fresh draft includes Highlight %s: %s", id, box)
		}
		for _, section := range []string{domain.HighlightInsight, domain.HighlightAccomplishment, domain.HighlightMiss} {
			if radio := formInput(t, form, "pick-"+id, section); isChecked(radio) != (section == kind) {
				t.Errorf("fresh draft, Highlight %s's %s radio: %s; want only its own kind, %s, selected", id, section, radio, kind)
			}
		}
	}

	resp := postForm(t, client, reportURL+"/narrative", url.Values{
		"include-" + signed: {"on"},
		"pick-" + signed:    {domain.HighlightMiss},
	})
	page = readBody(t, resp)
	if count := pageElement(t, page, "span", "curation-count"); !strings.HasSuffix(count, ">1 of 2 in") {
		t.Errorf("count after saving one Highlight %q, want 1 of 2 in", count)
	}
	form = between(t, page, `<form data-testid="narrative-curation"`, "</form>")
	if box := formInput(t, form, "include-"+signed, "on"); !isChecked(box) {
		t.Errorf("saved Highlight's box is not ticked: %s", box)
	}
	for _, section := range []string{domain.HighlightInsight, domain.HighlightAccomplishment, domain.HighlightMiss} {
		if radio := formInput(t, form, "pick-"+signed, section); isChecked(radio) != (section == domain.HighlightMiss) {
			t.Errorf("Highlight saved as a Miss, its %s radio: %s; want only Miss selected", section, radio)
		}
	}
	if box := formInput(t, form, "include-"+euros, "on"); isChecked(box) {
		t.Errorf("a Highlight left out is ticked: %s", box)
	}
}

// Each section's text folds under the section's heading: open when the author
// has written some, closed and offering "+ Add note" when not (ticket #155).
func TestCurationFormFoldsEmptySectionTextOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	resp := postForm(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/narrative", url.Values{
		"text-" + domain.HighlightAccomplishment: {"EU is open for business."},
	})
	form := between(t, readBody(t, resp), `<form data-testid="narrative-curation"`, "</form>")

	written := pageElement(t, form, "details", "section-text-"+domain.HighlightAccomplishment)
	if open := tagAround(t, written, `data-testid="section-text-`); !strings.Contains(open, " open") {
		t.Errorf("section with text is folded: %s", open)
	}
	if summary := pageElement(t, written, "summary", "section-text-summary"); !strings.Contains(summary, "Accomplishments") || strings.Contains(summary, "+ Add note") {
		t.Errorf("written section's summary %q, want its heading alone", summary)
	}
	if !strings.Contains(written, "EU is open for business.") || !strings.Contains(written, `name="text-`+domain.HighlightAccomplishment+`"`) {
		t.Errorf("written section does not hold its text box; details:\n%s", written)
	}

	empty := pageElement(t, form, "details", "section-text-"+domain.HighlightMiss)
	if open := tagAround(t, empty, `data-testid="section-text-`); strings.Contains(open, " open") {
		t.Errorf("empty section is unfolded: %s", open)
	}
	if summary := pageElement(t, empty, "summary", "section-text-summary"); !strings.Contains(summary, "Misses") || !strings.Contains(summary, "+ Add note") {
		t.Errorf("empty section's summary %q, want its heading and + Add note", summary)
	}
	if !strings.Contains(empty, `name="text-`+domain.HighlightMiss+`"`) {
		t.Errorf("empty section has no text box; details:\n%s", empty)
	}
	if !strings.Contains(form, `<button type="submit" class="btn">Save`) {
		t.Errorf("curation form has no Save button for a browser without JS; form:\n%s", form)
	}
}

// formInput is the <input> in form with this name and value.
func formInput(t *testing.T, form, name, value string) string {
	t.Helper()
	for _, tag := range regexp.MustCompile(`<input[^>]*>`).FindAllString(form, -1) {
		if attr(tag, "name") == name && attr(tag, "value") == value {
			return tag
		}
	}
	t.Fatalf("form has no input %s=%s; form:\n%s", name, value, form)
	return ""
}

// isChecked reports whether an <input> tag carries checked.
func isChecked(tag string) bool {
	return regexp.MustCompile(`\schecked[\s>/=]`).MatchString(tag)
}

// draftHighlightID is the ID of the Highlight with this note in the Report
// Definition's draft.
func draftHighlightID(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition, note string) int64 {
	t.Helper()
	draft, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	for _, nh := range draft.Highlights {
		if nh.Highlight.Note == note {
			return nh.Highlight.ID
		}
	}
	t.Fatalf("draft has no Highlight %q", note)
	return 0
}

// The publication opens with a Health summary: how many of the Report's
// selected Goals are Red, Yellow, Green, and Stale, so an exec sees the shape
// of the org before reading a block.
func TestPublicationSummarisesHealthOverHTTP(t *testing.T) {
	t.Parallel()

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
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, yellow.ID, green.ID, stale.ID}})
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
// Needs attention, headed by how many there are, each with its Health badge and its So What, Status, and
// Path to Green — marked Overdue once its target date has passed — and the
// rest as a table of Health, Goal, Owner, and Due under On track.
func TestPublicationSplitsNeedsAttentionFromOnTrackOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 0, 1))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(3 * 24 * time.Hour)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, green.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	attention := pageElement(t, page, "section", "needs-attention")
	if heading := between(t, attention, "<h2>", "</h2>"); !strings.HasPrefix(heading, "<h2>Needs attention ") || !strings.HasSuffix(pageElement(t, heading, "span", "needs-attention-count"), ">1") {
		t.Errorf("Needs attention heading does not count its 1 exception: %s", heading)
	}
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	g := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(alice, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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

// The draft page keeps Publish in reach: a sticky header carries the name, the
// baseline, the save status, the History of publications and Publish — the
// primary button, saying what it does. Below it the author curates the
// narrative in the compose panel, then checks the preview beside it.
func TestDraftPagePublishesOnlyAfterThePreviewOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	header := between(t, page, `<header data-testid="draft-header"`, "</header>")
	last := -1
	for _, part := range []string{`data-testid="report-name"`, `data-testid="baseline-chip"`, `data-testid="save-status"`, `data-testid="report-history"`, `data-testid="publish-confirm"`} {
		at := strings.Index(header, part)
		if at <= last {
			t.Errorf("%s is out of order in the header (at %d, after %d); header:\n%s", part, at, last, header)
		}
		last = at
	}
	if chip := between(t, header, `data-testid="baseline-chip"`, `data-testid="save-status"`); !strings.Contains(chip, `data-testid="report-baseline"`) {
		t.Errorf("the baseline chip does not hold the baseline form; chip:\n%s", chip)
	}
	publish := pageElement(t, header, "details", "publish-confirm")
	if !strings.Contains(publish, `<summary class="btn primary">Publish…</summary>`) || !strings.Contains(publish, "Readers can comment on the frozen copy.") {
		t.Errorf("Publish… is not the primary button with its summary; disclosure:\n%s", publish)
	}
	history := pageElement(t, header, "details", "report-history")
	if !strings.Contains(history, "History (0)") || !strings.Contains(pageElement(t, history, "li", "no-publications"), "Not published yet.") {
		t.Errorf("an unpublished Report's History does not say so; menu:\n%s", history)
	}
	if strings.Contains(page, `data-testid="report-publications"`) {
		t.Errorf("publications are still listed in a sidebar; body:\n%s", page)
	}

	panes := between(t, page, `<div data-testid="draft-panes"`, "")
	curation, preview := strings.Index(panes, `data-testid="report-curation"`), strings.Index(panes, `id="draft-preview"`)
	if curation < 0 || preview < curation {
		t.Errorf("the narrative form does not come before the preview (at %d and %d); panes:\n%s", curation, preview, panes)
	}
	if !strings.Contains(between(t, panes, `data-testid="compose-panel"`, "</aside>"), `data-testid="narrative-curation"`) {
		t.Errorf("the narrative form is not in the compose panel; panes:\n%s", panes)
	}
}

// The reports page is headed Reports with a plain line on what a Report is,
// and New report links to the builder in place of a form of its own: POST
// /reports is gone. Each definition is a row of the status table: its name and
// scope, its Goal count, and, never published, "Never" and "Not published". Its
// one Goal has no Health yet, so the row draws no Health bar.
func TestReportsListLinksToTheBuilderOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	root := h.MarkTopLevel(boss, h.ActiveGoal(boss, "Grow revenue", "The org needs to grow."))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{root.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	page := getBody(t, client, ts.URL+"/reports")

	if !strings.Contains(page, "<h1>Reports</h1>") || strings.Contains(page, "Saved Report Definitions") {
		t.Errorf("the page is not headed Reports alone; body:\n%s", page)
	}
	if desc := pageElement(t, page, "p", "reports-description"); !strings.Contains(desc, "Publish a frozen copy") {
		t.Errorf("the page has no plain description: %s", desc)
	}
	if link := pageElement(t, page, "a", "new-report"); !strings.Contains(link, `href="/reports/new"`) || !strings.Contains(link, "New report") {
		t.Errorf("New report doesn't link to the builder: %s", link)
	}
	row := pageElement(t, page, "tr", "report-row")
	for _, want := range []string{
		fmt.Sprintf(`href="/reports/%d"`, def.ID), ">MBR</a>",
		`<span data-testid="report-scope" class="small muted">1 Goal picked by hand</span>`,
		`<span data-testid="report-goals">1</span>`,
		`<span data-testid="report-published" class="muted">Never</span>`,
		`<span data-testid="report-draft" class="muted">Not published</span>`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("row lacks %s; row:\n%s", want, row)
		}
	}
	if strings.Contains(row, "rp-bar") {
		t.Errorf("a report whose Goals have no Health draws a Health bar; row:\n%s", row)
	}
	if strings.Contains(page, "<form") && strings.Contains(page, `action="/reports"`) || strings.Contains(page, `data-testid="create-report"`) {
		t.Errorf("the reports page still has its own create form; body:\n%s", page)
	}
	resp := postForm(t, noRedirects(client), ts.URL+"/reports", url.Values{"name": {"Old way"}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /reports: status %d, want 405; body:\n%s", resp.StatusCode, body)
	}
}

// A published definition's row dates its last publication in the org's
// timezone, "Jan 1" for 03:00 UTC on 2 January in Los Angeles, with who
// published it; counts its draft's changes since in an amber pill; and draws
// its Goals that have a Health as a bar labelled for assistive technology,
// leaving out the three new Goals that have none yet.
func TestReportsListRowShowsTheLastPublicationAndChangesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	h.Clock.Set(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:  "MBR",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{strconv.FormatInt(boss.ID, 10)}}},
	})
	h.Clock.Set(time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC))
	h.PublishReport(ada, def)
	h.Clock.Advance(24 * time.Hour)
	for _, title := range []string{"Hire", "Ship", "Sell"} {
		h.ActiveGoal(boss, title, "why")
	}

	ts := httptest.NewServer(web.NewServer(domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))))
	t.Cleanup(ts.Close)
	row := pageElement(t, getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports"), "tr", "report-row")
	row = regexp.MustCompile(`>\s+<`).ReplaceAllString(row, "><")

	for _, want := range []string{
		`<span data-testid="report-scope" class="small muted">Owner: boss</span>`,
		`<span data-testid="report-goals">5</span>`,
		`<div data-testid="report-health-bar" class="rp-bar" role="img" aria-label="1 Green, 0 Yellow, 1 Red"><span class="g"></span><span class="r"></span></div>`,
		`<span data-testid="report-published">Jan 1</span>`,
		shownAs("ada.okafor@example.com", "Ada Okafor"),
		`<span data-testid="report-draft" class="rp-changes-hot">3 changes</span>`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("row lacks %s; row:\n%s", want, row)
		}
	}
}

// The status table lists never-published reports first, then those with
// changes, most first, then the rest by when they were last published, oldest
// first.
func TestReportsListSortsWhatNeedsPublishingFirstOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	report := func(name string) (domain.Account, domain.ReportDefinition) {
		owner := h.SignIn(strings.ToLower(name) + "@example.com")
		h.ActiveGoal(owner, name+" goal", "why")
		return owner, h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
			Name:  name,
			Mode:  domain.ReportModeRules,
			Rules: []domain.ReportRule{{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{strconv.FormatInt(owner.ID, 10)}}},
		})
	}
	_, alpha := report("Alpha")
	betaOwner, beta := report("Beta")
	echoOwner, echo := report("Echo")
	report("Yankee")
	_, zulu := report("Zulu")
	h.Clock.Advance(24 * time.Hour)
	h.PublishReport(boss, zulu)
	h.PublishReport(boss, beta)
	h.PublishReport(boss, echo)
	h.Clock.Advance(24 * time.Hour)
	h.PublishReport(boss, alpha)
	h.Clock.Advance(24 * time.Hour)
	h.ActiveGoal(betaOwner, "Beta, new", "why")
	for _, title := range []string{"Echo, new", "Echo, newer", "Echo, newest"} {
		h.ActiveGoal(echoOwner, title, "why")
	}

	ts := newServer(t, h)
	table := pageElement(t, getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports"), "table", "report-table")
	var got []string
	for _, m := range regexp.MustCompile(`<a href="/reports/\d+">([^<]+)</a>`).FindAllStringSubmatch(table, -1) {
		got = append(got, m[1])
	}
	if want := []string{"Yankee", "Echo", "Beta", "Zulu", "Alpha"}; !slices.Equal(got, want) {
		t.Errorf("reports listed %q, want %q", got, want)
	}
}

// At phone width the status table drops its column headings and each row
// stacks as a block, its cells one under another, so nothing scrolls the page
// sideways.
func TestReportsListRowsStackAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports")

	phone := between(t, page, "@media (max-width:600px){", "}}") + "}"
	for selector, want := range map[string]string{
		".rp-table thead": "display:none",
		".rp-table tr":    "display:block",
		".rp-table td":    "display:block",
	} {
		if rule := cssRule(t, phone, selector); !strings.Contains(rule, want) {
			t.Errorf("at 600px %s{%s} lacks %s", selector, rule, want)
		}
	}
}

// The print page sets the Report in the design system's serif, falling back to
// Georgia, and marks Health with a shape as well as its name so it survives
// black-and-white printing: ■ Red, ▲ Yellow, ● Green.
func TestPrintPageMarksHealthWithShapesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, green.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	printed := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")

	if !strings.Contains(printed, "--print-font: 'Source Serif 4', Georgia, serif") || !strings.Contains(printed, "font-family: var(--print-font)") {
		t.Errorf("print page is not set in Source Serif 4 with a Georgia fallback; body:\n%s", printed)
	}
	if count := pageElement(t, printed, "span", "needs-attention-count"); !strings.HasSuffix(count, ">1") {
		t.Errorf("print page's Needs attention does not count its 1 exception: %s", count)
	}
	if block := pageElement(t, printed, "article", "report-exception"); !strings.Contains(block, "■</span>Red") {
		t.Errorf("Red is not marked ■; block:\n%s", block)
	}
	if row := pageElement(t, printed, "tr", "selected-goal"); !strings.Contains(row, "●</span>Green") {
		t.Errorf("Green is not marked ●; row:\n%s", row)
	}
}

// The draft sets the compose panel beside the preview. At phone width the two
// panes stack into one column, compose first, and each pane shrinks to the
// viewport rather than to its widest content — the On track table scrolls
// inside its card instead of pushing the baseline line and the Narrative card
// off the right edge (#43).
func TestDraftPanesStackAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	panes := between(t, page, `<div data-testid="draft-panes"`, "")
	compose, preview := strings.Index(panes, `data-testid="compose-panel"`), strings.Index(panes, `id="draft-preview"`)
	if compose < 0 || preview < 0 || compose > preview {
		t.Errorf("the compose panel does not come before the preview in the panes (at %d and %d):\n%s", compose, preview, panes)
	}
	if !strings.Contains(cssRule(t, page, ".rp-panes>*"), "min-width:0") {
		t.Errorf("draft panes keep their content's minimum width, so a wide table scrolls the page sideways")
	}
	phone := between(t, page, "@media (max-width:900px){", "}}")
	if rule := cssRule(t, phone+"}", ".rp-panes"); !strings.Contains(rule, "grid-template-columns:minmax(0,1fr)") {
		t.Errorf("at 900px the draft panes are not one column: .rp-panes{%s}", rule)
	}
}

// The draft's own rules outrank the generic ones they sit inside (#173): a
// Highlight's section picker is an inline segmented control, not the form's
// column of fields, the Highlight list sits flush in its card, and the preview's
// Health tiles stay a row of four. The baseline chip is a pill.
func TestDraftRulesWinTheCascadeOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	// .rp-form fieldset sets display:flex;flex-direction:column, so the
	// segmented control needs a rule at least that specific.
	if rule := cssRule(t, page, ".rp-form fieldset.rp-sections"); !strings.Contains(rule, "display:inline-flex") || !strings.Contains(rule, "flex-direction:row") {
		t.Errorf("the section control is not an inline row: .rp-form fieldset.rp-sections{%s}", rule)
	}
	// .rp-card ul indents the exception cards' lists; the Highlight list sits
	// in a card too and stays flush.
	if rule := cssRule(t, page, ".rp-card .rp-list"); !strings.Contains(rule, "padding:0") {
		t.Errorf("the Highlight list keeps the card's list indent: .rp-card .rp-list{%s}", rule)
	}
	// .rp-body section stacks every section in the preview as a column; the
	// Health tiles stay four across, two at 900px.
	if rule := cssRule(t, page, ".rp-body .rp-tiles"); !strings.Contains(rule, "display:grid") || !strings.Contains(rule, "grid-template-columns:repeat(4,minmax(0,1fr))") {
		t.Errorf("the draft's Health tiles are not four across: .rp-body .rp-tiles{%s}", rule)
	}
	narrow := between(t, page, "@media (max-width:900px){", "}}") + "}"
	if rule := cssRule(t, narrow, ".rp-body .rp-tiles"); !strings.Contains(rule, "grid-template-columns:repeat(2,minmax(0,1fr))") {
		t.Errorf("at 900px the draft's Health tiles are not two across: .rp-body .rp-tiles{%s}", rule)
	}
	// The baseline chip is a bordered pill, as a .tag is.
	if rule := cssRule(t, page, ".rp-menu.rp-chip>summary"); !strings.Contains(rule, "border:1px solid") || !strings.Contains(rule, "border-radius:var(--radius-full)") {
		t.Errorf("the baseline chip is not a bordered pill: .rp-menu.rp-chip>summary{%s}", rule)
	}
}

// The Introduction belongs to the Report Definition, so the draft shows it
// read-only in the compose panel: there is nothing to type it into.
func TestDraftShowsTheIntroductionReadOnlyOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Introduction: "Where the EU launch stands.", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	compose := pageElement(t, page, "aside", "compose-panel")
	if intro := pageElement(t, compose, "section", "draft-introduction"); !strings.Contains(intro, "Where the EU launch stands.") {
		t.Errorf("the compose panel does not show the Introduction; section:\n%s", intro)
	}
	if strings.Contains(page, `name="introduction"`) {
		t.Errorf("the draft offers to edit the Introduction; body:\n%s", page)
	}
}

// The Print view has no hover, so it introduces each person as Name (email) at
// their first mention and by Name after that (CONTEXT.md: Name).
func TestPrintPageIntroducesEachPersonOnceOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	first := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	second := h.ActiveGoal(ada, "Cut churn", "Keep customers.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{first.ID, second.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	g := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "ceo@example.com")
	ceo := h.SignInNamed("ceo@example.com", "Dana Whitfield")
	g := h.ActiveGoal(ceo, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(ceo, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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

// A Retired Dimension drops out of the builder's rules, yet a Report
// Definition saved with a rule on its value still drafts the same Goals.
// Restoring the Dimension returns it to the builder (CONTEXT.md: Retired; ADR
// 0005).
func TestRetiredDimensionLeavesReportFormButSavedFilterKeepsWorkingOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.CreateDimension(boss, "Team", "Core")
	growth, trust := pillar.Values[0], pillar.Values[1]
	grower := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	truster := h.ActiveGoal(boss, "Earn trust", "Customers need to trust us.")
	h.AssignGoalValue(grower, growth)
	h.AssignGoalValue(truster, trust)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Growth MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{
		{Attribute: domain.RuleDimension, DimensionID: pillar.ID, Op: domain.RuleIsAnyOf, Values: []string{strconv.FormatInt(growth.ID, 10)}},
	}})
	ctx := context.Background()
	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	rules := pageElement(t, getBody(t, client, ts.URL+"/reports/new"), "section", "report-rules")
	if strings.Contains(rules, "Pillar") || strings.Contains(rules, "Growth") {
		t.Errorf("the Retired Pillar is offered for a Report rule:\n%s", rules)
	}
	if !strings.Contains(rules, "Team") {
		t.Errorf("the live Team isn't offered for a Report rule:\n%s", rules)
	}

	draft := getBody(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	if !strings.Contains(draft, grower.Title) || strings.Contains(draft, truster.Title) {
		t.Errorf("the saved Growth filter no longer drafts just %q; body:\n%s", grower.Title, draft)
	}

	if err := h.Service.RestoreDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RestoreDimension: %v", err)
	}
	rules = pageElement(t, getBody(t, client, ts.URL+"/reports/new"), "section", "report-rules")
	pillarKey := fmt.Sprintf("dimension:%d", pillar.ID)
	if !strings.Contains(rules, `<option value="`+pillarKey+`">Pillar</option>`) || !strings.Contains(rules, fmt.Sprintf(`value="%s=%d"`, pillarKey, growth.ID)) {
		t.Errorf("the restored Pillar isn't offered for a Report rule:\n%s", rules)
	}
}

// The builder offers the Fields still offered (none Retired) to show beside
// each Goal. The chosen ones then appear beside each Goal that has
// a value, with its unit, on the exception card and the On track row alike; a
// long text reads as a labelled value there, not in the narrative. A number
// Field is never totalled across Goals (ADR 0005). A definition that chose no
// Fields shows none (ticket #78).
func TestReportDefinitionShowsChosenFieldsOverHTTP(t *testing.T) {
	t.Parallel()

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

	choice := between(t, getBody(t, client, ts.URL+"/reports/new"), `<fieldset data-testid="report-field-choice"`, `</fieldset>`)
	for _, f := range []domain.Field{budget, notes, sponsor} {
		if !strings.Contains(choice, f.Name) || !strings.Contains(choice, fmt.Sprintf(`name="field" value="%d"`, f.ID)) {
			t.Errorf("the form doesn't offer %s to show; fieldset:\n%s", f.Name, choice)
		}
	}
	if strings.Contains(choice, legacy.Name) {
		t.Errorf("the form offers the Retired %s; fieldset:\n%s", legacy.Name, choice)
	}

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":   {"EU MBR"},
		"mode":   {domain.ReportModePicked},
		"picked": {strconv.FormatInt(red.ID, 10), strconv.FormatInt(green.ID, 10)},
		"field":  {strconv.FormatInt(budget.ID, 10), strconv.FormatInt(notes.ID, 10)},
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

	plain := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Plain MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, green.ID}})
	page := getBody(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, plain.ID))
	if strings.Contains(page, `data-testid="report-fields"`) || strings.Contains(page, "Counsel hired in March.") {
		t.Errorf("a definition with no Fields chosen shows Fields; body:\n%s", page)
	}
}

// A publication's page, its Print view and its Markdown all show the chosen
// Fields as they were published: editing the value, renaming the Field or
// retiring it afterwards changes none of them (ticket #78).
func TestPublicationKeepsTheChosenFieldsAsPublishedOverHTTP(t *testing.T) {
	t.Parallel()

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
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, green.ID}, FieldIDs: []int64{budget.ID}})
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
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	g := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlights(alice, g.ID,
		domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Signed the first EU customer."},
		domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Hired the EU lead."},
	)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
		"include-" + strconv.FormatInt(signed, 10): {"on"},
		"pick-" + strconv.FormatInt(signed, 10):    {domain.HighlightAccomplishment},
		"pick-" + strconv.FormatInt(hired, 10):     {domain.HighlightAccomplishment},
	})
	resp := postForm(t, client, reportURL+"/publications", url.Values{})
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

// A Report Definition saved from the builder with a rule on one value of a
// several-values Dimension selects a Goal that carries that value among others,
// and leaves out a Goal that carries only other values (ticket #69).
func TestReportFilterSelectsAGoalCarryingTheValueAmongSeveralOverHTTP(t *testing.T) {
	t.Parallel()

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

	dim := fmt.Sprintf("dimension:%d", customer.ID)
	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":               {"Globex MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {dim},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {fmt.Sprintf("%s=%d", dim, globex.ID)},
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

// The draft's header carries a History menu in place of the Publications
// sidebar: each publication newest first, with its publisher and the date and
// time it was published in the org's timezone, not UTC.
func TestDraftHistoryListsPublicationsInTheOrgsTimezoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	// 03:00 UTC on 2 and 3 March is the evening before in Los Angeles.
	h.Clock.Set(time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC))
	h.PublishReport(boss, def)
	h.Clock.Set(time.Date(2026, 3, 3, 3, 0, 0, 0, time.UTC))
	h.PublishReport(ada, def)

	ts := httptest.NewServer(web.NewServer(domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))))
	t.Cleanup(ts.Close)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	header := pageElement(t, page, "header", "draft-header")
	history := pageElement(t, header, "details", "report-history")
	if !strings.Contains(history, "<summary>History (2)</summary>") {
		t.Errorf("History menu does not count its publications; menu:\n%s", history)
	}
	items := strings.Split(history, `data-testid="report-publication"`)[1:]
	if len(items) != 2 {
		t.Fatalf("History lists %d publications, want 2; menu:\n%s", len(items), history)
	}
	for i, want := range []struct{ when, by string }{
		{"2 Mar 2026, 19:00 PST", shownAs("ada.okafor@example.com", "Ada Okafor")},
		{"1 Mar 2026, 19:00 PST", shownAs("boss@example.com", "boss")},
	} {
		if !strings.Contains(items[i], want.when) || !strings.Contains(items[i], want.by) {
			t.Errorf("History entry %d is not %s by %s; entry:\n%s", i+1, want.when, want.by, items[i])
		}
	}
	if strings.Contains(history, "UTC") {
		t.Errorf("History shows UTC times; menu:\n%s", history)
	}
	if strings.Contains(page, `data-testid="report-publications"`) {
		t.Errorf("the Publications sidebar is still on the draft; body:\n%s", page)
	}
}

// The draft's preview is the publication: the same Health summary and Report
// body a reader will see, read off the same component. The export links and
// the Action Items raised on a publication stay on the publication, since a
// draft has nothing to export or discuss yet.
func TestDraftPreviewIsThePublicationOverHTTP(t *testing.T) {
	t.Parallel()

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
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Introduction: "Where we stand.", Mode: domain.ReportModePicked, Picked: []int64{red.ID, yellow.ID, green.ID, stale.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	preview := between(t, page, `id="draft-preview"`, "")
	summary := pageElement(t, preview, "section", "health-summary")
	for tile, want := range map[string]string{"red": "1", "yellow": "1", "green": "1", "stale": "1"} {
		if count := pageElement(t, summary, "span", "health-count-"+tile); !strings.HasSuffix(count, ">"+want) {
			t.Errorf("preview's %s tile: %q, want a count of %s", tile, count, want)
		}
	}
	snapshot := between(t, preview, `<section data-testid="report-snapshot"`, "")
	for _, want := range []string{`data-testid="report-introduction"`, `data-testid="needs-attention"`, `data-testid="on-track"`} {
		if !strings.Contains(snapshot, want) {
			t.Errorf("preview's Report body missing %s; preview:\n%s", want, preview)
		}
	}
	for _, publicationOnly := range []string{`data-testid="report-export"`, `data-testid="publication-action-items"`} {
		if strings.Contains(page, publicationOnly) {
			t.Errorf("the draft carries the publication's %s; body:\n%s", publicationOnly, page)
		}
	}
}

// Above the preview the author sees which Goals the Report selects — every
// exception and every line, each once with its Health and the exceptions
// marked for attention — with a slot for the scope summary. The panel is for
// the author: the publication does not carry it.
func TestDraftListsTheGoalsInTheReportOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	stale := h.ActiveGoal(boss, "Open Tokyo", "Expand east.")
	proposed := h.CreateGoal(boss, "Hire a CFO", "We need one.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, green.ID, stale.ID, proposed.ID}})
	report, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	panel := between(t, page, `<section data-testid="report-goals"`, `id="draft-preview"`)
	if heading := pageElement(t, panel, "h2", "report-goal-count"); !strings.HasSuffix(heading, ">4 Goals in this report") {
		t.Errorf("Goals panel heading %q, want 4 Goals in this report", heading)
	}
	if n := len(report.Exceptions) + len(report.Lines); n != 4 {
		t.Errorf("the Report selects %d Goals, want the 4 the panel counts", n)
	}
	if !strings.Contains(panel, `data-testid="scope-summary"`) {
		t.Errorf("Goals panel has no scope summary slot; panel:\n%s", panel)
	}
	items := strings.Split(panel, `data-testid="report-goal"`)[1:]
	if len(items) != 4 {
		t.Fatalf("Goals panel lists %d Goals, want 4; panel:\n%s", len(items), panel)
	}
	for _, g := range []domain.Goal{red, green, stale, proposed} {
		if n := strings.Count(panel, ">"+g.Title+"<"); n != 1 {
			t.Errorf("Goals panel lists %q %d times, want once; panel:\n%s", g.Title, n, panel)
		}
	}
	for _, item := range items {
		if !strings.Contains(item, `class="dot"`) {
			t.Errorf("Goal in the panel has no Health dot: %s", item)
		}
	}
	if n := strings.Count(panel, `data-testid="report-goal-attention"`); n != len(report.Exceptions) {
		t.Errorf("Goals panel marks %d for attention, want the %d exceptions; panel:\n%s", n, len(report.Exceptions), panel)
	}
	for _, b := range report.Exceptions {
		if item := between(t, panel, ">"+b.Goal.Title+"<", "</li>"); !strings.Contains(item, ">attention<") {
			t.Errorf("exception %q is not marked for attention: %s", b.Goal.Title, item)
		}
	}

	published := readBody(t, postForm(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications", url.Values{}))
	if strings.Contains(published, `data-testid="report-goals"`) {
		t.Errorf("the publication carries the author's Goals panel; body:\n%s", published)
	}
}

// The Goals panel says what the definition selects in plain words, and marks
// "added" each Goal in it only because of Also include: not one listed there
// that meets the rules anyway, and never one picked by hand.
func TestDraftGoalsPanelShowsTheScopeAndMarksAlsoIncludeOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Dana Okafor")
	lee := h.SignIn("lee@example.com")
	matches := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	listedAndMatches := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	addedOnly := h.ActiveGoal(lee, "Open Tokyo", "Expand east.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:    "MBR",
		Mode:    domain.ReportModeRules,
		Rules:   []domain.ReportRule{{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{strconv.FormatInt(boss.ID, 10)}}},
		Include: []int64{listedAndMatches.ID, addedOnly.ID},
	})
	picked := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Picked", Mode: domain.ReportModePicked, Picked: []int64{matches.ID, addedOnly.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	panel := between(t, getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)), `<section data-testid="report-goals"`, `id="draft-preview"`)

	if scope := pageElement(t, panel, "span", "scope-summary"); !strings.HasSuffix(scope, ">Owner: Dana Okafor · 2 added") {
		t.Errorf("scope summary %q, want Owner: Dana Okafor · 2 added", scope)
	}
	for _, c := range []struct {
		goal  domain.Goal
		added bool
	}{{matches, false}, {listedAndMatches, false}, {addedOnly, true}} {
		item := between(t, panel, ">"+c.goal.Title+"<", "</li>")
		if got := strings.Contains(item, `data-testid="report-goal-added"`) && strings.Contains(item, ">added<"); got != c.added {
			t.Errorf("%q marked added %t, want %t: %s", c.goal.Title, got, c.added, item)
		}
	}

	pickedPanel := between(t, getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(picked.ID, 10)), `<section data-testid="report-goals"`, `id="draft-preview"`)
	if scope := pageElement(t, pickedPanel, "span", "scope-summary"); !strings.HasSuffix(scope, ">2 Goals picked by hand") {
		t.Errorf("picked scope summary %q, want 2 Goals picked by hand", scope)
	}
	if strings.Contains(pickedPanel, `data-testid="report-goal-added"`) {
		t.Errorf("a picked definition's panel marks a Goal added; panel:\n%s", pickedPanel)
	}
}

// The Goals panel shows the first 6 Goals and keeps the rest behind Show all.
func TestDraftGoalsPanelShowsSixThenTheRestOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	var roots []int64
	for i := 1; i <= 8; i++ {
		roots = append(roots, h.ActiveGoal(boss, fmt.Sprintf("Goal %d", i), "Matters.").ID)
	}
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: roots})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	panel := between(t, page, `<section data-testid="report-goals"`, `id="draft-preview"`)
	shown, rest, ok := strings.Cut(panel, `<details data-testid="report-goals-more"`)
	if !ok {
		t.Fatalf("Goals panel keeps no Goals behind Show all; panel:\n%s", panel)
	}
	if n := strings.Count(shown, `data-testid="report-goal"`); n != 6 {
		t.Errorf("Goals panel shows %d Goals up front, want 6; panel:\n%s", n, panel)
	}
	if !strings.Contains(rest, "<summary>Show all 8</summary>") || strings.Count(rest, `data-testid="report-goal"`) != 2 {
		t.Errorf("Show all 8 does not hold the other 2 Goals; details:\n%s", rest)
	}
}

// Readers see the Goals that entered or left a Report since its latest
// publication, each linking to the Goal, with Added or Left and why: on the
// draft's preview and on the publication. With none, neither shows the list.
func TestReportShowsTheGoalsThatEnteredOrLeftOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	stays := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	dropped := h.ActiveGoal(boss, "Hire a CFO", "We need one.")
	picked := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{stays.ID, dropped.ID}})
	first := h.PublishReport(boss, def)
	h.Clock.Advance(24 * time.Hour)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	draftURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	pubURL := func(p domain.Publication) string { return draftURL + "/publications/" + strconv.FormatInt(p.ID, 10) }
	for name, page := range map[string]string{"first publication": getBody(t, client, pubURL(first)), "unchanged draft": getBody(t, client, draftURL)} {
		if strings.Contains(page, `data-testid="membership-changes"`) {
			t.Errorf("%s with no Goal entering or leaving shows the list:\n%s", name, page)
		}
	}

	// The definition's picked list changes; editing it arrives with #151.
	if _, err := h.DB.Exec(`UPDATE report_definition_goals SET goal_id = ? WHERE report_definition_id = ? AND goal_id = ?`, picked.ID, def.ID, dropped.ID); err != nil {
		t.Fatalf("change the picked list: %v", err)
	}
	draft := getBody(t, client, draftURL)
	second := h.PublishReport(boss, def)
	for name, page := range map[string]string{"draft preview": between(t, draft, `id="draft-preview"`, ""), "publication": getBody(t, client, pubURL(second))} {
		list := pageElement(t, page, "section", "membership-changes")
		if !strings.Contains(list, "Goals that entered or left this report") {
			t.Errorf("%s list has no heading:\n%s", name, list)
		}
		for g, want := range map[domain.Goal][]string{picked: {"Added", "picked by hand"}, dropped: {"Left", "removed by hand"}} {
			row := between(t, list, fmt.Sprintf(`<tr data-testid="membership-change" id="membership-%d"`, g.ID), "</tr>")
			for _, w := range append(want, fmt.Sprintf(`href="/goals/%d"`, g.ID), g.Title) {
				if !strings.Contains(row, w) {
					t.Errorf("%s: %s's row missing %q:\n%s", name, g.Title, w, row)
				}
			}
		}
		if strings.Contains(list, stays.Title) {
			t.Errorf("%s lists %s, which stayed in:\n%s", name, stays.Title, list)
		}
	}
}

// A date the reader picks lives in the draft's URL: the narrative form carries
// it on its action, not a hidden input, and saving comes back to the same
// baseline.
func TestSavingTheNarrativeKeepsTheChosenBaselineOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	path := "/reports/" + strconv.FormatInt(def.ID, 10)

	form := pageElement(t, getBody(t, client, ts.URL+path+"?baseline=2026-01-01"), "form", "narrative-curation")
	if !strings.Contains(form, `action="`+path+`/narrative?baseline=2026-01-01"`) || strings.Contains(form, `name="baseline"`) {
		t.Errorf("narrative form does not carry the chosen baseline on its action alone; form:\n%s", form)
	}
	resp := postForm(t, client, ts.URL+path+"/narrative?baseline=2026-01-01", url.Values{"text-" + domain.HighlightInsight: {"Steady."}})
	_ = readBody(t, resp)
	if want := path + "?baseline=2026-01-01"; resp.StatusCode != http.StatusOK || resp.Request.URL.RequestURI() != want {
		t.Errorf("saving the narrative: status %d at %s, want back on %s", resp.StatusCode, resp.Request.URL.RequestURI(), want)
	}

	form = pageElement(t, getBody(t, client, ts.URL+path), "form", "narrative-curation")
	if !strings.Contains(form, `action="`+path+`/narrative"`) {
		t.Errorf("the default narrative form carries a baseline; form:\n%s", form)
	}
}

// The draft header's chip says what the draft reads against, picked from the
// URL: 30 days ago before the first publication, the last publication after
// it, and a date the reader chose over either. Its menu offers the Last
// publication only once there is one.
func TestBaselineChipSaysWhatTheDraftReadsAgainstOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	path := "/reports/" + strconv.FormatInt(def.ID, 10)
	chip := func(query string) string {
		t.Helper()
		header := pageElement(t, getBody(t, client, ts.URL+path+query), "header", "draft-header")
		return pageElement(t, header, "details", "baseline-chip")
	}
	label := func(chip string) string { return chipLabel(t, chip) }

	// Today is 2 Jan 2026, so 30 days ago is 3 Dec 2025.
	unpublished := chip("")
	if got := label(unpublished); got != "30 days ago · Dec 3" {
		t.Errorf("unpublished chip label %q, want %q; chip:\n%s", got, "30 days ago · Dec 3", unpublished)
	}
	if !strings.Contains(unpublished, "Changes since <strong") {
		t.Errorf("chip does not read Changes since …; chip:\n%s", unpublished)
	}
	if strings.Contains(unpublished, `data-testid="baseline-last-publication"`) {
		t.Errorf("an unpublished Report's chip offers the last publication; chip:\n%s", unpublished)
	}
	if item := pageElement(t, unpublished, "a", "baseline-30-days"); !strings.Contains(item, `href="`+path+`?baseline=2025-12-03"`) {
		t.Errorf("30 days ago does not link to 2025-12-03; item:\n%s", item)
	}
	pick := pageElement(t, unpublished, "form", "report-baseline")
	if !strings.Contains(pick, `method="get"`) || !strings.Contains(pick, `type="date" name="baseline"`) || !strings.Contains(pick, "Apply") {
		t.Errorf("Pick a date… is not a GET date form with Apply; form:\n%s", pick)
	}

	h.Clock.Set(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC))
	h.PublishReport(boss, def)
	h.Clock.Set(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))

	if page := getBody(t, client, ts.URL+path); strings.Contains(page, "Showing what changed since") || strings.Count(page, `data-testid="report-baseline"`) != 1 {
		t.Errorf("the draft keeps the old baseline sentence or a baseline form outside the chip; body:\n%s", page)
	}
	published := chip("")
	if got := label(published); got != "last publication · Sep 22" {
		t.Errorf("published chip label %q, want %q; chip:\n%s", got, "last publication · Sep 22", published)
	}
	if item := pageElement(t, published, "a", "baseline-last-publication"); !strings.Contains(item, `href="`+path+`"`) {
		t.Errorf("Last publication does not link to the draft with no baseline; item:\n%s", item)
	}
	if item := pageElement(t, published, "a", "baseline-30-days"); !strings.Contains(item, `href="`+path+`?baseline=2026-09-04"`) {
		t.Errorf("30 days ago does not link to 2026-09-04; item:\n%s", item)
	}

	chosen := chip("?baseline=2026-01-01")
	if got := label(chosen); got != "Jan 1" {
		t.Errorf("chosen chip label %q, want %q; chip:\n%s", got, "Jan 1", chosen)
	}
	if !strings.Contains(chosen, `data-testid="baseline-last-publication"`) {
		t.Errorf("after a chosen date the chip no longer offers the last publication; chip:\n%s", chosen)
	}
}

// The chip dates a publication in the org's calendar: published the evening
// of 1 March in Los Angeles (03:00 UTC on 2 March), it reads Mar 1.
func TestBaselineChipDatesThePublicationInTheOrgsTimezoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.Clock.Set(time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC))
	h.PublishReport(boss, def)

	ts := httptest.NewServer(web.NewServer(domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))))
	t.Cleanup(ts.Close)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	chip := pageElement(t, pageElement(t, page, "header", "draft-header"), "details", "baseline-chip")
	if got := chipLabel(t, chip); got != "last publication · Mar 1" {
		t.Errorf("chip label %q, want %q; chip:\n%s", got, "last publication · Mar 1", chip)
	}
}

// Closing an Action Item from the draft comes back to the same draft, the
// chosen baseline included.
func TestClosingAnActionItemFromTheDraftKeepsTheBaselineOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	if _, err := h.Service.RaiseActionItem(context.Background(), boss.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Get a second vendor quote.", OwnerID: boss.ID, DueDate: h.Clock.Now(),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	path := "/reports/" + strconv.FormatInt(def.ID, 10) + "?baseline=2026-01-01"

	form := pageElement(t, getBody(t, client, ts.URL+path), "form", "close-action-item")
	if want := `name="return" value="` + path + `"`; !strings.Contains(form, want) {
		t.Errorf("close form does not return to %s; form:\n%s", path, form)
	}
}

// chipLabel is the bold part of the baseline chip: what it says the draft
// reads changes since.
func chipLabel(t *testing.T, chip string) string {
	t.Helper()
	const start = `data-testid="baseline-label">`
	return strings.TrimPrefix(between(t, chip, start, "</strong>"), start)
}

// Publish… opens a confirmation in the draft header: what is about to be
// frozen, counted from the draft, and the publish form beside a Cancel that
// reloads the draft as it stands, baseline and all.
func TestPublishConfirmsWhatItWillFreezeOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.Clock.Set(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	late := h.ActiveGoal(boss, "Open Tokyo", "Expand east.")
	churn := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	hire := h.ActiveGoal(boss, "Hire a CFO", "We need one.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Platform MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, late.ID, churn.ID, hire.ID}})
	h.Clock.Set(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC))
	h.PublishReport(boss, def)
	h.Clock.Set(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, late.ID, domain.HealthRed, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 1, 0))
	h.CheckinWithHighlights(boss, churn.ID,
		domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Churn halved."},
		domain.HighlightInput{Kind: domain.HighlightInsight, Note: "Churn is seasonal."})
	h.CheckinWithHighlight(boss, hire.ID, domain.HighlightMiss, "Top candidate declined.")
	draft, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(draft.Exceptions) != 2 || len(draft.Lines) != 2 || len(draft.Highlights) != 3 {
		t.Fatalf("draft has %d exceptions, %d lines and %d Highlights, want 2, 2 and 3", len(draft.Exceptions), len(draft.Lines), len(draft.Highlights))
	}
	var halved int64
	for _, nh := range draft.Highlights {
		if nh.Highlight.Note == "Churn halved." {
			halved = nh.Highlight.ID
		}
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	path := "/reports/" + strconv.FormatInt(def.ID, 10)
	_ = readBody(t, postForm(t, client, ts.URL+path+"/narrative", url.Values{
		"include-" + strconv.FormatInt(halved, 10): {"on"},
		"pick-" + strconv.FormatInt(halved, 10):    {domain.HighlightAccomplishment},
	}))

	page := getBody(t, client, ts.URL+path)
	if n := strings.Count(page, `action="`+path+`/publications`); n != 1 {
		t.Errorf("the draft has %d forms publishing, want 1; body:\n%s", n, page)
	}
	header := pageElement(t, page, "header", "draft-header")
	confirm := pageElement(t, header, "details", "publish-confirm")
	if summary := between(t, confirm, "<summary", "</summary>"); !strings.Contains(summary, "Publish…") {
		t.Errorf("the disclosure does not open on Publish…; summary:\n%s", summary)
	}
	if !strings.Contains(confirm, "Publish Platform MBR?") {
		t.Errorf("the confirmation does not name the Report; disclosure:\n%s", confirm)
	}
	const want = "4 Goals · 2 need attention · 1 Highlight · changes since last publication · Sep 22. Readers can comment on the frozen copy."
	if got := pageElement(t, confirm, "p", "publish-summary"); !strings.HasSuffix(got, ">"+want) {
		t.Errorf("publish summary %q, want %q", got, want)
	}
	form := pageElement(t, confirm, "form", "report-publish")
	if !strings.Contains(form, `method="post"`) || !strings.Contains(form, `action="`+path+`/publications"`) || !strings.Contains(form, `class="btn primary"`) {
		t.Errorf("the default publish form is not a POST to the publications with no baseline; form:\n%s", form)
	}
	if cancel := pageElement(t, confirm, "a", "publish-cancel"); !strings.Contains(cancel, `href="`+path+`"`) || !strings.HasSuffix(cancel, ">Cancel") {
		t.Errorf("Cancel does not reload the draft; link:\n%s", cancel)
	}

	confirm = pageElement(t, getBody(t, client, ts.URL+path+"?baseline=2026-01-01"), "details", "publish-confirm")
	if got := pageElement(t, confirm, "p", "publish-summary"); !strings.Contains(got, "· changes since Jan 1.") {
		t.Errorf("publish summary %q does not read against the chosen date", got)
	}
	if form := pageElement(t, confirm, "form", "report-publish"); !strings.Contains(form, `action="`+path+`/publications?baseline=2026-01-01"`) {
		t.Errorf("the publish form does not carry the chosen baseline; form:\n%s", form)
	}
	if cancel := pageElement(t, confirm, "a", "publish-cancel"); !strings.Contains(cancel, `href="`+path+`?baseline=2026-01-01"`) {
		t.Errorf("Cancel does not keep the chosen baseline; link:\n%s", cancel)
	}
	resp := postForm(t, client, ts.URL+path+"/publications?baseline=2026-01-01", url.Values{})
	if line := pageElement(t, readBody(t, resp), "p", "report-published"); !strings.Contains(line, "changes since 2026-01-01") {
		t.Errorf("publication does not read against the chosen baseline; line:\n%s", line)
	}
}

// The compose form autosaves: htmx posts it and gets back only the preview,
// showing the narrative as saved, and the header's save status saying so. The
// compose fields aren't in the answer, so what the author is typing stays put.
// A plain post still reloads the draft (ticket #156).
func TestAutosaveNarrativeRefreshesThePreviewOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	signed := strconv.FormatInt(draftHighlightID(t, h, def, "Signed the first EU customer."), 10)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	form := url.Values{
		"include-" + signed:                      {"on"},
		"pick-" + signed:                         {domain.HighlightAccomplishment},
		"text-" + domain.HighlightAccomplishment: {"EU is open for business."},
	}

	resp, body := postHX(t, client, reportURL+"/narrative", form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("autosave: status %d, want 200; body:\n%s", resp.StatusCode, body)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, `data-testid="narrative-curation"`) {
		t.Errorf("autosave answers more than the preview; body:\n%s", body)
	}
	preview := between(t, body, `id="draft-preview"`, "")
	narrative := pageElement(t, preview, "section", "report-narrative")
	for _, want := range []string{"EU is open for business.", "Signed the first EU customer."} {
		if !strings.Contains(narrative, want) {
			t.Errorf("autosaved preview missing %q; preview:\n%s", want, preview)
		}
	}
	status := tagAround(t, body, `data-testid="save-status"`)
	if !strings.Contains(status, `hx-swap-oob="true"`) {
		t.Errorf("the save status is not swapped out of band: %s", status)
	}
	if result := pageElement(t, body, "span", "save-result"); !strings.HasSuffix(result, ">Saved") {
		t.Errorf("the save status does not say Saved; body:\n%s", body)
	}

	plain := postForm(t, client, reportURL+"/narrative", form)
	if plain.StatusCode != http.StatusOK || plain.Request.URL.Path != "/reports/"+strconv.FormatInt(def.ID, 10) {
		t.Errorf("plain post: status %d at %s, want a redirect to the draft", plain.StatusCode, plain.Request.URL)
	}
}

// An autosave the domain refuses — a Highlight not on a Goal the Report
// selects — changes only the save status, saying why, and tells htmx to swap
// nothing else: the preview and what the author typed stay (ticket #156).
func TestAutosaveRefusalOnlyChangesTheSaveStatusOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	resp, body := postHX(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/narrative", url.Values{
		"include-9999": {"on"},
		"pick-9999":    {domain.HighlightInsight},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refused autosave: status %d, want 200 so htmx swaps the status; body:\n%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("HX-Reswap"); got != "none" {
		t.Errorf("HX-Reswap = %q, want none", got)
	}
	if !strings.Contains(tagAround(t, body, `data-testid="save-status"`), `hx-swap-oob="true"`) {
		t.Errorf("the save status is not swapped out of band; body:\n%s", body)
	}
	result := pageElement(t, body, "span", "save-result")
	if !strings.Contains(result, "is not on a Goal this Report selects") || strings.Contains(result, "validation failed") {
		t.Errorf("the save status %q, want the domain's reason without its prefix", result)
	}
	for _, untouched := range []string{`data-testid="narrative-curation"`, `id="draft-preview"`, "<html"} {
		if strings.Contains(body, untouched) {
			t.Errorf("refused autosave answers %s; body:\n%s", untouched, body)
		}
	}
}

// The compose form autosaves as the author types — debounced, the latest edit
// replacing one in flight — to its own action, the reader's baseline and all,
// swapping in the preview beside it. The save status says Saving… meanwhile
// and Not saved when the answer is an error or never comes (ticket #156).
func TestComposeFormAutosavesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"?baseline=2025-12-01")
	form := openTag(pageElement(t, page, "form", "narrative-curation"))
	action := html.UnescapeString(attr(form, "action"))
	if want := "/reports/" + strconv.FormatInt(def.ID, 10) + "/narrative?baseline=2025-12-01"; action != want {
		t.Fatalf("the compose form posts to %q, want %q", action, want)
	}
	for name, want := range map[string]string{
		"hx-post":      action,
		"hx-trigger":   "change, input delay:400ms",
		"hx-target":    "#draft-preview",
		"hx-swap":      "outerHTML",
		"hx-sync":      "this:replace",
		"hx-indicator": "#save-status",
	} {
		if got := html.UnescapeString(attr(form, name)); got != want {
			t.Errorf("compose form %s = %q, want %q", name, got, want)
		}
	}
	for _, event := range []string{"hx-on:htmx:response-error", "hx-on:htmx:send-error"} {
		handler := html.UnescapeString(attr(form, event))
		if !strings.Contains(handler, "save-result") || !strings.Contains(handler, "Not saved") || strings.Contains(handler, "htmx.swap") {
			t.Errorf("compose form %s = %q, want it to put Not saved in the save status", event, handler)
		}
	}
	if !strings.Contains(pageElement(t, page, "span", "save-status"), "Saving…") {
		t.Errorf("the save status has no Saving… indicator; page:\n%s", page)
	}
	// Autosave covers readers with JavaScript, so only readers without it see
	// Save narrative.
	if !strings.Contains(pageElement(t, page, "form", "narrative-curation"), `<noscript><button type="submit" class="btn">Save narrative</button></noscript>`) {
		t.Errorf("the compose form's Save button is not there for, and only for, readers without JavaScript")
	}
}

// postHX posts a form with the HX-Request header set, as htmx does, and
// returns the response, its headers intact, and its body.
func postHX(t *testing.T, client *http.Client, rawURL string, form url.Values) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	return resp, readBody(t, resp)
}

// Editing a published definition's name and scope answers 303 to its draft,
// which then shows the new name and exactly the new Goals; the publication
// still shows the old name and the old Goals (ticket #151).
func TestEditDefinitionChangesOnlyTheDraftOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Web")
	platform, web := team.Values[0], team.Values[1]
	reliability := h.ActiveGoal(boss, "Platform reliability", "Matters.")
	redesign := h.ActiveGoal(boss, "Web redesign", "Matters.")
	h.AssignGoalValue(reliability, platform)
	h.AssignGoalValue(redesign, web)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:  "Platform MBR",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: domain.RuleDimension, DimensionID: team.ID, Op: domain.RuleIs, Values: []string{strconv.FormatInt(platform.ID, 10)}}},
	})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	draftPath := "/reports/" + strconv.FormatInt(def.ID, 10)
	dim := "dimension:" + strconv.FormatInt(team.ID, 10)

	resp := postForm(t, noRedirects(client), ts.URL+draftPath+"/edit", url.Values{
		"name":               {"Web MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {dim},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {dim + "=" + strconv.FormatInt(web.ID, 10)},
	})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != draftPath {
		t.Fatalf("edit: status %d to %q, want 303 to %s", resp.StatusCode, resp.Header.Get("Location"), draftPath)
	}
	draft := getBody(t, client, ts.URL+draftPath)
	if name := pageElement(t, draft, "h1", "report-name"); !strings.Contains(name, "Web MBR") {
		t.Errorf("the draft is named %s, want Web MBR", name)
	}
	if got, want := draftGoalTitles(t, draft), []string{redesign.Title}; !slices.Equal(got, want) {
		t.Errorf("the edited draft selects %q, want %q", got, want)
	}

	published := getBody(t, client, ts.URL+draftPath+"/publications/"+strconv.FormatInt(pub.ID, 10))
	if name := pageElement(t, published, "h1", "report-name"); !strings.Contains(name, "Platform MBR") {
		t.Errorf("the publication is named %s, want the old Platform MBR", name)
	}
	if !strings.Contains(published, fmt.Sprintf(`id="goal-%d"`, reliability.ID)) || strings.Contains(published, fmt.Sprintf(`id="goal-%d"`, redesign.ID)) {
		t.Errorf("the publication doesn't keep its old Goals; body:\n%s", published)
	}
}

// The edit page is the builder filled with the saved definition: its name,
// introduction, mode, rules, Also include, Leave out and Fields, posting back
// to /reports/{id}/edit, its rail reading against the definition (ticket
// #151). A picked definition comes back with its picked Goals.
func TestEditDefinitionShowsTheSavedValuesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Web")
	platform := team.Values[0]
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "USD")
	sdk := h.ActiveGoal(boss, "Mobile SDK auth update", "Matters.")
	sso := h.ActiveGoal(boss, "Legacy SSO cleanup", "Matters.")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	rules := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:         "Platform MBR",
		Introduction: "Where Platform stands.",
		Mode:         domain.ReportModeRules,
		Rules: []domain.ReportRule{
			{Attribute: domain.RuleDimension, DimensionID: team.ID, Op: domain.RuleIs, Values: []string{id(platform.ID)}},
			{Attribute: domain.RuleLifecycle, Op: domain.RuleIsNot, Values: []string{domain.LifecycleOnHold}},
		},
		Include:  []int64{sdk.ID},
		Exclude:  []int64{sso.ID},
		FieldIDs: []int64{budget.ID},
	})
	picked := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Picked MBR", Mode: domain.ReportModePicked, Picked: []int64{sso.ID}})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	dim := "dimension:" + id(team.ID)

	page := getBody(t, client, ts.URL+"/reports/"+id(rules.ID)+"/edit")
	builder := pageElement(t, page, "form", "report-builder")
	for _, want := range []string{
		`action="/reports/` + id(rules.ID) + `/edit"`,
		`<input type="hidden" name="id" value="` + id(rules.ID) + `">`,
		`name="name" value="Platform MBR"`,
		`name="introduction">Where Platform stands.</textarea>`,
		`value="rules" checked`,
		`<option value="` + dim + `" selected>Team</option>`,
		`<option value="` + dim + "=" + id(platform.ID) + `" selected>Platform</option>`,
		`<option value="lifecycle" selected>Lifecycle</option>`,
		`<option value="is not" selected>is not</option>`,
		`<option value="lifecycle=` + domain.LifecycleOnHold + `" selected>`,
		`<input type="hidden" name="include" value="` + id(sdk.ID) + `">`,
		`<input type="hidden" name="exclude" value="` + id(sso.ID) + `">`,
		`name="field" value="` + id(budget.ID) + `" checked`,
	} {
		if !strings.Contains(builder, want) {
			t.Errorf("the edit page lacks %s:\n%s", want, builder)
		}
	}
	if !strings.Contains(page, "Edit report") {
		t.Errorf("the edit page isn't titled Edit report; body:\n%s", page)
	}

	builder = pageElement(t, getBody(t, client, ts.URL+"/reports/"+id(picked.ID)+"/edit"), "form", "report-builder")
	for _, want := range []string{
		`value="picked" checked`,
		`<input type="hidden" name="picked" value="` + id(sso.ID) + `">`,
	} {
		if !strings.Contains(builder, want) {
			t.Errorf("the picked edit page lacks %s:\n%s", want, builder)
		}
	}
}

// A refused edit answers 422 with the builder as typed, still posting to the
// edit, each error beside its input, and the definition as it was. Add rule
// on the edit page comes back to it, 200, having saved nothing (ticket #151).
func TestEditDefinitionRefusalKeepsValuesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	editURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/edit"
	typed := url.Values{
		"name":               {"  "},
		"introduction":       {"Now by rules."},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleLifecycle},
		"rules[0].op":        {domain.RuleIs},
	}

	resp := postForm(t, client, editURL, typed)
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("refused edit: status %d, want 422; body:\n%s", resp.StatusCode, page)
	}
	builder := pageElement(t, page, "form", "report-builder")
	for _, want := range []string{
		`action="/reports/` + strconv.FormatInt(def.ID, 10) + `/edit"`,
		`name="introduction">Now by rules.</textarea>`,
		`value="rules" checked`,
		`<option value="lifecycle" selected>Lifecycle</option>`,
	} {
		if !strings.Contains(builder, want) {
			t.Errorf("the refused edit lost %s:\n%s", want, builder)
		}
	}
	if nameField := between(t, builder, `<span>Name</span>`, `</label>`); !strings.Contains(nameField, `id="name-error"`) {
		t.Errorf("the blank name's error isn't beside it:\n%s", nameField)
	}
	if row := between(t, builder, `data-testid="report-rule"`, `data-testid="rules-hint"`); !strings.Contains(row, `id="rules[0]-error"`) {
		t.Errorf("the Lifecycle rule with no value isn't refused beside it:\n%s", row)
	}
	if got, _ := h.Service.GetReportDefinition(context.Background(), def.ID); got.Name != "MBR" || got.Mode != domain.ReportModePicked {
		t.Errorf("a refused edit changed the definition to %q, %s", got.Name, got.Mode)
	}

	typed.Set("do", "add-rule")
	resp = postForm(t, client, editURL, typed)
	page = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Add rule on the edit page: status %d, want 200; body:\n%s", resp.StatusCode, page)
	}
	builder = pageElement(t, page, "form", "report-builder")
	if !strings.Contains(builder, `action="/reports/`+strconv.FormatInt(def.ID, 10)+`/edit"`) || strings.Count(builder, `data-testid="report-rule"`) != 2 {
		t.Errorf("Add rule didn't come back to the edit page with two rule rows:\n%s", builder)
	}
	if got, _ := h.Service.GetReportDefinition(context.Background(), def.ID); got.Name != "MBR" {
		t.Errorf("Add rule saved the edit: name %q", got.Name)
	}
}

// Only the definition's creator or an Admin may edit it: anyone else signed in
// gets 403 on both the page and the post, which changes nothing. An unknown
// definition is Not found (ticket #151).
func TestEditDefinitionOnlyByItsCreatorOrAnAdminOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	h.SignIn("sam@example.com")
	g := h.ActiveGoal(alice, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(alice, domain.SaveReportDefinitionInput{Name: "Alice's MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)
	editURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/edit"
	edit := func(name string) url.Values {
		return url.Values{"name": {name}, "mode": {domain.ReportModePicked}, "picked": {strconv.FormatInt(g.ID, 10)}}
	}

	sam := signInClient(t, ts.URL, "sam@example.com")
	resp, err := sam.Get(editURL)
	if err != nil {
		t.Fatalf("GET %s: %v", editURL, err)
	}
	if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden {
		t.Errorf("Sam's GET of Alice's edit page: status %d, want 403; body:\n%s", resp.StatusCode, body)
	}
	resp = postForm(t, noRedirects(sam), editURL, edit("Sam's now"))
	if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden {
		t.Errorf("Sam's POST to Alice's edit: status %d, want 403; body:\n%s", resp.StatusCode, body)
	}
	if got, _ := h.Service.GetReportDefinition(context.Background(), def.ID); got.Name != "Alice's MBR" {
		t.Errorf("Sam's refused edit renamed it %q", got.Name)
	}

	for _, who := range []string{"alice@example.com", "boss@example.com"} {
		client := signInClient(t, ts.URL, who)
		getBody(t, client, editURL)
		resp := postForm(t, noRedirects(client), editURL, edit("Edited by "+who))
		if body := readBody(t, resp); resp.StatusCode != http.StatusSeeOther {
			t.Errorf("%s's edit: status %d, want 303; body:\n%s", who, resp.StatusCode, body)
		}
	}

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, err := http.NewRequest(method, ts.URL+"/reports/9999/edit", strings.NewReader(edit("Nobody's").Encode()))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := sam.Do(req)
		if err != nil {
			t.Fatalf("%s an unknown definition's edit: %v", method, err)
		}
		if body := readBody(t, resp); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s an unknown definition's edit: status %d, want 404; body:\n%s", method, resp.StatusCode, body)
		}
	}
}

// Edit definition links to the edit page from both the draft's header and its
// Goals panel, for the definition's creator and an Admin; anyone else sees
// neither (ticket #151).
func TestDraftLinksToEditTheDefinitionForThoseWhoMayOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	h.SignIn("sam@example.com")
	g := h.ActiveGoal(alice, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(alice, domain.SaveReportDefinitionInput{Name: "Alice's MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)
	draftURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	link := `data-testid="edit-definition"`
	href := `href="/reports/` + strconv.FormatInt(def.ID, 10) + `/edit"`

	for _, who := range []string{"alice@example.com", "boss@example.com"} {
		draft := getBody(t, signInClient(t, ts.URL, who), draftURL)
		header := pageElement(t, draft, "header", "draft-header")
		panel := pageElement(t, draft, "section", "report-goals")
		for where, part := range map[string]string{"header": header, "Goals panel": panel} {
			if at := strings.Index(part, link); at < 0 || !strings.Contains(part[at:], href) {
				t.Errorf("%s's draft %s has no Edit definition linking to the edit page:\n%s", who, where, part)
			}
		}
	}
	if draft := getBody(t, signInClient(t, ts.URL, "sam@example.com"), draftURL); strings.Contains(draft, link) {
		t.Errorf("Sam, who may not edit, is offered Edit definition; body:\n%s", draft)
	}
}

// A saved rule on a Dimension or value since Retired still comes back on the
// edit page, chosen, so saving the edit keeps it; a new rule still can't
// choose them (CONTEXT.md: Retired; ticket #151).
func TestEditDefinitionKeepsARuleOnARetiredDimensionOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	team := h.CreateDimension(boss, "Team", "Core", "Edge")
	growth, core := pillar.Values[0], team.Values[0]
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{
		{Attribute: domain.RuleDimension, DimensionID: pillar.ID, Op: domain.RuleIs, Values: []string{id(growth.ID)}},
		{Attribute: domain.RuleDimension, DimensionID: team.ID, Op: domain.RuleIs, Values: []string{id(core.ID)}},
	}})
	ctx := context.Background()
	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	if err := h.Service.RetireDimensionValue(ctx, boss.ID, core.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pillarKey, teamKey := "dimension:"+id(pillar.ID), "dimension:"+id(team.ID)

	builder := pageElement(t, getBody(t, client, ts.URL+"/reports/"+id(def.ID)+"/edit"), "form", "report-builder")
	rows := strings.Split(builder, `data-testid="report-rule"`)[1:]
	if len(rows) != 2 {
		t.Fatalf("the edit page shows %d rule rows, want 2:\n%s", len(rows), builder)
	}
	for _, want := range []string{
		`<option value="` + pillarKey + `" selected>Pillar</option>`,
		`<option value="` + pillarKey + "=" + id(growth.ID) + `" selected>Growth</option>`,
	} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("the rule on the Retired Pillar lacks %s:\n%s", want, rows[0])
		}
	}
	if want := `<option value="` + teamKey + "=" + id(core.ID) + `" selected>Core</option>`; !strings.Contains(rows[1], want) {
		t.Errorf("the rule on the Retired Core lacks %s:\n%s", want, rows[1])
	}
	if strings.Contains(rows[1], `value="`+pillarKey+`"`) {
		t.Errorf("the Retired Pillar is offered to a rule that doesn't test it:\n%s", rows[1])
	}
	if strings.Contains(rows[0], `value="`+teamKey+"="+id(core.ID)+`"`) {
		t.Errorf("the Retired Core is offered to a rule that doesn't test it:\n%s", rows[0])
	}
}
