package web_test

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// followUpsStyleSheet is the Follow-ups page's own stylesheet, its <style>.
func followUpsStyleSheet(t *testing.T, page string) string {
	t.Helper()
	return between(t, page, "<style>", "</style>")
}

// assertFollowUpsRuleDeclares fails unless the rule in css for sel declares
// each of wants.
func assertFollowUpsRuleDeclares(t *testing.T, css, sel string, wants ...string) {
	t.Helper()
	decls, ok := ruleFor(css, sel)
	for _, want := range wants {
		if !ok || !strings.Contains(decls, want) {
			t.Errorf("%s{%s} doesn't declare %s", sel, decls, want)
		}
	}
}

// The Follow-ups table is a full-bleed list with no card around it, straight
// inside its .rk-list, which scrolls sideways inside itself should the table
// outgrow a narrow screen.
func TestFollowUpsListScrollsSidewaysStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")
	table := findByTestID(t, parsePage(t, page), "follow-ups-table")
	if list := table.Parent; list.Data != "div" || !flatFrameHasClass(list, "rk-list") {
		t.Errorf("the Follow-ups table doesn't sit straight inside its .rk-list: %s", renderNode(list))
	}
	assertFollowUpsRuleDeclares(t, followUpsStyleSheet(t, page), ".rk-list", "overflow-x:auto")
}

// An Unlinked chip is the outlined chip the structural signals take: an
// .rk-tag, outlined with the badges' small radius.
func TestFollowUpsOutlinedChipStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")
	if chip := followUpChipIn(t, followUpRowIn(t, parsePage(t, page), loner), "unlinked"); !flatFrameHasClass(chip, "rk-tag") {
		t.Errorf("the Unlinked chip isn't an .rk-tag: %s", renderNode(chip))
	}
	assertFollowUpsRuleDeclares(t, followUpsStyleSheet(t, page), ".rk-tag", "border-radius:var(--radius-sm)", "border:1px solid var(--color-border-strong)")
}

// A group card holding no Goals while another holds some is muted, taking no
// dashed edge: the groups are plain blocks, not cards.
func TestFollowUpsEmptyGroupIsMutedStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")
	if card := followUpsCardIn(t, parsePage(t, page), "admin"); !flatFrameHasClass(card, "rk-zero") {
		t.Errorf("the empty admin card isn't an .rk-zero: %s", renderNode(card))
	}
	css := followUpsStyleSheet(t, page)
	assertFollowUpsRuleDeclares(t, css, ".rk-zero", "color:var(--color-ink-muted)")
	if decls, _ := ruleFor(css, ".rk-zero"); strings.Contains(decls, "border") {
		t.Errorf("an empty group .rk-zero{%s} takes an edge", decls)
	}
}

// An Owner's name stays on one line at desktop width rather than wrapping
// onto two in the table's narrow Owner column.
func TestFollowUpsPageKeepsOwnersOnOneLine(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")
	doc := parsePage(t, page)
	if table := findByTestID(t, doc, "follow-ups-table"); !flatFrameHasClass(table, "rk-table") {
		t.Fatalf("the Follow-ups table isn't an .rk-table: %s", renderNode(table))
	}
	if owner := findByTestID(t, followUpRowIn(t, doc, silent), "follow-up-owner"); !flatFrameHasClass(owner, "rk-owner") {
		t.Fatalf("the Owner cell has no rk-owner class to style: %s", renderNode(owner))
	}
	assertFollowUpsRuleDeclares(t, followUpsStyleSheet(t, page), ".rk-table .rk-owner", "white-space:nowrap")
}

// A Not Green row's Health badge and its Run off Green stay on one line
// however the table squeezes the column, so "· 10 wks" never drops its "wks"
// under the badge.
func TestFollowUpsPageKeepsHealthAndItsRunOnOneLine(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	yellow := h.ActiveGoal(sam, "Launch in EU", "Expand the market.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Add a reviewer.", h.Clock.Now().AddDate(0, 1, 0))
	h.Clock.Advance(3 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")
	row := followUpRowIn(t, parsePage(t, page), yellow)
	cell := findByTestID(t, row, "follow-up-health").Parent
	if cell.Data != "td" || !flatFrameHasClass(cell, "rk-health") || findByTestID(t, row, "follow-up-run-off-green").Parent != cell {
		t.Fatalf("the rk-health cell doesn't hold the Health and its Run off Green: %s", renderNode(cell))
	}
	assertFollowUpsRuleDeclares(t, followUpsStyleSheet(t, page), ".rk-table .rk-health", "white-space:nowrap")
}

// Below 600px each Follow-ups row stacks as a card rather than hiding its Fix off
// to the side of a sideways scroller: its Health then the Goal and its chips on
// one line, as at every width, then its Owner under the Goal, then its Fix.
// The cells run Health, Goal, Owner, Fix in the HTML and none is reordered
// (#453).
func TestFollowUpsRowsStackAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")
	doc := parsePage(t, page)
	if table := findByTestID(t, doc, "follow-ups-table"); !flatFrameHasClass(table, "rk-table") {
		t.Fatalf("the Follow-ups table isn't an .rk-table: %s", renderNode(table))
	}
	var cells []string
	for _, td := range findAll(followUpRowIn(t, doc, silent), func(n *html.Node) bool { return n.Data == "td" }) {
		cells = append(cells, attrOf(td, "class"))
	}
	if want := []string{"rk-health", "rk-goal", "rk-owner", "rk-fix"}; !slices.Equal(cells, want) {
		t.Errorf("the row's cells are %q, want %q to style", cells, want)
	}
	phone := mediaBlock(t, followUpsStyleSheet(t, page), "@media (max-width:600px)")
	for sel, want := range map[string]string{
		".rk-table thead":     "display:none",
		".rk-table tr":        "display:grid",
		".rk-table td":        "display:block",
		".rk-table .rk-goal":  "grid-column:2",
		".rk-table .rk-owner": "grid-column:2",
		".rk-table .rk-fix":   "grid-column:1/-1",
	} {
		if decls, ok := ruleFor(phone, sel); !ok || !strings.Contains(decls, want) {
			t.Errorf("at 600px %s{%s} lacks %s", sel, decls, want)
		}
	}
	if strings.Contains(phone, "order:") {
		t.Errorf("at 600px a Follow-ups row reorders its cells, which read Health first: %s", phone)
	}
}

// The Follow-ups filters wrap, as the Goal list's filter bar does, and never grow
// past their column: a Scope select listing Other orgs by their long labels
// shrinks within the form rather than pushing the Team select off a phone's
// screen (#268).
func TestFollowUpsFiltersWrapAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	newScopeOrgFixture(h)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "vera@example.com"), ts.URL+"/follow-ups")
	doc := parsePage(t, page)
	choices, _, _ := selectChoices(scopeSelectIn(t, doc, "follow-ups-scope"))
	if !slices.ContainsFunc(choices, func(c string) bool { return strings.HasPrefix(c, "Other orgs: ") }) {
		t.Fatalf("the Scope select lists no Other orgs: %q", choices)
	}
	if form := findByTestID(t, doc, "follow-ups-filters"); !flatFrameHasClass(form, "rk-filters") {
		t.Fatalf("the filters form isn't .rk-filters: %s", renderNode(form))
	}
	assertFollowUpsRuleDeclares(t, followUpsStyleSheet(t, page), ".rk-filters", "flex-wrap:wrap", "min-width:0")
}

// The Follow-ups filters' selects sit in app.css's filter chips, and no rule
// of the page's own restyles the selects inside them (#406).
func TestFollowUpsFilterChipSelectsKeepTheirStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	h.SignIn("sam@example.com")
	h.CreateDimension(boss, "Team", "Platform")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")
	filters := findByTestID(t, parsePage(t, page), "follow-ups-filters")
	if !flatFrameHasClass(filters, "rk-filters") {
		t.Fatalf("the filters form isn't .rk-filters: %s", renderNode(filters))
	}
	selects := findAll(filters, func(n *html.Node) bool { return n.Data == "select" })
	if len(selects) != 2 {
		t.Fatalf("the filters hold %d selects, want the Scope and Team selects", len(selects))
	}
	for _, sel := range selects {
		if label := sel.Parent; label.Data != "label" || !flatFrameHasClass(label, "filter-chip") {
			t.Errorf("the %s select isn't in a .filter-chip: %s", attrOf(sel, "aria-label"), renderNode(label))
		}
	}
	for _, rule := range strings.Split(followUpsStyleSheet(t, page), "}") {
		selectors, _, _ := strings.Cut(rule, "{")
		for _, s := range strings.Split(selectors, ",") {
			if strings.HasPrefix(strings.TrimSpace(s), ".rk-filters select") {
				t.Errorf("the page's own styles restyle the chips' selects: %s", strings.TrimSpace(s))
			}
		}
	}
}

// The Follow-ups page's five group cards sit five across at the page's full
// width, and two per row where the page is narrower, a phone's included: no
// rule stacks them one per row.
func TestFollowUpsGroupCardsFitFiveAcross(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")
	groups := findByTestID(t, parsePage(t, page), "follow-ups-groups")
	if !flatFrameHasClass(groups, "rk-groups") {
		t.Fatalf("the group cards don't sit in an .rk-groups: %s", renderNode(groups))
	}
	if cards := findAll(groups, func(n *html.Node) bool { return n.Parent == groups && flatFrameHasClass(n, "rk-group") }); len(cards) != 5 {
		t.Errorf("the .rk-groups holds %d group cards, want 5", len(cards))
	}
	css := followUpsStyleSheet(t, page)
	assertFollowUpsRuleDeclares(t, css, ".rk-groups", "grid-template-columns:repeat(5,minmax(0,1fr))")
	assertFollowUpsRuleDeclares(t, mediaBlock(t, css, "@media (max-width:1151px)"), ".rk-groups", "grid-template-columns:repeat(2,minmax(0,1fr))")
	for _, query := range []string{"@media (max-width:900px)", "@media (max-width:600px)"} {
		for _, block := range mediaBlocks(css, query) {
			if decls, ok := ruleFor(block, ".rk-groups"); ok {
				t.Errorf("%q restyles .rk-groups{%s}, so a phone doesn't keep two per row", query, decls)
			}
		}
	}
}

// An open popover saying what a signal means fits a 390px phone: it is never
// wider than the window less a margin, scrolls rather than grows past its
// height, and the full list stacks each name over its meaning below 600px.
func TestFollowUpsSignalPopoversFitAPhoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")
	doc := parsePage(t, page)
	tip := attrOf(followUpTipIn(t, followUpChipIn(t, followUpRowIn(t, doc, silent), "checkin-overdue")), "popovertarget")
	for _, id := range []string{tip, "follow-ups-meanings"} {
		if pop := followUpPopoverIn(t, doc, id); !flatFrameHasClass(pop, "rk-pop") {
			t.Errorf("#%s isn't an .rk-pop: %s", id, renderNode(pop))
		}
	}
	if list := followUpPopoverIn(t, doc, "follow-ups-meanings"); !flatFrameHasClass(list, "rk-defs") {
		t.Errorf("the full list isn't an .rk-defs: %s", renderNode(list))
	}
	css := followUpsStyleSheet(t, page)
	assertFollowUpsRuleDeclares(t, css, ".rk-pop", "box-sizing:border-box", "max-width:min(360px,calc(100vw - 32px))", "max-height:calc(100dvh - 32px)", "overflow:auto")
	assertFollowUpsRuleDeclares(t, mediaBlock(t, css, "@media (max-width:600px)"), ".rk-defs dl", "grid-template-columns:minmax(0,1fr)")
}
