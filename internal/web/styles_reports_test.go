package web_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// From 1280px wide the draft header holds one row whatever the save status
// says (ticket #455). It doesn't wrap. The save status doesn't break; it keeps
// room for Unsaved changes, so the header doesn't shift as it changes, and
// takes only the room left over beyond that, cutting a long Not saved reason
// short rather than squeezing the Report's name. The baseline chip and History,
// Edit definition and Publish… keep their width, so the name gives way, cut
// short. Narrower, beside the sidebar there is no room for one row, and the
// header wraps.
func TestReportDraftHeaderKeepsOneRowStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	header := findByTestID(t, parsePage(t, page), "draft-header")
	for testID, class := range map[string]string{"save-status": "rp-save", "baseline-chip": "rp-chip"} {
		if n := findByTestID(t, header, testID); !slices.Contains(strings.Fields(attrOf(n, "class")), class) {
			t.Errorf("the header's %s has classes %q, want %s", testID, attrOf(n, "class"), class)
		}
	}
	if len(findAll(header, func(n *html.Node) bool { return slices.Contains(strings.Fields(attrOf(n, "class")), "rp-head-actions") })) != 1 {
		t.Errorf("the header has no rp-head-actions holding History, Edit definition and Publish…")
	}

	styles := between(t, page, ".rp-draft-head{", "</style>")
	wide := mediaBlock(t, styles, "@media (min-width:1280px)")
	for sel, wants := range map[string][]string{
		".rp-draft-head":                  {"flex-wrap:nowrap"},
		".rp-draft-head .rp-save":         {"white-space:nowrap", "flex-basis:9em", "min-width:9em", "overflow:hidden", "text-overflow:ellipsis"},
		".rp-draft-head .rp-chip":         {"flex-shrink:0"},
		".rp-draft-head .rp-head-actions": {"flex-shrink:0"},
	} {
		decls, ok := ruleFor(wide, sel)
		for _, want := range wants {
			if !ok || !strings.Contains(decls, want) {
				t.Errorf("from 1280px %s{%s} doesn't declare %s", sel, decls, want)
			}
		}
	}
	if decls, ok := ruleFor(styles, ".rp-draft-head"); !ok || !strings.Contains(decls, "flex-wrap:wrap") {
		t.Errorf(".rp-draft-head{%s} doesn't wrap where one row doesn't fit", decls)
	}
	// The name is what gives way: its breadcrumb shrinks and cuts it short.
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if decls, ok := ruleFor(css, ".ph-title"); !ok || !strings.Contains(decls, "min-width:0") {
		t.Errorf(".ph-title{%s} doesn't shrink", decls)
	}
	if decls, ok := ruleFor(between(t, page, "<style>", "</style>"), ".rp-crumb"); !ok || !strings.Contains(decls, "text-overflow:ellipsis") || !strings.Contains(decls, "min-width:0") {
		t.Errorf(".rp-crumb{%s} doesn't cut a long name short", decls)
	}
}

// The draft's body is two columns, the Report's main column at most 780px
// beside a 300px one for its Delegates and History, 56px apart, in a page
// wide enough to hold them; its Goal sections follow in a row of their own,
// spanning both columns and the gap (#713), 24px under the main column as
// they read inside it before. Under 960px of the page's content width — a
// container query on the page body, so the sidebar counts — the page is one
// column, the narrow one under the Goal sections at full width, 40px apart
// (#473), so a 1280px window keeps them side by side (#477).
func TestReportDraftColumnsStackUnder960pxStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	doc := parsePage(t, page)
	main, goals, side := findByTestID(t, doc, "report-draft"), findByTestID(t, doc, "report-goals"), findByTestID(t, doc, "draft-side")
	body := main.Parent
	if side.Parent != body || goals.Parent != body || !slices.Contains(strings.Fields(attrOf(body, "class")), "rp-draft-page") || attrOf(body.Parent, "class") != "page" {
		t.Fatalf("the main and narrow columns and the Goal sections aren't the page body's .rp-draft-page's own: %s", renderNode(body))
	}
	if !precedes(main, goals) || !precedes(goals, side) {
		t.Errorf("the Goal sections don't come between the main and narrow columns, so they wouldn't stack in that order")
	}
	assertReportsCarriesClass(t, "the narrow column", side, "rp-side")
	assertReportsCarriesClass(t, "the Goal sections", goals, "rp-goals")

	styles := between(t, page, "<style>", "</style>")
	for sel, wants := range map[string][]string{
		".page:has(>.rp-draft-page)": {"container:rp-draft/inline-size", "max-width:1200px"},
		".rp-draft-page":             {"display:grid", "grid-template-columns:minmax(0,780px) minmax(0,300px)", "gap:24px 56px"},
		".rp-draft-page>*":           {"min-width:0"},
		".rp-draft-page>.rp-side":    {"grid-column:2", "grid-row:1"},
		".rp-draft-page>.rp-goals":   {"grid-column:1/-1", "grid-row:2"},
	} {
		decls, ok := ruleFor(styles, sel)
		for _, want := range wants {
			if !ok || !strings.Contains(decls, want) {
				t.Errorf("%s{%s} doesn't declare %s", sel, decls, want)
			}
		}
	}
	narrow := mediaBlock(t, styles, "@container rp-draft (max-width:960px)")
	if decls, ok := ruleFor(narrow, ".rp-draft-page"); !ok || !strings.Contains(decls, "grid-template-columns:minmax(0,1fr)") {
		t.Errorf("under 960px .rp-draft-page{%s} isn't one full-width column", decls)
	}
	// 24px between the narrative and the Goal sections, and with the narrow
	// column's 16px margin, 40px above it (#715).
	if decls, _ := ruleFor(narrow, ".rp-draft-page"); !slices.Contains(strings.Split(decls, ";"), "gap:24px") {
		t.Errorf("under 960px .rp-draft-page{%s} doesn't set its rows 24px apart", decls)
	}
	assertReportsRuleDeclares(t, narrow, ".rp-draft-page>.rp-side", "grid-column:auto", "grid-row:auto", "margin-top:16px")
	assertReportsRuleDeclares(t, narrow, ".rp-draft-page>.rp-goals", "grid-row:auto")
}

// The draft's History hangs its entries off a rule, each on a marker: the
// Draft's hollow, a publication's filled with the accent, and any other
// Report log entry's plain (#474).
func TestReportDraftHistoryMarkersStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.Clock.Advance(time.Hour)
	h.PublishReport(boss, def)
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	history := findByTestID(t, parsePage(t, page), "report-history")
	markOf := func(entry *html.Node) []string {
		marks := findAll(entry, func(n *html.Node) bool { return slices.Contains(strings.Fields(attrOf(n, "class")), "rp-mark") })
		if len(marks) != 1 {
			t.Fatalf("%q has %d markers, want 1", textOf(entry), len(marks))
		}
		return strings.Fields(attrOf(marks[0], "class"))
	}
	entries := findAllByTestID(history, "report-history-entry")
	if len(entries) != 2 {
		t.Fatalf("History has %d entries after the Draft, want the publication and Created", len(entries))
	}
	for name, c := range map[string]struct {
		entry *html.Node
		want  []string
	}{
		"the Draft's":            {findByTestID(t, history, "report-history-draft"), []string{"rp-mark", "rp-mark-draft"}},
		"the publication's":      {entries[0], []string{"rp-mark", "rp-mark-published"}},
		"the Report log entry's": {entries[1], []string{"rp-mark"}},
	} {
		if got := markOf(c.entry); !slices.Equal(got, c.want) {
			t.Errorf("%s marker is %q, want %q", name, got, c.want)
		}
	}

	styles := between(t, page, "<style>", "</style>")
	for sel, wants := range map[string][]string{
		".rp-history-list":   {"border-left:1px solid var(--color-border)"},
		".rp-mark":           {"position:absolute", "border-radius:50%", "background:var(--color-ink-muted)"},
		".rp-mark-draft":     {"background:var(--color-canvas)", "border:1.5px solid var(--color-ink-muted)"},
		".rp-mark-published": {"background:var(--color-primary)"},
	} {
		decls, ok := ruleFor(styles, sel)
		for _, want := range wants {
			if !ok || !strings.Contains(decls, want) {
				t.Errorf("%s{%s} doesn't declare %s", sel, decls, want)
			}
		}
	}
}

// Opened, the History entries behind Show all scroll inside a list at most
// 320px tall, so they lengthen the narrow column only so far; the newest five
// above it aren't capped (#715, TestALongDraftHistoryShowsTheNewestFiveFirst).
// A box around the list scrolls, not the list drawing the timeline, so the
// markers hanging off its rule aren't cut in half (#716).
func TestReportDraftOlderHistoryScrollsInPlaceStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR 0", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	for i := 1; i <= 6; i++ {
		h.Clock.Advance(time.Minute)
		if _, err := h.Service.UpdateReportDefinition(context.Background(), boss.ID, def.ID, domain.SaveReportDefinitionInput{
			Name: fmt.Sprintf("MBR %d", i), Mode: domain.ReportModePicked, Picked: []int64{g.ID},
		}); err != nil {
			t.Fatalf("UpdateReportDefinition: %v", err)
		}
	}
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	history := findByTestID(t, parsePage(t, page), "report-history")
	older := findAllByTestID(findByTestID(t, history, "report-history-more"), "report-history-entry")
	if len(older) == 0 {
		t.Fatalf("History has nothing behind Show all: %s", renderNode(history))
	}
	list, scroller := older[0].Parent, older[0].Parent.Parent
	assertReportsCarriesClass(t, "the list behind Show all", list, "rp-history-list")
	assertReportsCarriesClass(t, "the box the list behind Show all scrolls in", scroller, "rp-history-older")
	if flatFrameHasClass(list, "rp-history-older") {
		t.Errorf("the list behind Show all scrolls itself, so it clips its own markers: %s", renderNode(list))
	}
	assertReportsCarriesClass(t, "the newest entries' list", findByTestID(t, history, "report-history-draft").Parent, "rp-history-list")
	if flatFrameHasClass(findByTestID(t, history, "report-history-draft").Parent, "rp-history-older") {
		t.Errorf("the newest entries' list carries rp-history-older, so it would scroll too")
	}

	styles := between(t, page, "<style>", "</style>")
	assertReportsRuleDeclares(t, styles, ".rp-history-older", "max-height:320px", "overflow-y:auto")
	timeline, _ := ruleFor(styles, ".rp-history-list")
	if !strings.Contains(timeline, "border-left") {
		t.Errorf(".rp-history-list{%s} doesn't draw the timeline", timeline)
	}
	for _, scrolls := range []string{"max-height", "overflow"} {
		if strings.Contains(timeline, scrolls) {
			t.Errorf(".rp-history-list{%s} declares %s, so the timeline would scroll and clip its markers", timeline, scrolls)
		}
	}
	// A marker hangs 4.5px left of the rule, and its ring 2px more: the box
	// scrolling the older entries pads them clear of its clipping edge, and
	// pulls back by as much so their rule lines up with the newest entries'.
	box, _ := ruleFor(styles, ".rp-history-older")
	px := func(v string) int {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "px"))
		if err != nil {
			t.Fatalf("%q isn't a length in px", v)
		}
		return n
	}
	pad := px(declValue(t, box, "padding-left"))
	if pad < 8 {
		t.Errorf(".rp-history-older{%s} pads its entries %dpx, too little to keep their markers whole; want at least 8px", box, pad)
	}
	if pull := px(declValue(t, box, "margin-left")); pull != -pad {
		t.Errorf(".rp-history-older{%s} pulls back %dpx for %dpx of padding, so the older entries' rule doesn't line up with the newest entries'", box, -pull, pad)
	}
}

// A Goal block's facts rail sets each label — Owner, Metrics, Rolled-up
// Health, Supported by — over what it labels (#712). Its one tinted box is a
// Red or Yellow Goal's Path to Green, in that Health's tint with its label in
// the Health's ink: no other rule tints it (#475).
func TestReportBlockRailLabelsAndPathTintStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	block := findByTestID(t, parsePage(t, page), "report-exception")
	for _, testID := range []string{"report-owner", "report-due"} {
		n := findByTestID(t, block, testID)
		for n.Data != "dd" {
			n = n.Parent
		}
		row := n.Parent
		dt := row.FirstChild
		for dt != nil && dt.Type != html.ElementNode {
			dt = dt.NextSibling
		}
		if rail := row.Parent; rail.Data != "dl" || !slices.Contains(strings.Fields(attrOf(rail, "class")), "rp-rail") ||
			dt == nil || dt.Data != "dt" || !slices.Contains(strings.Fields(attrOf(dt, "class")), "label") {
			t.Errorf("%s isn't a labelled item of the block's .rp-rail: %s", testID, renderNode(row.Parent))
		}
	}

	styles := between(t, page, "<style>", "</style>")
	for sel, wants := range map[string][]string{
		".rp-rail>div":              {"display:flex", "flex-direction:column"},
		".rp-tint-r":                {"background:var(--health-r-bg)"},
		".rp-tint-y":                {"background:var(--health-y-bg)"},
		".rp-tint-r .rp-tint-label": {"color:var(--health-r-ink)"},
		".rp-tint-y .rp-tint-label": {"color:var(--health-y-ink)"},
	} {
		decls, ok := ruleFor(styles, sel)
		for _, want := range wants {
			if !ok || !strings.Contains(decls, want) {
				t.Errorf("%s{%s} doesn't declare %s", sel, decls, want)
			}
		}
	}
	var tinting []string
	for _, rule := range strings.Split(styles, "}") {
		sel, decls, ok := strings.Cut(rule, "{")
		if ok && strings.Contains(sel, "rp-tint") && strings.Contains(decls, "background") {
			tinting = append(tinting, strings.TrimSpace(sel))
		}
	}
	if want := []string{".rp-tint-r", ".rp-tint-y"}; !slices.Equal(tinting, want) {
		t.Errorf("the rules tinting a block's box are %q, want Red's and Yellow's alone, %q", tinting, want)
	}
}

// A Report reads on one heading scale, on the draft and the publication
// alike: its name leads at 26px (24px on a phone), its section headings sit
// at 18px under a full-width rule, well apart from what comes before, its Goal
// titles at 16px, and its block labels recede, at the label size and muted. The
// Exceptions count stays beside its heading, smaller and muted, and the
// narrative's Insights, Accomplishments and Misses read at 15px (#475).
func TestReportHeadingScaleStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	if err := h.Service.CurateNarrative(context.Background(), boss.ID, def.ID, domain.CurateNarrativeInput{
		Notes: map[string][]string{domain.HighlightMiss: {"Slipped a week."}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if decls, ok := ruleFor(css, ".label"); !ok || !strings.Contains(decls, "font-size:var(--text-label)") || !strings.Contains(decls, "color:var(--color-ink-muted)") {
		t.Errorf(".label{%s} isn't a muted label at --text-label", decls)
	}
	// The draft reads its Goal sections in .rp-goals, spanning under its
	// sidebar, and the rest of the Report in .rp-main (#713).
	for name, c := range map[string]struct{ url, column, goals string }{
		"draft":       {fmt.Sprintf("%s/reports/%d", ts.URL, def.ID), "rp-main", "rp-goals"},
		"publication": {fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID), "rp-read", "rp-read"},
	} {
		page := getBody(t, client, c.url)
		doc := parsePage(t, page)
		column := findAll(doc, func(n *html.Node) bool { return slices.Contains(strings.Fields(attrOf(n, "class")), c.column) })
		if len(column) != 1 {
			t.Fatalf("%s has %d .%s columns, want 1", name, len(column), c.column)
		}
		goals := findAll(doc, func(n *html.Node) bool { return slices.Contains(strings.Fields(attrOf(n, "class")), c.goals) })
		if len(goals) != 1 {
			t.Fatalf("%s has %d .%s columns, want 1", name, len(goals), c.goals)
		}
		if heading := findByTestID(t, findByTestID(t, goals[0], "needs-attention"), "needs-attention-count").Parent; heading.Data != "h2" || heading.Parent.Data != "section" {
			t.Errorf("%s: the Exceptions heading isn't its section's own h2: %s", name, renderNode(heading))
		}
		// The manager composes the draft's narrative under the composer's
		// own h2; the publication reads it under the same level-2 heading,
		// so .rp-read h2 and .rp-read section>h2 give it 18px and a rule.
		if name == "publication" {
			narrative := findByTestID(t, column[0], "report-narrative")
			if heading := headingNamed(narrative, "Narrative"); heading == nil || heading.Data != "h2" || heading.Parent != narrative || narrative.Data != "section" {
				t.Errorf("publication: the Narrative heading isn't its section's own h2: %s", renderNode(narrative))
			}
		}
		// The Report's sections sit .rp-body's 24px apart on both pages; a
		// section wrapping them would take .rp-body section's 12px instead.
		if snapshot := findByTestID(t, column[0], "report-snapshot"); snapshot.Data == "section" && slices.Contains(strings.Fields(attrOf(snapshot.Parent, "class")), "rp-body") {
			t.Errorf("%s: the Report's sections are wrapped in a section inside .rp-body, so they sit 12px apart: %s", name, renderNode(snapshot.Parent))
		}
		if title := findByTestID(t, column[0], "report-name"); title.Data != "h1" || !slices.Contains(strings.Fields(attrOf(title.Parent, "class")), "rp-head") {
			t.Errorf("%s: the Report's name isn't the .rp-head's h1: %s", name, renderNode(title.Parent))
		}

		styles := between(t, page, "<style>", "</style>")
		for sel, wants := range map[string][]string{
			".rp-head h1":                  {"font-size:26px", "font-weight:700"},
			"." + c.column + " h2":         {"font-size:18px", "font-weight:650"},
			"." + c.column + " section>h2": {"border-top:1px solid var(--color-border)", "margin-top:20px"},
			"." + c.goals + " h2":          {"font-size:18px", "font-weight:650"},
			"." + c.goals + " section>h2":  {"border-top:1px solid var(--color-border)", "margin-top:20px"},
			".rp-curation-head":            {"border-top:1px solid var(--color-border)", "margin-top:20px"},
			".rp-exceptions>h2>.num":       {"font-size:var(--text-body)"},
			".rp-exceptions>h2+.rp-block":  {"border-top:none"},
			".rp-card-head h3":             {"font-size:16px", "font-weight:600"},
			".rp-narrative h3.label":       {"font-size:15px", "font-weight:600"},
			".rp-narrative>h2+.label":      {"margin-top:0"},
			".rp-section-text>summary":     {"font-size:15px", "font-weight:600"},
		} {
			decls, ok := ruleFor(styles, sel)
			for _, want := range wants {
				if !ok || !strings.Contains(decls, want) {
					t.Errorf("%s: %s{%s} doesn't declare %s", name, sel, decls, want)
				}
			}
		}
		if decls, ok := ruleFor(mediaBlock(t, styles, "@media (max-width:600px)"), ".rp-head h1"); !ok || !strings.Contains(decls, "font-size:24px") {
			t.Errorf("%s: at a phone's width .rp-head h1{%s} isn't 24px", name, decls)
		}
	}
}

// The Report's Health summary is one row sized to its content, not a grid of
// tiles: a "Health" label at the Report's level 4, then each chip, count and
// word as a pair that never breaks, the row wrapping between pairs on a
// phone, so it needs no container or breakpoint of its own (#512). On the
// draft it sits in .rp-body, whose sections stack as a column, so its rule
// names .rp-body to stay a row.
func TestReportHealthSummaryIsOneWrappingRowStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if decls, ok := ruleFor(css, ".label"); !ok || !strings.Contains(decls, "font-size:var(--text-label)") || !strings.Contains(decls, "font-weight:500") || !strings.Contains(decls, "color:var(--color-ink-muted)") {
		t.Errorf(".label{%s} isn't the Report's level 4 label, --text-label, 500, muted", decls)
	}
	for name, url := range map[string]string{
		"draft":       fmt.Sprintf("%s/reports/%d", ts.URL, def.ID),
		"publication": fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID),
	} {
		page := getBody(t, client, url)
		summary := findByTestID(t, parsePage(t, page), "health-summary")
		if !flatFrameHasClass(summary, "rp-summary") {
			t.Fatalf("%s: the Health summary isn't styled as the summary row: %s", name, renderNode(summary))
		}
		count := findByTestID(t, summary, "health-count-red")
		labels := findAll(summary, func(n *html.Node) bool { return flatFrameHasClass(n, "label") })
		if len(labels) != 1 || textOf(labels[0]) != "Health" || labels[0].Data != "span" || !precedes(labels[0], count) {
			t.Errorf("%s: the Health summary isn't headed by one \"Health\" .label, a label not a heading: %s", name, renderNode(summary))
		}
		if pair := count.Parent.Parent; !flatFrameHasClass(pair, "rp-summary-pair") || !hasTestID(pair, "health-count-red") || len(findAll(pair, func(n *html.Node) bool { return flatFrameHasClass(n, "badge") })) != 1 {
			t.Errorf("%s: a count, its word and its chip aren't one pair: %s", name, renderNode(count.Parent.Parent))
		}

		styles := between(t, page, "<style>", "</style>")
		row, ok := ruleFor(styles, ".rp-body .rp-summary")
		if !ok || strings.Contains(row, "display:grid") || strings.Contains(row, "grid-template-columns") {
			t.Errorf("%s: the Health summary is a grid, or has no rule that outranks .rp-body section: .rp-body .rp-summary{%s}", name, row)
		}
		for _, want := range []string{"display:flex", "flex-direction:row", "flex-wrap:wrap", "align-items:center"} {
			if !strings.Contains(row, want) {
				t.Errorf("%s: the Health summary isn't a row that wraps between its pairs: .rp-body .rp-summary{%s} lacks %s", name, row, want)
			}
		}
		if pair, _ := ruleFor(styles, ".rp-summary-pair"); !strings.Contains(pair, "white-space:nowrap") {
			t.Errorf("%s: a Health summary pair can break across lines: .rp-summary-pair{%s}", name, pair)
		}
		if num, ok := ruleFor(styles, ".rp-summary .num"); !ok || !strings.Contains(num, "color:var(--color-ink)") || strings.Contains(num, "font-size") {
			t.Errorf("%s: a Health summary count isn't --color-ink at the row's own size: .rp-summary .num{%s}", name, num)
		}
		for _, gone := range []string{"@container rp-tiles", "container:rp-tiles", ".rp-tile"} {
			if strings.Contains(styles, gone) {
				t.Errorf("%s: the tile grid's %s is still in the Report's styles", name, gone)
			}
		}
	}
}

// The "·" between two Health summary pairs is the first thing in the pair
// after it, so it wraps with that pair and never ends a line, and it is
// aria-hidden, so a screen reader doesn't say "middle dot" between the pairs.
// It takes no room in the pair: it hangs to the pair's left, centred in the
// row's column gap, and the row clips what lies past its sides, so a pair that
// starts a wrapped line starts it without its "·" (#532). "Health" sits as
// far from the first pair as a chip from its count, not a whole column gap.
func TestReportHealthSummarySeparatorKeepsWithItsPairStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	for name, url := range map[string]string{
		"draft":       fmt.Sprintf("%s/reports/%d", ts.URL, def.ID),
		"publication": fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID),
	} {
		page := getBody(t, client, url)
		summary := findByTestID(t, parsePage(t, page), "health-summary")
		pairs := findAll(summary, func(n *html.Node) bool { return flatFrameHasClass(n, "rp-summary-pair") })
		if len(pairs) != 4 {
			t.Fatalf("%s: the Health summary has %d pairs, want 4: %s", name, len(pairs), renderNode(summary))
		}
		held := map[*html.Node]int{}
		for n := range summary.Descendants() {
			if n.Type != html.TextNode || !strings.Contains(n.Data, "·") {
				continue
			}
			sep := n.Parent
			if attrOf(sep, "aria-hidden") != "true" || strings.TrimSpace(textOf(sep)) != "·" {
				t.Errorf("%s: a Health summary separator reads to a screen reader: %s", name, renderNode(sep))
			}
			pair := sep.Parent
			if !flatFrameHasClass(pair, "rp-summary-pair") || firstElementChild(pair) != sep {
				t.Errorf("%s: a Health summary separator isn't the first thing in the pair after it, so a line can end on it: %s", name, renderNode(sep.Parent))
				continue
			}
			if !flatFrameHasClass(sep, "rp-summary-sep") {
				t.Errorf("%s: a Health summary separator isn't styled to hang outside its pair: %s", name, renderNode(sep))
			}
			held[pair]++
		}
		for i, pair := range pairs {
			if want := min(i, 1); held[pair] != want {
				t.Errorf("%s: Health summary pair %d leads with %d separators, want %d: %s", name, i+1, held[pair], want, renderNode(pair))
			}
		}

		styles := between(t, page, "<style>", "</style>")
		row, _ := ruleFor(styles, ".rp-body .rp-summary")
		if !strings.Contains(row, "overflow-x:clip") {
			t.Errorf("%s: the Health summary doesn't clip a separator hanging past its side: .rp-body .rp-summary{%s}", name, row)
		}
		pair, _ := ruleFor(styles, ".rp-summary-pair")
		if !strings.Contains(pair, "position:relative") {
			t.Errorf("%s: a Health summary pair isn't what its separator hangs from: .rp-summary-pair{%s}", name, pair)
		}
		gap := regexp.MustCompile(`gap:\S+ (\d+px)`).FindStringSubmatch(row)
		sep, _ := ruleFor(styles, ".rp-summary-sep")
		for _, want := range []string{"position:absolute", "right:100%", "text-align:center"} {
			if !strings.Contains(sep, want) {
				t.Errorf("%s: a Health summary separator takes room in its pair, so it can start a line: .rp-summary-sep{%s} lacks %s", name, sep, want)
			}
		}
		if gap == nil || !strings.Contains(sep, "width:"+gap[1]) {
			t.Errorf("%s: a Health summary separator isn't as wide as the column gap it sits in: .rp-body .rp-summary{%s}, .rp-summary-sep{%s}", name, row, sep)
		}
		px := func(v string) int {
			n, err := strconv.Atoi(strings.TrimSuffix(v, "px"))
			if err != nil {
				t.Fatalf("%s: %q isn't a length in px", name, v)
			}
			return n
		}
		label, _ := ruleFor(styles, ".rp-summary>.label")
		if gap == nil || px(gap[1])+px(declValue(t, label, "margin-right")) != px(declValue(t, pair, "gap")) {
			t.Errorf("%s: the gap after \"Health\" isn't the gap inside a pair: .rp-body .rp-summary{%s}, .rp-summary>.label{%s}, .rp-summary-pair{%s}", name, row, label, pair)
		}
	}
}

// reportsStyleSheetIn is the stylesheet a Report page carries itself: the
// text of its <style> elements, in order.
func reportsStyleSheetIn(root *html.Node) string {
	var b strings.Builder
	for _, s := range findAll(root, func(n *html.Node) bool { return n.Data == "style" }) {
		for c := s.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				b.WriteString(c.Data)
			}
		}
	}
	return b.String()
}

// assertReportsRuleDeclares fails unless css has a rule for sel declaring
// each of wants.
func assertReportsRuleDeclares(t *testing.T, css, sel string, wants ...string) {
	t.Helper()
	decls, ok := ruleFor(css, sel)
	if !ok {
		t.Errorf("there is no %s rule", sel)
		return
	}
	for _, want := range wants {
		if !strings.Contains(decls, want) {
			t.Errorf("%s{%s} doesn't declare %s", sel, decls, want)
		}
	}
}

// assertReportsCarriesClass fails unless n, which a Report page shows as
// what, carries each of classes.
func assertReportsCarriesClass(t *testing.T, what string, n *html.Node, classes ...string) {
	t.Helper()
	for _, class := range classes {
		if !flatFrameHasClass(n, class) {
			t.Errorf("%s doesn't carry the %s class: %s", what, class, renderNode(n))
		}
	}
}

// Without JS a Highlight's section radios stay in the curation form: CSS shows
// them, the Highlight's .rp-sections, only while its box is ticked, and shows
// the Highlight's kind, its .rp-kind, as text only while it isn't
// (TestCurationFormShowsWhatIsIncludedOverHTTP).
func TestCurationFormShowsWhatIsIncludedStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(time.Hour)
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightInsight, "Buyers want invoices in euros.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	euros := strconv.FormatInt(draftHighlightID(t, h, def, "Buyers want invoices in euros."), 10)
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))
	highlight := findByTestID(t, findByTestID(t, root, "narrative-curation"), "curation-highlight")
	assertReportsCarriesClass(t, "a Highlight on the curation form", highlight, "rp-curate")
	if box := curationChoice(t, highlight, "include-"+euros, "on"); box.Type != "checkbox" {
		t.Errorf("the Highlight's include-%s is a %s, want the box its rules read", euros, box.Type)
	}
	kinds := findAll(highlight, func(n *html.Node) bool { return flatFrameHasClass(n, "rp-kind") })
	if len(kinds) != 1 || textOf(kinds[0]) != "· "+domain.HighlightInsight {
		t.Errorf("the Highlight's kind isn't one .rp-kind reading · %s: %s", domain.HighlightInsight, renderNode(highlight))
	}
	radios := formFields(highlight, "pick-"+euros)
	if len(radios) != 3 {
		t.Fatalf("the Highlight has %d section radios, want 3: %s", len(radios), renderNode(highlight))
	}
	for p := radios[0].Node; p != highlight; p = p.Parent {
		if p.Data == "fieldset" {
			assertReportsCarriesClass(t, "the Highlight's section control", p, "rp-sections")
			break
		}
		if p.Parent == highlight {
			t.Errorf("the Highlight's section radios aren't in a fieldset: %s", renderNode(highlight))
		}
	}

	css := reportsStyleSheetIn(root)
	assertReportsRuleDeclares(t, css, ".rp-curate:not(:has(input[name^=include-]:checked)) .rp-sections", "display:none")
	assertReportsRuleDeclares(t, css, ".rp-curate:has(input[name^=include-]:checked) .rp-kind", "display:none")
}

// A publication's exception is an entry ruled off from the next by a
// hairline, not a card, and a missed Path to Green's Overdue badge is drawn in
// Red's tint (TestPublicationSplitsExceptionsFromOnTrackOverHTTP).
func TestPublicationSplitsExceptionsFromOnTrackStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 0, 1))
	h.Clock.Advance(3 * 24 * time.Hour)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID}})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))
	card := findByTestID(t, findByTestID(t, doc, "needs-attention"), "report-exception")
	assertReportsCarriesClass(t, "the exception", card, "rp-block")
	if flatFrameHasClass(card, "card") {
		t.Errorf("the exception is a card: %s", renderNode(card))
	}
	assertReportsCarriesClass(t, "the Path to Green's Overdue", findByTestID(t, card, "path-overdue"), "badge", "r")

	assertReportsRuleDeclares(t, reportsStyleSheetIn(doc), ".rp-block", "border-top:1px solid var(--color-border)")
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	assertReportsRuleDeclares(t, css, ".r", "background:var(--health-r-bg)", "color:var(--health-r-ink)")
}

// A Goal block's one tinted box is a Red or Yellow Goal's Path to Green, in
// that Health's tint, Ask for help with it; nothing else in a block is tinted,
// and a Green Goal's block, or one only Check-in overdue, has no tinted box, on
// the draft and the publication alike (#475,
// TestReportBlockTintsOnlyAPathToGreen). The tints' colours are
// TestReportBlockRailLabelsAndPathTintStyles.
func TestReportBlockTintsOnlyAPathToGreenStyles(t *testing.T) {
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
			goal domain.Goal
			tint string // the tint class of its one tinted box, or "" for none
		}{
			{red, "rp-tint-r"},
			{yellow, "rp-tint-y"},
			{green, ""},
			{silent, ""},
		} {
			block := reportBlockOf(t, doc, c.goal)
			tints := findAll(block, func(n *html.Node) bool { return flatFrameHasClass(n, "rp-tint") })
			if c.tint == "" {
				if len(tints) != 0 {
					t.Errorf("%s: %q's block has %d tinted boxes, want none:\n%s", name, c.goal.Title, len(tints), renderNode(block))
				}
				continue
			}
			if len(tints) != 1 || attrOf(tints[0], "data-testid") != "report-path-box" {
				t.Errorf("%s: %q's block tints %d boxes, want its Path to Green's alone:\n%s", name, c.goal.Title, len(tints), renderNode(block))
				continue
			}
			assertReportsCarriesClass(t, name+": "+c.goal.Title+"'s Path to Green box", tints[0], c.tint)
		}
		ask := findByTestID(t, doc, "report-ask-for-help")
		assertReportsCarriesClass(t, name+": the box around Launch in EU's Ask for help", ask.Parent, "rp-tint", "rp-tint-r")
		assertReportsRuleDeclares(t, reportsStyleSheetIn(doc), ".rp-tint", "border-radius:var(--radius-md)", "padding:12px 16px")
	}
}

// A publication's narrative reads as prose, not a card, each section's
// heading a label (TestPublicationNarrativeReadsAsProseOverHTTP). The label's
// size is TestReportHeadingScaleStyles.
func TestPublicationNarrativeReadsAsProseStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	if err := h.Service.CurateNarrative(t.Context(), boss.ID, def.ID, domain.CurateNarrativeInput{
		Notes: map[string][]string{domain.HighlightAccomplishment: {"EU is open for business."}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))
	narrative := findByTestID(t, doc, "report-narrative")
	assertReportsCarriesClass(t, "the narrative", narrative, "rp-narrative")
	if flatFrameHasClass(narrative, "card") {
		t.Errorf("the narrative is a card: %s", renderNode(narrative))
	}
	for _, heading := range findAllByTestID(narrative, "narrative-section") {
		assertReportsCarriesClass(t, "the narrative's "+textOf(heading)+" heading", heading, "label")
	}
	assertReportsRuleDeclares(t, reportsStyleSheetIn(doc), ".rp-narrative", "font-size:15px", "line-height:1.7")
}

// The draft's header is the page's header strip, its breadcrumb's Reports
// muted like any page's, and Publish… its primary button. Above phone width
// the name's block lets its children into the header's row and the save
// status is ordered after the chip, beside History (#172,
// TestDraftHeaderKeepsPublishInReachOverHTTP). How the row fits at 1280px is
// TestReportDraftHeaderKeepsOneRowStyles.
func TestDraftHeaderKeepsPublishInReachStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))
	header := findByTestID(t, doc, "draft-header")
	assertReportsCarriesClass(t, "the draft's header", header, "page-header")
	if reports := linkTo(header, "/reports"); reports == nil {
		t.Errorf("the header has no Reports link: %s", renderNode(header))
	} else {
		assertReportsCarriesClass(t, "the breadcrumb's Reports", reports, "ph-parent")
	}
	save := findByTestID(t, header, "save-status")
	assertReportsCarriesClass(t, "the save status", save, "rp-save")
	assertReportsCarriesClass(t, "the name's block", save.Parent, "rp-draft-title")
	assertReportsCarriesClass(t, "the header's actions", findByTestID(t, header, "edit-definition").Parent, "rp-head-actions")
	summaries := findAll(findByTestID(t, header, "publish-confirm"), func(n *html.Node) bool { return n.Data == "summary" })
	if len(summaries) != 1 {
		t.Fatalf("Publish… has %d summaries, want 1", len(summaries))
	}
	assertReportsCarriesClass(t, "Publish…", summaries[0], "btn", "primary", "sm")

	styles := reportsStyleSheetIn(doc)
	for sel, want := range map[string]string{
		".rp-draft-title":         "display:contents",
		".rp-draft-head .rp-save": "order:1",
		".rp-head-actions":        "order:1",
	} {
		assertReportsRuleDeclares(t, styles, sel, want)
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	assertReportsRuleDeclares(t, css, ".page-header", "display:flex", "border-bottom:1px solid var(--color-border)")
	assertReportsRuleDeclares(t, css, ".ph-parent", "color:var(--color-ink-muted)")
	assertReportsRuleDeclares(t, css, ".btn.primary", "background:var(--color-primary-strong)")
}

// A never-published Report's row mutes its scope, in small type, and its
// Never and Not published (TestReportsListLinksToTheBuilderOverHTTP).
func TestReportsListLinksToTheBuilderStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Grow revenue", "The company needs to grow.")
	h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	row := findByTestID(t, getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports"), "report-row")
	assertReportsCarriesClass(t, "the row's scope", findByTestID(t, row, "report-scope"), "small", "muted")
	assertReportsCarriesClass(t, "the row's Never", findByTestID(t, row, "report-published"), "muted")
	assertReportsCarriesClass(t, "the row's Not published", findByTestID(t, row, "report-draft"), "muted")
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	assertReportsRuleDeclares(t, css, ".muted", "color:var(--color-ink-muted)")
	assertReportsRuleDeclares(t, css, ".small", "font-size:var(--text-secondary)")
}

// A published Report's row shows its last publication's date in full ink at
// 500, like the Report's name, counts its draft's changes in an amber pill,
// and draws its Goals' Health as a bar, a Green unit then a Red one in their
// Health's mark (TestReportsListRowShowsTheLastPublicationAndChangesOverHTTP).
func TestReportsListRowShowsTheLastPublicationAndChangesStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:  "MBR",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{strconv.FormatInt(boss.ID, 10)}}},
	})
	h.PublishReport(boss, def)
	h.Clock.Advance(24 * time.Hour)
	for _, title := range []string{"Hire", "Ship", "Sell"} {
		h.ActiveGoal(boss, title, "why")
	}
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports")
	row := findByTestID(t, doc, "report-row")
	published := findByTestID(t, row, "report-published")
	if flatFrameHasClass(published, "muted") {
		t.Errorf("the last publication's date is muted: %s", renderNode(published))
	}
	assertReportsCarriesClass(t, "the last publication's date", published, "rp-published")
	assertReportsCarriesClass(t, "the row's changes", findByTestID(t, row, "report-draft"), "rp-changes-hot")
	bar := findByTestID(t, row, "report-health-bar")
	assertReportsCarriesClass(t, "the Health bar", bar, "rp-bar")
	var units []string
	for c := bar.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			units = append(units, attrOf(c, "class"))
		}
	}
	if want := []string{"g", "r"}; strings.Join(units, " ") != strings.Join(want, " ") {
		t.Errorf("the Health bar's units are %q, want %q", units, want)
	}

	styles := reportsStyleSheetIn(doc)
	assertReportsRuleDeclares(t, styles, ".rp-published", "font-weight:500", "color:var(--color-ink)")
	assertReportsRuleDeclares(t, styles, ".rp-changes-hot", "border-radius:var(--radius-full)", "background:var(--health-y-bg)", "color:var(--health-y-ink)")
	assertReportsRuleDeclares(t, styles, ".rp-bar>.g", "background:var(--health-g-mark)")
	assertReportsRuleDeclares(t, styles, ".rp-bar>.r", "background:var(--health-r-mark)")
}

// At phone width the status table drops its column headings and each row
// stacks as a block, its cells one under another, so nothing scrolls the page
// sideways.
func TestReportsListRowsStackAtPhoneWidthStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports")
	assertReportsCarriesClass(t, "the status table's wrapper", findByTestID(t, doc, "report-table").Parent, "rp-table")
	phone := mediaBlock(t, reportsStyleSheetIn(doc), "@media (max-width:600px)")
	for sel, want := range map[string]string{
		".rp-table thead": "display:none",
		".rp-table tr":    "display:block",
		".rp-table td":    "display:block",
	} {
		assertReportsRuleDeclares(t, phone, sel, want)
	}
}

// At phone width, and in a narrow block, as in the draft's column under
// 1280px, a Report card's head keeps its order: the badges drop under the
// Health and title, left-aligned, rather than squeezing the title into a
// narrow column beside them, and no rule reorders the head, on screen or on
// the print page (#229, #453, TestReportCardHeadLeadsWithItsHealth).
func TestReportCardHeadLeadsWithItsHealthStyles(t *testing.T) {
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
	for where, doc := range map[string]*html.Node{"the draft": page, "the print page": printed} {
		block := findByTestID(t, doc, "report-exception")
		assertReportsCarriesClass(t, where+"'s block", block, "rp-block")
		health := findByTestID(t, block, "report-health").Parent
		assertReportsCarriesClass(t, where+"'s Health", health, "rp-health")
		assertReportsCarriesClass(t, where+"'s card head", health.Parent, "rp-card-head")
		if title := linkTo(block, "/goals/"+strconv.FormatInt(red.ID, 10)); title == nil {
			t.Errorf("%s's block doesn't link its title: %s", where, renderNode(block))
		} else {
			assertReportsCarriesClass(t, where+"'s title", title.Parent, "rp-card-title")
		}
		assertReportsCarriesClass(t, where+"'s badges", findByTestID(t, block, "report-badge").Parent, "rp-badges")
	}

	styles := reportsStyleSheetIn(page)
	phone := mediaBlock(t, styles, "@media (max-width:600px)")
	assertReportsRuleDeclares(t, phone, ".rp-card-head", "flex-wrap:wrap")
	assertReportsRuleDeclares(t, phone, ".rp-badges", "flex-basis:100%", "justify-content:flex-start")
	if cardHead, _ := ruleFor(phone, ".rp-card-head"); strings.Contains(cardHead, "column") {
		t.Errorf("at 600px the card head stacks as a column rather than keeping Health and title on a line: .rp-card-head{%s}", cardHead)
	}
	// A narrow block does the same, so its title isn't squeezed until its
	// words break.
	assertReportsRuleDeclares(t, styles, ".rp-block", "container:rp-block/inline-size")
	narrow := mediaBlock(t, styles, "@container rp-block (max-width:28rem)")
	assertReportsRuleDeclares(t, narrow, ".rp-card-head", "flex-wrap:wrap")
	assertReportsRuleDeclares(t, narrow, ".rp-badges", "flex-basis:100%", "justify-content:flex-start")
	for _, sel := range []string{".rp-card-head", ".rp-health", ".rp-card-title", ".rp-badges"} {
		for _, css := range []string{phone, narrow, reportsStyleSheetIn(printed)} {
			if rule, _ := ruleFor(css, sel); strings.Contains(strings.ReplaceAll(rule, " ", ""), "order:") || strings.Contains(rule, "reverse") {
				t.Errorf("the card head reorders itself: %s{%s}", sel, rule)
			}
		}
	}
}

// The print page sets the Report in the design system's serif, falling back to
// Georgia, and draws each Health badge's shape as the screen does, a Red
// octagon, a Yellow triangle pointing down and a Green circle, in the Health's
// ink and kept when printed, not ■ ▲ ● characters (#452,
// TestPrintPageMarksHealthWithShapesOverHTTP).
func TestPrintPageMarksHealthWithShapesStyles(t *testing.T) {
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
	for _, c := range []struct{ in, badge, class string }{
		{"report-exception", "report-health", "r"},
		{"selected-goal", "selected-health", "g"},
	} {
		badge := findByTestID(t, findByTestID(t, doc, c.in), c.badge)
		assertReportsCarriesClass(t, c.in+"'s Health badge", badge, "badge", c.class)
		dots := findAll(badge, func(n *html.Node) bool { return flatFrameHasClass(n, "dot") })
		if len(dots) != 1 {
			t.Errorf("%s's Health badge has %d shapes, want one: %s", c.in, len(dots), renderNode(badge))
		}
	}

	style := reportsStyleSheetIn(doc)
	if root := printPageRule(t, style, ":root"); !strings.Contains(root, "--print-font: 'Source Serif 4', Georgia, serif") {
		t.Errorf("print page's font isn't Source Serif 4 with a Georgia fallback: :root { %s }", root)
	}
	if body := printPageRule(t, style, "body"); !strings.Contains(body, "font-family: var(--print-font)") {
		t.Errorf("print page isn't set in its font: body { %s }", body)
	}
	if strings.ContainsAny(style, "■▲●") {
		t.Errorf("print page's style still marks Health with ■ ▲ ● characters:\n%s", style)
	}
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
func TestPrintPageColoursHealthBadgesStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{red.ID}})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")
	assertReportsCarriesClass(t, "the Red Goal's Health badge", findByTestID(t, doc, "report-health"), "badge", "r")
	style := reportsStyleSheetIn(doc)

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
// Check-in overdue badge in its grey with a dashed edge (#389).
func TestPrintPageColoursOverdueMarksStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	goal := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID}})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10)+"/print")
	badge := findByTestID(t, doc, "report-badge")
	if textOf(badge) != domain.BadgeCheckinOverdue {
		t.Fatalf("the Goal's badge reads %q, want %s", textOf(badge), domain.BadgeCheckinOverdue)
	}
	assertReportsCarriesClass(t, "the Check-in overdue badge", badge, "badge", "st")
	style := reportsStyleSheetIn(doc)

	red := printPageColour(t, style, printPageRule(t, style, ".r"), "background")
	overdue := printPageRule(t, style, ".st")
	if bg, ink := printPageColour(t, style, overdue, "background"), printPageColour(t, style, overdue, "color"); bg == ink || bg == red {
		t.Errorf("Check-in overdue badge is not its own grey: .st { %s }", overdue)
	}
	if !strings.Contains(overdue, "border: 0.5pt dashed var(") {
		t.Errorf("Check-in overdue badge has no dashed edge: .st { %s }", overdue)
	}
}

// The print page lays a Goal block out as the screen does (#475, #712), its
// story beside its facts rail behind a hairline, each rail label over its
// value with no indent, and the Path to Green in a box tinted
// in its Goal's Health, Red or Yellow, kept when printed. An overdue Path to
// Green keeps its Health's tint, so an overdue Yellow one prints Yellow, not
// Red (#478, TestPrintPageLaysOutTheGoalBlockAsOnScreen).
func TestPrintPageLaysOutTheGoalBlockAsOnScreenStyles(t *testing.T) {
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
		goal domain.Goal
		tint string
	}{
		{red, "rp-tint-r"},
		{yellow, "rp-tint-y"},
	} {
		block := reportBlockOf(t, doc, c.goal)
		assertReportsCarriesClass(t, c.goal.Title+"'s story", findByTestID(t, block, "report-story"), "rp-story")
		assertReportsCarriesClass(t, c.goal.Title+"'s rail", findByTestID(t, block, "report-rail"), "rp-rail")
		assertReportsCarriesClass(t, c.goal.Title+"'s Path to Green box", findByTestID(t, block, "report-path-box"), "rp-tint", c.tint)
	}

	style := reportsStyleSheetIn(doc)
	if cols := printPageRule(t, style, ".rp-cols"); !strings.Contains(cols, "display: grid;") || !strings.Contains(cols, "grid-template-columns: minmax(0, 1fr) 13rem;") {
		t.Errorf("print page doesn't lay the story and the rail out side by side: .rp-cols { %s }", cols)
	}
	if rail := printPageRule(t, style, ".rp-rail"); !strings.Contains(rail, "border-left: 0.5pt solid var(--print-rule);") {
		t.Errorf("print page doesn't rule the rail off by a hairline: .rp-rail { %s }", rail)
	}
	if dd := printPageRule(t, style, ".rp-rail dd"); !strings.Contains(dd, "margin: 0;") {
		t.Errorf("print page indents a fact's value by the browser's dd margin: .rp-rail dd { %s }", dd)
	}
	tint := printPageRule(t, style, ".rp-tint")
	for _, want := range []string{"padding: 0.2rem 0.5rem;", "border-radius: 2pt;", "-webkit-print-color-adjust: exact;", " print-color-adjust: exact;"} {
		if !strings.Contains(tint, want) {
			t.Errorf("Path to Green box lacks %q: .rp-tint { %s }", want, tint)
		}
	}
	for tint, health := range map[string]string{".rp-tint-r": ".r", ".rp-tint-y": ".y"} {
		badge := printPageRule(t, style, health)
		want, ink := printPageColour(t, style, badge, "background"), printPageColour(t, style, badge, "color")
		if bg := printPageColour(t, style, printPageRule(t, style, tint), "background"); bg != want {
			t.Errorf("print page tints %s with %s, not its Health's tint %s", tint, bg, want)
		}
		if got := printPageColour(t, style, printPageRule(t, style, tint+" .rp-tint-label"), "color"); got != ink {
			t.Errorf("print page's %s Path to Green label is %s, not its Health's ink %s", tint, got, ink)
		}
	}
	if _, ok := ruleFor(style, ".rp-tint.overdue"); ok {
		t.Errorf("print page still tints an overdue Path to Green Red whatever its Health: style:\n%s", style)
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
// screen with Cancel beyond reach (#172): the card is the confirmation's
// .rp-publish, fixed between 16px gutters under 600px.
func TestDraftPublishCardFitsAPhoneStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	confirm := findByTestID(t, page, "publish-confirm")
	if cards := findAll(confirm, func(n *html.Node) bool { return flatFrameHasClass(n, "rp-publish") }); len(cards) != 1 || !hasTestID(cards[0], "report-publish") {
		t.Errorf("Publish…'s card, holding the publish form, isn't one .rp-publish: %s", renderNode(confirm))
	}
	phone := mediaBlock(t, reportsStyleSheetIn(page), "@media (max-width:600px)")
	assertReportsRuleDeclares(t, phone, ".rp-publish", "position:fixed", "left:16px", "right:16px", "width:auto")
}

// At phone width the draft header scrolls away with the page instead of
// sticking over a third of the screen. The name takes its own line with the
// save status beside it, the baseline chip sits on its own row below, and
// History, Edit definition and Publish… share the last row: five controls
// don't fit one row at 390px (#172). The name and save status are the
// header's .rp-draft-title, the actions its .rp-head-actions.
func TestDraftHeaderStacksAtPhoneWidthStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	header := findByTestID(t, page, "draft-header")
	classed := func(class string) *html.Node {
		found := findAll(header, func(n *html.Node) bool { return flatFrameHasClass(n, class) })
		if len(found) != 1 {
			t.Fatalf("the header has %d .%s, want 1: %s", len(found), class, renderNode(header))
		}
		return found[0]
	}
	title, actions := classed("rp-draft-title"), classed("rp-head-actions")
	for _, part := range []string{"report-breadcrumb", "save-status"} {
		if !hasTestID(title, part) {
			t.Errorf("the name block lacks %s: %s", part, renderNode(title))
		}
	}
	if chip := findByTestID(t, header, "baseline-chip"); within(chip, title) || within(chip, actions) {
		t.Errorf("the baseline chip isn't a row of its own: %s", renderNode(header))
	}
	for _, part := range []string{"edit-definition", "publish-confirm"} {
		if !hasTestID(actions, part) {
			t.Errorf("the actions row lacks %s: %s", part, renderNode(actions))
		}
	}
	if hasTestID(actions, "save-status") {
		t.Errorf("the save status is in the actions row, not beside the name: %s", renderNode(actions))
	}

	phone := mediaBlock(t, reportsStyleSheetIn(page), "@media (max-width:600px)")
	assertReportsRuleDeclares(t, phone, ".rp-draft-head", "position:static")
	assertReportsRuleDeclares(t, phone, ".rp-draft-title", "display:flex", "flex-basis:100%")
	assertReportsRuleDeclares(t, phone, ".rp-head-actions", "flex-basis:100%", "flex-wrap:nowrap", "margin-left:0")
}

// A Due date in the draft's On track table wraps as a whole, never mid-date
// ("2026-12-" over "14") in a narrow column (#172): it is an .rp-date, kept on
// one line.
func TestDraftOnTrackDueDatesDoNotBreakStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(40 * 24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(3 * 24 * time.Hour)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	due := findByTestID(t, findByTestID(t, page, "on-track"), "selected-due")
	if !flatFrameHasClass(due, "rp-date") {
		t.Fatalf("the On track Due date is not marked rp-date: %s", renderNode(due))
	}
	assertReportsRuleDeclares(t, reportsStyleSheetIn(page), ".rp-date", "white-space:nowrap")
}

// At a phone's width each On track row stacks as a block rather than leaving
// its Due off the side of the card (#180): its Health then the Goal on one
// line, as at every width, then its Owner and Due under the Goal, in a
// two-column grid. No rule reorders the cells, which run Health, Goal, Owner,
// Due (#453), and a publication's discussion row spans both columns. The
// table is an .rp-track, each cell classed for its column, the discussion's
// cell an .rp-track-talk.
func TestOnTrackRowsStackAtPhoneWidthStyles(t *testing.T) {
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
	assertReportsCarriesClass(t, "the On track table", onTrack, "rp-track")
	cells := findAll(findByTestID(t, onTrack, "selected-goal"), func(n *html.Node) bool { return n.Data == "td" })
	var classes []string
	for _, c := range cells {
		classes = append(classes, attrOf(c, "class"))
	}
	if want := []string{"rp-track-health", "rp-track-goal", "rp-track-owner", "rp-track-due"}; strings.Join(classes, " ") != strings.Join(want, " ") {
		t.Errorf("the row's cells are classed %q, want %q to style", classes, want)
	}
	assertReportsCarriesClass(t, "the discussion's cell", findByTestID(t, onTrack, "goal-discussion").Parent, "rp-track-talk")

	phone := mediaBlock(t, reportsStyleSheetIn(page), "@media (max-width:600px)")
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
		assertReportsRuleDeclares(t, phone, selector, wants...)
	}
}

// An exception card's due date and its Milestones' dates wrap between their
// struck slips but never mid-date ("2026-07-" over "16") on a phone (#180):
// each sits in an .rp-dates whose every child is one whole date, kept on one
// line, while the wrapper itself may wrap.
func TestExceptionCardDatesDoNotBreakStyles(t *testing.T) {
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
	// wholeDates fails unless dates is an .rp-dates whose children are want,
	// each one element holding one whole date, with only spaces between.
	wholeDates := func(what string, dates *html.Node, want ...string) {
		t.Helper()
		assertReportsCarriesClass(t, what, dates, "rp-dates")
		var parts []string
		for c := dates.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.ElementNode:
				parts = append(parts, "<"+c.Data+">"+textOf(c))
			case c.Type == html.TextNode && strings.TrimSpace(c.Data) != "":
				parts = append(parts, c.Data)
			}
		}
		if strings.Join(parts, "|") != strings.Join(want, "|") {
			t.Errorf("%s is %q, want each whole date its own element, %q: %s", what, parts, want, renderNode(dates))
		}
	}
	wholeDates("the card's due date", findByTestID(t, block, "report-due"), "<del>Jul 2, 2026", "<span>Jul 16, 2026")
	milestone := findByTestID(t, block, "report-milestone")
	wrappers := findAll(milestone, func(n *html.Node) bool { return flatFrameHasClass(n, "rp-dates") })
	if len(wrappers) != 1 {
		t.Fatalf("the Milestone has %d .rp-dates, want 1: %s", len(wrappers), renderNode(milestone))
	}
	wholeDates("the Milestone's dates", wrappers[0], "<span>"+slippedBeta.Format("Jan 2, 2006"), "<del>"+beta.TargetDate.Format("Jan 2, 2006"))

	css := reportsStyleSheetIn(page)
	assertReportsRuleDeclares(t, css, ".rp-dates>*", "white-space:nowrap")
	if rule, ok := ruleFor(css, ".rp-dates"); ok && strings.Contains(rule, "nowrap") {
		t.Errorf("the whole wrapper is kept on one line, so its slips can't wrap: .rp-dates{%s}", rule)
	}
}

// The draft's own rules outrank the generic ones they sit inside (#173): a
// Highlight's section picker, the .rp-sections fieldset in the curation
// .rp-form, is an inline segmented control, not the form's column of fields,
// and the Highlight list, an .rp-list, and its .rp-curate items sit flush
// (the Health summary's row is pinned in
// TestReportHealthSummaryIsOneWrappingRowStyles). The baseline chip, an
// .rp-menu.rp-chip, is a pill as tall as the buttons beside it (#180).
func TestDraftRulesWinTheCascadeStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))

	form := findByTestID(t, page, "narrative-curation")
	assertReportsCarriesClass(t, "the curation form", form, "rp-form")
	highlight := findByTestID(t, form, "curation-highlight")
	assertReportsCarriesClass(t, "a Highlight", highlight, "rp-curate")
	assertReportsCarriesClass(t, "the Highlight list", highlight.Parent, "rp-list")
	if pickers := findAll(highlight, func(n *html.Node) bool { return n.Data == "fieldset" && flatFrameHasClass(n, "rp-sections") }); len(pickers) != 1 {
		t.Errorf("a Highlight's section picker isn't one fieldset.rp-sections: %s", renderNode(highlight))
	}
	chip := findByTestID(t, page, "baseline-chip")
	assertReportsCarriesClass(t, "the baseline chip", chip, "rp-menu", "rp-chip")
	if len(findAll(chip, func(n *html.Node) bool { return n.Data == "summary" && n.Parent == chip })) != 1 {
		t.Errorf("the baseline chip has no summary of its own to draw as a pill: %s", renderNode(chip))
	}

	css := reportsStyleSheetIn(page)
	// .rp-form fieldset sets display:flex;flex-direction:column, so the
	// segmented control needs a rule at least that specific.
	assertReportsRuleDeclares(t, css, ".rp-form fieldset.rp-sections", "display:inline-flex", "flex-direction:row")
	// .rp-block ul indents the exception entries' lists; the Highlight list
	// isn't in one and stays flush.
	assertReportsRuleDeclares(t, css, ".rp-list", "padding:0")
	// A Highlight lines up with the "Highlights since" heading above it (#180).
	assertReportsRuleDeclares(t, css, ".rp-list>li.rp-curate", "padding-left:0", "padding-right:0")
	assertReportsRuleDeclares(t, css, ".rp-list>li", "padding:12px 0")
	// The baseline chip is a bordered pill, as a .tag is. app.css's
	// details>summary is 44px tall; the chip matches the 32px small buttons
	// beside it in the header strip, its border inside that height (#180).
	assertReportsRuleDeclares(t, css, ".rp-menu.rp-chip>summary", "border:1px solid", "border-radius:var(--radius-full)", "box-sizing:border-box", "min-height:32px")
	// On a touch screen app.css raises those buttons to 44px; the chip
	// follows them (#183).
	assertReportsRuleDeclares(t, mediaBlock(t, css, "@media (pointer:coarse)"), ".rp-menu.rp-chip>summary", "min-height:44px")
}

// Publish… confirms with a primary Publish button: a .btn.primary, which
// app.css fills with the strong primary colour
// (TestPublishConfirmsWhatItWillFreezeOverHTTP).
func TestPublishConfirmsWhatItWillFreezeStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Platform MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	publish := buttonNamed(findByTestID(t, findByTestID(t, page, "publish-confirm"), "report-publish"), "Publish")
	if publish == nil {
		t.Fatalf("the publish form has no Publish button")
	}
	assertReportsCarriesClass(t, "the publish form's Publish button", publish, "btn", "primary")
	assertReportsRuleDeclares(t, getBody(t, http.DefaultClient, ts.URL+"/static/app.css"), ".btn.primary", "background:var(--color-primary-strong)")
}

// An exception block's Health, its Run off Green and its earlier Health sit in
// one .rp-health group that never breaks, so a marker can't wrap onto a line
// of its own (TestReportMarksAChangeOfHealthOverHTTP).
func TestReportMarksAChangeOfHealthStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.PublishReport(boss, def)
	h.Clock.Advance(24 * time.Hour)
	h.Checkin(boss, g.ID, domain.HealthYellow, "Slipping.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0))
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	block := reportBlockOf(t, page, g)
	group := findByTestID(t, block, "report-health").Parent
	if !hasTestID(group, "report-prior-health") {
		t.Fatalf("the block's earlier Health isn't in its Health's group: %s", renderNode(group))
	}
	assertReportsCarriesClass(t, "the block's Health and its markers", group, "rp-health")
	assertReportsRuleDeclares(t, reportsStyleSheetIn(page), ".rp-health", "white-space:nowrap")
}

// The Reports page's Open Action Items heads each group with a header row,
// its count muted beside its name, and flags an overdue item with a badge in
// Red's tint (TestReportsPageListsOpenActionItemsToChase).
func TestReportsPageListsOpenActionItemsToChaseStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	viewer := h.SignIn("viewer@example.com")
	owner := h.SignIn("owner@example.com")
	goal := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(viewer, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID}})
	pub := h.PublishReport(viewer, def)
	if _, err := h.Service.RaiseActionItem(context.Background(), viewer.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: goal.ID, Text: "Explain the slip.", OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, -2),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "viewer@example.com"), ts.URL+"/reports")
	section := findByTestID(t, page, "reports-action-items")
	heading := headingNamed(section, "Yours 1")
	if heading == nil {
		t.Fatalf("Open Action Items has no Yours heading: %s", renderNode(section))
	}
	assertReportsCarriesClass(t, "the Yours heading", heading, "rp-group")
	counts := findAll(heading, func(n *html.Node) bool { return n.Data == "span" })
	if len(counts) != 1 {
		t.Fatalf("the Yours heading has %d counts, want 1: %s", len(counts), renderNode(heading))
	}
	assertReportsCarriesClass(t, "the Yours heading's count", counts[0], "rp-group-count")
	assertReportsCarriesClass(t, "the overdue flag", findByTestID(t, section, "reports-action-item-overdue"), "badge", "r")

	styles := reportsStyleSheetIn(page)
	assertReportsRuleDeclares(t, styles, ".rp-group", "display:flex", "background:var(--color-group)")
	assertReportsRuleDeclares(t, styles, ".rp-group-count", "color:var(--color-ink-muted)")
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	assertReportsRuleDeclares(t, css, ".r", "background:var(--health-r-bg)", "color:var(--health-r-ink)")
}

// An exception block's story and facts rail are two columns, the story at
// most 680px and the rail at most 320px, ruled off by a hairline; under 720px
// wide the rail stacks under the story, ruled off above it instead, and so it
// does in a block too narrow for both. The block's Status leads its story a
// step above the body's size, and its title reads in the ink, not a link's
// colour, until hovered, when it turns the links' teal, its rule beating the
// ink's (#712, #713, TestReportBlockLaysOutAStoryBesideAFactsRail).
func TestReportBlockRailStacksUnderTheStoryBelow720pxStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	block := reportBlockOf(t, doc, g)
	story, rail := findByTestID(t, block, "report-story"), findByTestID(t, block, "report-rail")
	assertReportsCarriesClass(t, "the story", story, "rp-story")
	assertReportsCarriesClass(t, "the rail", rail, "rp-rail")
	assertReportsCarriesClass(t, "the story and rail's row", story.Parent, "rp-cols")
	assertReportsCarriesClass(t, "the Status", findByTestID(t, block, "report-status"), "rp-lead-text")
	assertReportsCarriesClass(t, "the title", headingNamed(block, g.Title), "rp-card-title")

	styles := reportsStyleSheetIn(doc)
	assertReportsRuleDeclares(t, styles, ".rp-cols", "display:grid", "grid-template-columns:minmax(0,680px) minmax(0,320px)")
	assertReportsRuleDeclares(t, styles, ".rp-rail", "border-left:1px solid var(--color-border)")
	assertReportsRuleDeclares(t, styles, ".rp-lead-text", "font-size:calc(var(--text-body) + 2px)")
	assertReportsRuleDeclares(t, styles, ".rp-card-title>a", "color:var(--color-ink)")
	// .rp-card-title>a:hover is (0,2,1), over .rp-card-title>a's (0,1,1).
	assertReportsRuleDeclares(t, styles, ".rp-card-title>a:hover", "color:var(--color-primary-strong)")
	for _, query := range []string{"@media (max-width:720px)", "@container rp-block (max-width:34rem)"} {
		narrow := mediaBlock(t, styles, query)
		assertReportsRuleDeclares(t, narrow, ".rp-cols", "grid-template-columns:minmax(0,1fr)")
		assertReportsRuleDeclares(t, narrow, ".rp-rail", "border-left:none", "border-top:1px solid var(--color-border)")
	}
}

// An exception block narrower than 61.5rem (984px) gives its story two-thirds
// of its width and the rail one-third, the rail reaching its 320px at 984px,
// so a mid-width block doesn't split evenly. The rule comes before both
// stacking blocks, so they still win in a narrower block (#715).
func TestReportBlockStoryTakesTwoThirdsAtMidWidthsStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	assertReportsCarriesClass(t, "the block", reportBlockOf(t, doc, g), "rp-block")

	styles := reportsStyleSheetIn(doc)
	const midWidth = "@container rp-block (max-width:61.5rem)"
	assertReportsRuleDeclares(t, mediaBlock(t, styles, midWidth), ".rp-cols", "grid-template-columns:minmax(0,2fr) minmax(0,1fr)")
	at := strings.Index(styles, midWidth+"{")
	for _, stacking := range []string{"@media (max-width:720px)", "@container rp-block (max-width:34rem)"} {
		if stacks := strings.Index(styles, stacking+"{"); stacks < 0 || at > stacks {
			t.Errorf("the %s block doesn't come before the %s block, so it would win over the stacked column", midWidth, stacking)
		}
	}
}
