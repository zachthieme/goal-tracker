package web_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
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
	// Two weeks out from a day later moves the back-to-Green date later.
	h.CheckinMovingPath(boss, g.ID, domain.HealthRed, "Vendor is gone.", "Find a new vendor.", h.Clock.Now().AddDate(0, 0, 14), "A new vendor takes longer.")
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
	if line := pageElement(t, readBody(t, resp), "p", "report-published"); !strings.Contains(line, "changes since the previous publication, Feb 11, 2026, ") {
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
	if line := pageElement(t, readBody(t, resp), "p", "report-published"); !strings.Contains(line, "changes since Jan 1, 2026") {
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
	if link := pageElement(t, page, "a", "download-markdown"); !strings.Contains(link, pubPath+"/markdown") {
		t.Errorf("publication page's Download menu does not link its Markdown export; link:\n%s", link)
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
	for _, want := range []string{"### Red · Launch in EU", "Vendor is late."} {
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

	page := getBody(t, client, ts.URL+pubPath)
	if link := pageElement(t, page, "a", "download-pdf"); !strings.Contains(link, pubPath+"/print") {
		t.Errorf("publication page's Download menu does not link its print page; link:\n%s", link)
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

	resp, err := client.Get(ts.URL + f.pubPath + "/pdf")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
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

	resp, err := client.Get(ts.URL + f.pubPath + "/pdf")
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
	resp, err := signInClient(t, ts.URL, "boss@example.com").Get(wrong)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("pdf download under another definition: status %d, want 404", resp.StatusCode)
	}

	resp, err = noRedirects(&http.Client{}).Get(ts.URL + f.pubPath + "/pdf")
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

	resp, err := client.Get(ts.URL + f.pubPath + "/pdf")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
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

// reportRowNamed returns the Reports page's status-table row for the Report
// named name, up to its closing tag.
func reportRowNamed(t *testing.T, page, name string) string {
	t.Helper()
	for _, row := range strings.Split(page, `<tr data-testid="report-row"`)[1:] {
		row, _, _ = strings.Cut(row, "</tr>")
		if strings.Contains(row, ">"+name+"</a>") {
			return row
		}
	}
	t.Fatalf("Reports page has no row for %q:\n%s", name, page)
	return ""
}

// downloadItem returns the href and text of the Download menu's item carrying
// testID, failing the test when there's none.
func downloadItem(t *testing.T, menu, testID string) (href, text string) {
	t.Helper()
	item := pageElement(t, menu, "a", testID)
	return attr(openTag(item), "href"), strings.TrimSpace(item[strings.Index(item, ">")+1:])
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

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports")

	row := reportRowNamed(t, page, "EU MBR")
	menu := between(t, row, `data-testid="download-menu"`, "")
	if !strings.Contains(menu, ">Download</button>") {
		t.Errorf("the row's menu isn't opened by a Download button:\n%s", menu)
	}
	for testID, want := range map[string][2]string{
		"download-markdown": {latestPath + "/markdown", "Markdown (.md)"},
		"download-pdf":      {latestPath + "/pdf", "PDF (.pdf)"},
	} {
		if href, text := downloadItem(t, menu, testID); href != want[0] || text != want[1] {
			t.Errorf("%s links %q reading %q, want %q reading %q", testID, href, text, want[0], want[1])
		}
	}
	if never := reportRowNamed(t, page, "WBR"); strings.Contains(never, `data-testid="download-menu"`) {
		t.Errorf("a never-published Report's row has a Download menu:\n%s", never)
	}
}

// A publication's page downloads through the same Download menu, in place of
// the Markdown and Print / PDF buttons it had.
func TestThePublicationPageHasTheDownloadMenuOverHTTP(t *testing.T) {
	t.Parallel()

	f := newPDFDownloadFixture(t)
	ts := httptest.NewServer(web.NewServer(f.h.Service, web.WithPDFRenderer(&pdfDownloadFake{})))
	t.Cleanup(ts.Close)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+f.pubPath)

	menu := between(t, pageElement(t, page, "div", "report-export"), `data-testid="download-menu"`, "")
	if href, _ := downloadItem(t, menu, "download-markdown"); href != f.pubPath+"/markdown" {
		t.Errorf("Markdown item links %q, want %q", href, f.pubPath+"/markdown")
	}
	if href, _ := downloadItem(t, menu, "download-pdf"); href != f.pubPath+"/pdf" {
		t.Errorf("PDF item links %q, want %q", href, f.pubPath+"/pdf")
	}
	for _, gone := range []string{`data-testid="export-markdown"`, `data-testid="export-print"`, "Print / PDF"} {
		if strings.Contains(page, gone) {
			t.Errorf("publication page still carries the old %s", gone)
		}
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
		menu := between(t, getBody(t, client, ts.URL+path), `data-testid="download-menu"`, "")
		if href, text := downloadItem(t, menu, "download-pdf"); href != f.pubPath+"/print" || text != "PDF (print from browser)" {
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
	// Alice, who may not manage the Report, reads the narrative as it will
	// be published.
	narrative := pageElement(t, getBody(t, signInClient(t, ts.URL, "alice@example.com"), reportURL), "section", "report-narrative")
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
	narrative := pageElement(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), reportURL), "section", "report-narrative")
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
	if save := buttonNamed(parsePage(t, form), "Save narrative"); save == nil || attrOf(save, "type") != "submit" {
		t.Errorf("curation form has no Save narrative submit button; form:\n%s", form)
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
			got = append(got, textOf(findAll(hl, func(n *nethtml.Node) bool { return attrOf(n, "class") == "rp-include" })[0]))
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
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/reports/"+strconv.FormatInt(def.ID, 10) {
		t.Fatalf("curate: status %d at %s, want the draft page", resp.StatusCode, resp.Request.URL)
	}
	page := readBody(t, resp)

	form := pageElement(t, page, "form", "narrative-curation")
	for section, want := range map[string][]string{
		domain.HighlightInsight:        {"Pricing drives churn.", "Discounts don't save accounts.", ""},
		domain.HighlightAccomplishment: {"EU is open for business.", ""},
		domain.HighlightMiss:           {""},
	} {
		if got := sectionNoteBoxes(t, form, section); !slices.Equal(got, want) {
			t.Errorf("%s note boxes %q, want %q", section, got, want)
		}
	}

	want := []string{"Pricing drives churn.", "Discounts don't save accounts.", "EU is open for business."}
	reader := getBody(t, signInClient(t, ts.URL, "sam@example.com"), reportURL)
	if got := narrativeTexts(pageElement(t, reader, "section", "report-narrative")); !slices.Equal(got, want) {
		t.Errorf("draft narrative paragraphs %q, want %q", got, want)
	}

	published := readBody(t, postForm(t, client, reportURL+"/publications", url.Values{}))
	if got := narrativeTexts(pageElement(t, published, "section", "report-narrative")); !slices.Equal(got, want) {
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
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"?baseline=2025-12-01")
	form := pageElement(t, page, "form", "narrative-curation")
	if id := attr(openTag(form), "id"); id != "narrative-curation" {
		t.Fatalf("curation form id = %q, want narrative-curation", id)
	}
	action := html.UnescapeString(attr(openTag(form), "action"))
	for _, section := range []string{domain.HighlightInsight, domain.HighlightAccomplishment, domain.HighlightMiss} {
		details := pageElement(t, form, "details", "section-text-"+section)
		button := regexp.MustCompile(`<button[^>]*name="add_note"[^>]*>Add another</button>`).FindString(details)
		if button == "" {
			t.Errorf("%s offers no Add another; details:\n%s", section, details)
			continue
		}
		for name, want := range map[string]string{
			"type":      "submit",
			"value":     section,
			"hx-post":   action,
			"hx-target": "#report-curation",
			"hx-swap":   "outerHTML",
		} {
			if got := html.UnescapeString(attr(button, name)); got != want {
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
			if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != draftPath+"/narrative" {
				t.Fatalf("add another: status %d at %s, want 200 with no redirect; body:\n%s", resp.StatusCode, resp.Request.URL, body)
			}

			form := between(t, body, `<form data-testid="narrative-curation"`, "</form>")
			for section, want := range map[string][]string{
				domain.HighlightInsight:        {"Pricing drives churn.", "", ""},
				domain.HighlightAccomplishment: {"EU is open for business."},
				domain.HighlightMiss:           {""},
			} {
				if got := sectionNoteBoxes(t, form, section); !slices.Equal(got, want) {
					t.Errorf("%s note boxes %q, want %q", section, got, want)
				}
			}
			if box := formInput(t, form, "include-"+signed, "on"); !isChecked(box) {
				t.Errorf("the ticked Highlight lost its tick: %s", box)
			}
			if radio := formInput(t, form, "pick-"+signed, domain.HighlightInsight); !isChecked(radio) {
				t.Errorf("the ticked Highlight lost its section: %s", radio)
			}
			if box := formInput(t, form, "include-"+lost, "on"); isChecked(box) {
				t.Errorf("the unticked Highlight is ticked: %s", box)
			}
			if radio := formInput(t, form, "pick-"+lost, domain.HighlightInsight); !isChecked(radio) {
				t.Errorf("the unticked Highlight lost the section picked for it: %s", radio)
			}
			answer := parsePage(t, body)
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
			for _, untouched := range []string{"report-draft", "publish-summary", "report-log"} {
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
			if resp.StatusCode != tc.status {
				t.Fatalf("refused add another: status %d, want %d; body:\n%s", resp.StatusCode, tc.status, body)
			}
			form := between(t, body, `<form data-testid="narrative-curation"`, "</form>")
			if got, want := sectionNoteBoxes(t, form, domain.HighlightMiss), []string{"Slipped a week.", ""}; !slices.Equal(got, want) {
				t.Errorf("Misses note boxes %q, want %q", got, want)
			}
			if box := formInput(t, form, "include-"+signed, "on"); !isChecked(box) {
				t.Errorf("the ticked Highlight lost its tick: %s", box)
			}
			result := pageElement(t, body, "span", "save-result")
			if !strings.Contains(result, ">Not saved: ") || !strings.Contains(result, "section") {
				t.Errorf("the save status %q, want Not saved and why", result)
			}
			if draft, err := h.Service.DraftReport(context.Background(), def, time.Time{}); err != nil || len(draft.Narrative) != 0 {
				t.Errorf("narrative after a refusal %+v (err %v), want nothing saved", draft.Narrative, err)
			}
			if tc.name == "htmx" {
				for _, untouched := range []string{`data-testid="report-draft"`, `data-testid="publish-summary"`, "<html"} {
					if strings.Contains(body, untouched) {
						t.Errorf("refused add another answers %s; body:\n%s", untouched, body)
					}
				}
			}
		})
	}
}

// A refused Add another without script answers the draft page from one build
// of the draft: the form and the preview both show the draft as saved, so it
// reads the saved narrative once (ticket #455).
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
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write a pre-several-notes snapshot: %v", err)
	}

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID))
	narrative := pageElement(t, page, "section", "report-narrative")
	if got := narrativeTexts(narrative); !slices.Equal(got, []string{"EU is open for business."}) {
		t.Errorf("published notes %q, want the one note frozen before; narrative:\n%s", got, narrative)
	}
	if note, hl := strings.Index(narrative, "EU is open for business."), strings.Index(narrative, "Signed the first EU customer."); note < 0 || hl < note {
		t.Errorf("the note does not come before the picked Highlight; narrative:\n%s", narrative)
	}
}

// sectionNoteBoxes is what each of a narrative section's note boxes on the
// curation form holds, in order.
func sectionNoteBoxes(t *testing.T, form, section string) []string {
	t.Helper()
	details := pageElement(t, form, "details", "section-text-"+section)
	var out []string
	for _, m := range regexp.MustCompile(`<textarea name="text-`+regexp.QuoteMeta(section)+`"[^>]*>([^<]*)</textarea>`).FindAllStringSubmatch(details, -1) {
		out = append(out, html.UnescapeString(m[1]))
	}
	return out
}

// narrativeTexts is each of the author's notes a Report's narrative shows, as
// its own paragraph, in order.
func narrativeTexts(narrative string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`<p data-testid="narrative-text">([^<]*)</p>`).FindAllStringSubmatch(narrative, -1) {
		out = append(out, html.UnescapeString(m[1]))
	}
	return out
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
// selected Goals are Red, Yellow, Green, and Check-in overdue, so an exec sees
// the shape of the company before reading a block.
func TestPublicationSummarisesHealthOverHTTP(t *testing.T) {
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
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID, yellow.ID, green.ID, checkinOverdue.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	summary := pageElement(t, page, "section", "health-summary")
	for tile, want := range map[string]string{"red": "1", "yellow": "1", "green": "1", "checkin-overdue": "1"} {
		if count := pageElement(t, summary, "span", "health-count-"+tile); !strings.HasSuffix(count, ">"+want) {
			t.Errorf("%s tile: %q, want a count of %s", tile, count, want)
		}
	}
	if !strings.Contains(summary, `<span class="dot"></span>Check-in overdue</span>`) {
		t.Errorf("the summary has no Check-in overdue tile: %s", summary)
	}
	if badges := elementTexts(page, "span", "report-badge"); !slices.Contains(badges, "Check-in overdue") {
		t.Errorf("report badges = %q, want Open Tokyo's Check-in overdue", badges)
	}
}

// The publication splits the selected Goals in two: exceptions as entries
// ruled off by hairlines, not cards, under Exceptions, headed by how many there are, each with its Health badge and its
// Why it matters, Status, and Path to Green — marked Overdue once its target
// date has passed — and the rest as a table of Health, Goal, Owner, and Due
// under On track.
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
	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	attention := pageElement(t, page, "section", "needs-attention")
	if heading := between(t, attention, "<h2>", "</h2>"); !strings.HasPrefix(heading, "<h2>Exceptions ") || !strings.HasSuffix(pageElement(t, heading, "span", "needs-attention-count"), ">1") {
		t.Errorf("Exceptions heading does not count its 1 exception: %s", heading)
	}
	card := pageElement(t, attention, "article", "report-exception")
	if tag := openTag(card); hasClass(tag, "card") || !hasClass(tag, "rp-block") || !strings.Contains(card, `class="badge r"`) {
		t.Errorf("exception is not a hairline entry with a Red badge; card:\n%s", card)
	}
	if rule := cssRule(t, page, ".rp-block"); !strings.Contains(rule, "border-top:1px solid var(--color-border)") {
		t.Errorf("an exception isn't ruled off by a hairline: .rp-block{%s}", rule)
	}
	if status := pageElement(t, card, "div", "report-status-box"); !strings.Contains(status, "Hire counsel.") ||
		!strings.Contains(status, `data-testid="path-overdue"`) {
		t.Errorf("status box does not mark the missed Path to Green Overdue; box:\n%s", status)
	}
	if lead := pageElement(t, card, "p", "report-why-it-matters"); !strings.Contains(lead, "<strong>Why it matters:</strong> Expand the market.") {
		t.Errorf("exception does not lead with its Why it matters: %s", lead)
	}

	onTrack := pageElement(t, page, "table", "on-track")
	row := pageElement(t, onTrack, "tr", "selected-goal")
	for _, want := range []string{"Cut churn", "boss@example.com", domain.HealthGreen} {
		if !strings.Contains(row, want) {
			t.Errorf("On track row missing %q; row:\n%s", want, row)
		}
	}
}

// A publication's narrative is a plain section, not a card, each section under
// its heading (a label on the print page) and each Highlight credited "—
// owner, Goal".
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
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	narrative := pageElement(t, page, "section", "report-narrative")
	if tag := openTag(narrative); hasClass(tag, "card") || !hasClass(tag, "rp-narrative") {
		t.Errorf("narrative is not a plain section; narrative:\n%s", narrative)
	}
	if heading := pageElement(t, narrative, "h3", "narrative-section"); !strings.Contains(heading, `class="label"`) || !strings.HasSuffix(heading, ">Accomplishments") {
		t.Errorf("narrative section heading %q, want an Accomplishments label", heading)
	}
	if credit := between(t, narrative, `data-testid="highlight-credit"`, "</a>"); !strings.Contains(credit, "— "+shownAs("alice@example.com", "alice")+", ") || !strings.Contains(credit, "Launch in EU") {
		t.Errorf("Highlight credit %q, want — owner, Goal", credit)
	}
}

// The draft page keeps Publish in reach: a sticky header strip carries the
// breadcrumb to the name, the baseline, the save status, the History of
// publications and Publish — the primary button at the right, saying what it
// does. The save status sits in the name's block, so at phone width it can
// stay beside the name (#172); above that the block lets its children into the
// header's row and the save status is ordered after the chip, beside History.
func TestDraftHeaderKeepsPublishInReachOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	header := between(t, page, `<header data-testid="draft-header"`, "</header>")
	if tag := openTag(header); !hasClass(tag, "page-header") {
		t.Errorf("the draft's header isn't the page's header strip: %s", tag)
	}
	last := -1
	for _, part := range []string{`<a class="ph-parent" href="/reports">Reports</a>`, `aria-current="page">MBR<`, `data-testid="save-status"`, `data-testid="baseline-chip"`, `data-testid="report-history"`, `data-testid="publish-confirm"`} {
		at := strings.Index(header, part)
		if at <= last {
			t.Errorf("%s is out of order in the header (at %d, after %d); header:\n%s", part, at, last, header)
		}
		last = at
	}
	for selector, want := range map[string]string{
		".rp-draft-title":         "display:contents",
		".rp-draft-head .rp-save": "order:1",
		".rp-head-actions":        "order:1",
	} {
		if rule := cssRule(t, page, selector); !strings.Contains(rule, want) {
			t.Errorf("the header does not show the save status after the chip: %s{%s} lacks %s", selector, rule, want)
		}
	}
	if chip := between(t, header, `data-testid="baseline-chip"`, `data-testid="report-history"`); !strings.Contains(chip, `data-testid="report-baseline"`) {
		t.Errorf("the baseline chip does not hold the baseline form; chip:\n%s", chip)
	}
	publish := pageElement(t, header, "details", "publish-confirm")
	if !strings.Contains(publish, `<summary class="btn primary sm">Publish…</summary>`) || !strings.Contains(publish, "Readers can comment on the frozen copy.") {
		t.Errorf("Publish… is not the primary button with its summary; disclosure:\n%s", publish)
	}
	history := pageElement(t, header, "details", "report-history")
	if !strings.Contains(history, "History (0)") || !strings.Contains(pageElement(t, history, "li", "no-publications"), "Not published yet.") {
		t.Errorf("an unpublished Report's History does not say so; menu:\n%s", history)
	}
	if strings.Contains(page, `data-testid="report-publications"`) {
		t.Errorf("publications are still listed in a sidebar; body:\n%s", page)
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
	root := h.MarkTopLevel(boss, h.ActiveGoal(boss, "Grow revenue", "The company needs to grow."))
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

// A published definition's row dates its last publication in the company's
// timezone, "Jan 1, 2026" for 03:00 UTC on 2 January in Los Angeles, with who
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
	h.AddReportDelegate(boss, ada, def)
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
		`<span data-testid="report-published">Jan 1, 2026</span>`,
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

// A Report card's head reads Health first, its "· N wks" and "· was X" straight
// after it, then the title and its meta line, then the other badges, on the
// draft and on the print page. At phone width it keeps that order: the badges
// drop under the Health and title, left-aligned, rather than squeezing the
// title into a narrow column beside them (#229, #453).
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
	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))
	printed := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")
	for where, body := range map[string]string{"the draft": page, "the print page": printed} {
		block := findByTestID(t, parsePage(t, body), "report-exception")
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
	assertHealthChip(t, findByTestID(t, parsePage(t, page), "report-health"), "Red")

	phone := mediaBlock(t, page, "@media (max-width:600px)")
	cardHead, _ := ruleFor(phone, ".rp-card-head")
	badges, _ := ruleFor(phone, ".rp-badges")
	if !strings.Contains(cardHead, "flex-wrap:wrap") || !strings.Contains(badges, "flex-basis:100%") || strings.Contains(cardHead, "column") {
		t.Errorf("at 600px the card head doesn't keep Health and title on a line with its badges under them: .rp-card-head{%s} .rp-badges{%s}", cardHead, badges)
	}
	if !strings.Contains(badges, "justify-content:flex-start") {
		t.Errorf("at 600px the badges under the title stay right-aligned: .rp-badges{%s}", badges)
	}
	// A narrow block, as in the draft's column under 1280px, does the same, so
	// its title isn't squeezed until its words break.
	if block, _ := ruleFor(page, ".rp-block"); !strings.Contains(block, "container:rp-block/inline-size") {
		t.Errorf("a Report block isn't a container its head can fit itself to: .rp-block{%s}", block)
	}
	narrow := mediaBlock(t, page, "@container rp-block (max-width:28rem)")
	narrowHead, _ := ruleFor(narrow, ".rp-card-head")
	narrowBadges, _ := ruleFor(narrow, ".rp-badges")
	if !strings.Contains(narrowHead, "flex-wrap:wrap") || !strings.Contains(narrowBadges, "flex-basis:100%") || !strings.Contains(narrowBadges, "justify-content:flex-start") {
		t.Errorf("a narrow block's head doesn't drop its badges under Health and the title: .rp-card-head{%s} .rp-badges{%s}", narrowHead, narrowBadges)
	}
	for _, sel := range []string{".rp-card-head", ".rp-health", ".rp-card-title", ".rp-badges"} {
		for _, css := range []string{phone, narrow, between(t, printed, "<style>", "</style>")} {
			if rule, _ := ruleFor(css, sel); strings.Contains(strings.ReplaceAll(rule, " ", ""), "order:") || strings.Contains(rule, "reverse") {
				t.Errorf("the card head reorders itself: %s{%s}", sel, rule)
			}
		}
	}
}

// The Health summary tiles go 2 × 2 whenever the element holding them is too
// narrow for four, as the draft's preview column is under 1600px, so the
// Check-in overdue chip never runs past its tile, and stack where even two
// don't fit; at 900px and below they go 2 × 2 whatever the column (#459).
func TestReportHealthTilesGoTwoByTwoInANarrowColumnOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	goal := h.ActiveGoal(boss, "Cut infrastructure cost per request", "Margins matter.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	draft := "/reports/" + strconv.FormatInt(def.ID, 10)
	for _, path := range []string{draft, draft + "/publications/" + strconv.FormatInt(pub.ID, 10)} {
		page := getBody(t, client, ts.URL+path)
		findByTestID(t, parsePage(t, page), "health-summary")
		if holder, _ := ruleFor(page, ":has(>.rp-tiles)"); !strings.Contains(holder, "container:rp-tiles/inline-size") {
			t.Errorf("%s: the tiles' holder isn't a container they can fit themselves to: :has(>.rp-tiles){%s}", path, holder)
		}
		narrow := mediaBlock(t, page, "@container rp-tiles (max-width:44rem)")
		tiles, _ := ruleFor(narrow, ".rp-body .rp-tiles")
		odd, _ := ruleFor(narrow, ".rp-tile:nth-child(odd)")
		lower, _ := ruleFor(narrow, ".rp-tile:nth-child(n+3)")
		// Each column is at least as wide as its widest tile, so in the draft's
		// column at 1000px the Check-in overdue chip's column takes what it
		// needs rather than half.
		if !strings.Contains(tiles, "grid-template-columns:repeat(2,minmax(min-content,1fr))") {
			t.Errorf("%s: narrow tiles don't form two columns each as wide as its tiles: .rp-tiles{%s}", path, tiles)
		}
		if !strings.Contains(odd, "border-left:none") || !strings.Contains(odd, "padding-left:0") || !strings.Contains(lower, "border-top:1px solid var(--color-border)") {
			t.Errorf("%s: narrow tiles don't rule their 2 × 2 grid as at 900px: .rp-tile:nth-child(odd){%s} .rp-tile:nth-child(n+3){%s}", path, odd, lower)
		}
		// Too narrow even for that, as the draft's column is just above 900px,
		// they stack, each under a hairline.
		narrowest := mediaBlock(t, page, "@container rp-tiles (max-width:14rem)")
		stacked, _ := ruleFor(narrowest, ".rp-body .rp-tiles")
		tile, _ := ruleFor(narrowest, ".rp-tile")
		below, _ := ruleFor(narrowest, ".rp-tile:nth-child(n+2)")
		if !strings.Contains(stacked, "grid-template-columns:minmax(0,1fr)") || !strings.Contains(tile, "border-left:none") || !strings.Contains(tile, "padding-left:0") || !strings.Contains(below, "border-top:1px solid var(--color-border)") {
			t.Errorf("%s: the narrowest tiles don't stack under hairlines: .rp-tiles{%s} .rp-tile{%s} .rp-tile:nth-child(n+2){%s}", path, stacked, tile, below)
		}
		if strings.Index(page, "@container rp-tiles (max-width:14rem)") < strings.Index(page, "@container rp-tiles (max-width:44rem)") {
			t.Errorf("%s: the stacked tiles' rule comes before the 2 × 2 rule, which overrides it", path)
		}
		for _, css := range []string{narrow, narrowest} {
			for _, sel := range []string{".rp-tiles", ".rp-body .rp-tiles", ".rp-tile"} {
				if rule, _ := ruleFor(css, sel); strings.Contains(rule, "order:") || strings.Contains(rule, "dense") {
					t.Errorf("%s: narrow tiles reorder themselves: %s{%s}", path, sel, rule)
				}
			}
		}
	}
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
	printed := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")

	badge := findByTestID(t, findByTestID(t, parsePage(t, printed), "report-exception"), "report-health")
	dots := findAll(badge, func(n *nethtml.Node) bool { return attrOf(n, "class") == "dot" })
	if shownTextOf(badge) != "—" || len(dots) != 0 {
		t.Errorf("a Goal with no Health doesn't print its dash alone: %s", renderNode(badge))
	}
}

// The print page sets the Report in the design system's serif, falling back to
// Georgia, and marks Health with a shape as well as its word, shown, not left
// to a screen reader as on screen, so it survives black-and-white printing: the screen's drawn shapes, a Red octagon, a Yellow
// triangle pointing down and a Green circle, in the Health's ink and kept when
// printed, not ■ ▲ ● characters (#452).
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
		t.Errorf("print page's Exceptions does not count its 1 exception: %s", count)
	}
	if strings.ContainsAny(printed, "■▲●") {
		t.Errorf("print page still marks Health with ■ ▲ ● characters; body:\n%s", printed)
	}
	doc := parsePage(t, printed)
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

	style := between(t, printed, "<style>", "</style>")
	dot := printPageRule(t, style, ".dot")
	if strings.Contains(dot, "display: none") || !strings.Contains(dot, "background: currentColor") || !strings.Contains(dot, " print-color-adjust: exact;") {
		t.Errorf("print page doesn't draw the dot in its Health's ink, kept when printed: .dot { %s }", dot)
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	unspaced := strings.NewReplacer(" ", "", "\n", "", "\t", "")
	for _, sel := range []string{".r .dot", ".y .dot", ".g .dot"} {
		screen, _ := ruleFor(css, sel)
		if print := printPageRule(t, style, sel); unspaced.Replace(print) != unspaced.Replace(screen)+";" && unspaced.Replace(print) != unspaced.Replace(screen) {
			t.Errorf("print page draws %s { %s }, not the screen's %s{%s}", sel, print, sel, screen)
		}
	}
}

// The print page draws each Health badge in its Health's colour, a tint and
// an ink of its own for Red, Yellow, Green and No Health yet, and asks the
// browser to keep that colour when it prints, so a PDF carries it (#389).
func TestPrintPageColoursHealthBadgesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	printed := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")
	style := between(t, printed, "<style>", "</style>")

	if rule := printPageRule(t, style, ".badge"); !strings.Contains(rule, "-webkit-print-color-adjust: exact;") || !strings.Contains(rule, " print-color-adjust: exact;") {
		t.Errorf("print page's badges don't keep their colour in print: .badge { %s }", rule)
	}
	seen := map[string]string{}
	for health, class := range map[string]string{"Red": ".r", "Yellow": ".y", "Green": ".g", "No Health": ".lc"} {
		rule := printPageRule(t, style, class)
		bg, ink := printPageColour(t, style, rule, "background"), printPageColour(t, style, rule, "color")
		if bg == ink {
			t.Errorf("%s badge's ink is its tint, %s: %s { %s }", health, bg, class, rule)
		}
		for _, c := range []string{bg, ink} {
			if other, ok := seen[c]; ok {
				t.Errorf("%s badge shares %s with %s: %s { %s }", health, c, other, class, rule)
			}
			seen[c] = health
		}
	}
}

// The print page keeps the screen's other marks of trouble in colour too: a
// Check-in overdue badge in its grey with a dashed edge, and the status box of
// a Path to Green past its date in Red's tint, kept when printed (#389).
func TestPrintPageColoursOverdueMarksOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	goal := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID}})
	pub := h.PublishReport(boss, def)

	ts := newServer(t, h)
	printed := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")
	style := between(t, printed, "<style>", "</style>")

	red := printPageColour(t, style, printPageRule(t, style, ".r"), "background")
	overdue := printPageRule(t, style, ".st")
	if bg, ink := printPageColour(t, style, overdue, "background"), printPageColour(t, style, overdue, "color"); bg == ink || bg == red {
		t.Errorf("Check-in overdue badge is not its own grey: .st { %s }", overdue)
	}
	if !strings.Contains(overdue, "border: 0.5pt dashed var(") {
		t.Errorf("Check-in overdue badge has no dashed edge: .st { %s }", overdue)
	}
	box := printPageRule(t, style, ".rp-tint.overdue")
	if bg := printPageColour(t, style, box, "background"); bg != red {
		t.Errorf("overdue status box is %s, not Red's tint %s: .rp-tint.overdue { %s }", bg, red, box)
	}
	if !strings.Contains(box, "-webkit-print-color-adjust: exact;") || !strings.Contains(box, " print-color-adjust: exact;") {
		t.Errorf("overdue status box doesn't keep its tint in print: .rp-tint.overdue { %s }", box)
	}
}

// printPageRule is the declarations of the print page's style rule for
// selector alone, failing the test when it has none.
func printPageRule(t *testing.T, style, selector string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`).FindStringSubmatch(style)
	if m == nil {
		t.Fatalf("print page has no %s rule; style:\n%s", selector, style)
	}
	return strings.TrimSpace(m[1])
}

// printPageColour is the hex colour a print page rule gives property, through
// the var() token it names, failing the test when it gives none.
func printPageColour(t *testing.T, style, rule, property string) string {
	t.Helper()
	m := regexp.MustCompile(`(?:^|[;\s])` + property + `:\s*([^;]+);`).FindStringSubmatch(rule)
	if m == nil {
		t.Fatalf("print page rule sets no %s: %s", property, rule)
	}
	value := strings.TrimSpace(m[1])
	if v := regexp.MustCompile(`^var\((--[\w-]+)\)$`).FindStringSubmatch(value); v != nil {
		token := regexp.MustCompile(regexp.QuoteMeta(v[1]) + `:\s*([^;]+);`).FindStringSubmatch(printPageRule(t, style, ":root"))
		if token == nil {
			t.Fatalf("print page's :root declares no %s", v[1])
		}
		value = strings.TrimSpace(token[1])
	}
	if !regexp.MustCompile(`^#[0-9A-Fa-f]{3,6}$`).MatchString(value) {
		t.Fatalf("print page rule's %s is not a colour: %s", property, rule)
	}
	return strings.ToUpper(value)
}

// At phone width the Publish… card is pinned to the viewport's gutters rather
// than hung off its button's right edge, where it ran off the left of a 390px
// screen with Cancel beyond reach (#172).
func TestDraftPublishCardFitsAPhoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	rule, ok := ruleFor(mediaBlock(t, page, "@media (max-width:600px)"), ".rp-publish")
	if !ok {
		t.Fatalf("at 600px there is no .rp-publish rule")
	}
	for _, want := range []string{"position:fixed", "left:16px", "right:16px", "width:auto"} {
		if !strings.Contains(rule, want) {
			t.Errorf("at 600px .rp-publish{%s} lacks %s", rule, want)
		}
	}
}

// At phone width the draft header scrolls away with the page instead of
// sticking over a third of the screen. The name takes its own line with the
// save status beside it, the baseline chip sits on its own row below, and
// History, Edit definition and Publish… share the last row: five controls
// don't fit one row at 390px (#172).
func TestDraftHeaderStacksAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	header := pageElement(t, page, "header", "draft-header")
	title := between(t, header, `<div class="rp-draft-title">`, `data-testid="baseline-chip"`)
	for _, part := range []string{`data-testid="report-breadcrumb"`, `data-testid="save-status"`} {
		if !strings.Contains(title, part) {
			t.Errorf("the name block lacks %s; block:\n%s", part, title)
		}
	}
	actions := between(t, header, `<div class="rp-head-actions">`, "")
	for _, part := range []string{`data-testid="report-history"`, `data-testid="edit-definition"`, `data-testid="publish-confirm"`} {
		if !strings.Contains(actions, part) {
			t.Errorf("the actions row lacks %s; row:\n%s", part, actions)
		}
	}
	if strings.Contains(actions, `data-testid="save-status"`) {
		t.Errorf("the save status is in the actions row, not beside the name; row:\n%s", actions)
	}

	phone := mediaBlock(t, page, "@media (max-width:600px)")
	for selector, wants := range map[string][]string{
		".rp-draft-head":   {"position:static"},
		".rp-draft-title":  {"display:flex", "flex-basis:100%"},
		".rp-head-actions": {"flex-basis:100%", "flex-wrap:nowrap", "margin-left:0"},
	} {
		rule, ok := ruleFor(phone, selector)
		if !ok {
			t.Errorf("at 600px there is no %s rule", selector)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(rule, want) {
				t.Errorf("at 600px %s{%s} lacks %s", selector, rule, want)
			}
		}
	}
}

// A Due date in the draft's On track table wraps as a whole, never mid-date
// ("2026-12-" over "14") in a narrow column (#172).
func TestDraftOnTrackDueDatesDoNotBreakOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(3 * 24 * time.Hour)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	due := tagAround(t, pageElement(t, page, "table", "on-track"), `data-testid="selected-due"`)
	if !strings.Contains(due, "rp-date") {
		t.Fatalf("the On track Due date is not marked rp-date: %s", due)
	}
	if rule := cssRule(t, page, ".rp-date"); !strings.Contains(rule, "white-space:nowrap") {
		t.Errorf("a Due date can break mid-date: .rp-date{%s}", rule)
	}
}

// At a phone's width each On track row stacks as a block rather than leaving
// its Due off the side of the card (#180): its Health then the Goal on one
// line, as at every width, then its Owner and Due under the Goal. The cells
// run Health, Goal, Owner, Due in the HTML and none is reordered (#453). A
// publication's discussion row spans the full width.
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
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))

	onTrack := between(t, page, `<table data-testid="on-track"`, "</table>")
	if !strings.HasPrefix(onTrack, `<table data-testid="on-track" class="rp-track">`) {
		t.Errorf("the On track table has no rp-track class to style: %s", onTrack)
	}
	row := pageElement(t, onTrack, "tr", "selected-goal")
	for _, class := range []string{"rp-track-health", "rp-track-goal", "rp-track-owner", "rp-track-due"} {
		if !strings.Contains(row, `class="`+class+`"`) {
			t.Errorf("the row has no %s cell to style:\n%s", class, row)
		}
	}
	if !strings.Contains(onTrack, `<td colspan="4" class="rp-track-talk">`) {
		t.Errorf("the discussion row has no rp-track-talk cell to style:\n%s", onTrack)
	}
	phone := mediaBlock(t, page, "@media (max-width:600px)")
	for selector, wants := range map[string][]string{
		".rp-track thead":           {"display:none"},
		".rp-track tr":              {"display:grid", "grid-template-columns:auto minmax(0,1fr)"},
		".rp-track td":              {"display:block"},
		".rp-track .rp-track-goal":  {"grid-column:2"},
		".rp-track .rp-track-owner": {"grid-column:2"},
		".rp-track .rp-track-due":   {"grid-column:2"},
		".rp-track .rp-track-talk":  {"grid-column:1/-1"},
	} {
		if rule, _ := ruleFor(phone, selector); strings.Contains(rule, "order:") {
			t.Errorf("at 600px %s{%s} reorders the row, which reads Health first", selector, rule)
		}
		rule, ok := ruleFor(phone, selector)
		if !ok {
			t.Errorf("at 600px there is no %s rule", selector)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(rule, want) {
				t.Errorf("at 600px %s{%s} lacks %s", selector, rule, want)
			}
		}
	}
}

// An exception card's due date and its Milestones' dates wrap between their
// struck slips but never mid-date ("2026-07-" over "16") on a phone (#180).
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
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	block := pageElement(t, page, "article", "report-exception")
	due := strings.Join(strings.Fields(between(t, block, `<span data-testid="report-due"`, "</p>")), " ")
	if want := `<span data-testid="report-due" class="rp-dates"><del>Jul 2, 2026</del> <span>Jul 16, 2026</span></span>`; due != want {
		t.Errorf("the card's due date is not a wrapper of whole dates:\n got %s\nwant %s", due, want)
	}
	milestone := strings.Join(strings.Fields(pageElement(t, block, "li", "report-milestone")), " ")
	if want := `<span class="rp-dates"><span>` + slippedBeta.Format("Jan 2, 2006") + `</span> <del>` + beta.TargetDate.Format("Jan 2, 2006") + `</del> </span>`; !strings.Contains(milestone, want) {
		t.Errorf("the Milestone's dates are not a wrapper of whole dates:\nwant %s\nin %s", want, milestone)
	}
	if rule := cssRule(t, page, ".rp-dates>*"); !strings.Contains(rule, "white-space:nowrap") {
		t.Errorf("a card date can break mid-date: .rp-dates>*{%s}", rule)
	}
	if rule, ok := ruleFor(page, ".rp-dates"); ok && strings.Contains(rule, "nowrap") {
		t.Errorf("the whole wrapper is kept on one line, so its slips can't wrap: .rp-dates{%s}", rule)
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
	"Slipped Apr 20, 2026 Mar 20, 2026 (3) Vendor sign-off",
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
			block := pageElement(t, getBody(t, client, url), "article", "report-exception")
			if got := elementTexts(block, "li", "report-milestone"); !slices.Equal(got, reportMarkedMilestones) {
				t.Errorf("Milestones read\n %q\nwant\n %q", got, reportMarkedMilestones)
			}
			assertMilestoneMarksShowTheirWords(t, findByTestID(t, parsePage(t, block), "report-milestones"),
				[]string{"Overdue", "Done", "New", "Slipped", "Slipped", "Removed"})
			vendor := pageElement(t, block, "li", "report-milestone")
			for _, li := range strings.Split(block, `<li data-testid="report-milestone"`) {
				if strings.Contains(li, "Vendor sign-off") {
					vendor = li
				}
			}
			if !strings.Contains(vendor, "<del>Mar 20, 2026</del>") {
				t.Errorf("Vendor sign-off does not strike through the date it last slipped from:\n%s", vendor)
			}
			for _, older := range []string{"Mar 1,", "Mar 10,"} {
				if strings.Contains(vendor, older) {
					t.Errorf("Vendor sign-off still lists the older date %s:\n%s", older, vendor)
				}
			}
			if !strings.Contains(block, "<del>Launch party</del>") {
				t.Errorf("the Removed Milestone's name is not struck through:\n%s", block)
			}
			for _, want := range []string{"report-health", "rollup-health"} {
				if health := findByTestID(t, parsePage(t, block), want); !flatFrameHasClass(health, healthClassYellow) || textOf(health) != "Yellow" {
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
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID))
	want := []string{"Overdue Dec 20, 2025 Dec 1, 2025 Beta", "New Mar 1, 2026 GA", "Apr 1, 2026 Launch"}
	if got := elementTexts(pageElement(t, page, "article", "report-exception"), "li", "report-milestone"); !slices.Equal(got, want) {
		t.Errorf("Milestones read\n %q\nwant\n %q", got, want)
	}
}

// The draft's own rules outrank the generic ones they sit inside (#173): a
// Highlight's section picker is an inline segmented control, not the form's
// column of fields, the Highlight list and its items sit flush in its card, and
// the preview's Health tiles stay a row of four. The baseline chip is a pill as
// tall as the buttons beside it (#180).
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
	// .rp-block ul indents the exception entries' lists; the Highlight list
	// isn't in one and stays flush.
	if rule := cssRule(t, page, ".rp-list"); !strings.Contains(rule, "padding:0") {
		t.Errorf("the Highlight list is indented: .rp-list{%s}", rule)
	}
	// A Highlight lines up with the "Highlights since" heading above it (#180).
	if rule := cssRule(t, page, ".rp-list>li.rp-curate"); !strings.Contains(rule, "padding-left:0") || !strings.Contains(rule, "padding-right:0") {
		t.Errorf("a Highlight is indented from its heading: .rp-list>li.rp-curate{%s}", rule)
	}
	if rule := cssRule(t, page, ".rp-list>li"); !strings.Contains(rule, "padding:12px 0") {
		t.Errorf("list rows lost their padding: .rp-list>li{%s}", rule)
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
	// app.css's details>summary is 44px tall; the chip matches the 32px small
	// buttons beside it in the header strip, its border inside that height
	// (#180).
	if rule := cssRule(t, page, ".rp-menu.rp-chip>summary"); !strings.Contains(rule, "box-sizing:border-box") || !strings.Contains(rule, "min-height:32px") {
		t.Errorf("the baseline chip is not 32px tall: .rp-menu.rp-chip>summary{%s}", rule)
	}
	// On a touch screen app.css raises those buttons to 44px; the chip
	// follows them (#183).
	if rule, ok := ruleFor(mediaBlock(t, page, "@media (pointer:coarse)"), ".rp-menu.rp-chip>summary"); !ok || !strings.Contains(rule, "min-height:44px") {
		t.Errorf("on a coarse pointer the baseline chip is not 44px tall: .rp-menu.rp-chip>summary{%s}", rule)
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
			t.Errorf("%s: the Introduction isn't between the meta line and the Health tiles", who)
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
// Health tiles and before the exceptions: a reader who may manage the Report
// composes it there, under a Narrative heading, and anyone else reads it as it
// will be published, with no compose form. Everyone gets the narrow column
// beside it, its Delegates above its Report log; only the creator gets Remove
// and Add (#473).
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
		t.Errorf("the composer isn't between the Health tiles and the exceptions")
	}
	if hasTestID(page, "report-narrative") {
		t.Errorf("the manager reads the narrative as published beside the composer")
	}
	side := findByTestID(t, page, "draft-side")
	if within(side, main) {
		t.Fatalf("the narrow column is inside the main column")
	}
	delegates, log := findByTestID(t, side, "report-delegates"), findByTestID(t, side, "report-log")
	if !precedes(delegates, log) {
		t.Errorf("the Report log comes before the Delegates")
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
	narrative := findByTestID(t, main, "report-narrative")
	if got := textsOf(findAllByTestID(narrative, "narrative-text")); !slices.Equal(got, []string{"Slipped a week."}) {
		t.Errorf("the reader reads the notes %q, want the saved one", got)
	}
	if !precedes(findByTestID(t, main, "health-summary"), narrative) || !precedes(narrative, findByTestID(t, main, "needs-attention")) {
		t.Errorf("the reader's narrative isn't between the Health tiles and the exceptions")
	}
	side = findByTestID(t, page, "draft-side")
	if !hasTestID(side, "report-delegates") || !hasTestID(side, "report-log") {
		t.Errorf("the reader's narrow column lacks the Delegates or the Report log: %s", renderNode(side))
	}
	if hasTestID(side, "add-report-delegate") || hasTestID(side, "remove-report-delegate") {
		t.Errorf("the reader gets Add or Remove on the Delegates")
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
// time it was published in the company's timezone, not UTC.
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
	h.AddReportDelegate(boss, ada, def)
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
		{">Mar 2, 2026, 19:00<", shownAs("ada.okafor@example.com", "Ada Okafor")},
		{">Mar 1, 2026, 19:00<", shownAs("boss@example.com", "boss")},
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
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	main := findByTestID(t, parsePage(t, page), "report-draft")
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
		if hasTestID(parsePage(t, page), publicationOnly) {
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
	const want = "Owner is in the org of Priya Raman · 2 added"

	meta := textOf(findByTestID(t, getPage(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)), "report-meta"))
	if !strings.HasPrefix(meta, want+" · 2 Goals · ") {
		t.Errorf("the draft's meta line reads %q, want it to open with %s", meta, want)
	}
	if list := getBody(t, client, ts.URL+"/reports"); !strings.Contains(list, `<span data-testid="report-scope" class="small muted">`+want+`</span>`) {
		t.Errorf("the Reports list doesn't read the Org rule as %s:\n%s", want, list)
	}

	h.SaveReportDefinition(vp, domain.SaveReportDefinitionInput{
		Name:  "Outside Priya's org",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: domain.RuleOrg, Op: domain.RuleIsNot, Values: []string{strconv.FormatInt(vp.ID, 10)}}},
	})
	const notWant = "Owner is not in the org of Priya Raman"
	if list := getBody(t, client, ts.URL+"/reports"); !strings.Contains(list, `<span data-testid="report-scope" class="small muted">`+notWant+`</span>`) {
		t.Errorf("the Reports list doesn't read the is-not Org rule as %s:\n%s", notWant, list)
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
	for name, page := range map[string]string{"draft": renderNode(findByTestID(t, parsePage(t, draft), "report-draft")), "publication": getBody(t, client, pubURL(second))} {
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
	if got := label(unpublished); got != "30 days ago · Dec 3, 2025" {
		t.Errorf("unpublished chip label %q, want %q; chip:\n%s", got, "30 days ago · Dec 3, 2025", unpublished)
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
	// The session from before the clock moved has ended; sign in again.
	client = signInClient(t, ts.URL, "boss@example.com")

	if page := getBody(t, client, ts.URL+path); strings.Contains(page, "Showing what changed since") || strings.Count(page, `data-testid="report-baseline"`) != 1 {
		t.Errorf("the draft keeps the old baseline sentence or a baseline form outside the chip; body:\n%s", page)
	}
	published := chip("")
	if got := label(published); got != "last publication · Sep 22, 2026" {
		t.Errorf("published chip label %q, want %q; chip:\n%s", got, "last publication · Sep 22, 2026", published)
	}
	if item := pageElement(t, published, "a", "baseline-last-publication"); !strings.Contains(item, `href="`+path+`"`) {
		t.Errorf("Last publication does not link to the draft with no baseline; item:\n%s", item)
	}
	if item := pageElement(t, published, "a", "baseline-30-days"); !strings.Contains(item, `href="`+path+`?baseline=2026-09-04"`) {
		t.Errorf("30 days ago does not link to 2026-09-04; item:\n%s", item)
	}

	chosen := chip("?baseline=2026-01-01")
	if got := label(chosen); got != "Jan 1, 2026" {
		t.Errorf("chosen chip label %q, want %q; chip:\n%s", got, "Jan 1, 2026", chosen)
	}
	if !strings.Contains(chosen, `data-testid="baseline-last-publication"`) {
		t.Errorf("after a chosen date the chip no longer offers the last publication; chip:\n%s", chosen)
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
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	chip := pageElement(t, pageElement(t, page, "header", "draft-header"), "details", "baseline-chip")
	if got := chipLabel(t, chip); got != "last publication · Mar 1, 2026" {
		t.Errorf("chip label %q, want %q; chip:\n%s", got, "last publication · Mar 1, 2026", chip)
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
	const want = "4 Goals · 2 exceptions · 1 Highlight · changes since last publication · Sep 22, 2026. Readers can comment on the frozen copy."
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
	if got := pageElement(t, confirm, "p", "publish-summary"); !strings.Contains(got, "· changes since Jan 1, 2026.") {
		t.Errorf("publish summary %q does not read against the chosen date", got)
	}
	if form := pageElement(t, confirm, "form", "report-publish"); !strings.Contains(form, `action="`+path+`/publications?baseline=2026-01-01"`) {
		t.Errorf("the publish form does not carry the chosen baseline; form:\n%s", form)
	}
	if cancel := pageElement(t, confirm, "a", "publish-cancel"); !strings.Contains(cancel, `href="`+path+`?baseline=2026-01-01"`) {
		t.Errorf("Cancel does not keep the chosen baseline; link:\n%s", cancel)
	}
	resp := postForm(t, client, ts.URL+path+"/publications?baseline=2026-01-01", url.Values{})
	if line := pageElement(t, readBody(t, resp), "p", "report-published"); !strings.Contains(line, "changes since Jan 1, 2026") {
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
// and the Report log with the save's one entry — nothing else of the page.
// Saving again unchanged logs nothing. A plain post still reloads the draft
// (tickets #156, #166, #444, #473).
func TestSaveNarrativeLogsOnceAndRefreshesTheReportLogOverHTTP(t *testing.T) {
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
	for _, oob := range []string{"save-status", "publish-summary", "report-log"} {
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
	entries := textsOf(findAllByTestID(findByTestID(t, answer, "report-log"), "report-log-entry"))
	if len(entries) != len(logged)+1 || !strings.Contains(entries[0], "Narrative: picked “Signed the first EU customer.” into Accomplishments; Accomplishments notes changed") {
		t.Errorf("the Report log answered reads %q, want the save's one entry newest above the %d before", entries, len(logged))
	}

	_, body = postHX(t, client, reportURL+"/narrative", form)
	entries = textsOf(findAllByTestID(findByTestID(t, parsePage(t, body), "report-log"), "report-log-entry"))
	if after, err := h.Service.ReportLog(ctx, def.ID); err != nil || len(after) != len(logged)+1 || len(entries) != len(logged)+1 {
		t.Errorf("saving again unchanged leaves %d entries, answering %d (err %v), want the %d after the first save", len(after), len(entries), err, len(logged)+1)
	}

	plain := postForm(t, client, reportURL+"/narrative", form)
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

// Only the definition's creator, a Report Delegate or an Admin may edit it:
// anyone else signed in gets 403 on both the page and the post, which changes
// nothing. An unknown definition is Not found (tickets #151, #434).
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
	ticked := `name="field" value="` + id(legacy.ID) + `" checked`

	choice := between(t, getBody(t, client, editURL), `<fieldset data-testid="report-field-choice"`, `</fieldset>`)
	if !strings.Contains(choice, legacy.Name) || !strings.Contains(choice, ticked) {
		t.Errorf("the edit page doesn't list the Retired %s, ticked; fieldset:\n%s", legacy.Name, choice)
	}
	if !strings.Contains(choice, `name="field" value="`+id(budget.ID)+`"`) {
		t.Errorf("the edit page doesn't offer %s; fieldset:\n%s", budget.Name, choice)
	}
	if strings.Contains(choice, sunset.Name) {
		t.Errorf("the edit page offers the Retired %s, which the definition doesn't show; fieldset:\n%s", sunset.Name, choice)
	}

	form := url.Values{
		"name":   {""},
		"mode":   {domain.ReportModePicked},
		"picked": {id(goal.ID)},
		"field":  {id(legacy.ID)},
	}
	resp := postForm(t, noRedirects(client), editURL, form)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("save with no name: status %d, want 422", resp.StatusCode)
	}
	choice = between(t, readBody(t, resp), `<fieldset data-testid="report-field-choice"`, `</fieldset>`)
	if !strings.Contains(choice, ticked) {
		t.Errorf("the refused save doesn't list the Retired %s, ticked; fieldset:\n%s", legacy.Name, choice)
	}

	form.Set("name", "MBR renamed")
	form["field"] = []string{id(legacy.ID), id(sunset.ID)}
	resp = postForm(t, noRedirects(client), editURL, form)
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Sunset code is retired") {
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
	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	for _, tc := range []struct {
		g     domain.Goal
		reads string
	}{
		{worsened, "Yellow · 1 wk · was Green"},
		{recovered, "Green · was Yellow"},
		{staysRed, "Red · 1 wk"},
		{created, "Yellow · 1 wk"},
	} {
		block := parsePage(t, reportBlockFor(t, page, tc.g))
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
	if rule := cssRule(t, page, ".rp-health"); !strings.Contains(rule, "white-space:nowrap") {
		t.Errorf("a Health badge and its earlier Health can break apart: .rp-health{%s}", rule)
	}

	row := pageElement(t, getBody(t, client, ts.URL+"/reports"), "tr", "report-row")
	if want := "3 changes"; !strings.Contains(row, want) {
		t.Errorf("row lacks %q, counting the worsened, the recovered, and the created Goal; row:\n%s", want, row)
	}
}

// reportBlockFor is g's exception block on a Report page, failing the test
// when g has none.
func reportBlockFor(t *testing.T, page string, g domain.Goal) string {
	t.Helper()
	start := strings.Index(page, fmt.Sprintf(`<article data-testid="report-exception" class="rp-block" id="goal-%d"`, g.ID))
	if start < 0 {
		t.Fatalf("%s has no exception block", g.Title)
	}
	end := strings.Index(page[start:], "</article>")
	return page[start : start+end]
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
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, old.ID); err != nil {
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
		block := reportBlockFor(t, getBody(t, client, tc.url), g)
		if got := elementTexts(block, "span", "report-run-off-green"); !slices.Equal(got, []string{tc.reads}) {
			t.Errorf("%s's Run off Green reads %q, want %q", name, got, tc.reads)
		}
		run, prior := strings.Index(block, `data-testid="report-run-off-green"`), strings.Index(block, `data-testid="report-prior-health"`)
		if run < 0 || prior < 0 || run > prior {
			t.Errorf("%s does not show the Run off Green before the earlier Health; block:\n%s", name, block)
		}
	}
	block := reportBlockFor(t, getBody(t, client, fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, old.ID)), g)
	if !strings.Contains(block, "Blocked.") || strings.Contains(block, `data-testid="report-run-off-green"`) {
		t.Errorf("a publication from before the Run off Green does not render as published; block:\n%s", block)
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
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, old, before.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	publication := func(p domain.Publication) string {
		return getBody(t, client, fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, p.ID))
	}
	if got := elementTexts(reportBlockFor(t, publication(marked), g), "span", "report-prior-health"); !slices.Equal(got, []string{"· was Green"}) {
		t.Errorf("the publication's earlier Health reads %q, want the Green it was when published", got)
	}
	block := reportBlockFor(t, publication(before), g)
	if !strings.Contains(block, "Vendor is late.") || strings.Contains(block, `data-testid="report-prior-health"`) {
		t.Errorf("the older publication does not render its block as published; block:\n%s", block)
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

	if line := pageElement(t, getBody(t, client, pubPath), "p", "report-published"); !strings.Contains(line, "Published Jan 3, 2026, 00:04 by ") || strings.Contains(line, "UTC") {
		t.Errorf("the byline doesn't read Published Jan 3, 2026, 00:04, with no UTC:\n%s", line)
	}
	if title := between(t, getBody(t, client, pubPath+"/print"), "<title>", "</title>"); !strings.Contains(title, "Jan 3, 2026, 00:04") || strings.Contains(title, "UTC") {
		t.Errorf("the Print view's title is %q, want it dated Jan 3, 2026, 00:04", title)
	}
	if row := pageElement(t, getBody(t, client, ts.URL+"/reports"), "tr", "report-row"); !strings.Contains(row, `<span data-testid="report-published">Jan 3, 2026</span>`) || strings.Contains(row, "UTC") {
		t.Errorf("the Reports list doesn't date the publication Jan 3, 2026:\n%s", row)
	}
}

// A Report opens with a line counting what moved since its baseline, under its
// Health tiles on the draft and the publication and under the byline on Print;
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
	exceptions := func(page string) []string {
		var out []string
		for _, block := range findAllByTestID(parsePage(t, page), "report-exception") {
			out = append(out, textOf(findAll(block, func(n *nethtml.Node) bool { return n.Data == "h3" })[0]))
		}
		return out
	}
	movement := "Since the previous publication: 1 turned Red · 1 back to Green"

	draft := getBody(t, client, reportURL)
	if got := elementTexts(draft, "p", "report-movement"); !slices.Equal(got, []string{movement}) {
		t.Errorf("the draft's movement line reads %q, want %q", got, movement)
	}
	if tiles, line := strings.Index(draft, `data-testid="health-summary"`), strings.Index(draft, `data-testid="report-movement"`); tiles < 0 || line < tiles {
		t.Errorf("the draft's movement line doesn't come under its Health tiles")
	}
	if got := exceptions(draft); !slices.Equal(got, mostUrgentFirst) {
		t.Errorf("the draft's exceptions come %q, want %q", got, mostUrgentFirst)
	}
	if got := elementTexts(reportBlockFor(t, draft, turnedRed), "p", "report-changes"); !slices.Equal(got, []string{"Changed: Turned Red"}) {
		t.Errorf("the changed block's line reads %q, want [Changed: Turned Red]", got)
	}
	if got := elementTexts(reportBlockFor(t, draft, recovered), "span", "report-change"); !slices.Equal(got, []string{"Back to Green"}) {
		t.Errorf("the recovered block's changes read %q, want [Back to Green]", got)
	}
	if block := reportBlockFor(t, draft, staysRed); strings.Contains(block, `data-testid="report-changes"`) {
		t.Errorf("the standing Red block says something changed; block:\n%s", block)
	}

	againstDate := getBody(t, client, reportURL+"?baseline=2026-01-03")
	if want := "Since Jan 3, 2026: 1 turned Red · 1 back to Green"; !slices.Equal(elementTexts(againstDate, "p", "report-movement"), []string{want}) {
		t.Errorf("the draft against a date's movement line reads %q, want %q", elementTexts(againstDate, "p", "report-movement"), want)
	}

	pub := h.PublishReport(boss, def)
	pubURL := fmt.Sprintf("%s/publications/%d", reportURL, pub.ID)
	publication := getBody(t, client, pubURL)
	if got := elementTexts(publication, "p", "report-movement"); !slices.Equal(got, []string{movement}) {
		t.Errorf("the publication's movement line reads %q, want %q", got, movement)
	}
	if got := exceptions(publication); !slices.Equal(got, mostUrgentFirst) {
		t.Errorf("the publication's exceptions come %q, want %q", got, mostUrgentFirst)
	}
	print := getBody(t, client, pubURL+"/print")
	if got := elementTexts(print, "p", "report-movement"); !slices.Equal(got, []string{movement}) {
		t.Errorf("the Print page's movement line reads %q, want %q", got, movement)
	}
	if byline, line := strings.Index(print, `data-testid="report-published"`), strings.Index(print, `data-testid="report-movement"`); byline < 0 || line < byline {
		t.Errorf("the Print page's movement line doesn't come under its byline")
	}
	if got := exceptions(print); !slices.Equal(got, mostUrgentFirst) {
		t.Errorf("the Print page's exceptions come %q, want %q", got, mostUrgentFirst)
	}

	if want := "No movement since the previous publication."; !slices.Equal(elementTexts(getBody(t, client, reportURL), "p", "report-movement"), []string{want}) {
		t.Errorf("with nothing changed since, the draft's movement line reads %q, want %q", elementTexts(getBody(t, client, reportURL), "p", "report-movement"), want)
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
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, old, pub.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
	for name, page := range map[string]string{"publication": getBody(t, client, pubURL), "Print page": getBody(t, client, pubURL+"/print")} {
		if strings.Contains(page, `data-testid="report-movement"`) || strings.Contains(page, `data-testid="report-changes"`) {
			t.Errorf("the older %s shows a movement or a changes line it never had", name)
		}
		if !strings.Contains(reportBlockFor(t, page, g), "Vendor is gone.") {
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
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = json_remove(snapshot, '$.Scope') WHERE id = ?`, old.ID); err != nil {
		t.Fatalf("write a pre-selection snapshot: %v", err)
	}

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	pubPath := func(p domain.Publication) string {
		return ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(p.ID, 10)
	}
	for name, url := range map[string]string{"publication page": pubPath(pub), "print page": pubPath(pub) + "/print"} {
		page := getBody(t, client, url)
		line := pageElement(t, page, "p", "report-selection")
		if want := ">Selection: Owner: Dana Okafor · Active"; !strings.Contains(line, want) {
			t.Errorf("%s's selection line %q, want it to read %q", name, line, want)
		}
		published := strings.Index(page, `data-testid="report-published"`)
		selection := strings.Index(page, `data-testid="report-selection"`)
		if published < 0 {
			t.Errorf("%s has no report-published line", name)
		}
		if selection < 0 {
			t.Errorf("%s has no report-selection line", name)
		}
		if published < 0 || selection < 0 {
			continue
		}
		if selection < published {
			t.Errorf("%s shows the selection line before the published line", name)
		}
		if movement := strings.Index(page, `data-testid="report-movement"`); movement >= 0 && movement < selection {
			t.Errorf("%s shows the movement line before the selection line", name)
		}
	}
	for name, url := range map[string]string{
		"old publication page": pubPath(old),
		"old print page":       pubPath(old) + "/print",
		"draft":                ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10),
	} {
		if page := getBody(t, client, url); strings.Contains(page, `data-testid="report-selection"`) {
			t.Errorf("%s has a selection line", name)
		}
	}
}

// linkedBlockOf is the exception block a Report page gives g, up to its
// close.
func linkedBlockOf(t *testing.T, page string, g domain.Goal) string {
	t.Helper()
	start := strings.Index(page, fmt.Sprintf(`<article data-testid="report-exception" class="rp-block" id="goal-%d">`, g.ID))
	if start < 0 {
		t.Fatalf("page has no exception block for %q; body:\n%s", g.Title, page)
	}
	end := strings.Index(page[start:], "</article>")
	if end < 0 {
		t.Fatalf("exception block for %q is not closed", g.Title)
	}
	return page[start : start+end]
}

// linkedGoalLink is the report-linked-goal link in s to href with its text
// and what follows it up to the next entry, or "" when there is none.
func linkedGoalLink(s, href string) string {
	start := strings.Index(s, `<a data-testid="report-linked-goal" href="`+href+`">`)
	if start < 0 {
		return ""
	}
	rest := s[start:]
	if next := strings.Index(rest[1:], `<a data-testid="report-linked-goal"`); next >= 0 {
		rest = rest[:next+1]
	}
	return rest
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
		body := getBody(t, client, page.url)
		referral := linkedBlockOf(t, body, s.Referral)
		if got := elementTexts(referral, "span", "report-rollup-count"); !slices.Equal(got, []string{"2 of 5 Red · 1 Yellow"}) {
			t.Errorf("%s: Referral program count %q, want 2 of 5 Red · 1 Yellow", page.name, got)
		}
		behind := pageElement(t, referral, "span", "report-behind-it")
		// A linked Goal reads [Health] Title: on screen its chip, printed its
		// shape and word.
		alpha := linkTo(parsePage(t, behind), fmt.Sprintf("#goal-%d", s.Alpha.ID))
		if alpha == nil || textOf(alpha) != "Alpha ledger ↓" {
			t.Fatalf("%s: Supported by does not link Alpha ledger ↓ to its block; line:\n%s", page.name, behind)
		}
		alphaHealth := healthChipIn(t, alpha.Parent)
		if !precedes(alphaHealth, alpha) {
			t.Errorf("%s: Supported by doesn't read Alpha ledger's Health before its title; line:\n%s", page.name, behind)
		}
		if page.name == "print" {
			if shown := shownTextOf(alphaHealth); shown != "Red" {
				t.Errorf("print: Alpha ledger's Health shows %q, want its shape beside Red: %s", shown, renderNode(alphaHealth))
			}
		} else {
			assertHealthChip(t, alphaHealth, "Red")
		}
		if beta := linkedGoalLink(behind, fmt.Sprintf("/goals/%d", s.Beta.ID)); !strings.HasPrefix(beta, fmt.Sprintf(`<a data-testid="report-linked-goal" href="/goals/%d">Beta payouts</a>`, s.Beta.ID)) {
			t.Errorf("%s: Supported by does not link Beta payouts to its Goal page; line:\n%s", page.name, behind)
		}
		epsilon := fmt.Sprintf("#goal-%d", s.Epsilon.ID)
		if linkedGoalLink(behind, epsilon) == "" {
			t.Errorf("%s: Supported by does not link Epsilon signup to its line; line:\n%s", page.name, behind)
		}
		if !strings.Contains(body, fmt.Sprintf(`<tr data-testid="selected-goal" id="goal-%d">`, s.Epsilon.ID)) {
			t.Errorf("%s: %s does not resolve to Epsilon signup's on-track line", page.name, epsilon)
		}
		if strings.Contains(behind, "Delta copy") {
			t.Errorf("%s: Supported by names the Green Delta copy outside the Report; line:\n%s", page.name, behind)
		}
		contributes := pageElement(t, referral, "p", "report-contributes-to")
		if growth := linkedGoalLink(contributes, fmt.Sprintf("#goal-%d", s.Growth.ID)); !strings.HasPrefix(growth, fmt.Sprintf(`<a data-testid="report-linked-goal" href="#goal-%d">Growth ↓</a>`, s.Growth.ID)) {
			t.Errorf("%s: Contributes to does not link Growth ↓ to its block; line:\n%s", page.name, contributes)
		}
		alphaContributes := pageElement(t, linkedBlockOf(t, body, s.Alpha), "p", "report-contributes-to")
		if linkedGoalLink(alphaContributes, fmt.Sprintf("#goal-%d", s.Referral.ID)) == "" {
			t.Errorf("%s: Alpha ledger's Contributes to does not link Referral program; line:\n%s", page.name, alphaContributes)
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
	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(s.Definition.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))
	referral := linkedBlockOf(t, page, s.Referral)
	if got := elementTexts(referral, "p", "report-rollup"); len(got) != 1 || !strings.Contains(got[0], "Red · 1 of 3 overdue") {
		t.Errorf("Rolled-up Health %q, want Red · 1 of 3 overdue", got)
	}
	for _, testID := range []string{"report-behind-it", "report-contributes-to"} {
		if strings.Contains(page, `data-testid="`+testID+`"`) {
			t.Errorf("publication from before linked Goals shows %s; body:\n%s", testID, page)
		}
	}
}

// A block whose only child has no Health yet has no Rolled-up Health to count,
// so its Supported by line opens the paragraph rather than breaking a line
// under nothing (#246).
func TestReportSupportedByAloneOpensItsParagraph(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	parent := h.ActiveGoal(boss, "Growth", "Grow the business.")
	child := h.ActiveChildOf(boss, parent, "Referral program", "Customers bring customers.")
	h.Checkin(boss, parent.ID, domain.HealthRed, "Off track.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Growth", Mode: domain.ReportModePicked, Picked: []int64{parent.ID, child.ID}})

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	block := linkedBlockOf(t, getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)), parent)
	rollup := pageElement(t, block, "p", "report-rollup")
	if want := `<p data-testid="report-rollup"><span data-testid="report-behind-it"><strong>Supported by:</strong>`; !strings.HasPrefix(rollup, want) {
		t.Errorf("Rolled-up Health paragraph does not open with Supported by; paragraph:\n%s", rollup)
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
		page := getBody(t, client, tc.url)
		want := map[string]string{
			asking.Title: tc.of,
			alone.Title:  "Asks for help · no one above the Owner",
			quiet.Title:  "",
		}
		var blocks []string
		for _, block := range strings.Split(page, `<article data-testid="report-exception"`)[1:] {
			block, _, _ = strings.Cut(block, "</article>")
			blocks = append(blocks, block)
		}
		if len(blocks) != len(want) {
			t.Fatalf("%s: %d exception blocks, want %d:\n%s", name, len(blocks), len(want), page)
		}
		for _, block := range blocks {
			for title, ask := range want {
				if !strings.Contains(block, title) {
					continue
				}
				asks := elementTexts(block, "p", "report-ask-for-help")
				if ask == "" && len(asks) != 0 {
					t.Errorf("%s: %q, which doesn't ask, shows %q", name, title, asks)
				}
				if ask != "" && (len(asks) != 1 || asks[0] != ask) {
					t.Errorf("%s: %q shows %q, want %q", name, title, asks, ask)
				}
			}
		}
		if n := strings.Count(page, `<p data-testid="report-ask-for-help"><strong>Asks for help</strong>`); n != 2 {
			t.Errorf("%s: %d asks for help open with a strong Asks for help, want 2:\n%s", name, n, page)
		}
		if path, ask := strings.Index(page, "Need counsel budget."), strings.Index(page, `data-testid="report-ask-for-help"`); path < 0 || ask < path {
			t.Errorf("%s: the ask doesn't follow the Path to Green:\n%s", name, page)
		}
	}
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
func assertReportBlockOverdue(t *testing.T, page string, want bool) {
	t.Helper()
	block := pageElement(t, page, "article", "report-exception")
	beta := pageElement(t, block, "li", "report-milestone")
	if overdue := strings.Contains(beta, `data-testid="milestone-mark"`) && strings.Contains(beta, ">Overdue</span>"); overdue != want {
		t.Errorf("Beta marked Overdue %v, want %v:\n%s", overdue, want, beta)
	}
	status := pageElement(t, block, "div", "report-status-box")
	if overdue := slices.Contains(strings.Fields(attr(openTag(status), "class")), "overdue"); overdue != want {
		t.Errorf("status box has class overdue %v, want %v:\n%s", overdue, want, openTag(status))
	}
	if badge := strings.Contains(status, `data-testid="path-overdue"`); badge != want {
		t.Errorf("status box shows the Overdue badge %v, want %v:\n%s", badge, want, status)
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
			assertReportBlockOverdue(t, getBody(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, def.ID)), tc.overdue)
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
	assertReportBlockOverdue(t, getBody(t, client, pubPath), true)
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
		{"twice", []time.Time{time.Date(2026, 3, 13, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)}, "Mar 13, 2026", "(2)"},
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
				path := pageElement(t, reportBlockFor(t, getBody(t, client, url), g), "p", "report-path-to-green")
				if got := elementTexts(path, "del", "report-back-to-green-prior"); !slices.Equal(got, slippedBackToGreenTexts(tc.prior)) {
					t.Errorf("%s strikes through %q, want %q; path:\n%s", name, got, slippedBackToGreenTexts(tc.prior), path)
				}
				if got := elementTexts(path, "span", "report-back-to-green-slips"); !slices.Equal(got, slippedBackToGreenTexts(tc.count)) {
					t.Errorf("%s counts the slips as %q, want %q; path:\n%s", name, got, slippedBackToGreenTexts(tc.count), path)
				}
				if !strings.HasSuffix(strings.TrimSpace(path), ")") {
					t.Errorf("%s's back-to-Green date does not close its parenthesis; path:\n%s", name, path)
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
		{"slipped twice, back once", []time.Time{jun(16), jun(23)}, jun(16), "Jun 9, 2026", "(2)"},
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
				path := pageElement(t, reportBlockFor(t, getBody(t, client, url), g), "p", "report-path-to-green")
				if got := elementTexts(path, "del", "report-back-to-green-prior"); !slices.Equal(got, slippedBackToGreenTexts(tc.prior)) {
					t.Errorf("%s strikes through %q, want %q; path:\n%s", name, got, slippedBackToGreenTexts(tc.prior), path)
				}
				if got := elementTexts(path, "span", "report-back-to-green-slips"); !slices.Equal(got, slippedBackToGreenTexts(tc.count)) {
					t.Errorf("%s counts the slips as %q, want %q; path:\n%s", name, got, slippedBackToGreenTexts(tc.count), path)
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
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, frozen, pub.ID); err != nil {
		t.Fatalf("write a frozen snapshot: %v", err)
	}

	ts := newServer(t, h)
	block := reportBlockFor(t, getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)), g)
	path := pageElement(t, block, "p", "report-path-to-green")
	if got, want := elementTexts(path, "del", "report-back-to-green-prior"), []string{"Jun 9, 2026"}; !slices.Equal(got, want) {
		t.Errorf("the publication strikes through %q, want %q; path:\n%s", got, want, path)
	}
	if got, want := elementTexts(path, "span", "report-back-to-green-slips"), []string{"(2)"}; !slices.Equal(got, want) {
		t.Errorf("the publication counts the slips as %q, want %q; path:\n%s", got, want, path)
	}
	if want := `by Jun 16, 2026 <del data-testid="report-back-to-green-prior">Jun 9, 2026</del> <span data-testid="report-back-to-green-slips">(2)</span>)`; !strings.Contains(path, want) {
		t.Errorf("the path doesn't read %s; path:\n%s", want, path)
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
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}

	ts := newServer(t, h)
	block := reportBlockFor(t, getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)), g)
	path := pageElement(t, block, "p", "report-path-to-green")
	if got, want := elementTexts(block, "p", "report-path-to-green"), []string{"Path to Green: Hire counsel. (back to Green by Mar 13, 2026)"}; !slices.Equal(got, want) {
		t.Errorf("an older publication's Path to Green reads %q, want %q", got, want)
	}
	if strings.Contains(path, "<del") {
		t.Errorf("an older publication strikes a back-to-Green date through; path:\n%s", path)
	}
}

// reportsActionItemRows returns the rows of one group of the Reports page's
// Open Action Items, in order: testID names the group's list.
func reportsActionItemRows(t *testing.T, page, testID string) []string {
	t.Helper()
	list := pageElement(t, page, "ul", testID)
	var rows []string
	for _, part := range strings.Split(list, `<li data-testid="reports-action-item"`)[1:] {
		row, _, _ := strings.Cut(part, "</li>")
		rows = append(rows, row)
	}
	return rows
}

// The Reports page lists, above its table, the open Action Items its viewer
// has reason to chase: Yours, where they created the Report Definition,
// published, raised or own the item, and My org, where only someone in their
// Org did. Each row reads like Home's, with its owner, and closes nothing.
func TestReportsPageListsOpenActionItemsToChase(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
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
		item, err := h.Service.RaiseActionItem(ctx, author.ID, domain.RaiseActionItemInput{
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
	page := getBody(t, signInClient(t, ts.URL, "viewer@example.com"), ts.URL+"/reports")

	section := strings.Index(page, `<section data-testid="reports-action-items"`)
	table := strings.Index(page, `data-testid="report-table"`)
	if section < 0 || table < 0 || section > table {
		t.Fatalf("Open Action Items at %d, Reports table at %d; want the section above the table:\n%s", section, table, page)
	}
	if !strings.Contains(page[section:table], "<h2>Open Action Items</h2>") {
		t.Errorf("the section is not headed Open Action Items:\n%s", page[section:table])
	}
	// Open Action Items and the Reports table are plain sections on the
	// canvas, neither a card (DESIGN.md § System: One surface).
	if tag := openTag(page[section:]); hasClass(tag, "card") {
		t.Errorf("Open Action Items is a card: %s", tag)
	}
	if tag := openTag(page[strings.LastIndex(page[:table], "<div"):]); hasClass(tag, "card") {
		t.Errorf("the Reports table's container is a card: %s", tag)
	}
	if strings.Contains(page, "Nobody's to chase.") {
		t.Errorf("the page lists an item nobody in the viewer's Org is party to")
	}

	yours := reportsActionItemRows(t, page, "reports-action-items-yours")
	org := reportsActionItemRows(t, page, "reports-action-items-org")
	if len(yours) != 1 || len(org) != 1 {
		t.Fatalf("Yours lists %d, My org %d; want one each:\n%s", len(yours), len(org), page[section:table])
	}
	// Each group opens with a header row naming it and counting its items.
	for _, want := range []string{`<h3 class="rp-group">Yours<span class="rp-group-count">1</span></h3>`, `<h3 class="rp-group">My org<span class="rp-group-count">1</span></h3>`} {
		if !strings.Contains(page[section:table], want) {
			t.Errorf("Open Action Items lacks the group heading %s", want)
		}
	}
	for _, want := range []string{
		"Explain the slip.",
		fmt.Sprintf(`href="/goals/%d#action-items"`, goal.ID),
		`<span data-testid="reports-action-item-overdue" class="badge r">Overdue</span>`,
		"MBR · Launch in EU · due " + h.Clock.Now().AddDate(0, 0, -2).Format("Jan 2, 2006"),
		shownAs("outsider@example.com", "outsider"),
	} {
		if !strings.Contains(yours[0], want) {
			t.Errorf("Yours row lacks %s:\n%s", want, yours[0])
		}
	}
	for _, want := range []string{
		"Send the vendor plan.",
		fmt.Sprintf(`href="/reports/%d/publications/%d#action-items"`, orgs.DefinitionID, orgs.PublicationID),
		"QBR · due " + h.Clock.Now().AddDate(0, 0, 7).Format("Jan 2, 2006"),
		shownAs("mid@example.com", "mid"),
	} {
		if !strings.Contains(org[0], want) {
			t.Errorf("My org row lacks %s:\n%s", want, org[0])
		}
	}
	if strings.Contains(org[0], "reports-action-item-overdue") {
		t.Errorf("an item due next week is flagged Overdue:\n%s", org[0])
	}
	if strings.Contains(page[section:table], "<form") {
		t.Errorf("Open Action Items offers a form; Close and Reassign stay on the publication and Home:\n%s", page[section:table])
	}
	if nav := pageElement(t, page, "a", "nav-reports"); strings.Contains(nav, `class="count"`) {
		t.Errorf("the Reports nav item carries a count: %s", nav)
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
	page := getBody(t, signInClient(t, ts.URL, "reader@example.com"), ts.URL+"/reports")
	if !strings.Contains(page, `data-testid="report-table"`) {
		t.Fatalf("the Reports page has no table:\n%s", page)
	}
	for _, absent := range []string{"reports-action-items", "Open Action Items", "Send the vendor plan."} {
		if strings.Contains(page, absent) {
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
		{"the Reports list", "/reports", `data-testid="reports-action-item"`},
		{"a draft", draftPath, `data-testid="open-action-item"`},
		{"a publication", draftPath + "/publications/" + strconv.FormatInt(pub.ID, 10), `data-testid="comment-thread"`},
		{"the new builder", "/reports/new", `data-testid="report-matches"`},
		{"the edit builder", draftPath + "/edit", `data-testid="report-matches"`},
	} {
		page := getBody(t, client, ts.URL+tc.path)
		if !strings.Contains(page, tc.shows) {
			t.Fatalf("%s doesn't show what it should be checked with (%s):\n%s", tc.page, tc.shows, page)
		}
		for _, m := range regexp.MustCompile(`<[a-z]+[^>]* class="([^"]*)"`).FindAllStringSubmatch(page, -1) {
			if slices.Contains(strings.Fields(m[1]), "card") {
				t.Errorf("%s has a card: %s", tc.page, openTag(m[0]+">"))
			}
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
	delegates := func(page string) []string {
		return elementTexts(page, "li", "report-delegate")
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
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.String() != draftURL {
		t.Fatalf("adding Dana: status %d at %s, want the draft", resp.StatusCode, resp.Request.URL)
	}
	if got := delegates(page); len(got) != 1 || !strings.Contains(got[0], "dana@example.com") {
		t.Errorf("Report Delegates after adding Dana = %q, want Dana", got)
	}

	resp = postForm(t, creator, draftURL+"/delegates", url.Values{"email": {"nobody@example.com"}})
	page = readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("adding an unknown email: status %d, want 422", resp.StatusCode)
	}
	refusedPage := parsePage(t, page)
	if refused := textOf(findByTestID(t, refusedPage, "report-delegate-refused")); !strings.Contains(refused, "nobody@example.com") {
		t.Errorf("refusal %q doesn't name the email", refused)
	}
	if email := formField(t, findByTestID(t, refusedPage, "add-report-delegate"), "email"); email.Value != "nobody@example.com" {
		t.Errorf("the refused email isn't kept in the form: it holds %q", email.Value)
	}

	client := signInClient(t, ts.URL, "dana@example.com")
	page = getBody(t, client, draftURL)
	if got := delegates(page); len(got) != 1 {
		t.Errorf("Dana's draft lists Report Delegates %q, want Dana", got)
	}
	if delegatePage := parsePage(t, page); hasTestID(delegatePage, "add-report-delegate") || hasTestID(delegatePage, "remove-report-delegate") {
		t.Errorf("Dana, a Report Delegate, is offered to add or remove Report Delegates")
	}
	for path, form := range map[string]url.Values{
		"/delegates":        {"email": {"sam@example.com"}},
		"/delegates/remove": {"account": {strconv.FormatInt(dana.ID, 10)}},
	} {
		resp := postForm(t, noRedirects(client), draftURL+path, form)
		if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden {
			t.Errorf("Dana's POST to %s: status %d, want 403; body:\n%s", path, resp.StatusCode, body)
		}
	}

	resp = postForm(t, creator, draftURL+"/delegates/remove", url.Values{"account": {strconv.FormatInt(dana.ID, 10)}})
	page = readBody(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.String() != draftURL {
		t.Fatalf("removing Dana: status %d at %s, want the draft", resp.StatusCode, resp.Request.URL)
	}
	if got := delegates(page); len(got) != 0 {
		t.Errorf("Report Delegates after removing Dana = %q, want none", got)
	}
}

// The draft page's Report log shows anyone who can open the Report every
// change to its Definition, newest first, each with who made it and when in
// the company's timezone (CONTEXT.md: Report log; ticket #435).
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
	section := findByTestID(t, page, "report-log")
	got := textsOf(findAllByTestID(section, "report-log-entry"))
	// Jan 2, 2026, 15:04 UTC is 00:04 the next day in Tokyo.
	want := []string{
		"Title: EU MBR → EU monthly review — Dana Cole at Jan 3, 2026, 01:04",
		"Report Delegate added: Dana Cole — Priya Raman at Jan 3, 2026, 00:04",
		"Created — Priya Raman at Jan 3, 2026, 00:04",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Sam's Report log reads %q, want %q", got, want)
	}
	if hasTestID(section, "report-log-more") {
		t.Errorf("a short Report log hides some of its entries behind Show all")
	}
}

// A long Report log shows its newest 20 entries, and the rest behind Show
// all.
func TestALongReportLogShowsTheNewestTwentyFirst(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	priya := h.SignIn("priya@example.com")
	g := h.ActiveGoal(priya, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{Name: "Rename 0", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	for i := 1; i <= 24; i++ {
		if _, err := h.Service.UpdateReportDefinition(context.Background(), priya.ID, def.ID, domain.SaveReportDefinitionInput{
			Name: fmt.Sprintf("Rename %d", i), Mode: domain.ReportModePicked, Picked: []int64{g.ID},
		}); err != nil {
			t.Fatalf("UpdateReportDefinition: %v", err)
		}
	}
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "priya@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	section := findByTestID(t, page, "report-log")
	more := findByTestID(t, section, "report-log-more")
	all := findAllByTestID(section, "report-log-entry")
	rest := findAllByTestID(more, "report-log-entry")
	if len(all) != 25 || len(rest) != 5 {
		t.Fatalf("the Report log shows %d entries, %d behind Show all; want 25, 5 behind it", len(all), len(rest))
	}
	if first := textOf(all[0]); !strings.HasPrefix(first, "Title: Rename 23 → Rename 24") {
		t.Errorf("the newest entry reads %q, want the last rename", first)
	}
	if shown := textOf(more); !strings.HasPrefix(shown, "Show all 25 ") {
		t.Errorf("the rest of the Report log reads %q, want it behind Show all 25", shown)
	}
	if last := textOf(rest[len(rest)-1]); !strings.HasPrefix(last, "Created") {
		t.Errorf("the oldest entry reads %q, want Created", last)
	}
}
