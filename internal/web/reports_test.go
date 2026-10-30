package web_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	nethtml "golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/export"
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

	a := h.ActiveGoal(boss, "Grow revenue", "The company needs to grow.")
	b := h.ActiveChildOf(boss, a, "Launch in EU", "Expand the market.")
	c := h.ActiveChildOf(boss, a, "Cut churn", "Keep customers.")
	h.Checkin(boss, b.ID, domain.HealthYellow, "slipping", "add staff", h.Clock.Now().AddDate(0, 1, 0))

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	// The builder offers the Goals to pick.
	picker := findByTestID(t, getPage(t, client, ts.URL+"/reports/new"), "picked-select")
	if offered := textsOf(findAll(picker, func(n *nethtml.Node) bool { return n.Data == "option" })); !slices.Contains(offered, "Grow revenue") {
		t.Fatalf("the builder does not offer Goals to pick: it offers %q", offered)
	}

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":         {"EU MBR"},
		"introduction": {"Quarterly business review."},
		"mode":         {domain.ReportModePicked},
		"picked":       {strconv.FormatInt(a.ID, 10), strconv.FormatInt(b.ID, 10), strconv.FormatInt(c.ID, 10)},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save report: status %d", resp.StatusCode)
	}
	draft := parsePage(t, readBody(t, resp))
	for _, want := range []string{
		"EU MBR",
		"Quarterly business review.",
		"Grow revenue", "Launch in EU", "Cut churn", // selected Goals
		domain.HealthYellow, // B's Health
	} {
		if !strings.Contains(textOf(draft), want) {
			t.Errorf("live draft missing %q; page reads:\n%s", want, textOf(draft))
		}
	}
	if owners := reportPersonEmails(draft); !slices.Contains(owners, "boss@example.com") {
		t.Errorf("live draft doesn't show the Owner, boss@example.com: it shows %q", owners)
	}
	// Each selected Goal appears once, as a block or a line.
	if got := len(findAllByTestID(draft, "report-exception")) + len(findAllByTestID(draft, "selected-goal")); got != 3 {
		t.Errorf("draft shows %d selected Goals, want 3:\n%s", got, renderNode(draft))
	}

	// The saved definition shows up on the reports list.
	if list := getPage(t, client, ts.URL+"/reports"); !strings.Contains(textOf(list), "EU MBR") {
		t.Errorf("reports list missing the saved definition; page reads:\n%s", textOf(list))
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
	defer func() { _ = resp.Body.Close() }()
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

	page := getPage(t, client, reportURL)
	if !slices.ContainsFunc(formFields(page, "baseline"), func(f pageField) bool { return f.Value == "2026-01-12" }) {
		t.Errorf("page does not offer the default baseline, 30 days ago: %+v", formFields(page, "baseline"))
	}
	blocks := findAllByTestID(page, "report-exception")
	if len(blocks) != 1 {
		t.Errorf("%d exception blocks, want 1:\n%s", len(blocks), renderNode(page))
	}
	if len(blocks) > 0 {
		for _, want := range []string{"Launch in EU", domain.HealthRed, "Blocked on legal.", "Hire counsel."} {
			if !strings.Contains(textOf(blocks[0]), want) {
				t.Errorf("exception block missing %q; block reads %q", want, textOf(blocks[0]))
			}
		}
	}
	lines := findAllByTestID(page, "selected-goal")
	if len(lines) == 0 {
		t.Fatalf("draft has no one-line Goal:\n%s", renderNode(page))
	}
	if line := textOf(lines[0]); !strings.Contains(line, "Cut churn") {
		t.Errorf("unchanged Green is not one line; line reads %q", line)
	}

	// A baseline before both Goals were created makes both New.
	page = getPage(t, client, reportURL+"?baseline=2026-01-01")
	if got := len(findAllByTestID(page, "report-exception")); got != 2 {
		t.Errorf("with an earlier baseline, %d exception blocks, want 2:\n%s", got, renderNode(page))
	}

	resp, err := ctxGet(t, client, reportURL+"?baseline=not-a-date")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unreadable baseline: status %d, want 422", resp.StatusCode)
	}
	assertRefusalPage(t, parsePage(t, body), "Can't show Report", `"not-a-date" isn't a date.`, "/reports/"+strconv.FormatInt(def.ID, 10))
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

	unpublished := findByTestID(t, getPage(t, client, reportURL), "report-history")
	if got := textsOf(findAllByTestID(unpublished, "report-history-entry")); !slices.Equal(got, []string{"Created Feb 11, 2026, 15:04 · boss"}) {
		t.Errorf("an unpublished definition's History reads %q, want only its creation", got)
	}
	resp := postForm(t, client, reportURL+"/publications", url.Values{})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	pubURL := resp.Request.URL.String()
	if !strings.HasPrefix(pubURL, reportURL+"/publications/") {
		t.Fatalf("publish redirected to %s, want a publication of the definition", pubURL)
	}
	published := parsePage(t, readBody(t, resp))
	if line := findByTestID(t, published, "report-published"); !slices.Contains(reportPersonEmails(line), "boss@example.com") {
		t.Errorf("publication does not say who published it: %s", renderNode(line))
	}
	for _, want := range []string{"MBR", "Vendor is late."} {
		if !strings.Contains(textOf(published), want) {
			t.Errorf("publication missing %q; page reads:\n%s", want, textOf(published))
		}
	}

	h.Clock.Advance(24 * time.Hour)
	// Two weeks out from a day later moves the back-to-Green date later.
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Vendor is gone.", "Find a new vendor.", h.Clock.Now().AddDate(0, 0, 14), "A new vendor takes longer.")
	if read := textOf(getPage(t, client, pubURL)); !strings.Contains(read, "Vendor is late.") || strings.Contains(read, "Vendor is gone.") {
		t.Errorf("publication changed after a later Check-in; page reads:\n%s", read)
	}

	draft := getPage(t, client, reportURL)
	if linkTo(findByTestID(t, draft, "report-history"), strings.TrimPrefix(pubURL, ts.URL)) == nil {
		t.Errorf("the draft's History does not link its publication: %s", renderNode(findByTestID(t, draft, "report-history")))
	}
	// By default publishing reads against the previous publication, so the
	// publish form carries no baseline.
	form := findByTestID(t, draft, "report-publish")
	if attrOf(form, "action") != "/reports/"+strconv.FormatInt(def.ID, 10)+"/publications" || len(formFields(form, "baseline")) > 0 {
		t.Errorf("the default publish form carries a baseline: %s", renderNode(form))
	}
	resp = postForm(t, client, reportURL+"/publications", url.Values{})
	if line := textOf(findByTestID(t, parsePage(t, readBody(t, resp)), "report-published")); !strings.Contains(line, "changes since the previous publication, Feb 11, 2026, ") {
		t.Errorf("publication does not read against the previous publication; line reads %q", line)
	}

	// A date the reader picks lives in the URL and is carried into the
	// publication on the form's action, not in a hidden input.
	form = findByTestID(t, getPage(t, client, reportURL+"?baseline=2026-01-01"), "report-publish")
	if attrOf(form, "action") != "/reports/"+strconv.FormatInt(def.ID, 10)+"/publications?baseline=2026-01-01" || len(formFields(form, "baseline")) > 0 {
		t.Errorf("publish form does not carry the chosen baseline on its action alone: %s", renderNode(form))
	}
	resp = postForm(t, client, reportURL+"/publications?baseline=2026-01-01", url.Values{})
	if line := textOf(findByTestID(t, parsePage(t, readBody(t, resp)), "report-published")); !strings.Contains(line, "changes since Jan 1, 2026") {
		t.Errorf("publication does not read against the chosen baseline; line reads %q", line)
	}

	// A publication is only found under its own definition.
	wrong := ts.URL + "/reports/" + strconv.FormatInt(other.ID, 10) + strings.TrimPrefix(pubURL, reportURL)
	resp, err := ctxGet(t, client, wrong)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("publication under another definition: status %d, want 404", resp.StatusCode)
	}
}

// Publishing against a baseline that isn't a date is refused on the refusal
// page, 422, quoting it, with a way Back to the draft without it, and nothing
// is published.
func TestPublishReportAgainstABaselineThatIsntADateIsRefused(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	draft := "/reports/" + strconv.FormatInt(def.ID, 10)

	resp := postForm(t, client, ts.URL+draft+"/publications?baseline=soon", url.Values{})
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	assertRefusalPage(t, parsePage(t, body), "Can't publish Report", `"soon" isn't a date.`, draft)
	if pubs, err := h.Service.ListPublications(context.Background(), def.ID); err != nil || len(pubs) != 0 {
		t.Errorf("publications = %+v (%v), want none", pubs, err)
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

	if href := attrOf(findByTestID(t, getPage(t, client, ts.URL+pubPath), "download-markdown"), "href"); href != pubPath+"/markdown" {
		t.Errorf("publication page's Download menu links %q for Markdown, want its Markdown export %q", href, pubPath+"/markdown")
	}

	resp, err := ctxGet(t, client, ts.URL+pubPath+"/markdown")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
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
	for _, want := range []string{"### Red · Launch in EU", "Vendor is late."} {
		if !strings.Contains(md, want) {
			t.Errorf("export missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Vendor is gone.") {
		t.Errorf("export shows a Check-in made after publishing:\n%s", md)
	}

	// A publication exports only under its own definition.
	resp, err = ctxGet(t, client, ts.URL+"/reports/"+strconv.FormatInt(other.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/markdown")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("export under another definition: status %d, want 404", resp.StatusCode)
	}
}

// A published Report has a print-friendly page to print to PDF from the
// browser: the snapshot's content without the app's navigation. Without a PDF
// renderer, the Download menu's PDF item is this page.
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

	if href := attrOf(findByTestID(t, getPage(t, client, ts.URL+pubPath), "download-pdf"), "href"); href != pubPath+"/print" {
		t.Errorf("publication page's Download menu links %q for a PDF, want its print page %q", href, pubPath+"/print")
	}

	printed := getPage(t, client, ts.URL+pubPath+"/print")
	if line := findByTestID(t, printed, "report-published"); !slices.Contains(reportPersonEmails(line), "boss@example.com") {
		t.Errorf("print page does not say who published it: %s", renderNode(line))
	}
	block := findByTestID(t, printed, "report-exception")
	for _, want := range []string{"Launch in EU", domain.HealthRed, "Vendor is late.", "Chase the vendor."} {
		if !strings.Contains(textOf(block), want) {
			t.Errorf("print page's exception block missing %q; block reads %q", want, textOf(block))
		}
	}
	if !strings.Contains(textOf(printed), "Where the EU launch stands.") {
		t.Errorf("print page missing the introduction; page reads:\n%s", textOf(printed))
	}
	if hasTestID(printed, "nav-reports") {
		t.Errorf("print page carries the app's navigation, its Reports link:\n%s", renderNode(printed))
	}
	if signOut := findAll(printed, func(n *nethtml.Node) bool { return n.Data == "form" && attrOf(n, "action") == "/signout" }); len(signOut) > 0 {
		t.Errorf("print page carries the app's navigation, its Sign out: %s", renderNode(signOut[0]))
	}

	resp, err := ctxGet(t, client, ts.URL+"/reports/"+strconv.FormatInt(other.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("print page under another definition: status %d, want 404", resp.StatusCode)
	}
}

// A publication's print page carries no script, as the app's pages do: so its
// PDF needs none, and the renderer serves it to Chromium under a
// script-src 'none' Content-Security-Policy, which stops any script.
func TestAPublicationsPrintPageNeedsNoScriptOverHTTP(t *testing.T) {
	t.Parallel()

	f := newPDFDownloadFixture(t)
	ts := newServer(t, f.h)
	client := signInClient(t, ts.URL, "boss@example.com")

	isScript := func(n *nethtml.Node) bool { return n.Data == "script" }
	if scripts := findAll(getPage(t, client, ts.URL+f.pubPath), isScript); len(scripts) == 0 {
		t.Fatal("publication page has no <script> to tell the print page's lack of one from")
	}
	if scripts := findAll(getPage(t, client, ts.URL+f.pubPath+"/print"), isScript); len(scripts) != 0 {
		t.Errorf("print page has %d <script> elements, want none: the PDF renders under script-src 'none', which stops scripts; first:\n%s", len(scripts), renderNode(scripts[0]))
	}
}

// pdfDownloadFake is a PDF renderer that records what it was asked to render
// and answers with pdf, or with err when set.
type pdfDownloadFake struct {
	pdf     []byte
	err     error
	html    string
	baseURL string
}

func (f *pdfDownloadFake) Render(_ context.Context, html []byte, baseURL string) ([]byte, error) {
	f.html, f.baseURL = string(html), baseURL
	return f.pdf, f.err
}

// pdfDownloadFixture is a published Report to download, with a second
// Definition its publication isn't under.
type pdfDownloadFixture struct {
	h       *testsupport.Harness
	def     domain.ReportDefinition
	other   domain.ReportDefinition
	pub     domain.Publication
	pubPath string
}

func newPDFDownloadFixture(t *testing.T) pdfDownloadFixture {
	t.Helper()
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthRed, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 0, 14))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	other := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "WBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	return pdfDownloadFixture{
		h: h, def: def, other: other, pub: pub,
		pubPath: "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(pub.ID, 10),
	}
}

// With a renderer, a publication downloads as a PDF: its print page, rendered,
// as an attachment named for the Report and the publication. The renderer gets
// the print page's HTML and the app's own address, for its fonts to load from.
func TestAPublicationDownloadsAsAPDFOverHTTP(t *testing.T) {
	t.Parallel()

	f := newPDFDownloadFixture(t)
	fake := &pdfDownloadFake{pdf: []byte("%PDF-1.4 the rendered report")}
	ts := httptest.NewServer(web.NewServer(f.h.Service, web.WithPDFRenderer(fake)))
	t.Cleanup(ts.Close)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp, err := ctxGet(t, client, ts.URL+f.pubPath+"/pdf")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pdf download: status %d, body:\n%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type %q, want application/pdf", ct)
	}
	want := fmt.Sprintf(`attachment; filename="report-%d-publication-%d.pdf"`, f.def.ID, f.pub.ID)
	if cd := resp.Header.Get("Content-Disposition"); cd != want {
		t.Errorf("Content-Disposition %q, want %q", cd, want)
	}
	if body != "%PDF-1.4 the rendered report" {
		t.Errorf("body %q, want the renderer's bytes", body)
	}

	printed := getBody(t, client, ts.URL+f.pubPath+"/print")
	if fake.html != printed {
		t.Errorf("renderer got HTML other than the print page's:\n%s", fake.html)
	}
	if fake.baseURL != ts.URL {
		t.Errorf("renderer's base URL %q, want the app's own address %q", fake.baseURL, ts.URL)
	}
}

// Without a renderer, a PDF download is the print page, printed from the
// browser: the /pdf link an earlier page carried still works.
func TestWithoutARendererAPDFDownloadIsThePrintPageOverHTTP(t *testing.T) {
	t.Parallel()

	f := newPDFDownloadFixture(t)
	ts := newServer(t, f.h)
	client := noRedirects(signInClient(t, ts.URL, "boss@example.com"))

	resp, err := ctxGet(t, client, ts.URL+f.pubPath+"/pdf")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("pdf download without a renderer: status %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != f.pubPath+"/print" {
		t.Errorf("redirected to %q, want the print page %q", loc, f.pubPath+"/print")
	}
}

// A publication downloads as a PDF only under its own Definition, and only for
// someone signed in, as its print page shows.
func TestAPDFDownloadIsFoundOnlyUnderItsOwnDefinitionOverHTTP(t *testing.T) {
	t.Parallel()

	f := newPDFDownloadFixture(t)
	fake := &pdfDownloadFake{pdf: []byte("%PDF-1.4")}
	ts := httptest.NewServer(web.NewServer(f.h.Service, web.WithPDFRenderer(fake)))
	t.Cleanup(ts.Close)

	wrong := ts.URL + "/reports/" + strconv.FormatInt(f.other.ID, 10) + "/publications/" + strconv.FormatInt(f.pub.ID, 10) + "/pdf"
	resp, err := ctxGet(t, signInClient(t, ts.URL, "boss@example.com"), wrong)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("pdf download under another definition: status %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp, err = ctxGet(t, noRedirects(&http.Client{}), ts.URL+f.pubPath+"/pdf")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/signin" {
		t.Errorf("pdf download signed out: status %d to %q, want 303 to /signin", resp.StatusCode, resp.Header.Get("Location"))
	}
	if fake.html != "" {
		t.Error("the renderer ran for a download that was refused")
	}
}

// A render that fails gets the server-error page and an ERROR in the log
// naming the cause, never a broken PDF.
func TestAFailedPDFRenderIsAServerErrorOverHTTP(t *testing.T) {
	t.Parallel()

	f := newPDFDownloadFixture(t)
	var buf bytes.Buffer
	fake := &pdfDownloadFake{pdf: []byte("%PDF-1.4 half"), err: errors.New("chromium crashed")}
	ts := httptest.NewServer(web.NewServer(f.h.Service, web.WithPDFRenderer(fake), web.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil)))))
	t.Cleanup(ts.Close)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp, err := ctxGet(t, client, ts.URL+f.pubPath+"/pdf")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("failed render: status %d, want 500", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "pdf") || strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("failed render is served as a download: Content-Type %q, Content-Disposition %q", ct, resp.Header.Get("Content-Disposition"))
	}
	if strings.Contains(body, "%PDF") {
		t.Errorf("failed render serves the renderer's bytes: %q", body)
	}
	var logged bool
	for _, rec := range logRecords(t, &buf) {
		cause, _ := rec["err"].(string)
		if rec["level"] == "ERROR" && rec["path"] == f.pubPath+"/pdf" && strings.Contains(cause, "chromium crashed") {
			logged = true
		}
	}
	if !logged {
		t.Errorf("no ERROR naming the render's cause for %s/pdf; log:\n%s", f.pubPath, buf.String())
	}
}

// reportPersonEmails is the email of each person shown under root, in order:
// what each one's name discloses, and gives on hover (CONTEXT.md: Name).
func reportPersonEmails(root *nethtml.Node) []string {
	var out []string
	for _, n := range findAll(root, func(n *nethtml.Node) bool {
		return hasAttr(n, "title") && (flatFrameHasClass(n, "disclose") || flatFrameHasClass(n, "person"))
	}) {
		out = append(out, attrOf(n, "title"))
	}
	return out
}

// Each published Report's row on the Reports page has a Download menu for its
// latest publication: Markdown, or a PDF file with a renderer. A Report never
// published has nothing to download.
func TestAPublishedReportsRowDownloadsItsLatestPublicationOverHTTP(t *testing.T) {
	t.Parallel()

	f := newPDFDownloadFixture(t)
	f.h.Clock.Advance(24 * time.Hour)
	latest := f.h.PublishReport(f.h.SignIn("boss@example.com"), f.def)
	latestPath := "/reports/" + strconv.FormatInt(f.def.ID, 10) + "/publications/" + strconv.FormatInt(latest.ID, 10)
	ts := httptest.NewServer(web.NewServer(f.h.Service, web.WithPDFRenderer(&pdfDownloadFake{})))
	t.Cleanup(ts.Close)

	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports")

	menu := findByTestID(t, reportRowIn(t, page, "EU MBR"), "download-menu")
	if buttonNamed(menu, "Download") == nil {
		t.Errorf("the row's menu isn't opened by a Download button: %s", renderNode(menu))
	}
	for testID, want := range map[string][2]string{
		"download-markdown": {latestPath + "/markdown", "Markdown (.md)"},
		"download-pdf":      {latestPath + "/pdf", "PDF (.pdf)"},
	} {
		if href, text := downloadMenuItem(t, menu, testID); href != want[0] || text != want[1] {
			t.Errorf("%s links %q reading %q, want %q reading %q", testID, href, text, want[0], want[1])
		}
	}
	if never := reportRowIn(t, page, "WBR"); hasTestID(never, "download-menu") {
		t.Errorf("a never-published Report's row has a Download menu: %s", renderNode(never))
	}
}

// A publication's page downloads through the same Download menu, in place of
// the Markdown and Print / PDF buttons it had.
func TestThePublicationPageHasTheDownloadMenuOverHTTP(t *testing.T) {
	t.Parallel()

	f := newPDFDownloadFixture(t)
	ts := httptest.NewServer(web.NewServer(f.h.Service, web.WithPDFRenderer(&pdfDownloadFake{})))
	t.Cleanup(ts.Close)

	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+f.pubPath)

	menu := findByTestID(t, findByTestID(t, page, "report-export"), "download-menu")
	if href, _ := downloadMenuItem(t, menu, "download-markdown"); href != f.pubPath+"/markdown" {
		t.Errorf("Markdown item links %q, want %q", href, f.pubPath+"/markdown")
	}
	if href, _ := downloadMenuItem(t, menu, "download-pdf"); href != f.pubPath+"/pdf" {
		t.Errorf("PDF item links %q, want %q", href, f.pubPath+"/pdf")
	}
	for _, gone := range []string{"export-markdown", "export-print"} {
		if hasTestID(page, gone) {
			t.Errorf("publication page still carries the old %s", gone)
		}
	}
	if strings.Contains(textOf(page), "Print / PDF") {
		t.Errorf("publication page still offers the old Print / PDF")
	}
}

// Without a renderer, the menu's PDF item is the print page, and says the
// reader prints it from the browser, on the Reports page and the publication's.
func TestWithoutARendererThePDFItemIsThePrintPageOverHTTP(t *testing.T) {
	t.Parallel()

	f := newPDFDownloadFixture(t)
	ts := newServer(t, f.h)
	client := signInClient(t, ts.URL, "boss@example.com")

	for _, path := range []string{"/reports", f.pubPath} {
		menus := findAllByTestID(getPage(t, client, ts.URL+path), "download-menu")
		if len(menus) == 0 {
			t.Fatalf("%s has no Download menu", path)
		}
		if href, text := downloadMenuItem(t, menus[0], "download-pdf"); href != f.pubPath+"/print" || text != "PDF (print from browser)" {
			t.Errorf("%s: PDF item links %q reading %q, want %q reading %q", path, href, text, f.pubPath+"/print", "PDF (print from browser)")
		}
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

	form := findByTestID(t, getPage(t, client, reportURL), "narrative-curation")
	for _, want := range []string{"Signed the first EU customer.", "Lost the second customer.", "Launch in EU"} {
		if !strings.Contains(textOf(form), want) {
			t.Errorf("curation form missing %q; form reads %q", want, textOf(form))
		}
	}
	if owners := reportPersonEmails(form); !slices.Contains(owners, "alice@example.com") {
		t.Errorf("curation form doesn't credit the Owner, alice@example.com: it shows %q", owners)
	}

	resp := postForm(t, client, reportURL+"/narrative", url.Values{
		"include-" + strconv.FormatInt(signed, 10): {"on"},
		"pick-" + strconv.FormatInt(signed, 10):    {domain.HighlightAccomplishment},
		"pick-" + strconv.FormatInt(lost, 10):      {domain.HighlightMiss},
		"text-" + domain.HighlightAccomplishment:   {"EU is open for business."},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/reports/"+strconv.FormatInt(def.ID, 10) {
		t.Fatalf("curate: status %d at %s, want the draft page", resp.StatusCode, resp.Request.URL)
	}
	page := parsePage(t, readBody(t, resp))
	// Alice, who may not manage the Report, reads the narrative as it will
	// be published.
	narrative := findByTestID(t, getPage(t, signInClient(t, ts.URL, "alice@example.com"), reportURL), "report-narrative")
	for _, want := range []string{"Accomplishments", "EU is open for business.", "Signed the first EU customer."} {
		if !strings.Contains(textOf(narrative), want) {
			t.Errorf("draft narrative missing %q; narrative reads %q", want, textOf(narrative))
		}
	}
	if owners := reportPersonEmails(narrative); !slices.Contains(owners, "alice@example.com") {
		t.Errorf("draft narrative doesn't credit the Owner, alice@example.com: it shows %q", owners)
	}
	if strings.Contains(textOf(narrative), "Lost the second customer.") {
		t.Errorf("draft narrative shows a Highlight the author left out; narrative reads %q", textOf(narrative))
	}
	if boxes := reportNoteBoxes(t, findByTestID(t, page, "narrative-curation"), domain.HighlightAccomplishment); !slices.Contains(boxes, "EU is open for business.") {
		t.Errorf("curation form does not keep the author's text: its Accomplishments boxes hold %q", boxes)
	}

	resp = postForm(t, client, reportURL+"/publications", url.Values{})
	published := parsePage(t, readBody(t, resp))
	narrative = findByTestID(t, published, "report-narrative")
	for _, want := range []string{"EU is open for business.", "Signed the first EU customer."} {
		if !strings.Contains(textOf(narrative), want) {
			t.Errorf("published narrative missing %q; narrative reads %q", want, textOf(narrative))
		}
	}
	if owners := reportPersonEmails(narrative); !slices.Contains(owners, "alice@example.com") {
		t.Errorf("published narrative doesn't credit the Owner, alice@example.com: it shows %q", owners)
	}
	if hasTestID(published, "narrative-curation") {
		t.Errorf("publication offers to curate its frozen narrative:\n%s", renderNode(published))
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("curate: status %d, want the draft page", resp.StatusCode)
	}
	narrative := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), reportURL), "report-narrative")
	if headings := textsOf(findAllByTestID(narrative, "narrative-section")); !slices.Equal(headings, []string{"Misses"}) {
		t.Errorf("narrative's sections %q, want Misses alone", headings)
	}
	if !strings.Contains(textOf(narrative), "Signed the first EU customer.") {
		t.Errorf("narrative leaves out the ticked Highlight; narrative reads %q", textOf(narrative))
	}
	if read := textOf(narrative); strings.Contains(read, "Lost the second customer.") || strings.Contains(read, "Accomplishments") {
		t.Errorf("narrative includes a Highlight whose box wasn't ticked; narrative reads %q", read)
	}
	_ = resp.Body.Close()

	resp = postForm(t, client, reportURL+"/narrative", url.Values{"include-" + strconv.FormatInt(lost, 10): {"on"}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("including a Highlight without a section: status %d, want 422", resp.StatusCode)
	}
}

// On a fresh draft no Highlight is included, and each one's section control
// starts on its own kind, the Highlight showing its kind beside its Goal. Once
// the author saves a Highlight as a Miss, its box is ticked and Miss is
// selected. The count says how many of the Highlights are in (ticket #155).
// How CSS shows the section control only while the box is ticked is
// TestCurationFormShowsWhatIsIncludedStyles.
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

	page := getPage(t, client, reportURL)
	if count := textOf(findByTestID(t, page, "curation-count")); count != "0 of 2 in" {
		t.Errorf("fresh draft's count %q, want 0 of 2 in", count)
	}
	form := findByTestID(t, page, "narrative-curation")
	highlights := findAllByTestID(form, "curation-highlight")
	if len(highlights) == 0 {
		t.Fatalf("the curation form offers no Highlight: %s", renderNode(form))
	}
	// Without JS the radios stay in the form, so an unticked Highlight says
	// its kind beside its Goal as well.
	if line := textOf(highlights[0]); !strings.Contains(line, "· "+domain.HighlightInsight) {
		t.Errorf("unticked Highlight does not show its kind: %q", line)
	}
	for id, kind := range map[string]string{signed: domain.HighlightAccomplishment, euros: domain.HighlightInsight} {
		if box := curationChoice(t, form, "include-"+id, "on"); box.Checked {
			t.Errorf("fresh draft includes Highlight %s: %s", id, renderNode(box.Node))
		}
		for _, section := range []string{domain.HighlightInsight, domain.HighlightAccomplishment, domain.HighlightMiss} {
			if radio := curationChoice(t, form, "pick-"+id, section); radio.Checked != (section == kind) {
				t.Errorf("fresh draft, Highlight %s's %s radio: %s; want only its own kind, %s, selected", id, section, renderNode(radio.Node), kind)
			}
		}
	}

	resp := postForm(t, client, reportURL+"/narrative", url.Values{
		"include-" + signed: {"on"},
		"pick-" + signed:    {domain.HighlightMiss},
	})
	defer func() { _ = resp.Body.Close() }()
	page = parsePage(t, readBody(t, resp))
	if count := textOf(findByTestID(t, page, "curation-count")); count != "1 of 2 in" {
		t.Errorf("count after saving one Highlight %q, want 1 of 2 in", count)
	}
	form = findByTestID(t, page, "narrative-curation")
	if box := curationChoice(t, form, "include-"+signed, "on"); !box.Checked {
		t.Errorf("saved Highlight's box is not ticked: %s", renderNode(box.Node))
	}
	for _, section := range []string{domain.HighlightInsight, domain.HighlightAccomplishment, domain.HighlightMiss} {
		if radio := curationChoice(t, form, "pick-"+signed, section); radio.Checked != (section == domain.HighlightMiss) {
			t.Errorf("Highlight saved as a Miss, its %s radio: %s; want only Miss selected", section, renderNode(radio.Node))
		}
	}
	if box := curationChoice(t, form, "include-"+euros, "on"); box.Checked {
		t.Errorf("a Highlight left out is ticked: %s", renderNode(box.Node))
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
	defer func() { _ = resp.Body.Close() }()
	form := findByTestID(t, parsePage(t, readBody(t, resp)), "narrative-curation")

	written := findByTestID(t, form, "section-text-"+domain.HighlightAccomplishment)
	if !hasAttr(written, "open") {
		t.Errorf("section with text is folded: %s", renderNode(written))
	}
	if summary := textOf(findByTestID(t, written, "section-text-summary")); !strings.Contains(summary, "Accomplishments") || strings.Contains(summary, "+ Add note") {
		t.Errorf("written section's summary %q, want its heading alone", summary)
	}
	if boxes := reportNoteBoxes(t, form, domain.HighlightAccomplishment); !slices.Contains(boxes, "EU is open for business.") {
		t.Errorf("written section does not hold its text box: its boxes hold %q", boxes)
	}

	empty := findByTestID(t, form, "section-text-"+domain.HighlightMiss)
	if hasAttr(empty, "open") {
		t.Errorf("empty section is unfolded: %s", renderNode(empty))
	}
	if summary := textOf(findByTestID(t, empty, "section-text-summary")); !strings.Contains(summary, "Misses") || !strings.Contains(summary, "+ Add note") {
		t.Errorf("empty section's summary %q, want its heading and + Add note", summary)
	}
	if len(formFields(empty, "text-"+domain.HighlightMiss)) == 0 {
		t.Errorf("empty section has no text box: %s", renderNode(empty))
	}
	if save := buttonNamed(form, "Save narrative"); save == nil || attrOf(save, "type") != "submit" {
		t.Errorf("curation form has no Save narrative submit button: %s", renderNode(form))
	}
}

// The composer reads as the narrative will: under its Narrative heading, each
// section in turn — Insights, Accomplishments, Misses — with its own "+ Add
// note" and the Highlights its picks would put there, a Highlight picked into
// another section than its kind under that one. One Save narrative ends it.
// With no Highlight to pick it says so (#473).
func TestComposerGroupsEachSectionsNotesAndHighlightsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	quiet := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.Clock.Advance(time.Hour)
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightInsight, "Buyers want invoices in euros.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	if err := h.Service.CurateNarrative(context.Background(), boss.ID, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: draftHighlightID(t, h, def, "Signed the first EU customer."), Section: domain.HighlightMiss}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	none := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Quiet", Mode: domain.ReportModePicked, Picked: []int64{quiet.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	form := findByTestID(t, getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)), "narrative-curation")

	for section, want := range map[string][]string{
		domain.HighlightInsight:        {"Buyers want invoices in euros."},
		domain.HighlightAccomplishment: nil,
		domain.HighlightMiss:           {"Signed the first EU customer."},
	} {
		sec := findByTestID(t, form, "compose-section-"+section)
		if !strings.Contains(textOf(findByTestID(t, sec, "section-text-summary")), "+ Add note") {
			t.Errorf("the %s section has no + Add note: %s", section, renderNode(sec))
		}
		var got []string
		for _, hl := range findAllByTestID(sec, "curation-highlight") {
			boxes := findAll(hl, func(n *nethtml.Node) bool { return n.Data == "input" && attrOf(n, "type") == "checkbox" })
			if len(boxes) != 1 {
				t.Fatalf("a Highlight on the %s section has %d boxes to include it, want 1: %s", section, len(boxes), renderNode(hl))
			}
			got = append(got, fieldLabel(boxes[0]))
		}
		if !slices.Equal(got, want) {
			t.Errorf("the %s section offers %q, want %q", section, got, want)
		}
	}
	insights, accomplishments, misses := findByTestID(t, form, "compose-section-"+domain.HighlightInsight), findByTestID(t, form, "compose-section-"+domain.HighlightAccomplishment), findByTestID(t, form, "compose-section-"+domain.HighlightMiss)
	if !precedes(insights, accomplishments) || !precedes(accomplishments, misses) {
		t.Errorf("the sections aren't Insights, Accomplishments, Misses in turn")
	}
	saves := findAll(form, func(n *nethtml.Node) bool { return n.Data == "button" && textOf(n) == "Save narrative" })
	if len(saves) != 1 || within(saves[0], misses) || !precedes(misses, saves[0]) {
		t.Errorf("the composer has %d Save narrative, want one after the last section", len(saves))
	}

	empty := findByTestID(t, getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(none.ID, 10)), "report-curation")
	if got := textOf(findByTestID(t, empty, "no-highlights")); got != "No Highlights to pick since Dec 3, 2025." {
		t.Errorf("with no Highlights the composer says %q", got)
	}
}

// The author writes several notes into one section, each in its own box: the
// draft's form shows every saved note in its own box plus one empty box, and
// the draft and then the publication show each note as its own paragraph in
// the order entered. Notes in one section leave the others alone, and a blank
// note is ignored (ticket #187).
func TestSeveralNotesPerSectionOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	resp := postForm(t, client, reportURL+"/narrative", url.Values{
		"text-" + domain.HighlightInsight:        {"Pricing drives churn.", "  ", "Discounts don't save accounts."},
		"text-" + domain.HighlightAccomplishment: {"EU is open for business."},
		"text-" + domain.HighlightMiss:           {""},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/reports/"+strconv.FormatInt(def.ID, 10) {
		t.Fatalf("curate: status %d at %s, want the draft page", resp.StatusCode, resp.Request.URL)
	}

	form := findByTestID(t, parsePage(t, readBody(t, resp)), "narrative-curation")
	for section, want := range map[string][]string{
		domain.HighlightInsight:        {"Pricing drives churn.", "Discounts don't save accounts.", ""},
		domain.HighlightAccomplishment: {"EU is open for business.", ""},
		domain.HighlightMiss:           {""},
	} {
		if got := reportNoteBoxes(t, form, section); !slices.Equal(got, want) {
			t.Errorf("%s note boxes %q, want %q", section, got, want)
		}
	}

	want := []string{"Pricing drives churn.", "Discounts don't save accounts.", "EU is open for business."}
	reader := getPage(t, signInClient(t, ts.URL, "sam@example.com"), reportURL)
	if got := narrativeParagraphs(findByTestID(t, reader, "report-narrative")); !slices.Equal(got, want) {
		t.Errorf("draft narrative paragraphs %q, want %q", got, want)
	}

	published := parsePage(t, postFormBody(t, client, reportURL+"/publications", url.Values{}))
	if got := narrativeParagraphs(findByTestID(t, published, "report-narrative")); !slices.Equal(got, want) {
		t.Errorf("published narrative paragraphs %q, want %q", got, want)
	}
}

// Each section offers Add another, which posts the form to its own action
// with add_note set to the section and swaps its answer in for the whole
// Narrative section (tickets #187, #473).
func TestEachSectionOffersAddAnotherNoteOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"?baseline=2025-12-01")
	form := findByTestID(t, page, "narrative-curation")
	if id := attrOf(form, "id"); id != "narrative-curation" {
		t.Fatalf("curation form id = %q, want narrative-curation", id)
	}
	action := attrOf(form, "action")
	for _, section := range []string{domain.HighlightInsight, domain.HighlightAccomplishment, domain.HighlightMiss} {
		details := findByTestID(t, form, "section-text-"+section)
		buttons := findAll(details, func(n *nethtml.Node) bool {
			return n.Data == "button" && attrOf(n, "name") == "add_note" && textOf(n) == "Add another"
		})
		if len(buttons) == 0 {
			t.Errorf("%s offers no Add another: %s", section, renderNode(details))
			continue
		}
		for name, want := range map[string]string{
			"type":      "submit",
			"value":     section,
			"hx-post":   action,
			"hx-target": "#report-curation",
			"hx-swap":   "outerHTML",
		} {
			if got := attrOf(buttons[0], name); got != want {
				t.Errorf("%s Add another %s = %q, want %q", section, name, got, want)
			}
		}
	}
}

// Add another re-renders the curation form as posted, with one more empty box
// in that section only, everything typed or ticked anywhere on the form kept,
// and saves nothing: the save status says the form has unsaved changes —
// over htmx, where the form comes back with the save status out of band, and
// as a plain post, which gets the whole draft page rather than a redirect
// (tickets #187, #444).
func TestAddAnotherNoteKeepsTheFormAsPostedOverHTTP(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		post func(t *testing.T, client *http.Client, rawURL string, form url.Values) (*http.Response, string)
	}{
		{"htmx", postHX},
		{"plain", func(t *testing.T, client *http.Client, rawURL string, form url.Values) (*http.Response, string) {
			resp := postForm(t, client, rawURL, form)
			return resp, readBody(t, resp)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
			h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
			h.CheckinWithHighlight(boss, g.ID, domain.HighlightMiss, "Lost the second customer.")
			def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
			signed := strconv.FormatInt(draftHighlightID(t, h, def, "Signed the first EU customer."), 10)
			lost := strconv.FormatInt(draftHighlightID(t, h, def, "Lost the second customer."), 10)
			if err := h.Service.CurateNarrative(context.Background(), boss.ID, def.ID, domain.CurateNarrativeInput{
				Notes: map[string][]string{domain.HighlightMiss: {"Slipped a week."}},
			}); err != nil {
				t.Fatalf("CurateNarrative: %v", err)
			}
			logged, err := h.Service.ReportLog(context.Background(), def.ID)
			if err != nil {
				t.Fatalf("ReportLog: %v", err)
			}

			ts := newServer(t, h)
			client := signInClient(t, ts.URL, "boss@example.com")
			draftPath := "/reports/" + strconv.FormatInt(def.ID, 10)
			resp, body := tc.post(t, client, ts.URL+draftPath+"/narrative", url.Values{
				"include-" + signed:                      {"on"},
				"pick-" + signed:                         {domain.HighlightInsight},
				"pick-" + lost:                           {domain.HighlightInsight},
				"text-" + domain.HighlightInsight:        {"Pricing drives churn.", ""},
				"text-" + domain.HighlightAccomplishment: {"EU is open for business."},
				"text-" + domain.HighlightMiss:           {""},
				"add_note":                               {domain.HighlightInsight},
			})
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != draftPath+"/narrative" {
				t.Fatalf("add another: status %d at %s, want 200 with no redirect; body:\n%s", resp.StatusCode, resp.Request.URL, body)
			}

			answer := parsePage(t, body)
			form := findByTestID(t, answer, "narrative-curation")
			for section, want := range map[string][]string{
				domain.HighlightInsight:        {"Pricing drives churn.", "", ""},
				domain.HighlightAccomplishment: {"EU is open for business."},
				domain.HighlightMiss:           {""},
			} {
				if got := reportNoteBoxes(t, form, section); !slices.Equal(got, want) {
					t.Errorf("%s note boxes %q, want %q", section, got, want)
				}
			}
			if box := curationChoice(t, form, "include-"+signed, "on"); !box.Checked {
				t.Errorf("the ticked Highlight lost its tick: %s", renderNode(box.Node))
			}
			if radio := curationChoice(t, form, "pick-"+signed, domain.HighlightInsight); !radio.Checked {
				t.Errorf("the ticked Highlight lost its section: %s", renderNode(radio.Node))
			}
			if box := curationChoice(t, form, "include-"+lost, "on"); box.Checked {
				t.Errorf("the unticked Highlight is ticked: %s", renderNode(box.Node))
			}
			if radio := curationChoice(t, form, "pick-"+lost, domain.HighlightInsight); !radio.Checked {
				t.Errorf("the unticked Highlight lost the section picked for it: %s", renderNode(radio.Node))
			}
			if got := textOf(findByTestID(t, answer, "save-result")); got != "Unsaved changes" {
				t.Errorf("the save status says %q, want Unsaved changes", got)
			}
			if got := textOf(findByTestID(t, answer, "curation-count")); got != "1 of 2 in" {
				t.Errorf("the Highlights count %q, want 1 of 2 in, as ticked", got)
			}

			draft, err := h.Service.DraftReport(context.Background(), def, time.Time{})
			if err != nil {
				t.Fatalf("DraftReport: %v", err)
			}
			if len(draft.Narrative) != 1 || !slices.Equal(draft.Narrative[0].Notes, []string{"Slipped a week."}) {
				t.Errorf("saved narrative after add another %+v, want the one saved before", draft.Narrative)
			}
			if after, err := h.Service.ReportLog(context.Background(), def.ID); err != nil || len(after) != len(logged) {
				t.Errorf("Report log after add another has %d entries (err %v), want the %d before", len(after), err, len(logged))
			}

			if tc.name == "plain" {
				if !hasTestID(answer, "draft-header") {
					t.Errorf("plain add another does not answer the whole draft page; body:\n%s", body)
				}
				if main := findByTestID(t, answer, "report-draft"); !hasTestID(main, "narrative-curation") || hasTestID(main, "report-narrative") {
					t.Errorf("plain add another's main column doesn't compose the narrative as posted in place, alone")
				}
				return
			}
			if hasTestID(answer, "draft-header") {
				t.Errorf("htmx add another answers the whole page; body:\n%s", body)
			}
			if attrOf(findByTestID(t, answer, "save-status"), "hx-swap-oob") != "true" {
				t.Errorf("the save status is not swapped out of band; body:\n%s", body)
			}
			for _, untouched := range []string{"report-draft", "publish-summary", "report-history"} {
				if hasTestID(answer, untouched) {
					t.Errorf("htmx add another answers %s, which nothing saved changed; body:\n%s", untouched, body)
				}
			}
		})
	}
}

// Add another the domain refuses — a ticked Highlight with no section — still
// re-renders the form as posted with its added box, and the save status says
// why nothing was saved; the rest of the draft stays as it was. A plain post gets the
// draft page as 422 (ticket #187).
func TestAddAnotherNoteRefusedKeepsTheFormAsPostedOverHTTP(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		post   func(t *testing.T, client *http.Client, rawURL string, form url.Values) (*http.Response, string)
	}{
		{"htmx", http.StatusOK, postHX},
		{"plain", http.StatusUnprocessableEntity, func(t *testing.T, client *http.Client, rawURL string, form url.Values) (*http.Response, string) {
			resp := postForm(t, client, rawURL, form)
			return resp, readBody(t, resp)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
			h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
			def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
			signed := strconv.FormatInt(draftHighlightID(t, h, def, "Signed the first EU customer."), 10)

			ts := newServer(t, h)
			client := signInClient(t, ts.URL, "boss@example.com")
			resp, body := tc.post(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/narrative", url.Values{
				"include-" + signed:            {"on"},
				"text-" + domain.HighlightMiss: {"Slipped a week."},
				"add_note":                     {domain.HighlightMiss},
			})
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.status {
				t.Fatalf("refused add another: status %d, want %d; body:\n%s", resp.StatusCode, tc.status, body)
			}
			answer := parsePage(t, body)
			form := findByTestID(t, answer, "narrative-curation")
			if got, want := reportNoteBoxes(t, form, domain.HighlightMiss), []string{"Slipped a week.", ""}; !slices.Equal(got, want) {
				t.Errorf("Misses note boxes %q, want %q", got, want)
			}
			if box := curationChoice(t, form, "include-"+signed, "on"); !box.Checked {
				t.Errorf("the ticked Highlight lost its tick: %s", renderNode(box.Node))
			}
			if result := textOf(findByTestID(t, answer, "save-result")); !strings.HasPrefix(result, "Not saved: ") || !strings.Contains(result, "section") {
				t.Errorf("the save status %q, want Not saved and why", result)
			}
			if draft, err := h.Service.DraftReport(context.Background(), def, time.Time{}); err != nil || len(draft.Narrative) != 0 {
				t.Errorf("narrative after a refusal %+v (err %v), want nothing saved", draft.Narrative, err)
			}
			if tc.name == "plain" && !hasTestID(answer, "report-draft") {
				t.Errorf("refused add another without script doesn't answer the whole draft page; body:\n%s", body)
			}
			if tc.name == "htmx" {
				for _, untouched := range []string{"report-draft", "publish-summary", "draft-header"} {
					if hasTestID(answer, untouched) {
						t.Errorf("refused add another answers %s; body:\n%s", untouched, body)
					}
				}
				// A whole page carries its title; the form alone doesn't.
				if titles := findAll(answer, func(n *nethtml.Node) bool { return n.Data == "title" }); len(titles) > 0 {
					t.Errorf("refused add another answers the whole page, titled %q; body:\n%s", textOf(titles[0]), body)
				}
			}
		})
	}
}

// A refused Add another without script answers the draft page from one build
// of the draft: the form and the rest of the page show the draft as saved, so
// it reads the saved narrative once (ticket #455).
func TestRefusedAddAnotherWithoutScriptDraftsOnceOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, queryCountAdmin)
	admin := h.SignIn(queryCountAdmin)
	g := h.ActiveGoal(admin, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(admin, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(admin, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	signed := strconv.FormatInt(draftHighlightID(t, h, def, "Signed the first EU customer."), 10)
	ts, _, queries := newQueryCountingServer(t, h)
	client := signInClient(t, ts.URL, queryCountAdmin)

	queries.take()
	resp := postForm(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/narrative", url.Values{
		// Ticked with no section, which the narrative refuses.
		"include-" + signed: {"on"},
		"add_note":          {domain.HighlightMiss},
	})
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("refused add another: status %d, want 422; body:\n%s", resp.StatusCode, body)
	}
	drafts := 0
	for _, q := range queries.take() {
		if strings.HasPrefix(q, "-- name: ListNarrativePicks ") {
			drafts++
		}
	}
	if drafts != 1 {
		t.Errorf("a refused Add another read the saved narrative %d times, want once: the draft built once", drafts)
	}
}

// A publication frozen when a section held at most one of the author's notes
// still shows it, as its own paragraph before the Highlights picked into the
// section (ticket #187).
func TestPublicationFromBeforeSeveralNotesShowsItsNoteOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Narrative":[{"Kind":"Accomplishment","Text":"EU is open for business.",` +
		`"Highlights":[{"Highlight":{"ID":1,"Kind":"Accomplishment","Note":"Signed the first EU customer.",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"}},"GoalID":1,"GoalTitle":"Launch in EU","Section":"Accomplishment"}]}]}`
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write a pre-several-notes snapshot: %v", err)
	}

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID))
	narrative := findByTestID(t, page, "report-narrative")
	if got := narrativeParagraphs(narrative); !slices.Equal(got, []string{"EU is open for business."}) {
		t.Errorf("published notes %q, want the one note frozen before: %s", got, renderNode(narrative))
	}
	notes := findAllByTestID(narrative, "narrative-text")
	picked := findAll(narrative, func(n *nethtml.Node) bool {
		return attrOf(n, "data-testid") == "narrative-highlight" && strings.Contains(textOf(n), "Signed the first EU customer.")
	})
	if len(notes) == 0 || len(picked) == 0 || !precedes(notes[0], picked[0]) {
		t.Errorf("the note does not come before the picked Highlight: %s", renderNode(narrative))
	}
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

// The draft and the publication open with a Health summary: how many of the
// Report's selected Goals are Red, Yellow, Green, and Check-in overdue, so an
// exec sees the shape of the company before reading a block. It is one row
// headed "Health", each count with its word beside it, so it reads to someone
// who doesn't know the shapes, and each chip a shape with no word of its own,
// so a pair reads once: "1 Red", not "Red 1 Red" (#512).
func TestPublicationSummarisesHealthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	yellow := h.ActiveGoal(boss, "Hire a CFO", "We need one.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	alsoGreen := h.ActiveGoal(boss, "Ship billing", "Get paid.")
	checkinOverdue := h.ActiveGoal(boss, "Open Tokyo", "Expand east.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, yellow.ID, domain.HealthYellow, "Slow.", "Use a recruiter.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(boss, alsoGreen.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, yellow.ID, green.ID, alsoGreen.ID, checkinOverdue.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	draft := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	for _, url := range []string{draft, draft + "/publications/" + strconv.FormatInt(pub.ID, 10)} {
		page := getPage(t, client, url)
		summary := findByTestID(t, page, "health-summary")
		if got, want := textOf(summary), "Health 1 Red · 1 Yellow · 2 Green · 1 Check-in overdue"; got != want {
			t.Errorf("%s: the Health summary reads %q, want %q", url, got, want)
		}
		for _, pair := range []string{"1 Red", "1 Yellow", "2 Green", "1 Check-in overdue"} {
			if !strings.Contains(runningText(summary), pair) {
				t.Errorf("%s: the Health summary runs a count into its word, want %q: %q", url, pair, runningText(summary))
			}
		}
		for tile, want := range map[string]string{"red": "1", "yellow": "1", "green": "2", "checkin-overdue": "1"} {
			if count := textOf(findByTestID(t, summary, "health-count-"+tile)); count != want {
				t.Errorf("%s: the %s count reads %q, want %s", url, tile, count, want)
			}
		}
		chips := findAll(summary, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "badge") })
		if len(chips) != 4 {
			t.Errorf("%s: the Health summary has %d chips, want 4: %s", url, len(chips), renderNode(summary))
		}
		for _, chip := range chips {
			srOnly := findAll(chip, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "sr-only") })
			if word := textOf(chip); word != "" || len(srOnly) > 0 || attrOf(chip, "aria-hidden") != "true" {
				t.Errorf("%s: a Health summary chip carries a word of its own, or reads to a screen reader, beside the word after its count: %s", url, renderNode(chip))
			}
		}
		if badges := textsOf(findAllByTestID(page, "report-badge")); !slices.Contains(badges, "Check-in overdue") {
			t.Errorf("%s: report badges = %q, want Open Tokyo's Check-in overdue", url, badges)
		}
	}
}

// The publication splits the selected Goals in two: exceptions under
// Exceptions, headed by how many there are, each with its Health badge and its
// Why it matters, Status, and Path to Green — marked Overdue once its target
// date has passed — and the rest as a table of Health, Goal, Owner, and Due
// under On track. That an exception is an entry ruled off by a hairline, not
// a card, is TestPublicationSplitsExceptionsFromOnTrackStyles.
func TestPublicationSplitsExceptionsFromOnTrackOverHTTP(t *testing.T) {
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
	doc := getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	attention := findByTestID(t, doc, "needs-attention")
	count := findByTestID(t, attention, "needs-attention-count")
	if heading := count.Parent; heading.Data != "h2" || textOf(heading) != "Exceptions 1" || textOf(count) != "1" {
		t.Errorf("Exceptions heading does not count its 1 exception: %s", renderNode(heading))
	}
	card := findByTestID(t, attention, "report-exception")
	assertHealthChip(t, findByTestID(t, card, "report-health"), domain.HealthRed)
	if path := findByTestID(t, card, "report-path-box"); !strings.Contains(textOf(path), "Hire counsel.") ||
		!hasTestID(path, "path-overdue") || textOf(findByTestID(t, path, "path-overdue")) != "Overdue" {
		t.Errorf("Path to Green box does not mark the missed Path to Green Overdue; box:\n%s", renderNode(path))
	}
	if lead := findByTestID(t, card, "report-why-it-matters"); textOf(lead) != "Expand the market." || textOf(lead.Parent) != "Why it matters Expand the market." {
		t.Errorf("exception does not lead with its Why it matters: %s", renderNode(lead.Parent))
	}

	onTrack := findByTestID(t, doc, "on-track")
	row := findByTestID(t, onTrack, "selected-goal")
	for _, want := range []string{"Cut churn", domain.HealthGreen} {
		if !strings.Contains(textOf(row), want) {
			t.Errorf("On track row missing %q; row:\n%s", want, renderNode(row))
		}
	}
	if !reportShowsPerson(row, "boss@example.com", "boss") {
		t.Errorf("On track row doesn't show its Owner, boss@example.com; row:\n%s", renderNode(row))
	}
}

// A Goal block's only box is its Path to Green, and only on a Red or Yellow
// Goal, with Ask for help beside it in the box: a Green Goal's block, and an
// exception that is only Check-in overdue, render none. Why it matters and
// Status read as plain text outside it, on the draft and the publication alike
// (#475). That the box is tinted in its Health's tint, and nothing else in the
// block is, is TestReportBlockTintsOnlyAPathToGreenStyles.
func TestReportBlockTintsOnlyAPathToGreenOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.SetManager(boss, h.SignInNamed("mia@example.com", "Mia Chen"))
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	yellow := h.ActiveGoal(boss, "Hire a CFO", "We need one.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	silent := h.ActiveGoal(boss, "Open Tokyo", "Expand east.")
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(40 * 24 * time.Hour)
	h.AskForHelp(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, yellow.ID, domain.HealthYellow, "Slow.", "Use a recruiter.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, yellow.ID, green.ID, silent.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	for name, url := range map[string]string{
		"draft":       reportURL,
		"publication": reportURL + "/publications/" + strconv.FormatInt(pub.ID, 10),
	} {
		doc := getPage(t, client, url)
		for _, c := range []struct {
			goal  domain.Goal
			boxed bool // whether its block holds a Path to Green box
		}{
			{red, true},
			{yellow, true},
			{green, false},
			{silent, false},
		} {
			block := reportBlockOf(t, doc, c.goal)
			boxes := findAllByTestID(block, "report-path-box")
			for _, id := range []string{"report-why-it-matters", "report-status"} {
				for _, box := range boxes {
					if hasTestID(box, id) {
						t.Errorf("%s: %q's %s sits in the Path to Green box: %s", name, c.goal.Title, id, renderNode(box))
					}
				}
			}
			if !c.boxed {
				if len(boxes) != 0 {
					t.Errorf("%s: %q's block has %d Path to Green boxes, want none:\n%s", name, c.goal.Title, len(boxes), renderNode(block))
				}
				continue
			}
			if len(boxes) != 1 {
				t.Fatalf("%s: %q's block has %d Path to Green boxes, want one:\n%s", name, c.goal.Title, len(boxes), renderNode(block))
			}
			if len(findAllByTestID(boxes[0], "report-path-to-green")) != 1 {
				t.Errorf("%s: %q's box doesn't hold its Path to Green:\n%s", name, c.goal.Title, renderNode(boxes[0]))
			}
		}
		ask := findByTestID(t, doc, "report-ask-for-help")
		if box := findByTestID(t, reportBlockOf(t, doc, red), "report-path-box"); ask.Parent != box {
			t.Errorf("%s: Launch in EU's Ask for help doesn't sit in its Path to Green's box: %s", name, renderNode(ask.Parent))
		}
	}
}

// reportBlockOf is g's exception block on a Report page, failing the test
// when there is none.
func reportBlockOf(t *testing.T, doc *nethtml.Node, g domain.Goal) *nethtml.Node {
	t.Helper()
	for _, block := range findAllByTestID(doc, "report-exception") {
		if attrOf(block, "id") == fmt.Sprintf("goal-%d", g.ID) {
			return block
		}
	}
	t.Fatalf("page has no exception block for %q", g.Title)
	return nil
}

// reportShowsPerson reports whether root shows the person emailAddr by label
// as a control that discloses their email, the email on hover too (shownAs).
func reportShowsPerson(root *nethtml.Node, emailAddr, label string) bool {
	for _, p := range findAll(root, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "person") }) {
		buttons := findAll(p, func(n *nethtml.Node) bool { return n.Data == "button" })
		emails := findAll(p, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "person-email") })
		if len(buttons) != 1 || len(emails) != 1 {
			continue
		}
		b := buttons[0]
		if textOf(b) == label && attrOf(b, "title") == emailAddr && attrOf(b, "type") == "button" &&
			flatFrameHasClass(b, "disclose") && attrOf(b, "aria-expanded") == "false" &&
			isHiddenElement(emails[0]) && runningText(emails[0]) == emailAddr {
			return true
		}
	}
	return false
}

// A publication's narrative is a plain section, each section under its
// heading and each Highlight credited "— owner, Goal". That it is no card,
// and its headings labels, is TestPublicationNarrativeReadsAsProseStyles.
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
	if err := h.Service.CurateNarrative(context.Background(), boss.ID, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: draft.Highlights[0].Highlight.ID, Section: domain.HighlightAccomplishment}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	narrative := findByTestID(t, doc, "report-narrative")
	if narrative.Data != "section" {
		t.Errorf("narrative is not a section; narrative:\n%s", renderNode(narrative))
	}
	if heading := findAllByTestID(narrative, "narrative-section"); len(heading) == 0 || heading[0].Data != "h3" || textOf(heading[0]) != "Accomplishments" {
		t.Errorf("narrative section headings %q, want an Accomplishments heading", textsOf(heading))
	}
	credit := findByTestID(t, narrative, "highlight-credit")
	if got := runningText(credit); got != "— alice, Launch in EU" || !reportShowsPerson(credit, "alice@example.com", "alice") || linkTo(credit, fmt.Sprintf("/goals/%d", g.ID)) == nil {
		t.Errorf("Highlight credit %q, want — owner, Goal: %s", got, renderNode(credit))
	}
}

// The draft page keeps Publish in reach: a header strip carries the
// breadcrumb to the name, the baseline, the save status, the History of
// publications and Publish… — saying what it does. The save status sits in the
// name's block, so at phone width it can stay beside the name (#172). How the
// header lays them out, Publish… as the primary button, is
// TestDraftHeaderKeepsPublishInReachStyles.
func TestDraftHeaderKeepsPublishInReachOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	header := findByTestID(t, doc, "draft-header")
	if header.Data != "header" {
		t.Errorf("the draft's header isn't a header: %s", renderNode(header))
	}
	reports := linkTo(header, "/reports")
	current := findAll(header, func(n *nethtml.Node) bool { return attrOf(n, "aria-current") == "page" })
	if reports == nil || textOf(reports) != "Reports" || len(current) != 1 || textOf(current[0]) != "MBR" {
		t.Fatalf("the header's breadcrumb doesn't lead from Reports to MBR; header:\n%s", renderNode(header))
	}
	parts := []*nethtml.Node{reports, current[0]}
	for _, testID := range []string{"save-status", "baseline-chip", "edit-definition", "publish-confirm"} {
		parts = append(parts, findByTestID(t, header, testID))
	}
	for i := 1; i < len(parts); i++ {
		if !precedes(parts[i-1], parts[i]) {
			t.Errorf("%s is out of order in the header, before %s; header:\n%s", renderNode(parts[i]), renderNode(parts[i-1]), renderNode(header))
		}
	}
	if chip := findByTestID(t, header, "baseline-chip"); chip.Data != "details" || !hasTestID(chip, "report-baseline") {
		t.Errorf("the baseline chip does not hold the baseline form; chip:\n%s", renderNode(chip))
	}
	publish := findByTestID(t, header, "publish-confirm")
	summaries := findAll(publish, func(n *nethtml.Node) bool { return n.Data == "summary" })
	if publish.Data != "details" || len(summaries) != 1 || summaries[0].Parent != publish || textOf(summaries[0]) != "Publish…" ||
		!strings.Contains(textOf(publish), "Readers can comment on the frozen copy.") {
		t.Errorf("Publish… does not open its summary; disclosure:\n%s", renderNode(publish))
	}
	if hasTestID(header, "report-history") {
		t.Errorf("the header still has a History menu; header:\n%s", renderNode(header))
	}
	if hasTestID(doc, "report-publications") {
		t.Errorf("publications are still listed in a sidebar; body:\n%s", renderNode(doc))
	}
}

// The reports page is headed Reports with a plain line on what a Report is,
// and New report links to the builder in place of a form of its own: POST
// /reports is gone. Each definition is a row of the status table: its name and
// scope, its Goal count, and, never published, "Never", linking nowhere, and
// "Not published". Its one Goal has no Health yet, so the row draws no Health
// bar.
func TestReportsListLinksToTheBuilderOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	root := h.MarkTopLevel(boss, h.ActiveGoal(boss, "Grow revenue", "The company needs to grow."))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{root.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	doc := getPage(t, client, ts.URL+"/reports")

	h1s := findAll(doc, func(n *nethtml.Node) bool { return n.Data == "h1" })
	if len(h1s) != 1 || textOf(h1s[0]) != "Reports" || strings.Contains(textOf(doc), "Saved Report Definitions") {
		t.Errorf("the page is not headed Reports alone: headings %q", textsOf(h1s))
	}
	if desc := findByTestID(t, doc, "reports-description"); !strings.Contains(textOf(desc), "Publish a frozen copy") {
		t.Errorf("the page has no plain description: %s", renderNode(desc))
	}
	if link := findByTestID(t, doc, "new-report"); link.Data != "a" || attrOf(link, "href") != "/reports/new" || !strings.Contains(textOf(link), "New report") {
		t.Errorf("New report doesn't link to the builder: %s", renderNode(link))
	}
	row := findByTestID(t, doc, "report-row")
	if name := linkTo(row, fmt.Sprintf("/reports/%d", def.ID)); name == nil || textOf(name) != "MBR" {
		t.Errorf("row doesn't link MBR to its draft; row:\n%s", renderNode(row))
	}
	for testID, want := range map[string]string{
		"report-scope":     "1 Goal picked by hand",
		"report-goals":     "1",
		"report-published": "Never",
		"report-draft":     "Not published",
	} {
		if got := textOf(findByTestID(t, row, testID)); got != want {
			t.Errorf("row's %s reads %q, want %q", testID, got, want)
		}
	}
	if never := findByTestID(t, row, "report-published"); len(findAll(never.Parent, func(n *nethtml.Node) bool { return n.Data == "a" })) != 0 {
		t.Errorf("a never-published row's Last published cell has a link: %s", renderNode(never.Parent))
	}
	if hasTestID(row, "report-health-bar") {
		t.Errorf("a report whose Goals have no Health draws a Health bar; row:\n%s", renderNode(row))
	}
	creates := findAll(doc, func(n *nethtml.Node) bool { return n.Data == "form" && attrOf(n, "action") == "/reports" })
	if len(creates) != 0 || hasTestID(doc, "create-report") {
		t.Errorf("the reports page still has its own create form; body:\n%s", renderNode(doc))
	}
	resp := postForm(t, noRedirects(client), ts.URL+"/reports", url.Values{"name": {"Old way"}})
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /reports: status %d, want 405; body:\n%s", resp.StatusCode, body)
	}
}

// A published definition's row dates its last publication in the company's
// timezone, "Jan 1, 2026" for 03:00 UTC on 2 January in Los Angeles, a link to
// that publication, with who published it; counts its draft's changes since; and draws its Goals that
// have a Health as a bar labelled for assistive technology, leaving out the
// three new Goals that have none yet. The amber pill and the bar's units are
// TestReportsListRowShowsTheLastPublicationAndChangesStyles.
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
	h.AddReportDelegate(boss, ada, def)
	pub := h.PublishReport(ada, def)
	h.Clock.Advance(24 * time.Hour)
	for _, title := range []string{"Hire", "Ship", "Sell"} {
		h.ActiveGoal(boss, title, "why")
	}

	ts := httptest.NewServer(web.NewServer(domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))))
	t.Cleanup(ts.Close)
	row := findByTestID(t, getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports"), "report-row")

	for testID, want := range map[string]string{
		"report-scope":     "Owner: boss",
		"report-goals":     "5",
		"report-published": "Jan 1, 2026",
		"report-draft":     "3 changes",
	} {
		if got := textOf(findByTestID(t, row, testID)); got != want {
			t.Errorf("row's %s reads %q, want %q", testID, got, want)
		}
	}
	if bar := findByTestID(t, row, "report-health-bar"); attrOf(bar, "role") != "img" || attrOf(bar, "aria-label") != "1 Green, 0 Yellow, 1 Red" {
		t.Errorf("the Health bar isn't labelled 1 Green, 0 Yellow, 1 Red for assistive technology: %s", renderNode(bar))
	}
	if link := linkTo(row, fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)); link == nil || textOf(link) != "Jan 1, 2026" {
		t.Errorf("row doesn't link Jan 1, 2026 to its last publication; row:\n%s", renderNode(row))
	}
	if !reportShowsPerson(row, "ada.okafor@example.com", "Ada Okafor") {
		t.Errorf("row doesn't show Ada Okafor published it; row:\n%s", renderNode(row))
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
	table := findByTestID(t, getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports"), "report-table")
	draftPath := regexp.MustCompile(`^/reports/\d+$`)
	got := textsOf(findAll(table, func(n *nethtml.Node) bool { return n.Data == "a" && draftPath.MatchString(attrOf(n, "href")) }))
	if want := []string{"Yankee", "Echo", "Beta", "Zulu", "Alpha"}; !slices.Equal(got, want) {
		t.Errorf("reports listed %q, want %q", got, want)
	}
}

// A Report card's head reads Health first, its "· N wks" and "· was X" straight
// after it, then the title and its meta line, then the other badges, on the
// draft and on the print page (#229). That it keeps that order at phone width
// and in a narrow block is TestReportCardHeadLeadsWithItsHealthStyles.
func TestReportCardHeadLeadsWithItsHealthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Cut infrastructure cost per request", "Margins matter.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Clock.Advance(10 * 24 * time.Hour)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	page := getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))
	printed := getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")
	for where, doc := range map[string]*nethtml.Node{"the draft": page, "the print page": printed} {
		block := findByTestID(t, doc, "report-exception")
		health := findByTestID(t, block, "report-health")
		run := findByTestID(t, block, "report-run-off-green")
		title := linkTo(block, fmt.Sprintf("/goals/%d", red.ID))
		badge := findByTestID(t, block, "report-badge")
		if title == nil || !precedes(health, run) || !precedes(run, title) || !precedes(title, badge) {
			t.Errorf("%s's card head doesn't read Health, its Run off Green, the title, then its badges:\n%s", where, renderNode(block))
		}
		if textOf(badge) != domain.BadgeCheckinOverdue {
			t.Errorf("%s's card head badge reads %q, want %s", where, textOf(badge), domain.BadgeCheckinOverdue)
		}
	}
	assertHealthChip(t, findByTestID(t, page, "report-health"), "Red")
}

// A Goal with no Health yet prints its dash alone: no drawn shape, which the
// print page's base .dot rule would fill as a square.
func TestPrintPageDrawsNoShapeForAGoalWithNoHealthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fresh := h.ActiveGoal(boss, "Hire a PM", "Fill the role.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{fresh.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	printed := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")

	badge := findByTestID(t, findByTestID(t, printed, "report-exception"), "report-health")
	dots := findAll(badge, func(n *nethtml.Node) bool { return attrOf(n, "class") == "dot" })
	if shownTextOf(badge) != "—" || len(dots) != 0 {
		t.Errorf("a Goal with no Health doesn't print its dash alone: %s", renderNode(badge))
	}
}

// The print page marks Health with a shape as well as its word, shown, not
// left to a screen reader as on screen, so it survives black-and-white
// printing, and with no ■ ▲ ● characters (#452). Its serif and the shapes it
// draws are TestPrintPageMarksHealthWithShapesStyles.
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
	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")

	if count := findByTestID(t, doc, "needs-attention-count"); textOf(count) != "1" {
		t.Errorf("print page's Exceptions does not count its 1 exception: %s", renderNode(count))
	}
	if strings.ContainsAny(textOf(doc), "■▲●") {
		t.Errorf("print page still marks Health with ■ ▲ ● characters; body:\n%s", renderNode(doc))
	}
	for _, want := range []struct{ in, badge, health string }{
		{"report-exception", "report-health", "Red"},
		{"selected-goal", "selected-health", "Green"},
	} {
		badge := findByTestID(t, findByTestID(t, doc, want.in), want.badge)
		testID, health := want.in, want.health
		dots := findAll(badge, func(n *nethtml.Node) bool { return attrOf(n, "class") == "dot" })
		if shownTextOf(badge) != health || len(dots) != 1 {
			t.Errorf("%s's Health badge doesn't show %s beside one drawn shape: %s", testID, health, renderNode(badge))
		}
	}
}

// The print page lays a Goal block out as the screen does (#475): Why it
// matters and Status as the facts of a description list, and the Path to
// Green in a box of its own. An overdue Path to Green says Overdue in its
// badge, and one on time doesn't (#478). The label column, and the box's
// tint, are TestPrintPageLaysOutTheGoalBlockAsOnScreenStyles.
func TestPrintPageLaysOutTheGoalBlockAsOnScreenOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	yellow := h.ActiveGoal(boss, "Hire a CFO", "We need one.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, yellow.ID, domain.HealthYellow, "Slow.", "Use a recruiter.", h.Clock.Now().AddDate(0, 0, 7))
	h.Clock.Advance(14 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, yellow.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")

	for _, c := range []struct {
		goal    domain.Goal
		overdue bool
	}{
		{red, false},
		{yellow, true},
	} {
		block := reportBlockOf(t, doc, c.goal)
		for _, id := range []string{"report-why-it-matters", "report-status"} {
			dd := findByTestID(t, block, id)
			if dl := dd.Parent.Parent; dd.Data != "dd" || dl == nil || dl.Data != "dl" {
				t.Errorf("%q's %s isn't a fact of a description list: %s", c.goal.Title, id, renderNode(dd.Parent))
			}
		}
		box := findByTestID(t, block, "report-path-box")
		if badge := hasTestID(box, "path-overdue"); badge != c.overdue {
			t.Errorf("%q's Path to Green box shows the Overdue badge %v, want %v: %s", c.goal.Title, badge, c.overdue, renderNode(box))
		}
	}
}

// A publication's On track table gives each Goal's discussion a row of its
// own under the Goal's, its one cell spanning the row's four columns: Health,
// Goal, Owner and Due. How the rows stack at a phone's width is pinned in
// styles_test.go (TestOnTrackRowsStackAtPhoneWidthStyles).
func TestOnTrackRowsStackAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(3 * 24 * time.Hour)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	onTrack := findByTestID(t, page, "on-track")
	row := findByTestID(t, onTrack, "selected-goal")
	talk := findByTestID(t, onTrack, "goal-discussion")
	cell := talk.Parent
	if cell.Data != "td" || within(cell, row) || !precedes(row, cell) {
		t.Fatalf("the Goal's discussion isn't in a cell of its own after the Goal's row: %s", renderNode(onTrack))
	}
	if span := attrOf(cell, "colspan"); span != "4" {
		t.Errorf("the discussion's cell spans %q columns, want all 4: %s", span, renderNode(cell.Parent))
	}
}

// An exception card's due date reads as the date it slipped from, struck
// through, then the date it is now, and a Milestone's dates read as its date
// then the one it slipped from, struck through (#180). That neither breaks
// mid-date on a phone is pinned in styles_test.go
// (TestExceptionCardDatesDoNotBreakStyles).
func TestExceptionCardDatesDoNotBreakOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	beta := onlyMilestone(t, h, g)
	slippedBeta := beta.TargetDate.AddDate(0, 0, 10)
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID:             g.ID,
		AuthorID:           boss.ID,
		Health:             domain.HealthYellow,
		Status:             "Later than planned.",
		PathToGreen:        "Swap vendors.",
		PathTargetDate:     testsupport.Epoch.AddDate(0, 2, 0),
		DeliveryDate:       time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		DeliveryDateReason: "Vendor slipped.",
		Milestones:         []domain.MilestoneChangeInput{{MilestoneID: beta.ID, TargetDate: slippedBeta, DateReason: "Vendor slipped."}},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	block := findByTestID(t, page, "report-exception")
	due := findByTestID(t, block, "report-due")
	if got, struck := textOf(due), struckThrough(due); got != "Jul 2, 2026 Jul 16, 2026" || !slices.Equal(struck, []string{"Jul 2, 2026"}) {
		t.Errorf("the card's due date reads %q with %q struck, want Jul 2, 2026 struck then Jul 16, 2026", got, struck)
	}
	now, was := slippedBeta.Format("Jan 2, 2006"), beta.TargetDate.Format("Jan 2, 2006")
	milestone := findByTestID(t, block, "report-milestone")
	if got, struck := textOf(milestone), struckThrough(milestone); !strings.Contains(got, now+" "+was+" ") || !slices.Equal(struck, []string{was}) {
		t.Errorf("the Milestone reads %q with %q struck, want %s then %s struck", got, struck, now, was)
	}
}

// healthClassYellow is the class a Yellow Health badge carries.
const healthClassYellow = "y"

// reportMarkedMilestones is how the Report lists MilestoneMarksGoal's
// Milestones, earliest date first, each mark, then date, then name.
var reportMarkedMilestones = []string{
	"Overdue Jan 20, 2026 Security review",
	"Done Feb 1, 2026 Pilot",
	"New Mar 10, 2026 Docs",
	"Slipped Apr 12, 2026 Apr 2, 2026 Beta",
	"Slipped Apr 20, 2026 Mar 20, 2026 · 3 slips · Vendor sign-off",
	"May 1, 2026 Runbook",
	"Removed May 20, 2026 Launch party — Budget cut.",
	"Jun 15, 2026 GA launch",
}

// An exception block lists each Milestone as its mark, its date and its name,
// on the draft, the publication and its print page alike. The mark is the
// first that applies: Done, Removed, Overdue while past its date, Slipped once
// slipped, New since the baseline, and none while on track, each shown as its
// word: Overdue in Red's tint and Slipped in Yellow's, with no Health shape
// (#461). The date is the current one with the most
// recent it slipped from struck through, and a count once it slipped more than
// once. The marks are not a Health: the Goal keeps the Yellow its Owner set and
// its Rolled-up Health (ADR 0003).
func TestReportListsMilestonesAsMarkDateNameOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.MilestoneMarksGoal(boss)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	for name, url := range map[string]string{
		"draft":       fmt.Sprintf("%s/reports/%d?baseline=2026-01-12", ts.URL, def.ID),
		"publication": fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID),
		"print":       fmt.Sprintf("%s/reports/%d/publications/%d/print", ts.URL, def.ID, pub.ID),
	} {
		t.Run(name, func(t *testing.T) {
			block := findByTestID(t, getPage(t, client, url), "report-exception")
			milestones := findAllByTestID(block, "report-milestone")
			if got := textsOf(milestones); !slices.Equal(got, reportMarkedMilestones) {
				t.Errorf("Milestones read\n %q\nwant\n %q", got, reportMarkedMilestones)
			}
			assertMilestoneMarksShowTheirWords(t, findByTestID(t, block, "report-milestones"),
				[]string{"Overdue", "Done", "New", "Slipped", "Slipped", "Removed"})
			var vendor *nethtml.Node
			for _, li := range milestones {
				if strings.Contains(textOf(li), "Vendor sign-off") {
					vendor = li
				}
			}
			if vendor == nil {
				t.Fatalf("no Milestone reads Vendor sign-off: %s", renderNode(block))
			}
			if struck := struckThrough(vendor); !slices.Equal(struck, []string{"Mar 20, 2026"}) {
				t.Errorf("Vendor sign-off strikes through %q, want the date it last slipped from, Mar 20, 2026", struck)
			}
			for _, older := range []string{"Mar 1,", "Mar 10,"} {
				if strings.Contains(textOf(vendor), older) {
					t.Errorf("Vendor sign-off still lists the older date %s: %q", older, textOf(vendor))
				}
			}
			if !slices.Contains(struckThrough(block), "Launch party") {
				t.Errorf("the Removed Milestone's name is not struck through: %q struck", struckThrough(block))
			}
			for _, want := range []string{"report-health", "rollup-health"} {
				if health := findByTestID(t, block, want); !flatFrameHasClass(health, healthClassYellow) || textOf(health) != "Yellow" {
					t.Errorf("%s is not the Yellow set by the Owner and rolled up from the child: %s", want, renderNode(health))
				}
			}
		})
	}
}

// A publication frozen before Milestones read mark, date, name shows them in
// that form: the snapshot already holds all the mark needs.
func TestPublicationFromBeforeMilestoneMarksListsMarkDateNameOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[{"Goal":{"ID":1,"Title":"Launch in EU","WhyItMatters":"Expand the market.",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"},"Lifecycle":"Active"},"Health":"Red","Milestones":[` +
		`{"Milestone":{"ID":1,"Name":"Beta","TargetDate":"2025-12-20T00:00:00Z","Status":"Planned"},"New":false,"PriorDates":["2025-12-01T00:00:00Z"]},` +
		`{"Milestone":{"ID":2,"Name":"GA","TargetDate":"2026-03-01T00:00:00Z","Status":"Planned"},"New":true,"PriorDates":null},` +
		`{"Milestone":{"ID":3,"Name":"Launch","TargetDate":"2026-04-01T00:00:00Z","Status":"Planned"},"New":false,"PriorDates":null}]}]}`
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID))
	want := []string{"Overdue Dec 20, 2025 Dec 1, 2025 Beta", "New Mar 1, 2026 GA", "Apr 1, 2026 Launch"}
	if got := textsOf(findAllByTestID(findByTestID(t, page, "report-exception"), "report-milestone")); !slices.Equal(got, want) {
		t.Errorf("Milestones read\n %q\nwant\n %q", got, want)
	}
}

// The draft is one page: its main column is the Report as readers get it,
// headed by the Report's name, then a muted line saying what the definition
// selects, how many Goals that is and what it reads changes against, then the
// Introduction, once and read-only, since it belongs to the definition. The
// header strip keeps the name only as its breadcrumb. Its creator and a reader
// who may not manage it read the same column; the compose pane, the Goals panel
// and the preview's frame are gone (#473).
func TestDraftMainColumnReadsAsTheReportOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Dana Okafor")
	lee := h.SignIn("lee@example.com")
	h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	added := h.ActiveGoal(lee, "Open Tokyo", "Expand east.")
	alsoAdded := h.ActiveGoal(lee, "Hire a CFO", "We need one.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:         "Growth MBR",
		Introduction: "Where the EU launch stands.",
		Mode:         domain.ReportModeRules,
		Rules:        []domain.ReportRule{{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{strconv.FormatInt(boss.ID, 10)}}},
		Include:      []int64{added.ID, alsoAdded.ID},
	})

	ts := newServer(t, h)
	for _, who := range []string{"boss@example.com", "lee@example.com"} {
		page := getPage(t, signInClient(t, ts.URL, who), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

		main := findByTestID(t, page, "report-draft")
		if name := findByTestID(t, main, "report-name"); name.Data != "h1" || textOf(name) != "Growth MBR" {
			t.Errorf("%s: the main column's name is <%s> %q, want the h1 Growth MBR", who, name.Data, textOf(name))
		}
		header := findByTestID(t, page, "draft-header")
		if hasTestID(header, "report-name") || len(findAll(header, func(n *nethtml.Node) bool { return n.Data == "h1" })) != 0 {
			t.Errorf("%s: the header strip still heads the page with the name: %s", who, renderNode(header))
		}
		if got := textOf(findByTestID(t, header, "report-breadcrumb")); !strings.Contains(got, "Reports") || !strings.Contains(got, "Growth MBR") {
			t.Errorf("%s: the header's breadcrumb reads %q, want Reports › Growth MBR", who, got)
		}
		if got, want := textOf(findByTestID(t, main, "report-meta")), "Owner: Dana Okafor · 2 added · 3 Goals · changes since Dec 3, 2025"; got != want {
			t.Errorf("%s: the meta line reads %q, want %q", who, got, want)
		}
		intros := findAllByTestID(page, "report-introduction")
		if len(intros) != 1 || !within(intros[0], main) || textOf(intros[0]) != "Where the EU launch stands." {
			t.Errorf("%s: the Introduction shows %d times (%q), want once in the main column", who, len(intros), textsOf(intros))
		}
		if !precedes(findByTestID(t, main, "report-meta"), intros[0]) || !precedes(intros[0], findByTestID(t, main, "health-summary")) {
			t.Errorf("%s: the Introduction isn't between the meta line and the Health summary", who)
		}
		if formFields(page, "introduction") != nil {
			t.Errorf("%s: the draft offers to edit the Introduction", who)
		}
		for _, gone := range []string{"compose-panel", "draft-panes", "draft-introduction", "report-goals", "report-goal", "scope-summary"} {
			if hasTestID(page, gone) {
				t.Errorf("%s: the draft still has %s", who, gone)
			}
		}
		if strings.Contains(textOf(page), "Preview: exactly what readers will see") {
			t.Errorf("%s: the draft still labels a preview", who)
		}
	}
}

// The narrative sits in the main column where readers meet it, after the
// Health summary and before the exceptions: a reader who may manage the Report
// composes it there, under a Narrative heading, and anyone else reads it as it
// will be published, with no compose form. Everyone gets the narrow column
// beside it, its Delegates above its History; only the creator gets Remove
// and Add, and no reader who may not manage the Report gets History,
// Edit definition or Publish… in the header (#473, #474).
func TestDraftComposesTheNarrativeInTheMainColumnOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	lee := h.SignIn("lee@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID}})
	if err := h.Service.CurateNarrative(context.Background(), boss.ID, def.ID, domain.CurateNarrativeInput{
		Notes: map[string][]string{domain.HighlightMiss: {"Slipped a week."}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	if err := h.Service.AddReportDelegateByEmail(context.Background(), boss.ID, def.ID, lee.Email); err != nil {
		t.Fatalf("AddReportDelegateByEmail: %v", err)
	}
	h.SignIn("sam@example.com")

	ts := newServer(t, h)
	draftURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)

	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), draftURL)
	main := findByTestID(t, page, "report-draft")
	composer := findByTestID(t, main, "report-curation")
	if headingNamed(composer, "Narrative") == nil {
		t.Errorf("the composer has no Narrative heading: %s", renderNode(composer))
	}
	form := findByTestID(t, composer, "narrative-curation")
	if buttonNamed(form, "Save narrative") == nil {
		t.Errorf("the composer has no Save narrative")
	}
	if !precedes(findByTestID(t, main, "health-summary"), composer) || !precedes(composer, findByTestID(t, main, "needs-attention")) {
		t.Errorf("the composer isn't between the Health summary and the exceptions")
	}
	if hasTestID(page, "report-narrative") {
		t.Errorf("the manager reads the narrative as published beside the composer")
	}
	side := findByTestID(t, page, "draft-side")
	if within(side, main) {
		t.Fatalf("the narrow column is inside the main column")
	}
	delegates, history := findByTestID(t, side, "report-delegates"), findByTestID(t, side, "report-history")
	if !precedes(delegates, history) {
		t.Errorf("History comes before the Delegates")
	}
	if !hasTestID(side, "add-report-delegate") || !hasTestID(side, "remove-report-delegate") {
		t.Errorf("the creator can't add or remove Delegates: %s", renderNode(delegates))
	}

	// A Delegate composes the narrative too, but only its creator and Admins
	// manage its Delegates.
	page = getPage(t, signInClient(t, ts.URL, "lee@example.com"), draftURL)
	if !hasTestID(findByTestID(t, page, "report-draft"), "narrative-curation") {
		t.Errorf("a Delegate doesn't get the composer")
	}
	side = findByTestID(t, page, "draft-side")
	if !hasTestID(side, "report-delegates") || hasTestID(side, "add-report-delegate") || hasTestID(side, "remove-report-delegate") {
		t.Errorf("a Delegate's narrow column doesn't list the Delegates without Add and Remove: %s", renderNode(side))
	}

	page = getPage(t, signInClient(t, ts.URL, "sam@example.com"), draftURL)
	main = findByTestID(t, page, "report-draft")
	if hasTestID(page, "report-curation") || hasTestID(page, "narrative-curation") {
		t.Errorf("a reader who may not manage the Report gets the composer")
	}
	header := findByTestID(t, page, "draft-header")
	for _, action := range []string{"report-history", "edit-definition", "publish-confirm"} {
		if hasTestID(header, action) {
			t.Errorf("a reader who may not manage the Report has %s in the header", action)
		}
	}
	narrative := findByTestID(t, main, "report-narrative")
	if got := textsOf(findAllByTestID(narrative, "narrative-text")); !slices.Equal(got, []string{"Slipped a week."}) {
		t.Errorf("the reader reads the notes %q, want the saved one", got)
	}
	if !precedes(findByTestID(t, main, "health-summary"), narrative) || !precedes(narrative, findByTestID(t, main, "needs-attention")) {
		t.Errorf("the reader's narrative isn't between the Health summary and the exceptions")
	}
	side = findByTestID(t, page, "draft-side")
	if !hasTestID(side, "report-delegates") || !hasTestID(side, "report-history") {
		t.Errorf("the reader's narrow column lacks the Delegates or History: %s", renderNode(side))
	}
	if hasTestID(side, "add-report-delegate") || hasTestID(side, "remove-report-delegate") {
		t.Errorf("the reader gets Add or Remove on the Delegates")
	}
}

// A publication, the print page and a non-manager's draft head the narrative
// with a level-2 "Narrative", as the composer is headed; an empty narrative
// leaves the whole section out (#479).
func TestPublishedNarrativeHasItsNarrativeHeadingOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	empty := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Empty", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	emptyPub := h.PublishReport(boss, empty)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	// Publishing starts the next draft's narrative afresh, so the author
	// writes a note into each.
	writeMiss := func() {
		t.Helper()
		if err := h.Service.CurateNarrative(context.Background(), boss.ID, def.ID, domain.CurateNarrativeInput{
			Notes: map[string][]string{domain.HighlightMiss: {"Slipped a week."}},
		}); err != nil {
			t.Fatalf("CurateNarrative: %v", err)
		}
	}
	writeMiss()
	pub := h.PublishReport(boss, def)
	writeMiss()
	h.SignIn("sam@example.com")

	ts := newServer(t, h)
	bossClient, sam := signInClient(t, ts.URL, "boss@example.com"), signInClient(t, ts.URL, "sam@example.com")
	pubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
	for name, c := range map[string]struct {
		client *http.Client
		url    string
	}{
		"publication":       {bossClient, pubURL},
		"print page":        {bossClient, pubURL + "/print"},
		"non-manager draft": {sam, fmt.Sprintf("%s/reports/%d", ts.URL, def.ID)},
	} {
		narrative := findByTestID(t, getPage(t, c.client, c.url), "report-narrative")
		heading := headingNamed(narrative, "Narrative")
		if heading == nil || heading.Data != "h2" || heading.Parent != narrative {
			t.Errorf("%s: the narrative isn't headed by its own h2 Narrative: %s", name, renderNode(narrative))
			continue
		}
		if sec := findByTestID(t, narrative, "narrative-section"); textOf(sec) != "Misses" || !precedes(heading, sec) {
			t.Errorf("%s: Misses isn't under the Narrative heading: %s", name, renderNode(narrative))
		}
	}

	emptyPubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, empty.ID, emptyPub.ID)
	for name, c := range map[string]struct {
		client *http.Client
		url    string
	}{
		"empty publication":       {bossClient, emptyPubURL},
		"empty print page":        {bossClient, emptyPubURL + "/print"},
		"empty non-manager draft": {sam, fmt.Sprintf("%s/reports/%d", ts.URL, empty.ID)},
	} {
		if page := getPage(t, c.client, c.url); hasTestID(page, "report-narrative") || headingNamed(page, "Narrative") != nil {
			t.Errorf("%s: an empty narrative still shows its section or heading", name)
		}
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
	printed := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")

	if line := findByTestID(t, printed, "report-published"); reportTextsReading(line, "boss (boss@example.com)") != 1 {
		t.Errorf("print page does not introduce the publisher; line: %s", renderNode(line))
	}
	if n := reportTextsReading(printed, "Ada Okafor (ada.okafor@example.com)"); n != 1 {
		t.Errorf("print page introduces Ada %d times, want once; body:\n%s", n, renderNode(printed))
	}
	if n := reportTextsReading(printed, "Ada Okafor"); n != 1 {
		t.Errorf("print page names Ada alone %d times, want once after her introduction; body:\n%s", n, renderNode(printed))
	}
}

// reportTextsReading is how many of the texts under root, each run of text
// between two tags, read text and nothing more.
func reportTextsReading(root *nethtml.Node, text string) int {
	n := 0
	for c := range root.Descendants() {
		if c.Type == nethtml.TextNode && c.Data == text {
			n++
		}
	}
	return n
}

// reportPageMentions reports whether s shows anywhere on the page under root:
// in its text, hidden text included, or in a tooltip.
func reportPageMentions(root *nethtml.Node, s string) bool {
	for n := range root.Descendants() {
		switch {
		case n.Type == nethtml.TextNode && n.Parent != nil && n.Parent.Data != "style" && n.Parent.Data != "script" && strings.Contains(n.Data, s):
			return true
		case n.Type == nethtml.ElementNode && strings.Contains(attrOf(n, "title"), s):
			return true
		}
	}
	return false
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
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE accounts SET name = 'Ada Mensah' WHERE id = ?`, ada.ID); err != nil {
		t.Fatalf("rename Ada: %v", err)
	}
	before := fmt.Sprintf(`{"Definition":{"ID":%d,"Name":"MBR"},"Lines":[{"Goal":{"ID":%d,"Title":"Launch in EU",`+
		`"Owner":{"ID":%d,"Email":"ada.okafor@example.com","IsAdmin":false,"Departed":false}},"Health":""}]}`, def.ID, g.ID, ada.ID)
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, old.ID); err != nil {
		t.Fatalf("write a pre-Names snapshot: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubURL := func(p domain.Publication) string {
		return ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(p.ID, 10)
	}

	if page := getPage(t, client, pubURL(renamed)); !reportShowsPerson(page, "ada.okafor@example.com", "Ada Okafor") || reportPageMentions(page, "Ada Mensah") {
		t.Errorf("publication does not show Ada as published; body:\n%s", renderNode(page))
	}
	if row := findByTestID(t, getPage(t, client, pubURL(old)), "selected-owner"); !reportShowsPerson(row, "ada.okafor@example.com", "ada.okafor@example.com") {
		t.Errorf("pre-Names Owner reads %s, want the email", renderNode(row))
	}
	if printed := getPage(t, client, pubURL(old)+"/print"); reportTextsReading(printed, "ada.okafor@example.com") == 0 || reportPageMentions(printed, "(ada.okafor@example.com)") {
		t.Errorf("pre-Names print page does not show Ada by email alone; body:\n%s", renderNode(printed))
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
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE accounts SET name = 'Dana Renamed' WHERE id = ?`, ceo.ID); err != nil {
		t.Fatalf("rename the publisher: %v", err)
	}

	ts := newServer(t, h)
	// Read by someone else, so the signed-in header doesn't show the live Name.
	client := signInClient(t, ts.URL, "ada.okafor@example.com")
	pubURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(pub.ID, 10)

	page := getPage(t, client, pubURL)
	if line := findByTestID(t, page, "report-published"); !reportShowsPerson(line, "ceo@example.com", "Dana Whitfield") {
		t.Errorf("publication byline does not show Dana Whitfield as published; line: %s", renderNode(line))
	}
	printed := getPage(t, client, pubURL+"/print")
	if line := findByTestID(t, printed, "report-published"); reportTextsReading(line, "Dana Whitfield (ceo@example.com)") != 1 {
		t.Errorf("print byline does not introduce Dana Whitfield as published; line: %s", renderNode(line))
	}
	md := getBody(t, client, pubURL+"/markdown")
	if !strings.Contains(md, " by Dana Whitfield (ceo@example.com). ") {
		t.Errorf("Markdown byline does not introduce Dana Whitfield as published; body:\n%s", md)
	}
	for name, doc := range map[string]*nethtml.Node{"publication": page, "print page": printed} {
		if reportPageMentions(doc, "Dana Renamed") {
			t.Errorf("%s shows the Name given after publishing; body:\n%s", name, renderNode(doc))
		}
	}
	if strings.Contains(md, "Dana Renamed") {
		t.Errorf("Markdown shows the Name given after publishing; body:\n%s", md)
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
	grower := h.ActiveGoal(boss, "Grow revenue", "The company needs to grow.")
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

	rules := findByTestID(t, getPage(t, client, ts.URL+"/reports/new"), "report-rules")
	if offered := reportRuleChoices(rules); strings.Contains(offered, "Pillar") || strings.Contains(offered, "Growth") {
		t.Errorf("the Retired Pillar is offered for a Report rule: %q", offered)
	}
	if offered := reportRuleChoices(rules); !strings.Contains(offered, "Team") {
		t.Errorf("the live Team isn't offered for a Report rule: %q", offered)
	}

	draft := getPage(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	if !reportPageMentions(draft, grower.Title) || reportPageMentions(draft, truster.Title) {
		t.Errorf("the saved Growth filter no longer drafts just %q; body:\n%s", grower.Title, renderNode(draft))
	}

	if err := h.Service.RestoreDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RestoreDimension: %v", err)
	}
	rules = findByTestID(t, getPage(t, client, ts.URL+"/reports/new"), "report-rules")
	pillarKey := fmt.Sprintf("dimension:%d", pillar.ID)
	option := func(value string) *nethtml.Node {
		if found := findAll(rules, func(n *nethtml.Node) bool { return n.Data == "option" && attrOf(n, "value") == value }); len(found) > 0 {
			return found[0]
		}
		return nil
	}
	if attribute, value := option(pillarKey), option(fmt.Sprintf("%s=%d", pillarKey, growth.ID)); attribute == nil || textOf(attribute) != "Pillar" || value == nil {
		t.Errorf("the restored Pillar isn't offered for a Report rule: %s", renderNode(rules))
	}
}

// reportRuleChoices is everything the builder's rules offer to pick, each
// option and each group of them by its label, run together.
func reportRuleChoices(rules *nethtml.Node) string {
	var offered []string
	for _, n := range findAll(rules, func(n *nethtml.Node) bool { return n.Data == "option" || n.Data == "optgroup" }) {
		if n.Data == "optgroup" {
			offered = append(offered, attrOf(n, "label"))
		} else {
			offered = append(offered, textOf(n))
		}
	}
	return strings.Join(offered, " ")
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

	choice := findByTestID(t, getPage(t, client, ts.URL+"/reports/new"), "report-field-choice")
	for _, f := range []domain.Field{budget, notes, sponsor} {
		if !slices.ContainsFunc(formFields(choice, "field"), func(box pageField) bool {
			return box.Type == "checkbox" && box.Value == strconv.FormatInt(f.ID, 10) && box.Label == f.Name
		}) {
			t.Errorf("the form doesn't offer %s to show; fieldset:\n%s", f.Name, renderNode(choice))
		}
	}
	if strings.Contains(textOf(choice), legacy.Name) {
		t.Errorf("the form offers the Retired %s; fieldset:\n%s", legacy.Name, renderNode(choice))
	}

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":   {"EU MBR"},
		"mode":   {domain.ReportModePicked},
		"picked": {strconv.FormatInt(red.ID, 10), strconv.FormatInt(green.ID, 10)},
		"field":  {strconv.FormatInt(budget.ID, 10), strconv.FormatInt(notes.ID, 10)},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save report: status %d", resp.StatusCode)
	}
	draft := parsePage(t, readBody(t, resp))
	card := findByTestID(t, findByTestID(t, draft, "report-exception"), "report-fields")
	for _, want := range []string{"Budget", "120", "$", "Notes", "Counsel hired in March."} {
		if !strings.Contains(textOf(card), want) {
			t.Errorf("exception card's Fields missing %q: %q", want, textOf(card))
		}
	}
	if strings.Contains(textOf(card), "Dana") {
		t.Errorf("exception card shows the unchosen Sponsor: %q", textOf(card))
	}
	row := findByTestID(t, findByTestID(t, draft, "on-track"), "selected-goal")
	fields := textOf(findByTestID(t, row, "report-fields"))
	if !strings.Contains(fields, "Budget") || !strings.Contains(fields, "40") || !strings.Contains(fields, "$") {
		t.Errorf("On track row doesn't show Budget 40 $: %q", fields)
	}
	if strings.Contains(fields, "Notes") {
		t.Errorf("On track row shows Notes, which the Goal has no value in: %q", fields)
	}
	if reportPageMentions(draft, "160") {
		t.Errorf("the draft totals Budget across Goals; body:\n%s", renderNode(draft))
	}

	plain := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Plain MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, green.ID}})
	page := getPage(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, plain.ID))
	if hasTestID(page, "report-fields") || reportPageMentions(page, "Counsel hired in March.") {
		t.Errorf("a definition with no Fields chosen shows Fields; body:\n%s", renderNode(page))
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
	for name, page := range map[string]*nethtml.Node{
		"publication page": getPage(t, client, pubPath),
		"Print view":       getPage(t, client, pubPath+"/print"),
	} {
		card := textOf(findByTestID(t, findByTestID(t, page, "report-exception"), "report-fields"))
		if !strings.Contains(card, "Budget") || !strings.Contains(card, "120") || !strings.Contains(card, "$") {
			t.Errorf("%s's exception card doesn't show Budget 120 $ as published: %q", name, card)
		}
		row := findByTestID(t, findByTestID(t, page, "on-track"), "selected-goal")
		if fields := textOf(findByTestID(t, row, "report-fields")); !strings.Contains(fields, "Budget") || !strings.Contains(fields, "40") {
			t.Errorf("%s's On track row doesn't show Budget 40 $ as published: %q", name, fields)
		}
		if reportPageMentions(page, "8125") || reportPageMentions(page, "Spend") || reportPageMentions(page, "160") {
			t.Errorf("%s shows the Field as edited since, or a total; body:\n%s", name, renderNode(page))
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

	form := findByTestID(t, getPage(t, client, reportURL), "narrative-curation")
	if n := len(findAllByTestID(form, "curation-highlight")); n != 2 {
		t.Errorf("pick list has %d entries, want one per Highlight; form:\n%s", n, renderNode(form))
	}

	postFormStatus(t, client, reportURL+"/narrative", url.Values{
		"include-" + strconv.FormatInt(signed, 10): {"on"},
		"pick-" + strconv.FormatInt(signed, 10):    {domain.HighlightAccomplishment},
		"pick-" + strconv.FormatInt(hired, 10):     {domain.HighlightAccomplishment},
	})
	resp := postForm(t, client, reportURL+"/publications", url.Values{})
	defer func() { _ = resp.Body.Close() }()
	published := parsePage(t, readBody(t, resp))
	narrative := findByTestID(t, published, "report-narrative")
	if !reportPageMentions(narrative, "Signed the first EU customer.") || reportPageMentions(narrative, "Hired the EU lead.") {
		t.Errorf("published narrative should show only the Highlight pulled; narrative:\n%s", renderNode(narrative))
	}

	mdResp, err := ctxGet(t, client, ts.URL+resp.Request.URL.Path+"/markdown")
	if err != nil {
		t.Fatalf("GET markdown: %v", err)
	}
	defer func() { _ = mdResp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save report: status %d: %s", resp.StatusCode, body)
	}
	draft := parsePage(t, body)
	if !reportPageMentions(draft, both.Title) {
		t.Errorf("the Globex filter doesn't select %q, which carries Acme and Globex; body:\n%s", both.Title, body)
	}
	if reportPageMentions(draft, other.Title) {
		t.Errorf("the Globex filter selects %q, which carries only Initech; body:\n%s", other.Title, body)
	}
}

// The draft's History, in its narrow column, is one timeline newest first:
// the Draft, then each publication, linking to it, merged with the rest of
// the Report log, every time in the company's timezone, not UTC. Publishing
// shows once, as its publication, not again as its Report log entry.
func TestDraftHistoryMergesPublicationsWithTheReportLogOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	boss := h.SignIn("boss@example.com")
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.AddReportDelegate(boss, ada, def)
	// 03:00 UTC on 2 and 3 March is the evening before in Los Angeles.
	h.Clock.Set(time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC))
	first := h.PublishReport(boss, def)
	h.Clock.Set(time.Date(2026, 3, 2, 20, 0, 0, 0, time.UTC))
	if err := h.Service.CurateNarrative(ctx, boss.ID, def.ID, domain.CurateNarrativeInput{
		Notes: map[string][]string{domain.HighlightMiss: {"Slipped a week."}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	h.Clock.Set(time.Date(2026, 3, 3, 3, 0, 0, 0, time.UTC))
	second := h.PublishReport(ada, def)

	ts := httptest.NewServer(web.NewServer(domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))))
	t.Cleanup(ts.Close)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	if hasTestID(findByTestID(t, page, "draft-header"), "report-history") {
		t.Errorf("the draft header still has a History menu")
	}
	if hasTestID(page, "report-log") {
		t.Errorf("the draft still has a Report log apart from History")
	}
	history := findByTestID(t, findByTestID(t, page, "draft-side"), "report-history")
	if headingNamed(history, "History") == nil {
		t.Errorf("History has no History heading: %s", renderNode(history))
	}
	draft := findByTestID(t, history, "report-history-draft")
	if got := textOf(draft); got != "Draft changes since Mar 2, 2026" {
		t.Errorf("History's Draft entry reads %q, want the Draft and its baseline", got)
	}
	entries := findAllByTestID(history, "report-history-entry")
	want := []string{
		"Published Mar 2, 2026, 19:00 · Ada Okafor",
		"Narrative: Misses notes changed Mar 2, 2026, 12:00 · boss",
		"Published Mar 1, 2026, 19:00 · boss",
		"Report Delegate added: Ada Okafor Jan 2, 2026, 07:04 · boss",
		"Created Jan 2, 2026, 07:04 · boss",
	}
	if got := textsOf(entries); !slices.Equal(got, want) {
		t.Fatalf("History reads %q, want %q", got, want)
	}
	if !precedes(draft, entries[0]) {
		t.Errorf("History's Draft entry isn't first")
	}
	for i, p := range map[int]domain.Publication{0: second, 2: first} {
		if path := fmt.Sprintf("/reports/%d/publications/%d", def.ID, p.ID); linkTo(entries[i], path) == nil {
			t.Errorf("History's entry %q doesn't link to its publication %s", textOf(entries[i]), path)
		}
	}
}

// A publication made before the Report log was kept has no entry in it, and
// still shows on the draft's History, linking to it.
func TestDraftHistoryShowsAPublicationFromBeforeTheReportLogOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.Clock.Advance(time.Hour)
	pub := h.PublishBeforeTheReportLog(boss, def)
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.UpdateReportDefinition(context.Background(), boss.ID, def.ID, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID},
	}); err != nil {
		t.Fatalf("UpdateReportDefinition: %v", err)
	}
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))
	entries := findAllByTestID(findByTestID(t, page, "report-history"), "report-history-entry")
	want := []string{
		"Title: MBR → EU MBR Jan 2, 2026, 17:04 · boss",
		"Published Jan 2, 2026, 16:04 · boss",
		"Created Jan 2, 2026, 15:04 · boss",
	}
	if got := textsOf(entries); !slices.Equal(got, want) {
		t.Fatalf("History reads %q, want %q", got, want)
	}
	if path := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID); linkTo(entries[1], path) == nil {
		t.Errorf("the publication from before the Report log doesn't link to %s: %s", path, renderNode(entries[1]))
	}
}

// The draft's main column is the publication: the same Health summary and
// Report body a reader will see, read off the same components. The export
// links and the Action Items raised on a publication stay on the publication,
// since a draft has nothing to export or discuss yet.
func TestDraftMainColumnIsThePublicationOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	yellow := h.ActiveGoal(boss, "Hire a CFO", "We need one.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	checkinOverdue := h.ActiveGoal(boss, "Open Tokyo", "Expand east.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, yellow.ID, domain.HealthYellow, "Slow.", "Use a recruiter.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Introduction: "Where we stand.", Mode: domain.ReportModePicked, Picked: []int64{red.ID, yellow.ID, green.ID, checkinOverdue.ID}})

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	main := findByTestID(t, page, "report-draft")
	summary := findByTestID(t, main, "health-summary")
	for tile, want := range map[string]string{"red": "1", "yellow": "1", "green": "1", "checkin-overdue": "1"} {
		if count := textOf(findByTestID(t, summary, "health-count-"+tile)); count != want {
			t.Errorf("the main column's %s tile: %q, want a count of %s", tile, count, want)
		}
	}
	for _, want := range []string{"report-introduction", "needs-attention", "on-track"} {
		if !hasTestID(main, want) {
			t.Errorf("the main column's Report misses %s", want)
		}
	}
	for _, publicationOnly := range []string{"report-export", "publication-action-items"} {
		if hasTestID(page, publicationOnly) {
			t.Errorf("the draft carries the publication's %s", publicationOnly)
		}
	}
}

// An Org rule reads back as "Owner is in the org of <Name>" in the draft's
// meta line and on the Reports list, and a Goal listed to Also include whose
// Owner is in the org doesn't count as added (CONTEXT.md: Org).
func TestReportsReadAnOrgRuleBackOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignInNamed("vp@example.com", "Priya Raman")
	mia := h.SignInNamed("mia@example.com", "Mia Lund")
	lee := h.SignInNamed("lee@example.com", "Lee Chen")
	h.SetManager(mia, vp)
	mias := h.ActiveGoal(mia, "Cut churn", "Keep customers.")
	lees := h.ActiveGoal(lee, "Open Tokyo", "Expand east.")
	def := h.SaveReportDefinition(vp, domain.SaveReportDefinitionInput{
		Name:    "Priya's org",
		Mode:    domain.ReportModeRules,
		Rules:   []domain.ReportRule{{Attribute: domain.RuleOrg, Op: domain.RuleIs, Values: []string{strconv.FormatInt(vp.ID, 10)}}},
		Include: []int64{mias.ID, lees.ID},
	})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")
	const want = "Owner is in the org of Priya Raman · 1 added"

	meta := textOf(findByTestID(t, getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)), "report-meta"))
	if !strings.HasPrefix(meta, want+" · 2 Goals · ") {
		t.Errorf("the draft's meta line reads %q, want it to open with %s", meta, want)
	}
	if scopes := textsOf(findAllByTestID(getPage(t, client, ts.URL+"/reports"), "report-scope")); !slices.Contains(scopes, want) {
		t.Errorf("the Reports list reads its scopes %q, want the Org rule as %s", scopes, want)
	}

	h.SaveReportDefinition(vp, domain.SaveReportDefinitionInput{
		Name:  "Outside Priya's org",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: domain.RuleOrg, Op: domain.RuleIsNot, Values: []string{strconv.FormatInt(vp.ID, 10)}}},
	})
	const notWant = "Owner is not in the org of Priya Raman"
	if scopes := textsOf(findAllByTestID(getPage(t, client, ts.URL+"/reports"), "report-scope")); !slices.Contains(scopes, notWant) {
		t.Errorf("the Reports list reads its scopes %q, want the is-not Org rule as %s", scopes, notWant)
	}
}

// Readers see the Goals that entered or left a Report since its latest
// publication, each linking to the Goal, with Added or Left and why: in the
// draft's main column and on the publication. With none, neither shows the list.
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
	for name, page := range map[string]*nethtml.Node{"first publication": getPage(t, client, pubURL(first)), "unchanged draft": getPage(t, client, draftURL)} {
		if hasTestID(page, "membership-changes") {
			t.Errorf("%s with no Goal entering or leaving shows the list:\n%s", name, renderNode(findByTestID(t, page, "membership-changes")))
		}
	}

	// The definition's picked list changes; editing it arrives with #151.
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_definition_goals SET goal_id = ? WHERE report_definition_id = ? AND goal_id = ?`, picked.ID, def.ID, dropped.ID); err != nil {
		t.Fatalf("change the picked list: %v", err)
	}
	draft := getPage(t, client, draftURL)
	second := h.PublishReport(boss, def)
	for name, page := range map[string]*nethtml.Node{"draft": findByTestID(t, draft, "report-draft"), "publication": getPage(t, client, pubURL(second))} {
		list := findByTestID(t, page, "membership-changes")
		if headingNamed(list, "Goals that entered or left this report") == nil {
			t.Errorf("%s list has no heading:\n%s", name, renderNode(list))
		}
		for g, want := range map[domain.Goal][]string{picked: {"Added", "picked by hand"}, dropped: {"Left", "removed by hand"}} {
			row := membershipChangeRowOf(t, list, g)
			for _, w := range append(want, g.Title) {
				if !strings.Contains(textOf(row), w) {
					t.Errorf("%s: %s's row missing %q:\n%s", name, g.Title, w, renderNode(row))
				}
			}
			if link := linkTo(row, fmt.Sprintf("/goals/%d", g.ID)); link == nil || textOf(link) != g.Title {
				t.Errorf("%s: %s's row doesn't link to the Goal:\n%s", name, g.Title, renderNode(row))
			}
		}
		if strings.Contains(textOf(list), stays.Title) {
			t.Errorf("%s lists %s, which stayed in:\n%s", name, stays.Title, renderNode(list))
		}
	}
}

// membershipChangeRowOf is g's row in a Report's list of the Goals that
// entered or left it, failing the test when there's none.
func membershipChangeRowOf(t *testing.T, list *nethtml.Node, g domain.Goal) *nethtml.Node {
	t.Helper()
	for _, row := range findAllByTestID(list, "membership-change") {
		if attrOf(row, "id") == fmt.Sprintf("membership-%d", g.ID) {
			return row
		}
	}
	t.Fatalf("the list has no row for %q:\n%s", g.Title, renderNode(list))
	return nil
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

	form := findByTestID(t, getPage(t, client, ts.URL+path+"?baseline=2026-01-01"), "narrative-curation")
	if attrOf(form, "action") != path+"/narrative?baseline=2026-01-01" || len(findAll(form, func(n *nethtml.Node) bool { return attrOf(n, "name") == "baseline" })) != 0 {
		t.Errorf("narrative form does not carry the chosen baseline on its action alone; form:\n%s", renderNode(form))
	}
	resp := postForm(t, client, ts.URL+path+"/narrative?baseline=2026-01-01", url.Values{"text-" + domain.HighlightInsight: {"Steady."}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if want := path + "?baseline=2026-01-01"; resp.StatusCode != http.StatusOK || resp.Request.URL.RequestURI() != want {
		t.Errorf("saving the narrative: status %d at %s, want back on %s", resp.StatusCode, resp.Request.URL.RequestURI(), want)
	}

	form = findByTestID(t, getPage(t, client, ts.URL+path), "narrative-curation")
	if got := attrOf(form, "action"); got != path+"/narrative" {
		t.Errorf("the default narrative form submits to %q, want %q, with no baseline", got, path+"/narrative")
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
	chip := func(query string) *nethtml.Node {
		t.Helper()
		header := findByTestID(t, getPage(t, client, ts.URL+path+query), "draft-header")
		return findByTestID(t, header, "baseline-chip")
	}
	label := func(chip *nethtml.Node) string { return baselineChipLabel(t, chip) }
	href := func(chip *nethtml.Node, testID string) string { return attrOf(findByTestID(t, chip, testID), "href") }

	// Today is 2 Jan 2026, so 30 days ago is 3 Dec 2025.
	unpublished := chip("")
	if got := label(unpublished); got != "30 days ago · Dec 3, 2025" {
		t.Errorf("unpublished chip label %q, want %q; chip:\n%s", got, "30 days ago · Dec 3, 2025", renderNode(unpublished))
	}
	summary := findAll(unpublished, func(n *nethtml.Node) bool { return n.Data == "summary" })[0]
	if bold := findByTestID(t, summary, "baseline-label"); !strings.HasPrefix(textOf(summary), "Changes since "+textOf(bold)) || bold.Data != "strong" {
		t.Errorf("chip does not read Changes since …, the date in bold; chip:\n%s", renderNode(unpublished))
	}
	if hasTestID(unpublished, "baseline-last-publication") {
		t.Errorf("an unpublished Report's chip offers the last publication; chip:\n%s", renderNode(unpublished))
	}
	if got := href(unpublished, "baseline-30-days"); got != path+"?baseline=2025-12-03" {
		t.Errorf("30 days ago links to %q, want 2025-12-03", got)
	}
	pick := findByTestID(t, unpublished, "report-baseline")
	if attrOf(pick, "method") != "get" || formField(t, pick, "baseline").Type != "date" || buttonNamed(pick, "Apply") == nil {
		t.Errorf("Pick a date… is not a GET date form with Apply; form:\n%s", renderNode(pick))
	}

	h.Clock.Set(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC))
	h.PublishReport(boss, def)
	h.Clock.Set(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	// The session from before the clock moved has ended; sign in again.
	client = signInClient(t, ts.URL, "boss@example.com")

	if page := getPage(t, client, ts.URL+path); strings.Contains(textOf(page), "Showing what changed since") || len(findAllByTestID(page, "report-baseline")) != 1 {
		t.Errorf("the draft keeps the old baseline sentence or a baseline form outside the chip; body:\n%s", renderNode(page))
	}
	published := chip("")
	if got := label(published); got != "last publication · Sep 22, 2026" {
		t.Errorf("published chip label %q, want %q; chip:\n%s", got, "last publication · Sep 22, 2026", renderNode(published))
	}
	if got := href(published, "baseline-last-publication"); got != path {
		t.Errorf("Last publication links to %q, want the draft with no baseline", got)
	}
	if got := href(published, "baseline-30-days"); got != path+"?baseline=2026-09-04" {
		t.Errorf("30 days ago links to %q, want 2026-09-04", got)
	}

	chosen := chip("?baseline=2026-01-01")
	if got := label(chosen); got != "Jan 1, 2026" {
		t.Errorf("chosen chip label %q, want %q; chip:\n%s", got, "Jan 1, 2026", renderNode(chosen))
	}
	if !hasTestID(chosen, "baseline-last-publication") {
		t.Errorf("after a chosen date the chip no longer offers the last publication; chip:\n%s", renderNode(chosen))
	}
}

// The chip dates a publication in the company's calendar: published the evening
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
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	chip := findByTestID(t, findByTestID(t, page, "draft-header"), "baseline-chip")
	if got := baselineChipLabel(t, chip); got != "last publication · Mar 1, 2026" {
		t.Errorf("chip label %q, want %q; chip:\n%s", got, "last publication · Mar 1, 2026", renderNode(chip))
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

	form := findByTestID(t, getPage(t, client, ts.URL+path), "close-action-item")
	if got := formField(t, form, "return").Value; got != path {
		t.Errorf("close form returns to %q, want %s", got, path)
	}
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
	_ = postFormBody(t, client, ts.URL+path+"/narrative", url.Values{
		"include-" + strconv.FormatInt(halved, 10): {"on"},
		"pick-" + strconv.FormatInt(halved, 10):    {domain.HighlightAccomplishment},
	})

	page := getPage(t, client, ts.URL+path)
	publishing := findAll(page, func(n *nethtml.Node) bool {
		return strings.HasPrefix(attrOf(n, "action"), path+"/publications") || strings.HasPrefix(attrOf(n, "formaction"), path+"/publications")
	})
	if len(publishing) != 1 {
		t.Errorf("the draft has %d forms publishing, want 1; body:\n%s", len(publishing), renderNode(page))
	}
	header := findByTestID(t, page, "draft-header")
	confirm := findByTestID(t, header, "publish-confirm")
	if summary := findAll(confirm, func(n *nethtml.Node) bool { return n.Data == "summary" })[0]; !strings.Contains(textOf(summary), "Publish…") {
		t.Errorf("the disclosure does not open on Publish…; summary:\n%s", renderNode(summary))
	}
	if headingNamed(confirm, "Publish Platform MBR?") == nil {
		t.Errorf("the confirmation does not name the Report; disclosure:\n%s", renderNode(confirm))
	}
	const want = "4 Goals · 2 exceptions · 1 Highlight · changes since last publication · Sep 22, 2026. Readers can comment on the frozen copy."
	if got := textOf(findByTestID(t, confirm, "publish-summary")); got != want {
		t.Errorf("publish summary %q, want %q", got, want)
	}
	form := findByTestID(t, confirm, "report-publish")
	if attrOf(form, "method") != "post" || attrOf(form, "action") != path+"/publications" || buttonNamed(form, "Publish") == nil {
		t.Errorf("the default publish form is not a POST to the publications with no baseline; form:\n%s", renderNode(form))
	}
	if cancel := findByTestID(t, confirm, "publish-cancel"); attrOf(cancel, "href") != path || textOf(cancel) != "Cancel" {
		t.Errorf("Cancel does not reload the draft; link:\n%s", renderNode(cancel))
	}

	confirm = findByTestID(t, getPage(t, client, ts.URL+path+"?baseline=2026-01-01"), "publish-confirm")
	if got := textOf(findByTestID(t, confirm, "publish-summary")); !strings.Contains(got, "· changes since Jan 1, 2026.") {
		t.Errorf("publish summary %q does not read against the chosen date", got)
	}
	if got := attrOf(findByTestID(t, confirm, "report-publish"), "action"); got != path+"/publications?baseline=2026-01-01" {
		t.Errorf("the publish form submits to %q, want it to carry the chosen baseline", got)
	}
	if got := attrOf(findByTestID(t, confirm, "publish-cancel"), "href"); got != path+"?baseline=2026-01-01" {
		t.Errorf("Cancel links to %q, want it to keep the chosen baseline", got)
	}
	resp := postForm(t, client, ts.URL+path+"/publications?baseline=2026-01-01", url.Values{})
	defer func() { _ = resp.Body.Close() }()
	if line := textOf(findByTestID(t, parsePage(t, readBody(t, resp)), "report-published")); !strings.Contains(line, "changes since Jan 1, 2026") {
		t.Errorf("publication does not read against the chosen baseline; line:\n%s", line)
	}
}

// The live preview is gone: the composer is in the main column, so there is
// no separate preview to refresh, and POST /reports/{id}/narrative/preview
// isn't routed, for a manager or anyone else (#473).
func TestNarrativePreviewIsNoLongerRoutedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.SignIn("stranger@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	for _, who := range []string{"boss@example.com", "stranger@example.com"} {
		resp, body := postHX(t, signInClient(t, ts.URL, who), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/narrative/preview", url.Values{
			"text-" + domain.HighlightMiss: {"Slipped a week."},
		})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s's preview: status %d, want it unrouted; body:\n%s", who, resp.StatusCode, body)
		}
	}
	if draft, err := h.Service.DraftReport(context.Background(), def, time.Time{}); err != nil || len(draft.Narrative) != 0 {
		t.Errorf("narrative after a post to the old preview %+v (err %v), want nothing saved", draft.Narrative, err)
	}
}

// On a later baseline, the compose form doesn't list a saved pick from before
// it: the form as it stands reads as Saved, not Unsaved changes, and saving
// it keeps the pick (ticket #455).
func TestNarrativeAtALaterBaselineKeepsEarlierPicksOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	signed := draftHighlightID(t, h, def, "Signed the first EU customer.")
	if err := h.Service.CurateNarrative(ctx, boss.ID, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: signed, Section: domain.HighlightAccomplishment}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	h.Clock.Set(time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC))
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightMiss, "Lost the second customer.")
	h.Clock.Set(time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC))

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	form := findByTestID(t, getPage(t, client, reportURL+"?baseline=2026-01-10"), "narrative-curation")
	if got := textsOf(findAllByTestID(form, "curation-highlight")); len(got) != 1 || !strings.Contains(got[0], "Lost the second customer.") {
		t.Fatalf("the form at the baseline lists %q, want only the lost customer", got)
	}
	again := formSubmission(form)
	again.Set("add_note", domain.HighlightMiss)
	resp, body := postHX(t, client, ts.URL+attrOf(form, "action"), again)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add another: status %d, want 200; body:\n%s", resp.StatusCode, body)
	}
	if got := textOf(findByTestID(t, parsePage(t, body), "save-result")); got != "Saved" {
		t.Errorf("adding a box to the unchanged form at the baseline says %q, want Saved", got)
	}

	resp, body = postHX(t, client, ts.URL+attrOf(form, "action"), formSubmission(form))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save: status %d, want 200; body:\n%s", resp.StatusCode, body)
	}
	h.SignIn("sam@example.com")
	narrative := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), reportURL), "report-narrative")
	if !strings.Contains(textOf(narrative), "Signed the first EU customer.") {
		t.Errorf("saving at the baseline dropped the earlier pick; narrative: %s", textOf(narrative))
	}
}

// Save narrative saves the compose form: over htmx it answers the Narrative
// section, its compose form showing the narrative as saved with its Highlights
// count, and, out of band, the save status saying Saved, Publish…'s summary
// and the History with the save's one entry — nothing else of the page.
// Saving again unchanged logs nothing. A plain post still reloads the draft
// (tickets #156, #166, #444, #473, #474).
func TestSaveNarrativeLogsOnceAndRefreshesTheHistoryOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	signed := strconv.FormatInt(draftHighlightID(t, h, def, "Signed the first EU customer."), 10)
	logged, err := h.Service.ReportLog(ctx, def.ID)
	if err != nil {
		t.Fatalf("ReportLog: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	form := url.Values{
		"include-" + signed:                      {"on"},
		"pick-" + signed:                         {domain.HighlightAccomplishment},
		"text-" + domain.HighlightAccomplishment: {"EU is open for business."},
	}
	resp, body := postHX(t, client, reportURL+"/narrative", form)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save: status %d, want 200; body:\n%s", resp.StatusCode, body)
	}
	answer := parsePage(t, body)
	for _, untouched := range []string{"draft-header", "report-draft", "health-summary", "needs-attention"} {
		if hasTestID(answer, untouched) {
			t.Errorf("the save answers %s, which it didn't change; body:\n%s", untouched, body)
		}
	}
	section := findByTestID(t, answer, "report-curation")
	if hasAttr(section, "hx-swap-oob") {
		t.Errorf("the Narrative section is swapped out of band, want it in place of the target")
	}
	saved := findByTestID(t, section, "narrative-curation")
	var notes []string
	for _, f := range formFields(saved, "text-"+domain.HighlightAccomplishment) {
		notes = append(notes, f.Value)
	}
	if !slices.Equal(notes, []string{"EU is open for business.", ""}) {
		t.Errorf("the saved form's Accomplishments boxes hold %q, want the saved note and an empty box", notes)
	}
	if box := formField(t, saved, "include-"+signed); !box.Checked {
		t.Errorf("the saved form leaves the saved Highlight unticked")
	}
	for _, oob := range []string{"save-status", "publish-summary", "report-history"} {
		if attrOf(findByTestID(t, answer, oob), "hx-swap-oob") != "true" {
			t.Errorf("%s is not swapped out of band; body:\n%s", oob, body)
		}
	}
	status := findByTestID(t, answer, "save-status")
	if got := textOf(findByTestID(t, status, "save-result")); got != "Saved" || hasAttr(status, "data-unsaved") {
		t.Errorf("the save status says %q (unsaved %v), want Saved", got, hasAttr(status, "data-unsaved"))
	}
	if got := textOf(findByTestID(t, section, "curation-count")); got != "1 of 1 in" {
		t.Errorf("the Highlights count %q, want 1 of 1 in", got)
	}
	if got := textOf(findByTestID(t, answer, "publish-summary")); !strings.Contains(got, "· 1 Highlight · changes since 30 days ago · ") {
		t.Errorf("Publish…'s summary %q, want the saved Highlight against the default baseline", got)
	}
	history := findByTestID(t, answer, "report-history")
	if !hasTestID(history, "report-history-draft") {
		t.Errorf("the History answered has no Draft entry: %s", renderNode(history))
	}
	entries := textsOf(findAllByTestID(history, "report-history-entry"))
	if len(entries) != len(logged)+1 || !strings.Contains(entries[0], "Narrative: picked “Signed the first EU customer.” into Accomplishments; Accomplishments notes changed") {
		t.Errorf("the History answered reads %q, want the save's one entry newest above the %d before", entries, len(logged))
	}

	resp, body = postHX(t, client, reportURL+"/narrative", form)
	entries = textsOf(findAllByTestID(findByTestID(t, parsePage(t, body), "report-history"), "report-history-entry"))
	if after, err := h.Service.ReportLog(ctx, def.ID); err != nil || len(after) != len(logged)+1 || len(entries) != len(logged)+1 {
		t.Errorf("saving again unchanged leaves %d entries, answering %d (err %v), want the %d after the first save", len(after), len(entries), err, len(logged)+1)
	}

	plain := postForm(t, client, reportURL+"/narrative", form)
	defer func() { _ = plain.Body.Close() }()
	if plain.StatusCode != http.StatusOK || plain.Request.URL.Path != "/reports/"+strconv.FormatInt(def.ID, 10) {
		t.Errorf("plain post: status %d at %s, want a redirect to the draft", plain.StatusCode, plain.Request.URL)
	}
}

// The compose form posts nothing as the author types: nothing in it saves or
// previews on input or change. Save narrative, shown to everyone, saves it,
// its own post the plain submit without script, swapping in the Narrative
// section; the save status says Saving… meanwhile, and Not saved when an
// answer is an error or never comes (tickets #156, #444, #473).
func TestComposeFormSavesOnlyOnSaveNarrativeOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"?baseline=2025-12-01")
	form := findByTestID(t, page, "narrative-curation")
	save := "/reports/" + strconv.FormatInt(def.ID, 10) + "/narrative?baseline=2025-12-01"
	if got := attrOf(form, "action"); got != save {
		t.Fatalf("the compose form submits to %q, want %q", got, save)
	}
	if hasAttr(form, "hx-post") {
		t.Errorf("the compose form posts to %q as the author types", attrOf(form, "hx-post"))
	}
	for _, n := range findAll(form, func(n *nethtml.Node) bool { return hasAttr(n, "hx-trigger") }) {
		if trigger := attrOf(n, "hx-trigger"); strings.Contains(trigger, "input") || strings.Contains(trigger, "change") {
			t.Errorf("<%s> posts the narrative on %q", n.Data, trigger)
		}
	}
	for _, event := range []string{"hx-on:htmx:response-error", "hx-on:htmx:send-error"} {
		if handler := attrOf(form, event); !strings.Contains(handler, "save-result") || !strings.Contains(handler, "Not saved") {
			t.Errorf("compose form %s = %q, want it to put Not saved in the save status", event, handler)
		}
	}

	button := buttonNamed(form, "Save narrative")
	if button == nil {
		t.Fatalf("the compose form has no Save narrative button for readers with script")
	}
	for name, want := range map[string]string{
		"type":         "submit",
		"hx-post":      save,
		"hx-target":    "#report-curation",
		"hx-swap":      "outerHTML",
		"hx-sync":      "closest form:replace",
		"hx-indicator": "#save-status",
	} {
		if got := attrOf(button, name); got != want {
			t.Errorf("Save narrative %s = %q, want %q", name, got, want)
		}
	}
	if !strings.Contains(textOf(findByTestID(t, page, "save-status")), "Saving…") {
		t.Errorf("the save status has no Saving… indicator")
	}
}

// The compose form asks the browser not to restore what was typed into it
// after a reload, so a reloaded draft shows the narrative as saved, as its
// save status and Publish… do (ticket #455).
func TestComposeFormShowsTheSavedNarrativeAfterAReloadOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))
	if got := attrOf(findByTestID(t, page, "narrative-curation"), "autocomplete"); got != "off" {
		t.Errorf("the compose form's autocomplete = %q, want off", got)
	}
}

// postHX posts a form with the HX-Request header set, as htmx does, and
// returns the response, its headers intact, and its body.
func postHX(t *testing.T, client *http.Client, rawURL string, form url.Values) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rawURL, strings.NewReader(form.Encode()))
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
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != draftPath {
		t.Fatalf("edit: status %d to %q, want 303 to %s", resp.StatusCode, resp.Header.Get("Location"), draftPath)
	}
	draft := getBody(t, client, ts.URL+draftPath)
	if name := findByTestID(t, parsePage(t, draft), "report-name"); name.Data != "h1" || !strings.Contains(textOf(name), "Web MBR") {
		t.Errorf("the draft is named %s, want Web MBR", renderNode(name))
	}
	if got, want := draftGoalTitles(t, draft), []string{redesign.Title}; !slices.Equal(got, want) {
		t.Errorf("the edited draft selects %q, want %q", got, want)
	}

	published := getPage(t, client, ts.URL+draftPath+"/publications/"+strconv.FormatInt(pub.ID, 10))
	if name := findByTestID(t, published, "report-name"); name.Data != "h1" || !strings.Contains(textOf(name), "Platform MBR") {
		t.Errorf("the publication is named %s, want the old Platform MBR", renderNode(name))
	}
	holds := func(g domain.Goal) bool {
		return len(findAll(published, func(n *nethtml.Node) bool { return attrOf(n, "id") == fmt.Sprintf("goal-%d", g.ID) })) > 0
	}
	if !holds(reliability) || holds(redesign) {
		t.Errorf("the publication doesn't keep its old Goals; body:\n%s", renderNode(published))
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

	page := getPage(t, client, ts.URL+"/reports/"+id(rules.ID)+"/edit")
	builder := findByTestID(t, page, "report-builder")
	if got := attrOf(builder, "action"); got != "/reports/"+id(rules.ID)+"/edit" {
		t.Errorf("the edit page posts to %q, want /reports/%s/edit", got, id(rules.ID))
	}
	if f := formField(t, builder, "id"); f.Type != "hidden" || f.Value != id(rules.ID) {
		t.Errorf("the edit page carries id %q (%s), want %s hidden", f.Value, f.Type, id(rules.ID))
	}
	if got := formField(t, builder, "name").Value; got != "Platform MBR" {
		t.Errorf("the edit page's name is %q, want Platform MBR", got)
	}
	if f := formField(t, builder, "introduction"); f.Type != "textarea" || f.Value != "Where Platform stands." {
		t.Errorf("the edit page's introduction is %q, want Where Platform stands.", f.Value)
	}
	for name, value := range map[string]string{"mode": "rules", "field": id(budget.ID)} {
		if !reportBuilderTicks(builder, name, value) {
			t.Errorf("the edit page doesn't tick %s=%s", name, value)
		}
	}
	for value, label := range map[string]string{
		dim:                                   "Team",
		dim + "=" + id(platform.ID):           "Platform",
		"lifecycle":                           "Lifecycle",
		"is not":                              "is not",
		"lifecycle=" + domain.LifecycleOnHold: "",
	} {
		if opt := reportBuilderSelected(builder, value); opt == nil || label != "" && textOf(opt) != label {
			t.Errorf("the edit page doesn't choose %s (%s)", value, label)
		}
	}
	for name, want := range map[string]string{"include": id(sdk.ID), "exclude": id(sso.ID)} {
		if got := reportBuilderHeld(builder, name); !slices.Contains(got, want) {
			t.Errorf("the edit page's %s holds %q, want %s", name, got, want)
		}
	}
	if !strings.Contains(textOf(page), "Edit report") {
		t.Errorf("the edit page isn't titled Edit report; body:\n%s", renderNode(page))
	}

	builder = findByTestID(t, getPage(t, client, ts.URL+"/reports/"+id(picked.ID)+"/edit"), "report-builder")
	if !reportBuilderTicks(builder, "mode", "picked") {
		t.Errorf("the picked edit page doesn't tick mode=picked:\n%s", renderNode(builder))
	}
	if got := reportBuilderHeld(builder, "picked"); !slices.Contains(got, id(sso.ID)) {
		t.Errorf("the picked edit page holds %q, want %s", got, id(sso.ID))
	}
}

// reportBuilderTicks reports whether the builder's checkbox or radio named
// name with this value is ticked.
func reportBuilderTicks(builder *nethtml.Node, name, value string) bool {
	return slices.ContainsFunc(formFields(builder, name), func(f pageField) bool { return f.Value == value && f.Checked })
}

// reportBuilderSelected is the option under root with this value that is
// chosen, or nil.
func reportBuilderSelected(root *nethtml.Node, value string) *nethtml.Node {
	if opts := findAll(root, func(n *nethtml.Node) bool {
		return n.Data == "option" && attrOf(n, "value") == value && hasAttr(n, "selected")
	}); len(opts) > 0 {
		return opts[0]
	}
	return nil
}

// reportBuilderHeld is the value of each hidden field named name under the
// builder, the Goals a picker's chips hold, in order.
func reportBuilderHeld(builder *nethtml.Node, name string) []string {
	var out []string
	for _, f := range formFields(builder, name) {
		if f.Type == "hidden" {
			out = append(out, f.Value)
		}
	}
	return out
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
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("refused edit: status %d, want 422; body:\n%s", resp.StatusCode, page)
	}
	editPath := "/reports/" + strconv.FormatInt(def.ID, 10) + "/edit"
	builder := findByTestID(t, parsePage(t, page), "report-builder")
	if got := attrOf(builder, "action"); got != editPath {
		t.Errorf("the refused edit posts to %q, want %s", got, editPath)
	}
	if got := formField(t, builder, "introduction").Value; got != "Now by rules." {
		t.Errorf("the refused edit's introduction is %q, want Now by rules.", got)
	}
	if !reportBuilderTicks(builder, "mode", "rules") {
		t.Errorf("the refused edit lost mode=rules")
	}
	if opt := reportBuilderSelected(builder, "lifecycle"); opt == nil || textOf(opt) != "Lifecycle" {
		t.Errorf("the refused edit lost its Lifecycle rule")
	}
	if label := reportBuilderLabelOf(formField(t, builder, "name").Node); label == nil || !reportBuilderHasID(label, "name-error") {
		t.Errorf("the blank name's error isn't beside it:\n%s", renderNode(builder))
	}
	if rows := findAllByTestID(builder, "report-rule"); len(rows) == 0 || !reportBuilderHasID(rows[0], "rules[0]-error") {
		t.Errorf("the Lifecycle rule with no value isn't refused beside it:\n%s", renderNode(builder))
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
	builder = findByTestID(t, parsePage(t, page), "report-builder")
	if attrOf(builder, "action") != editPath || len(findAllByTestID(builder, "report-rule")) != 2 {
		t.Errorf("Add rule didn't come back to the edit page with two rule rows:\n%s", renderNode(builder))
	}
	if got, _ := h.Service.GetReportDefinition(context.Background(), def.ID); got.Name != "MBR" {
		t.Errorf("Add rule saved the edit: name %q", got.Name)
	}
}

// reportBuilderLabelOf is the label around n, or nil.
func reportBuilderLabelOf(n *nethtml.Node) *nethtml.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == nethtml.ElementNode && p.Data == "label" {
			return p
		}
	}
	return nil
}

// reportBuilderHasID reports whether an element under root, root included,
// carries id.
func reportBuilderHasID(root *nethtml.Node, id string) bool {
	return len(findAll(root, func(n *nethtml.Node) bool { return attrOf(n, "id") == id })) > 0
}

// Only the definition's creator, a Report Delegate or an Admin may edit it:
// anyone else signed in gets the refusal page, 403, saying who may, with a way
// Back to its draft, on both the page and the post, which changes nothing. An unknown definition is Not found (tickets #151, #434).
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
	refusedSays := "Only its creator, a Report Delegate or an Admin may edit Alice's MBR."
	draft := "/reports/" + strconv.FormatInt(def.ID, 10)
	resp, err := ctxGet(t, sam, editURL)
	if err != nil {
		t.Fatalf("GET %s: %v", editURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Sam's GET of Alice's edit page: status %d, want 403", resp.StatusCode)
	}
	assertRefusalPage(t, parsePage(t, readBody(t, resp)), "Can't edit Report", refusedSays, draft)
	resp = postForm(t, noRedirects(sam), editURL, edit("Sam's now"))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Sam's POST to Alice's edit: status %d, want 403", resp.StatusCode)
	}
	assertRefusalPage(t, parsePage(t, readBody(t, resp)), "Can't edit Report", refusedSays, draft)
	if got, _ := h.Service.GetReportDefinition(context.Background(), def.ID); got.Name != "Alice's MBR" {
		t.Errorf("Sam's refused edit renamed it %q", got.Name)
	}

	for _, who := range []string{"alice@example.com", "boss@example.com"} {
		client := signInClient(t, ts.URL, who)
		getBody(t, client, editURL)
		resp := postForm(t, noRedirects(client), editURL, edit("Edited by "+who))
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusSeeOther {
			t.Errorf("%s's edit: status %d, want 303; body:\n%s", who, resp.StatusCode, body)
		}
	}

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, err := http.NewRequestWithContext(t.Context(), method, ts.URL+"/reports/9999/edit", strings.NewReader(edit("Nobody's").Encode()))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := sam.Do(req)
		if err != nil {
			t.Fatalf("%s an unknown definition's edit: %v", method, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s an unknown definition's edit: status %d, want 404; body:\n%s", method, resp.StatusCode, body)
		}
	}
}

// Edit definition links to the edit page from the draft's header, and only
// there, for the definition's creator and an Admin; anyone else who isn't a
// Report Delegate doesn't see it (tickets #151, #473).
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

	for _, who := range []string{"alice@example.com", "boss@example.com"} {
		draft := getPage(t, signInClient(t, ts.URL, who), draftURL)
		links := findAllByTestID(draft, "edit-definition")
		if len(links) != 1 || !within(links[0], findByTestID(t, draft, "draft-header")) || attrOf(links[0], "href") != "/reports/"+strconv.FormatInt(def.ID, 10)+"/edit" {
			t.Errorf("%s's draft has %d Edit definition, want one in the header linking to the edit page", who, len(links))
		}
	}
	if draft := getPage(t, signInClient(t, ts.URL, "sam@example.com"), draftURL); hasTestID(draft, "edit-definition") {
		t.Errorf("Sam, who may not edit, is offered Edit definition")
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

	builder := findByTestID(t, getPage(t, client, ts.URL+"/reports/"+id(def.ID)+"/edit"), "report-builder")
	rows := findAllByTestID(builder, "report-rule")
	if len(rows) != 2 {
		t.Fatalf("the edit page shows %d rule rows, want 2:\n%s", len(rows), renderNode(builder))
	}
	for value, label := range map[string]string{pillarKey: "Pillar", pillarKey + "=" + id(growth.ID): "Growth"} {
		if opt := reportBuilderSelected(rows[0], value); opt == nil || textOf(opt) != label {
			t.Errorf("the rule on the Retired Pillar doesn't choose %s (%s):\n%s", value, label, renderNode(rows[0]))
		}
	}
	if opt := reportBuilderSelected(rows[1], teamKey+"="+id(core.ID)); opt == nil || textOf(opt) != "Core" {
		t.Errorf("the rule on the Retired Core doesn't choose it:\n%s", renderNode(rows[1]))
	}
	offers := func(row *nethtml.Node, value string) bool {
		return len(findAll(row, func(n *nethtml.Node) bool { return attrOf(n, "value") == value })) > 0
	}
	if offers(rows[1], pillarKey) {
		t.Errorf("the Retired Pillar is offered to a rule that doesn't test it:\n%s", renderNode(rows[1]))
	}
	if offers(rows[0], teamKey+"="+id(core.ID)) {
		t.Errorf("the Retired Core is offered to a rule that doesn't test it:\n%s", renderNode(rows[0]))
	}
}

// A definition showing a Field that is then Retired keeps it on edit: the
// edit page lists it, ticked, and saving the page unchanged keeps showing it.
// A refused save still lists it as ticked. A different Retired Field is
// neither offered nor accepted (ticket #164).
func TestEditDefinitionKeepsARetiredFieldOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	legacy := h.CreateField(boss, "Legacy code", domain.FieldShortText, "")
	goal := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID}, FieldIDs: []int64{legacy.ID},
	})
	sunset := h.CreateField(boss, "Sunset code", domain.FieldShortText, "")
	ctx := context.Background()
	for _, f := range []domain.Field{legacy, sunset} {
		if err := h.Service.RetireField(ctx, boss.ID, f.ID); err != nil {
			t.Fatalf("RetireField %s: %v", f.Name, err)
		}
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	editURL := ts.URL + "/reports/" + id(def.ID) + "/edit"
	choice := findByTestID(t, getPage(t, client, editURL), "report-field-choice")
	if !strings.Contains(textOf(choice), legacy.Name) || !reportBuilderTicks(choice, "field", id(legacy.ID)) {
		t.Errorf("the edit page doesn't list the Retired %s, ticked; fieldset:\n%s", legacy.Name, renderNode(choice))
	}
	if !slices.ContainsFunc(formFields(choice, "field"), func(f pageField) bool { return f.Value == id(budget.ID) }) {
		t.Errorf("the edit page doesn't offer %s; fieldset:\n%s", budget.Name, renderNode(choice))
	}
	if strings.Contains(textOf(choice), sunset.Name) {
		t.Errorf("the edit page offers the Retired %s, which the definition doesn't show; fieldset:\n%s", sunset.Name, renderNode(choice))
	}

	form := url.Values{
		"name":   {""},
		"mode":   {domain.ReportModePicked},
		"picked": {id(goal.ID)},
		"field":  {id(legacy.ID)},
	}
	resp := postForm(t, noRedirects(client), editURL, form)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("save with no name: status %d, want 422", resp.StatusCode)
	}
	choice = findByTestID(t, parsePage(t, readBody(t, resp)), "report-field-choice")
	if !reportBuilderTicks(choice, "field", id(legacy.ID)) {
		t.Errorf("the refused save doesn't list the Retired %s, ticked; fieldset:\n%s", legacy.Name, renderNode(choice))
	}

	form.Set("name", "MBR renamed")
	form["field"] = []string{id(legacy.ID), id(sunset.ID)}
	resp = postForm(t, noRedirects(client), editURL, form)
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(textOf(parsePage(t, body)), "Sunset code is retired") {
		t.Errorf("adding the Retired %s: status %d, want 422 naming it", sunset.Name, resp.StatusCode)
	}

	form["field"] = []string{id(legacy.ID)}
	resp = postForm(t, noRedirects(client), editURL, form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save unchanged: status %d, want 303; body:\n%s", resp.StatusCode, readBody(t, resp))
	}
	_ = resp.Body.Close()
	got, err := h.Service.GetReportDefinition(ctx, def.ID)
	if err != nil {
		t.Fatalf("GetReportDefinition: %v", err)
	}
	if !slices.Equal(got.FieldIDs, []int64{legacy.ID}) {
		t.Errorf("saved FieldIDs = %v, want [%d]", got.FieldIDs, legacy.ID)
	}
}

// Since the last publication, a Goal whose Health changed reads its Health with
// what it was — worsening or recovering, which keeps the full block — and
// counts as a change on the Reports list. One that stayed Red, or was created
// since and had no Health then, shows no earlier Health (CONTEXT.md: Report).
func TestReportMarksAChangeOfHealthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	// Every Path to Green keeps one back-to-Green date: moving it later would
	// be a Date Slip, a change of its own.
	backToGreenBy := h.Clock.Now().AddDate(0, 1, 0)
	checkin := func(g domain.Goal, health string) {
		h.Checkin(boss, g.ID, health, "Status.", "Path.", backToGreenBy)
	}
	worsened := h.ActiveGoal(boss, "Worsened", "why")
	recovered := h.ActiveGoal(boss, "Recovered", "why")
	staysRed := h.ActiveGoal(boss, "Stays Red", "why")
	checkin(worsened, domain.HealthGreen)
	checkin(recovered, domain.HealthYellow)
	checkin(staysRed, domain.HealthRed)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:  "MBR",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{strconv.FormatInt(boss.ID, 10)}}},
	})
	h.PublishReport(boss, def)
	h.Clock.Advance(24 * time.Hour)
	checkin(worsened, domain.HealthYellow)
	checkin(recovered, domain.HealthGreen)
	checkin(staysRed, domain.HealthRed)
	created := h.ActiveGoal(boss, "Created", "why")
	checkin(created, domain.HealthYellow)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	page := getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	for _, tc := range []struct {
		g     domain.Goal
		reads string
	}{
		{worsened, "Yellow · 1 wk · was Green"},
		{recovered, "Green · was Yellow"},
		{staysRed, "Red · 1 wk"},
		{created, "Yellow · 1 wk"},
	} {
		block := reportBlockOf(t, page, tc.g)
		health := findByTestID(t, block, "report-health")
		// The Health, its Run off Green and its earlier Health are one group,
		// so a marker can't wrap onto a line of its own.
		if got := textOf(health.Parent); got != tc.reads {
			t.Errorf("%s's Health reads %q, want %q", tc.g.Title, got, tc.reads)
		}
		for testID, marked := range map[string]bool{"report-prior-health": strings.Contains(tc.reads, "was"), "report-run-off-green": strings.Contains(tc.reads, "wk")} {
			got := findAllByTestID(block, testID)
			if (len(got) == 1) != marked || len(got) == 1 && got[0].Parent != health.Parent {
				t.Errorf("%s groups its %s with its Health %t, want %t; block:\n%s", tc.g.Title, testID, len(got) == 1, marked, renderNode(block))
			}
		}
	}

	row := findByTestID(t, getPage(t, client, ts.URL+"/reports"), "report-row")
	if want := "3 changes"; !strings.Contains(textOf(row), want) {
		t.Errorf("row lacks %q, counting the worsened, the recovered, and the created Goal; row:\n%s", want, renderNode(row))
	}
}

// A Red exception block shows its Run off Green beside its Health, before the
// Health it was at the baseline ("Red · 2 wks · was Green"), on the draft, the
// publication and its print page; a publication from before Reports showed it
// has none (CONTEXT.md: Run off Green).
func TestReportBlockShowsTheRunOffGreenOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	// Red on Mondays 16 and 23 February, 30-day baseline after the Green.
	// Each moves the back-to-Green date a week later, with the reason that
	// needs.
	for _, d := range []int{16, 23} {
		h.Clock.Set(time.Date(2026, time.February, d, 9, 0, 0, 0, time.UTC))
		h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Blocked.", "Unblock it.", h.Clock.Now().AddDate(0, 1, 0), "Unblocking takes longer.")
	}
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	old := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[{"Goal":{"ID":1,"Title":"Launch in EU","WhyItMatters":"Expand the market.",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"},"Lifecycle":"Active"},"Health":"Red","Status":"Blocked."}]}`
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, old.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}
	h.Clock.Advance(14 * 24 * time.Hour)
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Still blocked.", "Unblock it.", h.Clock.Now().AddDate(0, 1, 0), "Unblocking takes longer still.")

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
	for name, tc := range map[string]struct {
		url, reads string
	}{
		"draft":       {fmt.Sprintf("%s/reports/%d?baseline=2026-01-30", ts.URL, def.ID), "· 4 wks"},
		"publication": {pubURL, "· 2 wks"},
		"print page":  {pubURL + "/print", "· 2 wks"},
	} {
		block := reportBlockOf(t, getPage(t, client, tc.url), g)
		runs := findAllByTestID(block, "report-run-off-green")
		if got := textsOf(runs); !slices.Equal(got, []string{tc.reads}) {
			t.Errorf("%s's Run off Green reads %q, want %q", name, got, tc.reads)
		}
		if priors := findAllByTestID(block, "report-prior-health"); len(runs) == 0 || len(priors) == 0 || !precedes(runs[0], priors[0]) {
			t.Errorf("%s does not show the Run off Green before the earlier Health; block:\n%s", name, renderNode(block))
		}
	}
	block := reportBlockOf(t, getPage(t, client, fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, old.ID)), g)
	if !strings.Contains(textOf(block), "Blocked.") || hasTestID(block, "report-run-off-green") {
		t.Errorf("a publication from before the Run off Green does not render as published; block:\n%s", renderNode(block))
	}
}

// A publication freezes a Goal's earlier Health with the rest of its block, and
// one published before Reports marked a change of Health still renders, with
// no earlier Health to show.
func TestPublicationFreezesTheEarlierHealthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	before := h.PublishReport(boss, def)
	h.Clock.Advance(24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthYellow, "Vendor is late.", "Chase the vendor.", h.Clock.Now().AddDate(0, 1, 0))
	marked := h.PublishReport(boss, def)
	h.Clock.Advance(24 * time.Hour)
	// A month out from a day later moves the back-to-Green date later.
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Vendor is gone.", "Find another.", h.Clock.Now().AddDate(0, 1, 0), "Another vendor takes longer.")

	old := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[{"Goal":{"ID":1,"Title":"Launch in EU","WhyItMatters":"Expand the market.",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"},"Lifecycle":"Active"},"Health":"Yellow","Status":"Vendor is late."}]}`
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_publications SET snapshot = ? WHERE id = ?`, old, before.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	publication := func(p domain.Publication) *nethtml.Node {
		return getPage(t, client, fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, p.ID))
	}
	if got := textsOf(findAllByTestID(reportBlockOf(t, publication(marked), g), "report-prior-health")); !slices.Equal(got, []string{"· was Green"}) {
		t.Errorf("the publication's earlier Health reads %q, want the Green it was when published", got)
	}
	block := reportBlockOf(t, publication(before), g)
	if !strings.Contains(textOf(block), "Vendor is late.") || hasTestID(block, "report-prior-health") {
		t.Errorf("the older publication does not render its block as published; block:\n%s", renderNode(block))
	}
}

// A publication's byline, its Print view's title and the Reports list date it
// on the org's clock with its year, though it's the current year, and never
// say UTC: 15:04 UTC on Jan 2 is 00:04 on Jan 3 in Tokyo (ticket #276).
func TestPublicationIsDatedWithItsYearInTheOrgsTimezoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def) // Jan 2, 2026, 15:04 UTC

	ts := httptest.NewServer(web.NewServer(domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(tokyo))))
	t.Cleanup(ts.Close)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubPath := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)

	if line := textOf(findByTestID(t, getPage(t, client, pubPath), "report-published")); !strings.Contains(line, "Published Jan 3, 2026, 00:04 by ") || strings.Contains(line, "UTC") {
		t.Errorf("the byline doesn't read Published Jan 3, 2026, 00:04, with no UTC: %q", line)
	}
	titles := findAll(getPage(t, client, pubPath+"/print"), func(n *nethtml.Node) bool { return n.Data == "title" })
	if title := textsOf(titles); len(title) != 1 || !strings.Contains(title[0], "Jan 3, 2026, 00:04") || strings.Contains(title[0], "UTC") {
		t.Errorf("the Print view's title is %q, want it dated Jan 3, 2026, 00:04", title)
	}
	row := findByTestID(t, getPage(t, client, ts.URL+"/reports"), "report-row")
	if published := findByTestID(t, row, "report-published"); textOf(published) != "Jan 3, 2026" || strings.Contains(textOf(row), "UTC") {
		t.Errorf("the Reports list doesn't date the publication Jan 3, 2026:\n%s", renderNode(row))
	}
}

// A Report opens with a line counting what moved since its baseline, under its
// Health summary on the draft and the publication and under the byline on Print;
// each exception block opens with what changed on it, and the exceptions come
// most urgent first on every page (CONTEXT.md: Report).
func TestReportSaysWhatMovedSinceTheBaselineOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	// Every Path to Green keeps one back-to-Green date: moving it later would
	// be a Date Slip, a change of its own.
	backToGreenBy := h.Clock.Now().AddDate(0, 1, 0)
	checkin := func(g domain.Goal, health string) {
		if health == domain.HealthGreen {
			h.Checkin(boss, g.ID, health, "On track.", "", time.Time{})
			return
		}
		h.Checkin(boss, g.ID, health, "Status.", "Path.", backToGreenBy)
	}
	recovered := h.ActiveGoal(boss, "Recovered", "why")
	staysRed := h.ActiveGoal(boss, "Stays Red", "why")
	turnedRed := h.ActiveGoal(boss, "Turned Red", "why")
	checkin(recovered, domain.HealthYellow)
	checkin(staysRed, domain.HealthRed)
	checkin(turnedRed, domain.HealthGreen)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:  "MBR",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{strconv.FormatInt(boss.ID, 10)}}},
	})
	h.PublishReport(boss, def)
	h.Clock.Advance(24 * time.Hour)
	checkin(recovered, domain.HealthGreen)
	checkin(staysRed, domain.HealthRed)
	checkin(turnedRed, domain.HealthRed)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := fmt.Sprintf("%s/reports/%d", ts.URL, def.ID)
	mostUrgentFirst := []string{"Turned Red", "Stays Red", "Recovered"}
	exceptions := func(page *nethtml.Node) []string {
		var out []string
		for _, block := range findAllByTestID(page, "report-exception") {
			out = append(out, textOf(findAll(block, func(n *nethtml.Node) bool { return n.Data == "h3" })[0]))
		}
		return out
	}
	movementOf := func(page *nethtml.Node) []string { return textsOf(findAllByTestID(page, "report-movement")) }
	// comesUnder reports whether the page's first element carrying under
	// opens after its first carrying over.
	comesUnder := func(page *nethtml.Node, under, over string) bool {
		u, o := findAllByTestID(page, under), findAllByTestID(page, over)
		return len(u) > 0 && len(o) > 0 && precedes(o[0], u[0])
	}
	movement := "Since the previous publication: 1 turned Red · 1 back to Green"

	draft := getPage(t, client, reportURL)
	if got := movementOf(draft); !slices.Equal(got, []string{movement}) {
		t.Errorf("the draft's movement line reads %q, want %q", got, movement)
	}
	if !comesUnder(draft, "report-movement", "health-summary") {
		t.Errorf("the draft's movement line doesn't come under its Health summary")
	}
	if got := exceptions(draft); !slices.Equal(got, mostUrgentFirst) {
		t.Errorf("the draft's exceptions come %q, want %q", got, mostUrgentFirst)
	}
	if got := textsOf(findAllByTestID(reportBlockOf(t, draft, turnedRed), "report-changes")); !slices.Equal(got, []string{"Changed: Turned Red"}) {
		t.Errorf("the changed block's line reads %q, want [Changed: Turned Red]", got)
	}
	if got := textsOf(findAllByTestID(reportBlockOf(t, draft, recovered), "report-change")); !slices.Equal(got, []string{"Back to Green"}) {
		t.Errorf("the recovered block's changes read %q, want [Back to Green]", got)
	}
	if block := reportBlockOf(t, draft, staysRed); hasTestID(block, "report-changes") {
		t.Errorf("the standing Red block says something changed; block:\n%s", renderNode(block))
	}

	againstDate := movementOf(getPage(t, client, reportURL+"?baseline=2026-01-03"))
	if want := "Since Jan 3, 2026: 1 turned Red · 1 back to Green"; !slices.Equal(againstDate, []string{want}) {
		t.Errorf("the draft against a date's movement line reads %q, want %q", againstDate, want)
	}

	pub := h.PublishReport(boss, def)
	pubURL := fmt.Sprintf("%s/publications/%d", reportURL, pub.ID)
	publication := getPage(t, client, pubURL)
	if got := movementOf(publication); !slices.Equal(got, []string{movement}) {
		t.Errorf("the publication's movement line reads %q, want %q", got, movement)
	}
	if got := exceptions(publication); !slices.Equal(got, mostUrgentFirst) {
		t.Errorf("the publication's exceptions come %q, want %q", got, mostUrgentFirst)
	}
	print := getPage(t, client, pubURL+"/print")
	if got := movementOf(print); !slices.Equal(got, []string{movement}) {
		t.Errorf("the Print page's movement line reads %q, want %q", got, movement)
	}
	if !comesUnder(print, "report-movement", "report-published") {
		t.Errorf("the Print page's movement line doesn't come under its byline")
	}
	if got := exceptions(print); !slices.Equal(got, mostUrgentFirst) {
		t.Errorf("the Print page's exceptions come %q, want %q", got, mostUrgentFirst)
	}

	if want, got := "No movement since the previous publication.", movementOf(getPage(t, client, reportURL)); !slices.Equal(got, []string{want}) {
		t.Errorf("with nothing changed since, the draft's movement line reads %q, want %q", got, want)
	}
}

// A publication from before Reports said what moved and what changed renders
// with neither line.
func TestPublicationFromBeforeMovementHasNoMovementLineOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	old := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[{"Goal":{"ID":1,"Title":"Launch in EU","WhyItMatters":"Expand the market.",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"},"Lifecycle":"Active"},"Health":"Red","PriorHealth":"Green","Status":"Vendor is gone."}]}`
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_publications SET snapshot = ? WHERE id = ?`, old, pub.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
	for name, page := range map[string]*nethtml.Node{"publication": getPage(t, client, pubURL), "Print page": getPage(t, client, pubURL+"/print")} {
		if hasTestID(page, "report-movement") || hasTestID(page, "report-changes") {
			t.Errorf("the older %s shows a movement or a changes line it never had", name)
		}
		if !strings.Contains(textOf(reportBlockOf(t, page, g)), "Vendor is gone.") {
			t.Errorf("the older %s does not render its block as published", name)
		}
	}
}

// A publication's page and its print page say how it selected its Goals, as
// it read when published, under the published line; one frozen before
// selections were says nothing about it, and nor does the draft.
func TestPublicationSaysHowItSelects(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Dana Okafor")
	h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name: "EU MBR",
		Mode: domain.ReportModeRules,
		Rules: []domain.ReportRule{
			{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{strconv.FormatInt(boss.ID, 10)}},
			{Attribute: domain.RuleLifecycle, Op: domain.RuleIs, Values: []string{domain.LifecycleActive}},
		},
	})
	pub := h.PublishReport(boss, def)
	old := h.PublishReport(boss, def)
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_publications SET snapshot = json_remove(snapshot, '$.Scope') WHERE id = ?`, old.ID); err != nil {
		t.Fatalf("write a pre-selection snapshot: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubPath := func(p domain.Publication) string {
		return ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(p.ID, 10)
	}
	for name, url := range map[string]string{"publication page": pubPath(pub), "print page": pubPath(pub) + "/print"} {
		page := getPage(t, client, url)
		published := findAllByTestID(page, "report-published")
		selection := findAllByTestID(page, "report-selection")
		if len(published) == 0 {
			t.Errorf("%s has no report-published line", name)
		}
		if len(selection) == 0 {
			t.Errorf("%s has no report-selection line", name)
		}
		if len(published) == 0 || len(selection) == 0 {
			continue
		}
		if line, want := textOf(selection[0]), "Selection: Owner: Dana Okafor · Active"; !strings.HasPrefix(line, want) {
			t.Errorf("%s's selection line %q, want it to read %q", name, line, want)
		}
		if precedes(selection[0], published[0]) {
			t.Errorf("%s shows the selection line before the published line", name)
		}
		if movement := findAllByTestID(page, "report-movement"); len(movement) > 0 && precedes(movement[0], selection[0]) {
			t.Errorf("%s shows the movement line before the selection line", name)
		}
	}
	for name, url := range map[string]string{
		"old publication page": pubPath(old),
		"old print page":       pubPath(old) + "/print",
		"draft":                ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10),
	} {
		if hasTestID(getPage(t, client, url), "report-selection") {
			t.Errorf("%s has a selection line", name)
		}
	}
}

// reportLinkedGoalTo is the linked Goal under root that links to href, or
// nil when there is none.
func reportLinkedGoalTo(root *nethtml.Node, href string) *nethtml.Node {
	if links := findAll(root, func(n *nethtml.Node) bool {
		return n.Data == "a" && attrOf(n, "data-testid") == "report-linked-goal" && attrOf(n, "href") == href
	}); len(links) > 0 {
		return links[0]
	}
	return nil
}

// An exception block counts the Red, Yellow and Check-in overdue children
// behind its Rolled-up Health, names the Red and Yellow ones and every child
// the Report holds, and names the Goals in the Report it contributes to, each
// linking to its place in the Report or, outside it, to its Goal page; the
// draft, the publication and the print page all show it (#246, #247).
func TestReportBlockLinksTheGoalsBehindItAndThatItContributesTo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	s := h.LinkedGoalsReport(boss)
	pub := h.PublishReport(boss, s.Definition)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(s.Definition.ID, 10)
	pubURL := reportURL + "/publications/" + strconv.FormatInt(pub.ID, 10)
	for _, page := range []struct{ name, url string }{
		{"draft", reportURL},
		{"publication", pubURL},
		{"print", pubURL + "/print"},
	} {
		body := getPage(t, client, page.url)
		referral := reportBlockOf(t, body, s.Referral)
		if got := textsOf(findAllByTestID(referral, "report-rollup-count")); !slices.Equal(got, []string{"2 of 5 Red · 1 Yellow"}) {
			t.Errorf("%s: Referral program count %q, want 2 of 5 Red · 1 Yellow", page.name, got)
		}
		behind := findByTestID(t, referral, "report-behind-it")
		// A linked Goal reads [Health] Title: on screen its chip, printed its
		// shape and word.
		alpha := reportLinkedGoalTo(behind, fmt.Sprintf("#goal-%d", s.Alpha.ID))
		if alpha == nil || textOf(alpha) != "Alpha ledger ↓" {
			t.Fatalf("%s: Supported by does not link Alpha ledger ↓ to its block; line:\n%s", page.name, renderNode(behind))
		}
		alphaHealth := healthChipIn(t, alpha.Parent)
		if !precedes(alphaHealth, alpha) {
			t.Errorf("%s: Supported by doesn't read Alpha ledger's Health before its title; line:\n%s", page.name, renderNode(behind))
		}
		if page.name == "print" {
			if shown := shownTextOf(alphaHealth); shown != "Red" {
				t.Errorf("print: Alpha ledger's Health shows %q, want its shape beside Red: %s", shown, renderNode(alphaHealth))
			}
		} else {
			assertHealthChip(t, alphaHealth, "Red")
		}
		if beta := reportLinkedGoalTo(behind, fmt.Sprintf("/goals/%d", s.Beta.ID)); beta == nil || textOf(beta) != "Beta payouts" {
			t.Errorf("%s: Supported by does not link Beta payouts to its Goal page; line:\n%s", page.name, renderNode(behind))
		}
		epsilon := fmt.Sprintf("#goal-%d", s.Epsilon.ID)
		if reportLinkedGoalTo(behind, epsilon) == nil {
			t.Errorf("%s: Supported by does not link Epsilon signup to its line; line:\n%s", page.name, renderNode(behind))
		}
		if lines := findAll(body, func(n *nethtml.Node) bool {
			return n.Data == "tr" && attrOf(n, "data-testid") == "selected-goal" && attrOf(n, "id") == strings.TrimPrefix(epsilon, "#")
		}); len(lines) != 1 {
			t.Errorf("%s: %s does not resolve to Epsilon signup's on-track line", page.name, epsilon)
		}
		if strings.Contains(textOf(behind), "Delta copy") {
			t.Errorf("%s: Supported by names the Green Delta copy outside the Report; line:\n%s", page.name, renderNode(behind))
		}
		contributes := findByTestID(t, referral, "report-contributes-to")
		if growth := reportLinkedGoalTo(contributes, fmt.Sprintf("#goal-%d", s.Growth.ID)); growth == nil || textOf(growth) != "Growth ↓" {
			t.Errorf("%s: Contributes to does not link Growth ↓ to its block; line:\n%s", page.name, renderNode(contributes))
		}
		alphaContributes := findByTestID(t, reportBlockOf(t, body, s.Alpha), "report-contributes-to")
		if reportLinkedGoalTo(alphaContributes, fmt.Sprintf("#goal-%d", s.Referral.ID)) == nil {
			t.Errorf("%s: Alpha ledger's Contributes to does not link Referral program; line:\n%s", page.name, renderNode(alphaContributes))
		}
	}
}

// An exception block names each Schedule conflict its Goal is in, after what
// changed, grouped by side: as the child, under "Parent Goal due earlier", then
// as the parent, under "Contributing Goal due later", the groups joined by
// "; ". Each Goal at the other end links to its Goal page, outside the Report
// though it is, with its Owner shown as every person on that page is (a Name
// that discloses their email on the draft and the publication, Name (email) at
// their first mention in Print) and its delivery date as the page writes dates;
// the block's own delivery date, which its head shows, is not repeated.
func TestReportBlockNamesEachScheduleConflictOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Bo Boss")
	pat := h.SignInNamed("pat@example.com", "Pat Parker")
	sam := h.SignInNamed("sam@example.com", "Sam Lee")
	date := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	parent := h.ActiveGoalDueLater(pat, "Open in Iberia", date(10, 15))
	middle := h.ActiveGoalDueLater(boss, "Lisbon office", date(11, 3))
	child := h.ActiveGoalDueLater(sam, "Wire the network", date(12, 1))
	h.AcceptedLink(middle, parent)
	h.AcceptedLink(child, middle)
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, middle.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{middle.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	pubURL := reportURL + "/publications/" + strconv.FormatInt(pub.ID, 10)
	type conflictGroup struct {
		words   string
		entries []string
	}
	onPage := []conflictGroup{
		{"Parent Goal due earlier", []string{"Open in Iberia (Pat Parker), due Oct 15, 2026"}},
		{"Contributing Goal due later", []string{"Wire the network (Sam Lee), due Dec 1, 2026"}},
	}
	for _, page := range []struct {
		name, url string
		want      []conflictGroup
	}{
		{"draft", reportURL, onPage},
		{"publication", pubURL, onPage},
		{"print", pubURL + "/print", []conflictGroup{
			{"Parent Goal due earlier", []string{"Open in Iberia (Pat Parker (pat@example.com)), due Oct 15, 2026"}},
			{"Contributing Goal due later", []string{"Wire the network (Sam Lee (sam@example.com)), due Dec 1, 2026"}},
		}},
	} {
		block := reportBlockOf(t, getPage(t, client, page.url), middle)
		fact := findByTestID(t, block, "report-schedule-conflicts")
		if !strings.HasPrefix(textOf(fact), "Schedule conflict: ") {
			t.Errorf("%s: the fact reads %q, want it labelled Schedule conflict:", page.name, textOf(fact))
		}
		var wantGroups, gotGroups []string
		for _, g := range page.want {
			wantGroups = append(wantGroups, strings.Join(append([]string{g.words}, g.entries...), " · "))
		}
		groups := findAllByTestID(fact, "report-schedule-conflict-group")
		for _, g := range groups {
			gotGroups = append(gotGroups, runningText(g))
		}
		if !slices.Equal(gotGroups, wantGroups) {
			t.Errorf("%s: Schedule conflict groups read\n%q\nwant\n%q", page.name, gotGroups, wantGroups)
			continue
		}
		if got, want := runningText(fact), "Schedule conflict: "+strings.Join(wantGroups, "; "); got != want {
			t.Errorf("%s: the fact reads %q, want %q", page.name, got, want)
		}
		for i, other := range []struct {
			goal  domain.Goal
			email string
			name  string
		}{{parent, "pat@example.com", "Pat Parker"}, {child, "sam@example.com", "Sam Lee"}} {
			entries := findAllByTestID(groups[i], "report-schedule-conflict")
			var got []string
			for _, e := range entries {
				got = append(got, runningText(e))
			}
			if !slices.Equal(got, page.want[i].entries) {
				t.Errorf("%s: %s group's Schedule conflicts read %q, want %q", page.name, page.want[i].words, got, page.want[i].entries)
				continue
			}
			if strings.Contains(got[0], "Nov 3, 2026") {
				t.Errorf("%s: %q repeats the block's own delivery date", page.name, got[0])
			}
			if link := linkTo(entries[0], fmt.Sprintf("/goals/%d", other.goal.ID)); link == nil || textOf(link) != other.goal.Title {
				t.Errorf("%s: %q does not link %s to its Goal page", page.name, got[0], other.goal.Title)
			}
			if page.name != "print" && !reportShowsPerson(entries[0], other.email, other.name) {
				t.Errorf("%s: %q does not show %s as a Name that discloses their email", page.name, got[0], other.name)
			}
		}
		if !precedes(groups[0], groups[1]) {
			t.Errorf("%s: the contributing-Goal group comes before the parent-Goal group", page.name)
		}
		if changed := findAllByTestID(block, "report-changes"); len(changed) > 0 && !precedes(changed[0], fact) {
			t.Errorf("%s: Schedule conflict comes before Changed", page.name)
		}
		if contributes := findAllByTestID(block, "report-contributes-to"); len(contributes) > 0 && !precedes(fact, contributes[0]) {
			t.Errorf("%s: Schedule conflict comes after Contributes to", page.name)
		}
	}
}

// A Schedule conflict's Owner, in brackets after the other Goal's title, keeps
// the "(" with their Name, so a narrow window can't leave it ending a line;
// their email still discloses beside the Name, outside the part that can't
// break (#642).
func TestReportScheduleConflictOwnerKeepsTheirOpenBracketOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Bo Boss")
	pat := h.SignInNamed("pat@example.com", "Pat Parker")
	date := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	parent := h.ActiveGoalDueLater(pat, "Open in Iberia", date(10, 15))
	child := h.ActiveGoalDueLater(boss, "Lisbon office", date(11, 3))
	h.AcceptedLink(child, parent)
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, child.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{child.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	for _, page := range []struct{ name, url string }{
		{"draft", reportURL},
		{"publication", reportURL + "/publications/" + strconv.FormatInt(pub.ID, 10)},
	} {
		block := reportBlockOf(t, getPage(t, client, page.url), child)
		entry := findByTestID(t, block, "report-schedule-conflict")
		if got, want := runningText(entry), "Open in Iberia (Pat Parker), due Oct 15, 2026"; got != want {
			t.Errorf("%s: the Schedule conflict reads %q, want %q", page.name, got, want)
		}
		assertNameKeepsItsOpenBracket(t, entry, "Pat Parker")
		if !reportShowsPerson(entry, "pat@example.com", "Pat Parker") {
			t.Errorf("%s: the Schedule conflict doesn't show Pat Parker as a Name that discloses their email:\n%s", page.name, renderNode(entry))
		}
	}
}

// A publication frozen before Reports counted Red and Yellow children or
// named linked Goals reads as it did: its Rolled-up Health counts only the
// Check-in overdue children, and it names no other Goal (#246).
func TestPublicationFromBeforeLinkedGoalsReadsAsItDid(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	s := h.LinkedGoalsReport(boss)
	pub := h.PublishReport(boss, s.Definition)
	h.FreezeBeforeLinkedGoals(pub, s.Referral)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	page := getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(s.Definition.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))
	referral := reportBlockOf(t, page, s.Referral)
	if got := textsOf(findAllByTestID(referral, "report-rollup")); len(got) != 1 || !strings.Contains(got[0], "Red · 1 of 3 overdue") {
		t.Errorf("Rolled-up Health %q, want Red · 1 of 3 overdue", got)
	}
	for _, testID := range []string{"report-behind-it", "report-contributes-to"} {
		if hasTestID(page, testID) {
			t.Errorf("publication from before linked Goals shows %s; body:\n%s", testID, renderNode(page))
		}
	}
}

// A block whose only child has no Health yet has no Rolled-up Health to count,
// so it shows no Rolled-up Health row, only its Supported by, under its label
// (#246, #475).
func TestReportSupportedByStandsAloneWithoutARolledUpHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	parent := h.ActiveGoal(boss, "Growth", "Grow the business.")
	child := h.ActiveChildOf(boss, parent, "Referral program", "Customers bring customers.")
	h.Checkin(boss, parent.ID, domain.HealthRed, "Off track.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Growth", Mode: domain.ReportModePicked, Picked: []int64{parent.ID, child.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	block := reportBlockOf(t, getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)), parent)
	if hasTestID(block, "report-rollup") {
		t.Errorf("Growth shows a Rolled-up Health with none to count:\n%s", renderNode(block))
	}
	behind := findByTestID(t, block, "report-behind-it")
	if row := textOf(behind.Parent); !strings.HasPrefix(row, "Supported by ") || !strings.Contains(row, "Referral program") {
		t.Errorf("Growth's Supported by row reads %q, want its label over Referral program", row)
	}
}

// An exception block whose Goal asks for help says so after its Path to
// Green, naming whom it asks, or that no one is above the Owner, on the draft,
// the publication and its Print page alike. A block that doesn't ask says
// nothing of it.
func TestReportBlockShowsAnAskForHelpOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	lee := h.SignIn("lee@example.com")
	h.SetManager(sam, h.SignInNamed("mia@example.com", "Mia Chen"))
	later := h.Clock.Now().AddDate(0, 1, 0)
	asking := h.ActiveGoal(sam, "Launch in EU", "Expand the market.")
	h.AskForHelp(sam, asking.ID, domain.HealthRed, "Blocked on legal.", "Need counsel budget.", later)
	alone := h.ActiveGoal(lee, "Cut churn", "Keep customers.")
	h.AskForHelp(lee, alone.ID, domain.HealthYellow, "Slipping.", "Need a reviewer.", later)
	quiet := h.ActiveGoal(sam, "Ship billing", "Get paid.")
	h.Checkin(sam, quiet.ID, domain.HealthYellow, "Slipping.", "Add a team.", later)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{asking.ID, alone.ID, quiet.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	reportURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	pubURL := reportURL + "/publications/" + strconv.FormatInt(pub.ID, 10)

	for name, tc := range map[string]struct{ url, of string }{
		"draft":       {reportURL, "Asks for help of Mia Chen mia@example.com"},
		"publication": {pubURL, "Asks for help of Mia Chen mia@example.com"},
		"print":       {pubURL + "/print", "Asks for help of Mia Chen (mia@example.com)"},
	} {
		page := getPage(t, client, tc.url)
		want := map[string]string{
			asking.Title: tc.of,
			alone.Title:  "Asks for help · no one above the Owner",
			quiet.Title:  "",
		}
		blocks := findAllByTestID(page, "report-exception")
		if len(blocks) != len(want) {
			t.Fatalf("%s: %d exception blocks, want %d:\n%s", name, len(blocks), len(want), renderNode(page))
		}
		for _, block := range blocks {
			for title, ask := range want {
				if !strings.Contains(textOf(block), title) {
					continue
				}
				asks := reportTextsKeepingHidden(block, "report-ask-for-help")
				if ask == "" && len(asks) != 0 {
					t.Errorf("%s: %q, which doesn't ask, shows %q", name, title, asks)
				}
				if ask != "" && (len(asks) != 1 || asks[0] != ask) {
					t.Errorf("%s: %q shows %q, want %q", name, title, asks, ask)
				}
			}
		}
		asks := findAllByTestID(page, "report-ask-for-help")
		var strong int
		for _, ask := range asks {
			if lead := ask.FirstChild; lead != nil && lead.Type == nethtml.ElementNode && lead.Data == "strong" && textOf(lead) == "Asks for help" {
				strong++
			}
		}
		if strong != 2 {
			t.Errorf("%s: %d asks for help open with a strong Asks for help, want 2:\n%s", name, strong, renderNode(page))
		}
		paths := findAll(page, func(n *nethtml.Node) bool {
			return attrOf(n, "data-testid") == "report-path-to-green" && strings.Contains(textOf(n), "Need counsel budget.")
		})
		if len(paths) == 0 || len(asks) == 0 || !precedes(paths[0], asks[0]) {
			t.Errorf("%s: the ask doesn't follow the Path to Green:\n%s", name, renderNode(page))
		}
	}
}

// reportTextsKeepingHidden is what each element under root carrying testID
// reads, as textOf reads it but with hidden elements' text kept: a person's
// email, behind the control that discloses it, included.
func reportTextsKeepingHidden(root *nethtml.Node, testID string) []string {
	var out []string
	for _, n := range findAllByTestID(root, testID) {
		out = append(out, strings.Join(nodeWords(n, n, nil), " "))
	}
	return out
}

// reportTimezoneGoal arranges an Active Goal owned by someone in tz ("" for no
// Timezone), its Milestone Beta due 2026-04-02 and a Red Check-in back to Green
// by then, on a Report Definition, with the clock at 2026-04-02T20:00Z: still
// Apr 2 in UTC, the company's, but Apr 3, 05:00 in Tokyo.
func reportTimezoneGoal(t *testing.T, tz string) (*testsupport.Harness, domain.Account, domain.ReportDefinition) {
	t.Helper()
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	kenji := h.SignIn("kenji@example.com")
	h.SetTimezone(kenji, tz)
	g := h.ActiveGoal(kenji, "Ship search", "People can't find things.")
	h.Clock.Set(time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC))
	h.Checkin(kenji, g.ID, domain.HealthRed, "Blocked.", "Escalate to the vendor.", time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC))
	h.Clock.Set(time.Date(2026, 4, 2, 20, 0, 0, 0, time.UTC))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	return h, kenji, def
}

// assertReportBlockOverdue checks the page's exception block marks Beta Overdue and
// its Path to Green Overdue when want is true, and neither when it is false.
func assertReportBlockOverdue(t *testing.T, page *nethtml.Node, want bool) {
	t.Helper()
	block := findByTestID(t, page, "report-exception")
	beta := findAllByTestID(block, "report-milestone")[0]
	if overdue := slices.Contains(textsOf(findAllByTestID(beta, "milestone-mark")), "Overdue"); overdue != want {
		t.Errorf("Beta marked Overdue %v, want %v:\n%s", overdue, want, renderNode(beta))
	}
	path := findByTestID(t, block, "report-path-box")
	if overdue := flatFrameHasClass(path, "overdue"); overdue != want {
		t.Errorf("Path to Green box has class overdue %v, want %v: class %q", overdue, want, attrOf(path, "class"))
	}
	if badge := hasTestID(path, "path-overdue"); badge != want {
		t.Errorf("Path to Green box shows the Overdue badge %v, want %v:\n%s", badge, want, renderNode(path))
	}
}

// A Report marks a Milestone Overdue, and a Path to Green Overdue, by the date in
// the Goal's Owner's Timezone, the company's for an Owner with none
// (CONTEXT.md: Timezone): at 20:00 UTC on Apr 2 it is already Apr 3 in Tokyo.
func TestReportJudgesOverdueInTheOwnersTimezoneOverHTTP(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, tz string
		overdue  bool
	}{
		{"Owner in Tokyo", "Asia/Tokyo", true},
		{"Owner with no Timezone", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, _, def := reportTimezoneGoal(t, tc.tz)
			ts := newServer(t, h)
			client := signInClient(t, ts.URL, "boss@example.com")
			assertReportBlockOverdue(t, getPage(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, def.ID)), tc.overdue)
		})
	}
}

// A publication keeps the timezone its Goal was judged in when published: the
// Owner's Timezone cleared and a week on, its page and its Markdown export
// still mark Beta Overdue and the Path to Green Overdue as of the day it was
// published in Tokyo.
func TestPublicationKeepsTheTimezoneItWasJudgedInOverHTTP(t *testing.T) {
	t.Parallel()

	h, kenji, def := reportTimezoneGoal(t, "Asia/Tokyo")
	pub := h.PublishReport(h.SignIn("boss@example.com"), def)
	h.SetTimezone(kenji, "")
	h.Clock.Advance(7 * 24 * time.Hour)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubPath := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
	assertReportBlockOverdue(t, getPage(t, client, pubPath), true)
	if md := getBody(t, client, pubPath+"/markdown"); !strings.Contains(md, "- **[Overdue]** 2026-04-02 Beta\n") {
		t.Errorf("Markdown export does not mark Beta Overdue:\n%s", md)
	}
}

// A back-to-Green date that slipped within the Run off Green reads on an
// exception block as a Milestone's slipped date does: the current date, the
// one it last slipped from struck through, and the count once it slipped more
// than once. The draft, the publication and its print page all show it, and a
// publication from before Reports showed it renders as published.
func TestReportBlockStrikesTheBackToGreenDateItSlippedFromOverHTTP(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		slips []time.Time
		prior string // the struck-through date, "" for none
		count string // the count, "" for none
	}{
		{"twice", []time.Time{time.Date(2026, 3, 13, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)}, "Mar 13, 2026", "2 slips"},
		{"once", []time.Time{time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)}, "Mar 6, 2026", ""},
		{"never", nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
			h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC))
			for _, d := range tc.slips {
				h.Clock.Advance(24 * time.Hour)
				h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Still blocked.", "Hire counsel.", d, "Counsel starts later.")
			}
			def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
			pub := h.PublishReport(boss, def)

			ts := newServer(t, h)
			client := signInClient(t, ts.URL, "boss@example.com")
			pubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
			for name, url := range map[string]string{
				"draft":       fmt.Sprintf("%s/reports/%d", ts.URL, def.ID),
				"publication": pubURL,
				"print page":  pubURL + "/print",
			} {
				path := findByTestID(t, reportBlockOf(t, getPage(t, client, url), g), "report-path-to-green")
				if got := struckThrough(path); !slices.Equal(got, slippedBackToGreenTexts(tc.prior)) {
					t.Errorf("%s strikes through %q, want %q; path:\n%s", name, got, slippedBackToGreenTexts(tc.prior), renderNode(path))
				}
				if got := textsOf(findAllByTestID(path, "report-back-to-green-slips")); !slices.Equal(got, slippedBackToGreenTexts(tc.count)) {
					t.Errorf("%s counts the slips as %q, want %q; path:\n%s", name, got, slippedBackToGreenTexts(tc.count), renderNode(path))
				}
				if !strings.HasSuffix(runningText(path), ")") {
					t.Errorf("%s's back-to-Green date does not close its parenthesis: %q", name, runningText(path))
				}
				if want := "· " + tc.count + ")"; tc.count != "" && !strings.HasSuffix(runningText(path), want) {
					t.Errorf("%s's path doesn't end %q: %q", name, want, runningText(path))
				}
			}
		})
	}
}

// A back-to-Green date moved back earlier after slipping, which is no Date
// Slip, strikes no date equal to its own on an exception block: the most recent
// date it slipped from that differs, keeping the count, or none. The draft, the
// publication and its print page all show it (CONTEXT.md: Date Slip).
func TestReportBlockStrikesNoBackToGreenDateEqualToItsOwnOverHTTP(t *testing.T) {
	t.Parallel()

	jun := func(d int) time.Time { return time.Date(2026, 6, d, 0, 0, 0, 0, time.UTC) }
	for _, tc := range []struct {
		name  string
		slips []time.Time // the dates each slip moved it to
		back  time.Time   // the date a later Check-in moved it back to
		prior string      // the struck-through date, "" for none
		count string      // the count, "" for none
	}{
		{"slipped twice, back once", []time.Time{jun(16), jun(23)}, jun(16), "Jun 9, 2026", "2 slips"},
		{"slipped once, back to the start", []time.Time{jun(16)}, jun(9), "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
			h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", jun(9))
			for _, d := range tc.slips {
				h.Clock.Advance(24 * time.Hour)
				h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Still blocked.", "Hire counsel.", d, "Counsel starts later.")
			}
			h.Clock.Advance(24 * time.Hour)
			h.Checkin(boss, g.ID, domain.HealthRed, "Counsel found.", "Hire counsel.", tc.back)
			def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
			pub := h.PublishReport(boss, def)

			ts := newServer(t, h)
			client := signInClient(t, ts.URL, "boss@example.com")
			pubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
			for name, url := range map[string]string{
				"draft":       fmt.Sprintf("%s/reports/%d", ts.URL, def.ID),
				"publication": pubURL,
				"print page":  pubURL + "/print",
			} {
				path := findByTestID(t, reportBlockOf(t, getPage(t, client, url), g), "report-path-to-green")
				if got := struckThrough(path); !slices.Equal(got, slippedBackToGreenTexts(tc.prior)) {
					t.Errorf("%s strikes through %q, want %q; path:\n%s", name, got, slippedBackToGreenTexts(tc.prior), renderNode(path))
				}
				if got := textsOf(findAllByTestID(path, "report-back-to-green-slips")); !slices.Equal(got, slippedBackToGreenTexts(tc.count)) {
					t.Errorf("%s counts the slips as %q, want %q; path:\n%s", name, got, slippedBackToGreenTexts(tc.count), renderNode(path))
				}
			}
		})
	}
}

// A publication frozen with a back-to-Green date equal to the last date it
// slipped from renders under the rule of today, not of when it was frozen: the
// most recent prior date that differs is struck through, and the count kept.
func TestPublicationFrozenWithItsOwnBackToGreenDatePriorStrikesTheOneThatDiffersOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	frozen := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[{"Goal":{"ID":1,"Title":"Launch in EU","WhyItMatters":"Expand the market.",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"},"Lifecycle":"Active"},"Health":"Red","Status":"Blocked.",` +
		`"PathToGreen":"Hire counsel.","PathTargetDate":"2026-06-16T00:00:00Z",` +
		`"PriorPathTargetDates":["2026-06-09T00:00:00Z","2026-06-16T00:00:00Z"]}]}`
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_publications SET snapshot = ? WHERE id = ?`, frozen, pub.ID); err != nil {
		t.Fatalf("write a frozen snapshot: %v", err)
	}

	ts := newServer(t, h)
	block := reportBlockOf(t, getPage(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)), g)
	path := findByTestID(t, block, "report-path-to-green")
	if got, want := struckThrough(path), []string{"Jun 9, 2026"}; !slices.Equal(got, want) {
		t.Errorf("the publication strikes through %q, want %q; path:\n%s", got, want, renderNode(path))
	}
	if got, want := textsOf(findAllByTestID(path, "report-back-to-green-slips")), []string{"2 slips"}; !slices.Equal(got, want) {
		t.Errorf("the publication counts the slips as %q, want %q; path:\n%s", got, want, renderNode(path))
	}
	if want := "by Jun 16, 2026 Jun 9, 2026 · 2 slips)"; !strings.Contains(runningText(path), want) {
		t.Errorf("the path doesn't read %s: %q", want, runningText(path))
	}
}

// slippedBackToGreenTexts is s as the texts of the elements that show it: none for "".
func slippedBackToGreenTexts(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// A publication frozen before Reports struck the back-to-Green date through
// shows its date as published, with nothing struck through.
func TestPublicationFromBeforeBackToGreenSlipsShowsItsDateOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC))
	h.Clock.Advance(24 * time.Hour)
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Still blocked.", "Hire counsel.", time.Date(2026, 3, 13, 0, 0, 0, 0, time.UTC), "Counsel starts later.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[{"Goal":{"ID":1,"Title":"Launch in EU","WhyItMatters":"Expand the market.",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"},"Lifecycle":"Active"},"Health":"Red","Status":"Blocked.",` +
		`"PathToGreen":"Hire counsel.","PathTargetDate":"2026-03-13T00:00:00Z"}]}`
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}

	ts := newServer(t, h)
	block := reportBlockOf(t, getPage(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)), g)
	path := findByTestID(t, block, "report-path-to-green")
	if got, want := textsOf(findAllByTestID(block, "report-path-to-green")), []string{"Path to Green: Hire counsel. (back to Green by Mar 13, 2026)"}; !slices.Equal(got, want) {
		t.Errorf("an older publication's Path to Green reads %q, want %q", got, want)
	}
	if struck := struckThrough(path); len(struck) != 0 {
		t.Errorf("an older publication strikes a back-to-Green date through, %q; path:\n%s", struck, renderNode(path))
	}
}

// reportsActionItemRowsIn returns the rows of one group of the Reports
// page's Open Action Items, in order: testID names the group's list.
func reportsActionItemRowsIn(t *testing.T, page *nethtml.Node, testID string) []*nethtml.Node {
	t.Helper()
	return findAllByTestID(findByTestID(t, page, testID), "reports-action-item")
}

// assertReportsShowsPerson fails unless root shows the person as a page shows
// one, by label: a control reading it that expands their email inline beside
// it, with the email on hover too (CONTEXT.md: Name).
func assertReportsShowsPerson(t *testing.T, root *nethtml.Node, emailAddr, label string) {
	t.Helper()
	if !reportShowsPerson(root, emailAddr, label) {
		t.Errorf("%s isn't shown as %q with their email to expand:\n%s", emailAddr, label, renderNode(root))
	}
}

// The Reports page lists, above its table, the open Action Items its viewer
// has reason to chase: Yours, where they created the Report Definition,
// published, raised or own the item, and My org, where only someone in their
// Org did. Each row reads like Home's, with its owner, and closes nothing.
func TestReportsPageListsOpenActionItemsToChase(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	viewer := h.SignIn("viewer@example.com")
	mid := h.SignIn("mid@example.com")
	outsider := h.SignIn("outsider@example.com")
	stranger := h.SignIn("stranger@example.com")
	h.SetManager(mid, viewer)
	goal := h.ActiveGoal(outsider, "Launch in EU", "Expand the market.")
	raise := func(author domain.Account, report string, goalID int64, owner domain.Account, text string, dueIn int) domain.ActionItem {
		t.Helper()
		def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: report, Mode: domain.ReportModePicked, Picked: []int64{goal.ID}})
		pub := h.PublishReport(author, def)
		item, err := h.Service.RaiseActionItem(t.Context(), author.ID, domain.RaiseActionItemInput{
			PublicationID: pub.ID, GoalID: goalID, Text: text, OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, dueIn),
		})
		if err != nil {
			t.Fatalf("RaiseActionItem %q: %v", text, err)
		}
		return item
	}
	raise(viewer, "MBR", goal.ID, outsider, "Explain the slip.", -2)
	orgs := raise(outsider, "QBR", 0, mid, "Send the vendor plan.", 7)
	raise(outsider, "Ops review", 0, stranger, "Nobody's to chase.", 3)

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "viewer@example.com"), ts.URL+"/reports")

	section := findByTestID(t, page, "reports-action-items")
	table := findByTestID(t, page, "report-table")
	if !precedes(section, table) || within(table, section) {
		t.Fatalf("Open Action Items comes after the Reports table; want the section above it:\n%s", renderNode(page))
	}
	if heading := headingNamed(section, "Open Action Items"); heading == nil || heading.Data != "h2" {
		t.Errorf("the section is not headed Open Action Items:\n%s", renderNode(section))
	}
	// Open Action Items and the Reports table are plain sections on the
	// canvas, neither a card (DESIGN.md § System: One surface).
	if flatFrameHasClass(section, "card") {
		t.Errorf("Open Action Items is a card: class %q", attrOf(section, "class"))
	}
	if container := table.Parent; flatFrameHasClass(container, "card") {
		t.Errorf("the Reports table's container is a card: class %q", attrOf(container, "class"))
	}
	if strings.Contains(textOf(page), "Nobody's to chase.") {
		t.Errorf("the page lists an item nobody in the viewer's Org is party to")
	}

	yours := reportsActionItemRowsIn(t, page, "reports-action-items-yours")
	org := reportsActionItemRowsIn(t, page, "reports-action-items-org")
	if len(yours) != 1 || len(org) != 1 {
		t.Fatalf("Yours lists %d, My org %d; want one each:\n%s", len(yours), len(org), renderNode(section))
	}
	// Each group opens with a header row naming it and counting its items.
	for _, group := range []string{"Yours", "My org"} {
		heading := headingNamed(section, group+" 1")
		if heading == nil || heading.Data != "h3" {
			t.Errorf("Open Action Items lacks the group heading %s, counting 1", group)
			continue
		}
		if counts := textsOf(findAll(heading, func(n *nethtml.Node) bool { return n.Data == "span" })); !slices.Equal(counts, []string{"1"}) {
			t.Errorf("the %s heading's count reads %q, want 1 set apart from its name", group, counts)
		}
	}
	for _, want := range []string{
		"Explain the slip.",
		"MBR · Launch in EU · due " + h.Clock.Now().AddDate(0, 0, -2).Format("Jan 2, 2006"),
	} {
		if !strings.Contains(textOf(yours[0]), want) {
			t.Errorf("Yours row lacks %s: %q", want, textOf(yours[0]))
		}
	}
	if linkTo(yours[0], fmt.Sprintf("/goals/%d#action-items", goal.ID)) == nil {
		t.Errorf("Yours row doesn't link to the Goal's Action Items:\n%s", renderNode(yours[0]))
	}
	if overdue := findAllByTestID(yours[0], "reports-action-item-overdue"); len(overdue) != 1 || overdue[0].Data != "span" || textOf(overdue[0]) != "Overdue" {
		t.Errorf("Yours row isn't flagged Overdue:\n%s", renderNode(yours[0]))
	}
	assertReportsShowsPerson(t, yours[0], "outsider@example.com", "outsider")
	for _, want := range []string{
		"Send the vendor plan.",
		"QBR · due " + h.Clock.Now().AddDate(0, 0, 7).Format("Jan 2, 2006"),
	} {
		if !strings.Contains(textOf(org[0]), want) {
			t.Errorf("My org row lacks %s: %q", want, textOf(org[0]))
		}
	}
	if linkTo(org[0], fmt.Sprintf("/reports/%d/publications/%d#action-items", orgs.DefinitionID, orgs.PublicationID)) == nil {
		t.Errorf("My org row doesn't link to its publication's Action Items:\n%s", renderNode(org[0]))
	}
	assertReportsShowsPerson(t, org[0], "mid@example.com", "mid")
	if hasTestID(org[0], "reports-action-item-overdue") {
		t.Errorf("an item due next week is flagged Overdue:\n%s", renderNode(org[0]))
	}
	if forms := findAll(section, func(n *nethtml.Node) bool { return n.Data == "form" }); len(forms) != 0 {
		t.Errorf("Open Action Items offers a form; Close and Reassign stay on the publication and Home:\n%s", renderNode(section))
	}
	for _, nav := range findAllByTestID(page, "nav-reports") {
		if counts := findAll(nav, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "count") }); len(counts) != 0 {
			t.Errorf("the Reports nav item carries a count: %s", renderNode(nav))
		}
	}
}

// A viewer with no open Action Items to chase sees the Reports page as
// before: no Open Action Items section, not even an empty one.
func TestReportsPageWithoutActionItemsToChaseHasNoSection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	owner := h.SignIn("owner@example.com")
	h.SignIn("reader@example.com")
	goal := h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID}})
	pub := h.PublishReport(author, def)
	if _, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Send the vendor plan.", OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "reader@example.com"), ts.URL+"/reports")
	if !hasTestID(page, "report-table") {
		t.Fatalf("the Reports page has no table:\n%s", renderNode(page))
	}
	if groups := findAll(page, func(n *nethtml.Node) bool { return strings.HasPrefix(attrOf(n, "data-testid"), "reports-action-items") }); len(groups) != 0 {
		t.Errorf("a viewer with nothing to chase sees Open Action Items on the Reports page: %s", renderNode(groups[0]))
	}
	for _, absent := range []string{"Open Action Items", "Send the vendor plan."} {
		if strings.Contains(textOf(page), absent) {
			t.Errorf("a viewer with nothing to chase sees %q on the Reports page", absent)
		}
	}
}

// The Reports pages sit in the flat frame: no part of the Reports list, a
// draft, a publication or the builder is a card, its floating menus included,
// which take a raised surface of their own (DESIGN.md § System: Surfaces are
// for what floats).
func TestReportsPagesHaveNoCardsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.CheckinWithHighlight(boss, green.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, green.ID}})
	draft, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil || len(draft.Highlights) != 1 {
		t.Fatalf("DraftReport: %d Highlights, %v", len(draft.Highlights), err)
	}
	if err := h.Service.CurateNarrative(context.Background(), boss.ID, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: draft.Highlights[0].Highlight.ID, Section: domain.HighlightAccomplishment}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	pub := h.PublishReport(boss, def)
	comment, err := h.Service.AddComment(context.Background(), boss.ID, pub.ID, red.ID, "Why did legal slip?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := h.Service.RaiseActionItem(context.Background(), boss.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, CommentID: comment.ID, Text: "Chase legal", OwnerID: boss.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	draftPath := "/reports/" + strconv.FormatInt(def.ID, 10)
	for _, tc := range []struct{ page, path, shows string }{
		{"the Reports list", "/reports", "reports-action-item"},
		{"a draft", draftPath, "open-action-item"},
		{"a publication", draftPath + "/publications/" + strconv.FormatInt(pub.ID, 10), "comment-thread"},
		{"the new builder", "/reports/new", "report-matches"},
		{"the edit builder", draftPath + "/edit", "report-matches"},
	} {
		page := getPage(t, client, ts.URL+tc.path)
		if !hasTestID(page, tc.shows) {
			t.Fatalf("%s doesn't show what it should be checked with (%s):\n%s", tc.page, tc.shows, renderNode(page))
		}
		for _, card := range findAll(page, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "card") }) {
			t.Errorf("%s has a card: <%s class=%q>", tc.page, card.Data, attrOf(card, "class"))
		}
	}
}

// Only the definition's creator, a Report Delegate or an Admin may edit it,
// curate its narrative or publish it: anyone else signed in gets 403 on each
// post, which changes nothing, and still reads the draft but without Edit
// definition, the curation form or Publish…. A Report Delegate's draft offers
// all three, and their posts go through (CONTEXT.md: Report Delegate; ticket
// #434).
func TestOnlyThoseWhoManageAReportEditCurateAndPublishOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	dana := h.SignIn("dana@example.com")
	h.SignIn("sam@example.com")
	g := h.ActiveGoal(alice, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(alice, domain.SaveReportDefinitionInput{Name: "Alice's MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.AddReportDelegate(alice, dana, def)
	ts := newServer(t, h)
	draftURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	edit := url.Values{"name": {"Renamed"}, "mode": {domain.ReportModePicked}, "picked": {strconv.FormatInt(g.ID, 10)}}
	curate := url.Values{"text-" + domain.HighlightInsight: {"Pricing drives churn."}}
	controls := map[string]string{
		"Edit definition":   "edit-definition",
		"the curation form": "narrative-curation",
		"Publish…":          "publish-confirm",
	}

	sam := signInClient(t, ts.URL, "sam@example.com")
	draft := getPage(t, sam, draftURL)
	if !hasTestID(draft, "report-draft") {
		t.Errorf("Sam can't read the draft; body:\n%s", renderNode(draft))
	}
	for name, testID := range controls {
		if hasTestID(draft, testID) {
			t.Errorf("Sam, who may not manage the Report, is offered %s", name)
		}
	}
	for path, form := range map[string]url.Values{"/edit": edit, "/narrative": curate, "/publications": {}} {
		resp := postForm(t, noRedirects(sam), draftURL+path, form)
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden {
			t.Errorf("Sam's POST to %s: status %d, want 403; body:\n%s", path, resp.StatusCode, body)
		}
	}
	if got, _ := h.Service.GetReportDefinition(context.Background(), def.ID); got.Name != "Alice's MBR" {
		t.Errorf("Sam's refused edit renamed it %q", got.Name)
	}
	if pubs, _ := h.Service.ListPublications(context.Background(), def.ID); len(pubs) != 0 {
		t.Errorf("Sam's refused publish made %d publications", len(pubs))
	}

	client := signInClient(t, ts.URL, "dana@example.com")
	draft = getPage(t, client, draftURL)
	for name, testID := range controls {
		if !hasTestID(draft, testID) {
			t.Errorf("Dana, a Report Delegate, is not offered %s", name)
		}
	}
	for path, form := range map[string]url.Values{"/edit": edit, "/narrative": curate, "/publications": {}} {
		resp := postForm(t, noRedirects(client), draftURL+path, form)
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusSeeOther {
			t.Errorf("Dana's POST to %s: status %d, want 303; body:\n%s", path, resp.StatusCode, body)
		}
	}
}

// The draft's Delegates lists the Report Delegates to everyone. Its creator
// adds one by email and removes them again; an email the domain refuses comes
// back on the draft, 422, saying why. A Report Delegate is offered neither
// form, and their posts are 403 (CONTEXT.md: Report Delegate; ticket #434).
func TestCreatorAddsAndRemovesAReportDelegateOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	alice := h.SignIn("alice@example.com")
	dana := h.SignIn("dana@example.com")
	h.SignIn("sam@example.com")
	g := h.ActiveGoal(alice, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(alice, domain.SaveReportDefinitionInput{Name: "Alice's MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)
	draftURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	delegates := func(page *nethtml.Node) []string {
		return reportTextsKeepingHidden(page, "report-delegate")
	}

	creator := signInClient(t, ts.URL, "alice@example.com")
	draft := getPage(t, creator, draftURL)
	if !hasTestID(findByTestID(t, draft, "report-delegates"), "no-report-delegates") {
		t.Errorf("the draft's Delegates doesn't say there are none; body:\n%s", renderNode(draft))
	}
	if !hasTestID(draft, "add-report-delegate") {
		t.Fatalf("the creator is not offered Add Delegate; body:\n%s", renderNode(draft))
	}
	resp := postForm(t, creator, draftURL+"/delegates", url.Values{"email": {"dana@example.com"}})
	defer func() { _ = resp.Body.Close() }()
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusOK || resp.Request.URL.String() != draftURL {
		t.Fatalf("adding Dana: status %d at %s, want the draft", resp.StatusCode, resp.Request.URL)
	}
	if got := delegates(page); len(got) != 1 || !strings.Contains(got[0], "dana@example.com") {
		t.Errorf("Report Delegates after adding Dana = %q, want Dana", got)
	}

	resp = postForm(t, creator, draftURL+"/delegates", url.Values{"email": {"nobody@example.com"}})
	refusedPage := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("adding an unknown email: status %d, want 422", resp.StatusCode)
	}
	if refused, want := textOf(findByTestID(t, refusedPage, "report-delegate-refused")), `No account with email "nobody@example.com".`; refused != want {
		t.Errorf("refusal = %q, want %q", refused, want)
	}
	if email := formField(t, findByTestID(t, refusedPage, "add-report-delegate"), "email"); email.Value != "nobody@example.com" {
		t.Errorf("the refused email isn't kept in the form: it holds %q", email.Value)
	}

	client := signInClient(t, ts.URL, "dana@example.com")
	page = getPage(t, client, draftURL)
	if got := delegates(page); len(got) != 1 {
		t.Errorf("Dana's draft lists Report Delegates %q, want Dana", got)
	}
	if hasTestID(page, "add-report-delegate") || hasTestID(page, "remove-report-delegate") {
		t.Errorf("Dana, a Report Delegate, is offered to add or remove Report Delegates")
	}
	for path, form := range map[string]url.Values{
		"/delegates":        {"email": {"sam@example.com"}},
		"/delegates/remove": {"account": {strconv.FormatInt(dana.ID, 10)}},
	} {
		resp := postForm(t, noRedirects(client), draftURL+path, form)
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden {
			t.Errorf("Dana's POST to %s: status %d, want 403; body:\n%s", path, resp.StatusCode, body)
		}
	}

	resp = postForm(t, creator, draftURL+"/delegates/remove", url.Values{"account": {strconv.FormatInt(dana.ID, 10)}})
	page = parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusOK || resp.Request.URL.String() != draftURL {
		t.Fatalf("removing Dana: status %d at %s, want the draft", resp.StatusCode, resp.Request.URL)
	}
	if got := delegates(page); len(got) != 0 {
		t.Errorf("Report Delegates after removing Dana = %q, want none", got)
	}
}

// The draft page's History shows anyone who can open the Report every change
// in its Report log, newest first, each with who made it and when in the
// company's timezone (CONTEXT.md: Report log; tickets #435, #474).
func TestReportLogShowsOnTheDraftToAnyoneSignedIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	priya := h.SignInNamed("priya@example.com", "Priya Raman")
	dana := h.SignInNamed("dana@example.com", "Dana Cole")
	h.SignIn("sam@example.com")
	g := h.ActiveGoal(priya, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.AddReportDelegate(priya, dana, def)
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.UpdateReportDefinition(context.Background(), dana.ID, def.ID, domain.SaveReportDefinitionInput{
		Name: "EU monthly review", Mode: domain.ReportModePicked, Picked: []int64{g.ID},
	}); err != nil {
		t.Fatalf("UpdateReportDefinition: %v", err)
	}
	ts := httptest.NewServer(web.NewServer(domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(tokyo))))
	t.Cleanup(ts.Close)

	sam := signInClient(t, ts.URL, "sam@example.com")
	page := getPage(t, sam, fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	section := findByTestID(t, page, "report-history")
	got := textsOf(findAllByTestID(section, "report-history-entry"))
	// Jan 2, 2026, 15:04 UTC is 00:04 the next day in Tokyo.
	want := []string{
		"Title: EU MBR → EU monthly review Jan 3, 2026, 01:04 · Dana Cole",
		"Report Delegate added: Dana Cole Jan 3, 2026, 00:04 · Priya Raman",
		"Created Jan 3, 2026, 00:04 · Priya Raman",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Sam's History reads %q, want %q", got, want)
	}
	if hasTestID(section, "report-history-more") {
		t.Errorf("a short History hides some of its entries behind Show all")
	}
}

// A long History shows its newest 20 entries after the Draft, and the rest
// behind Show all, counting publications and the Report log together.
func TestALongDraftHistoryShowsTheNewestTwentyFirst(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	priya := h.SignIn("priya@example.com")
	g := h.ActiveGoal(priya, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{Name: "Rename 0", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	for i := 1; i <= 22; i++ {
		h.Clock.Advance(time.Minute)
		if _, err := h.Service.UpdateReportDefinition(context.Background(), priya.ID, def.ID, domain.SaveReportDefinitionInput{
			Name: fmt.Sprintf("Rename %d", i), Mode: domain.ReportModePicked, Picked: []int64{g.ID},
		}); err != nil {
			t.Fatalf("UpdateReportDefinition: %v", err)
		}
		if i == 11 || i == 22 {
			h.Clock.Advance(time.Minute)
			h.PublishReport(priya, def)
		}
	}
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "priya@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	section := findByTestID(t, page, "report-history")
	more := findByTestID(t, section, "report-history-more")
	all := findAllByTestID(section, "report-history-entry")
	rest := findAllByTestID(more, "report-history-entry")
	// 22 renames, Created and two publications, whose Published log entries
	// they stand for.
	if len(all) != 25 || len(rest) != 5 {
		t.Fatalf("History shows %d entries, %d behind Show all; want 25, 5 behind it", len(all), len(rest))
	}
	if first := textOf(all[0]); !strings.HasPrefix(first, "Published ") {
		t.Errorf("the newest entry reads %q, want the last publication", first)
	}
	if shown := textOf(more); !strings.HasPrefix(shown, "Show all 25 ") {
		t.Errorf("the rest of History reads %q, want it behind Show all 25", shown)
	}
	if got := textOf(rest[0]); !strings.HasPrefix(got, "Title: Rename 3 → Rename 4 ") {
		t.Errorf("the first entry behind Show all reads %q, want the 21st newest", got)
	}
	if last := textOf(rest[len(rest)-1]); !strings.HasPrefix(last, "Created") {
		t.Errorf("the oldest entry reads %q, want Created", last)
	}
}

// The helpers below read a Report page through the parser (helpers_test.go),
// as a person meets it.

// reportRowIn is the Reports page's status-table row for the Report named
// name: the row whose link reads it.
func reportRowIn(t *testing.T, root *nethtml.Node, name string) *nethtml.Node {
	t.Helper()
	for _, row := range findAllByTestID(root, "report-row") {
		if slices.ContainsFunc(findAll(row, func(n *nethtml.Node) bool { return n.Data == "a" }), func(a *nethtml.Node) bool { return textOf(a) == name }) {
			return row
		}
	}
	t.Fatalf("Reports page has no row for %q:\n%s", name, renderNode(root))
	return nil
}

// downloadMenuItem is where the Download menu's item carrying testID goes and
// what it reads, failing the test when there's none.
func downloadMenuItem(t *testing.T, menu *nethtml.Node, testID string) (href, text string) {
	t.Helper()
	item := findByTestID(t, menu, testID)
	return attrOf(item, "href"), textOf(item)
}

// reportNoteBoxes is what each of a narrative section's note boxes on the
// curation form holds, in order.
func reportNoteBoxes(t *testing.T, form *nethtml.Node, section string) []string {
	t.Helper()
	var out []string
	for _, f := range formFields(findByTestID(t, form, "section-text-"+section), "text-"+section) {
		if f.Type == "textarea" {
			out = append(out, f.Value)
		}
	}
	return out
}

// narrativeParagraphs is each of the author's notes a Report's narrative
// shows, as its own paragraph, in order.
func narrativeParagraphs(narrative *nethtml.Node) []string {
	return textsOf(findAllByTestID(narrative, "narrative-text"))
}

// curationChoice is the box or radio on the curation form named name with
// this value, failing the test when there's none.
func curationChoice(t *testing.T, form *nethtml.Node, name, value string) pageField {
	t.Helper()
	for _, f := range formFields(form, name) {
		if f.Value == value {
			return f
		}
	}
	t.Fatalf("form has no field %s=%s:\n%s", name, value, renderNode(form))
	return pageField{}
}

// baselineChipLabel is the bold part of the baseline chip: what it says the
// draft reads changes since.
func baselineChipLabel(t *testing.T, chip *nethtml.Node) string {
	t.Helper()
	return textOf(findByTestID(t, chip, "baseline-label"))
}

// A Report action refused for who is asking answers with the refusal page,
// 403, headed by what was refused and linking Back to the draft: saving its
// narrative, Add another included, publishing it, and changing its Report
// Delegates (#573).
func TestARefusedReportActionAnswersWithTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	h.SignIn("eve@example.com")
	g := h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)
	eve := noRedirects(signInClient(t, ts.URL, "eve@example.com"))
	draft := fmt.Sprintf("/reports/%d", def.ID)

	for _, c := range []struct {
		name, path, heading, reason string
		form                        url.Values
	}{
		{"save narrative", draft + "/narrative", "Can't save narrative", "Only its creator, a Report Delegate or an Admin may curate MBR.", url.Values{"text-" + domain.HighlightInsight: {"All good."}}},
		{"add another", draft + "/narrative", "Can't save narrative", "Only its creator, a Report Delegate or an Admin may curate MBR.", url.Values{"text-" + domain.HighlightInsight: {"All good."}, "add_note": {domain.HighlightInsight}}},
		{"publish", draft + "/publications", "Can't publish Report", "Only its creator, a Report Delegate or an Admin may publish MBR.", url.Values{}},
		{"add a Report Delegate", draft + "/delegates", "Can't change Report Delegates", "Only its creator or an Admin may change who are Report Delegates on MBR.", url.Values{"email": {"eve@example.com"}}},
		{"add an unknown email as a Report Delegate", draft + "/delegates", "Can't change Report Delegates", "Only its creator or an Admin may change who are Report Delegates on MBR.", url.Values{"email": {"nobody@example.com"}}},
	} {
		resp := postForm(t, eve, ts.URL+c.path, c.form)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", c.name, resp.StatusCode)
		}
		assertRefusalPage(t, page, c.heading, c.reason, draft)
	}
	if delegates, err := h.Service.ListReportDelegates(context.Background(), def.ID); err != nil || len(delegates) != 0 {
		t.Errorf("Report Delegates %+v (err %v), want none", delegates, err)
	}
}

// reportFactParityPublication is a publication holding a fact of every kind
// (domain.AllFactKinds), each in words no other fact uses, so an output that
// leaves one out can't pass on another's words.
func reportFactParityPublication() domain.Publication {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	publisher := domain.Account{ID: 1, Email: "pat.publisher@example.com", Name: "Pat Publisher"}
	owner := domain.Account{ID: 2, Email: "olu.owner@example.com", Name: "Olu Owner"}
	helper := domain.Account{ID: 3, Email: "hana.helper@example.com", Name: "Hana Helper"}
	chaser := domain.Account{ID: 4, Email: "cy.chaser@example.com", Name: "Cy Chaser"}
	writer := domain.Account{ID: 5, Email: "wren.writer@example.com", Name: "Wren Writer"}
	liner := domain.Account{ID: 6, Email: "lin.liner@example.com", Name: "Lin Liner"}
	planner := domain.Account{ID: 8, Email: "pia.planner@example.com", Name: "Pia Planner"}
	wirer := domain.Account{ID: 9, Email: "will.wirer@example.com", Name: "Will Wirer"}
	launch := domain.ReportBlock{
		Goal: domain.Goal{
			ID: 11, Title: "Launch the Lisbon office", WhyItMatters: "Southern Europe needs a base.",
			Owner: owner, Lifecycle: domain.LifecycleActive, DeliveryDate: day(6, 30),
		},
		Health:               domain.HealthRed,
		PriorHealth:          domain.HealthGreen,
		RunOffGreenWeeks:     4,
		Changes:              []string{"Delivery date slipped 3 weeks"},
		Badges:               []string{domain.BadgeCheckinOverdue},
		PriorDueDates:        []time.Time{day(6, 9)},
		Status:               "Lease talks stalled.",
		PathToGreen:          "Sign the backup lease.",
		PathTargetDate:       day(3, 1),
		PriorPathTargetDates: []time.Time{day(2, 1), day(2, 15)},
		Explanation:          "The fit-out crew is ahead.",
		Milestones: []domain.ReportMilestone{
			{Milestone: domain.Milestone{Name: "Lease signed", TargetDate: day(3, 2), Status: domain.MilestonePlanned}},
			{Milestone: domain.Milestone{Name: "Opening party", TargetDate: day(5, 1), Status: domain.MilestoneRemoved, RemovedReason: "Budget freeze."}},
		},
		Metrics: []domain.ReportMetric{
			{Metric: domain.Metric{Name: "Desks booked", Unit: "desks", Direction: domain.MetricUp, Baseline: 4, Target: 37, TargetDate: day(6, 30)}, Current: 19, Read: true},
			{Metric: domain.Metric{Name: "Local hires", Unit: "people", Direction: domain.MetricUp, Target: 12, TargetDate: day(6, 30)}},
		},
		RolledUp: domain.RolledUpHealth{Health: domain.HealthYellow, Present: true, ActiveChildren: 7, YellowChildren: 2},
		ScheduleConflicts: []domain.ReportScheduleConflict{
			{GoalID: 19, Title: "Staff the Lisbon desk", Owner: planner, Due: day(6, 1), OwnDue: day(6, 30)},
			{AsParent: true, GoalID: 20, Title: "Wire the Lisbon network", Owner: wirer, Due: day(7, 15), OwnDue: day(6, 30)},
		},
		ContributesTo: []domain.LinkedGoal{{GoalID: 12, Title: "Open in Iberia", Health: domain.HealthRed, InReport: true}},
		Behind:        []domain.LinkedGoal{{GoalID: 13, Title: "Fit out the Lisbon floor", Health: domain.HealthYellow}},
		Fields:        []domain.FieldValue{{Field: domain.Field{Name: "Cost centre", Type: domain.FieldShortText}, Value: "CC-4471"}},
		AsksForHelp:   true,
	}
	iberia := domain.ReportBlock{
		Goal:        domain.Goal{ID: 12, Title: "Open in Iberia", WhyItMatters: "Two markets at once.", Owner: owner, Lifecycle: domain.LifecycleActive},
		Health:      domain.HealthRed,
		AsksForHelp: true, AskAddressee: &helper,
	}
	return domain.Publication{
		ID: 7, DefinitionID: 3, PublishedBy: publisher,
		PublishedAt: time.Date(2026, 3, 10, 9, 30, 0, 0, time.UTC),
		Scope:       "Team: Southern Europe",
		Report: domain.Report{
			Definition:  domain.ReportDefinition{ID: 3, Name: "Iberia MBR", Introduction: "A month of leases and hiring."},
			Baseline:    day(2, 10),
			Previous:    domain.PreviousPublication{ID: 6, PublishedAt: time.Date(2026, 2, 10, 16, 0, 0, 0, time.UTC)},
			ActionItems: []domain.ActionItem{{ID: 1, Text: "Chase the landlord", Owner: chaser, DueDate: day(3, 20)}},
			Narrative: []domain.NarrativeSection{{
				Kind:  domain.HighlightInsight,
				Notes: []string{"Leases move slower in spring."},
				Highlights: []domain.NarrativeHighlight{{
					Highlight: domain.Highlight{Note: "Found a second site.", Owner: writer, Kind: domain.HighlightInsight},
					GoalID:    14, GoalTitle: "Scout Porto", Section: domain.HighlightInsight,
				}},
			}},
			Exceptions: []domain.ReportBlock{launch, iberia},
			Lines: []domain.SelectedGoal{
				{Goal: domain.Goal{ID: 15, Title: "Translate the handbook", Owner: liner}, Health: domain.HealthGreen},
				{Goal: domain.Goal{ID: 16, Title: "Renew the visas", Owner: liner}, Health: domain.HealthGreen},
				{Goal: domain.Goal{ID: 17, Title: "Book the movers", Owner: liner}, Health: domain.HealthGreen},
			},
			MembershipChanges: []domain.MembershipChange{
				{Goal: domain.Goal{ID: 18, Title: "Close the Madrid desk"}, Direction: domain.MembershipLeft, Reason: domain.MembershipRemovedByHand},
			},
			Movement: &domain.ReportMovement{TurnedRed: 1, New: 2},
		},
	}
}

// reportFactParityTexts are the words a fact shows in every output beside its
// label: its titles, names, notes, numbers and fixed phrases. Dates are left to
// each output's own tests, as each writes them its own way.
func reportFactParityTexts(t *testing.T, f domain.ReportFact) []string {
	t.Helper()
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	var out []string
	goals := func(gs []domain.LinkedGoal) {
		for _, g := range gs {
			out = append(out, g.Title)
		}
	}
	switch f.Kind {
	case domain.ReportFactByline:
		out = append(out, f.Byline.PublishedBy.Label(), f.Byline.Since.Words())
	case domain.ReportFactSelection, domain.ReportFactIntroduction, domain.ReportFactWhyItMatters,
		domain.ReportFactStatus, domain.ReportFactExplanation:
		out = append(out, f.Text)
	case domain.ReportFactHealthCounts:
		h := f.HealthCounts
		out = append(out,
			fmt.Sprintf("%d %s", h.Red, domain.HealthRed), fmt.Sprintf("%d %s", h.Yellow, domain.HealthYellow),
			fmt.Sprintf("%d %s", h.Green, domain.HealthGreen), fmt.Sprintf("%d %s", h.CheckinOverdue, domain.BadgeCheckinOverdue))
	case domain.ReportFactMovement:
		out = append(out, f.Movement.Since.Words())
		out = append(out, f.Movement.Parts...)
	case domain.ReportFactActionItems:
		for _, item := range f.ActionItems {
			out = append(out, item.Text, item.Owner.Label())
		}
	case domain.ReportFactNarrative:
		for _, sec := range f.Narrative {
			out = append(out, sec.Heading())
			out = append(out, sec.Notes...)
			for _, nh := range sec.Highlights {
				out = append(out, nh.Highlight.Note, nh.Highlight.Owner.Label(), nh.GoalTitle)
			}
		}
	case domain.ReportFactExceptions:
		// Each block's own facts are checked one by one.
	case domain.ReportFactOnTrack:
		for _, sg := range f.Lines {
			out = append(out, sg.Goal.Title, sg.Goal.Owner.Label())
		}
	case domain.ReportFactMembershipChanges:
		for _, c := range f.MembershipChanges {
			out = append(out, c.Goal.Title, c.Direction, c.Reason)
		}
	case domain.ReportFactBlockHead:
		h := f.Head
		out = append(out, h.Health, h.RunOffGreen, h.WasHealth, h.Title, h.Owner.Label(), h.Lifecycle)
		out = append(out, h.Badges...)
	case domain.ReportFactChanged:
		out = append(out, f.Changes...)
	case domain.ReportFactScheduleConflict:
		for _, g := range domain.ReportScheduleConflictGroups(f.ScheduleConflicts) {
			out = append(out, g.Words)
			for _, c := range g.Conflicts {
				out = append(out, c.Title, c.Owner.Label(), domain.ReportConflictOtherDue)
			}
		}
	case domain.ReportFactContributesTo, domain.ReportFactSupportedBy:
		goals(f.Goals)
	case domain.ReportFactFields:
		for _, v := range f.Fields {
			out = append(out, v.Field.Name, v.Value)
		}
	case domain.ReportFactPathToGreen:
		p := f.PathToGreen
		out = append(out, p.Plan, p.ByWords(), domain.SlipsWords(p.Slips))
		if p.Overdue {
			out = append(out, p.OverdueWords())
		}
	case domain.ReportFactAskForHelp:
		if f.Ask.Addressee != nil {
			out = append(out, f.Ask.Addressee.Label())
		} else {
			out = append(out, f.Ask.Words())
		}
	case domain.ReportFactMilestones:
		for _, m := range f.Milestones {
			out = append(out, m.Milestone.Name, m.Mark, m.Milestone.RemovedReason)
		}
	case domain.ReportFactMetrics:
		for _, m := range f.Metrics {
			out = append(out, m.Metric.Name, m.Reading(num), domain.ReportAgainstATargetOf, num(m.Metric.Target), m.Metric.Unit)
		}
	case domain.ReportFactRolledUp:
		out = append(out, f.RolledUp.Health, f.RolledUp.Count)
	default:
		t.Errorf("the parity test doesn't know the words of a %q fact", f.Kind)
	}
	return out
}

// The publication page and the Markdown export show the same facts: every
// fact the publication lists, and every fact of each of its blocks, shows its
// label and its words in both. The fixture holds a fact of every kind, so an
// output that drops one fails here, naming it.
func TestReportPageAndMarkdownExportShowTheSameFacts(t *testing.T) {
	t.Parallel()

	pub := reportFactParityPublication()
	outputs := []struct{ name, text string }{
		{"page", textOf(parsePage(t, web.RenderPublicationContent(pub, time.UTC)))},
		{"Markdown export", strings.Join(strings.Fields(export.Markdown(pub)), " ")},
	}
	listed := map[domain.ReportFactKind]bool{}
	check := func(where string, f domain.ReportFact) {
		listed[f.Kind] = true
		words := append([]string{f.Label}, reportFactParityTexts(t, f)...)
		for _, out := range outputs {
			for _, w := range words {
				if w != "" && !strings.Contains(out.text, w) {
					t.Errorf("the %s lacks %q from the %q fact%s:\n%s", out.name, w, f.Kind, where, out.text)
				}
			}
		}
	}
	for _, f := range pub.Facts() {
		check("", f)
		for _, b := range f.Exceptions {
			for _, bf := range b.Facts(pub.PublishedAt) {
				check(" of "+b.Goal.Title, bf)
			}
		}
	}
	for _, kind := range domain.AllFactKinds {
		if !listed[kind] {
			t.Errorf("the fixture holds no %q fact, so the test can't check both outputs show one", kind)
		}
	}
}
