package web_test

import (
	"fmt"
	"net/http"
	"net/url"
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

// The Latest status's cells each read as a small label over a value.
func TestLatestStatusCellsStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Slipping.", "Escalate.", pathDate)
	client := signInClient(t, ts.URL, "sam@example.com")

	cells := pageElement(t, getBody(t, client, goalPageURL(ts.URL, goal)), "dl", "goal-status-cells")
	for _, label := range []string{"Owner's Health", "Back to Green by"} {
		if !strings.Contains(cells, `<dt class="label">`+label+`</dt>`) {
			t.Errorf("the %s cell's label isn't a small label: %s", label, cells)
		}
	}
}

// The Goal list's Health mark draws the shared .dot in its Health's mark
// colour at 11px, rather than drawing shapes of its own; a Goal with no Health
// keeps its hollow ring (#452).
func TestGoalListHealthMarkDrawsTheSharedShape(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	red := h.ActiveGoal(sam, "Zulu fire", "It matters.")
	h.Checkin(sam, red.ID, domain.HealthRed, "On fire.", "Put it out.", pathDate)
	h.ActiveGoal(sam, "Overdue work", "It matters.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals")
	root := parsePage(t, page)
	for _, mark := range findAllByTestID(root, "goal-row-health") {
		dots := findAll(mark, func(n *html.Node) bool { return slices.Contains(strings.Fields(attrOf(n, "class")), "dot") })
		if health := attrOf(mark, "aria-label"); (health == "Red") != (len(dots) == 1) {
			t.Errorf("the %s row's mark holds %d .dot, want one for a Health and none without: %s", health, len(dots), renderNode(mark))
		}
	}
	for sel, want := range map[string]string{
		".gl-mark .dot": "--dot-size:11px",
		".gl-mark.r":    "color:var(--health-r-mark)",
		".gl-mark.y":    "color:var(--health-y-mark)",
		".gl-mark.g":    "color:var(--health-g-mark)",
		".gl-mark.none": "border:1.5px solid var(--color-ink-muted)",
	} {
		if decls, ok := ruleFor(page, sel); !ok || !strings.Contains(decls, want) {
			t.Errorf("the Goal list's %s{%s} doesn't declare %s", sel, decls, want)
		}
	}
	if strings.Contains(between(t, page, ".gl-mark{", "</style>"), "clip-path") {
		t.Errorf("the Goal list draws a Health shape of its own rather than the shared .dot")
	}
}

// A Goal's history marks a Check-in with the shared Health shape at 10px,
// scaled by --dot-size, rather than resizing Yellow's on its own (#452).
func TestHistoryCheckinMarkScalesTheSharedShape(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Slipping.", "Add a second on-call.", pathDate)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	if !strings.Contains(page, `<span class="tl-mark y" aria-hidden="true"><span class="dot"></span></span>`) {
		t.Fatalf("the history has no Yellow Check-in mark, so this test checks nothing:\n%s", page)
	}
	if decls, ok := ruleFor(page, ".tl-mark .dot"); !ok || !strings.Contains(decls, "--dot-size:10px") {
		t.Errorf(".tl-mark .dot{%s} doesn't scale the shared shape to 10px", decls)
	}
	for _, sel := range []string{".tl-mark.r .dot", ".tl-mark.y .dot", ".tl-mark.g .dot"} {
		if decls, ok := ruleFor(page, sel); ok {
			t.Errorf("the history sizes %s{%s} on its own", sel, decls)
		}
	}
}

// The Goal list, Goal page and Goal table style tests below pin the rules
// goals_test.go's behaviour tests leave to this file, and the classes those
// rules select (#521).

// goalListClasses are the classes n carries, in order.
func goalListClasses(n *html.Node) []string { return strings.Fields(attrOf(n, "class")) }

// assertGoalsRuleDeclares fails unless css has a rule for sel declaring
// each of wants.
func assertGoalsRuleDeclares(t *testing.T, css, sel string, wants ...string) {
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

// The Goal list's Health groups draw each header row and each row's Health
// mark in the group's tint, and a Goal with no Health shows its Lifecycle as a
// neutral chip: each header row is a .gl-group, its shape a .gl-mark in the
// group's tint, each row's mark a .gl-mark in its Health's, and the chip a
// .badge.lc (TestGoalListGroupsByHealthProblemsFirstInEach).
func TestGoalListGroupsByHealthProblemsFirstInEachStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	later := testsupport.Epoch.AddDate(0, 2, 0)
	h.CreateGoal(sam, "Proposed idea", "It matters.")
	for health, title := range map[string]string{domain.HealthRed: "Zulu fire", domain.HealthYellow: "Wobbly", domain.HealthGreen: "Alpha green"} {
		g := h.ActiveGoal(sam, title, "It matters.")
		h.Checkin(sam, g.ID, health, "Status.", "Fix it.", later)
	}

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals")
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	tints := map[string]string{"No Health": "none", "Red": "r", "Yellow": "y", "Green": "g"}
	groups := findAllByTestID(parsePage(t, page), "goal-health-group")
	if len(groups) != len(tints) {
		t.Fatalf("got %d Health groups, want %d", len(groups), len(tints))
	}
	for _, group := range groups {
		head := findByTestID(t, group, "goal-group-head")
		name := textOf(findByTestID(t, head, "goal-group-label"))
		tint := tints[name]
		if !flatFrameHasClass(head, "gl-group") {
			t.Errorf("%s: header row isn't a .gl-group row: %s", name, renderNode(head))
		}
		if mark := findByTestID(t, head, "goal-group-mark"); !slices.Equal(goalListClasses(mark), []string{"gl-mark", tint}) {
			t.Errorf("%s: header row's shape isn't a .gl-mark.%s: %s", name, tint, renderNode(mark))
		}
		for _, row := range goalListRowsIn(group) {
			if mark := findByTestID(t, row, "goal-row-health"); !slices.Equal(goalListClasses(mark), []string{"gl-mark", tint}) {
				t.Errorf("%s row's Health mark isn't a .gl-mark.%s: %s", name, tint, renderNode(mark))
			}
		}
		if tint == "none" {
			if chip := findByTestID(t, group, "goal-row-lifecycle"); !slices.Equal(goalListClasses(chip), []string{"badge", "lc"}) {
				t.Errorf("a Goal with no Health doesn't show its Lifecycle as a .badge.lc chip: %s", renderNode(chip))
			}
		}
	}
	for tint, want := range map[string]string{"r": "color:var(--health-r-mark)", "y": "color:var(--health-y-mark)", "g": "color:var(--health-g-mark)", "none": "border:1.5px solid var(--color-ink-muted)"} {
		assertGoalsRuleDeclares(t, page, ".gl-mark."+tint, want)
	}
	assertGoalsRuleDeclares(t, css, ".badge", "border-radius:var(--radius-sm)")
	assertGoalsRuleDeclares(t, css, ".lc", "background:var(--color-surface-alt)")
}

// A grouped Goal list's group headers sit on the neutral group-header surface,
// not a teal fill: a header row is nothing to act on, and teal is kept for
// what is (DESIGN.md § Components: List rows).
func TestGoalListGroupHeadersAreNeutralOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.AssignGoalValue(h.CreateGoal(sam, "Alpha", "A matters."), pillar.Values[0])
	ts := newServer(t, h)

	grouped := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals?group="+strconv.FormatInt(pillar.ID, 10))
	heads := findAllByTestID(parsePage(t, grouped), "goal-group-head")
	if len(heads) == 0 {
		t.Fatalf("grouped list has no group header row; body:\n%s", grouped)
	}
	for _, head := range heads {
		if !flatFrameHasClass(head, "gl-group") {
			t.Errorf("group header row isn't a .gl-group row: %s", renderNode(head))
		}
	}
	assertGoalsRuleDeclares(t, grouped, ".gl-group th", "background:var(--color-group)")
}

// A Green Goal past a Milestone is marked Milestone overdue in Check-in
// overdue's chip, a .badge.st, which app.css draws in Check-in overdue's fill
// and dashed edge (TestGoalListMarksAGreenGoalPastAMilestone).
func TestGoalListMarksAGreenGoalPastAMilestoneStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	overdue, _ := h.GreenGoalWithOverdueMilestone(sam, "Zulu launch")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals")
	if chip := findByTestID(t, goalListRowOf(t, page, overdue), "milestone-overdue"); !slices.Equal(goalListClasses(chip), []string{"badge", "st"}) {
		t.Errorf("Milestone overdue chip isn't a .badge.st: %s", renderNode(chip))
	}
	assertGoalsRuleDeclares(t, getBody(t, http.DefaultClient, ts.URL+"/static/app.css"), ".st", "background:var(--checkin-overdue-bg)", "border-style:dashed")
}

// A row's Due cell is a .num.gl-dates, whose dates, each its own element, are
// each kept whole on a line while the cell itself wraps between them, so a
// slipped Goal's dates never break mid-date (#180;
// TestGoalListShowsDueDateAndLastCheckin).
func TestGoalListShowsDueDateAndLastCheckinStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	slipped := h.ActiveGoal(sam, "Slipped", "It matters.")
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID:             slipped.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthYellow,
		Status:             "Later than planned.",
		PathToGreen:        "Swap vendors.",
		PathTargetDate:     testsupport.Epoch.AddDate(0, 2, 0),
		DeliveryDate:       time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		DeliveryDateReason: "Vendor slipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals")
	due := findByTestID(t, goalListRowOf(t, parsePage(t, page), slipped), "goal-row-due")
	if !slices.Equal(goalListClasses(due), []string{"num", "gl-dates"}) {
		t.Errorf("the Due cell isn't a .num.gl-dates: %s", renderNode(due))
	}
	if dates := goalListDueDates(due); len(dates) != 2 {
		t.Errorf("the Due cell's dates aren't each their own element for .gl-dates>* to keep whole: %s", renderNode(due))
	}
	assertGoalsRuleDeclares(t, page, ".gl-dates>*", "white-space:nowrap")
	if rule, ok := ruleFor(page, ".gl-dates"); ok && strings.Contains(rule, "nowrap") {
		t.Errorf("the whole Due cell is kept on one line, so its slips can't wrap: .gl-dates{%s}", rule)
	}
}

// At phone width the Goal list's rows stack (#258): the Goal across the top,
// its Health's shape in the row's gutter beside it, then Owner, Due and Last
// check-in wrapping under it, Due and Last check-in each led by a label only
// the phone shows, with the Dimension tags hidden.
func TestGoalListRowsStackAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship the phone layout", "It matters.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals")
	root := parsePage(t, page)
	tables := findAll(findByTestID(t, root, "goal-list"), func(n *html.Node) bool { return n.Data == "table" })
	if len(tables) != 1 || !slices.Equal(goalListClasses(tables[0]), []string{"gl-list"}) {
		t.Errorf("the list layout's table has no gl-list class to style")
	}
	rows := goalListRowsIn(root)
	if got := goalRowTitlesIn(rows, goal); !slices.Equal(got, []string{goal.Title}) {
		t.Fatalf("rows = %q", got)
	}
	cells := findAll(rows[0], func(n *html.Node) bool { return n.Data == "td" })
	for _, class := range [][]string{{"gl-health"}, {"gl-goal"}, {"gl-owner"}, {"num", "gl-dates"}, {"muted", "gl-checkin"}} {
		if !slices.ContainsFunc(cells, func(c *html.Node) bool { return slices.Equal(goalListClasses(c), class) }) {
			t.Errorf("the row has no %s cell to style: %s", strings.Join(class, " "), renderNode(rows[0]))
		}
	}
	if tags := findAll(rows[0], func(n *html.Node) bool { return slices.Equal(goalListClasses(n), []string{"gl-tags"}) }); len(tags) == 0 {
		t.Errorf("the row has no gl-tags to style: %s", renderNode(rows[0]))
	}

	phone := mediaBlock(t, page, "@media (max-width:600px)")
	for selector, want := range map[string]string{
		".gl-list thead":      "display:none",
		".gl-list tr":         "display:flex",
		".gl-list td":         "display:block",
		".gl-list .gl-goal":   "order:-1",
		".gl-list .gl-tags":   "display:none",
		".gl-dates::before":   `content:"Due "`,
		".gl-checkin::before": `content:"Last check-in "`,
		".gl-list .gl-owner":  "white-space:normal",
		".gl-list [colspan]":  "flex-basis:100%",
		".gl-list .gl-health": "order:-2",
	} {
		rule, ok := ruleFor(phone, selector)
		if !ok {
			t.Errorf("at 600px there is no %s rule", selector)
		} else if !strings.Contains(rule, want) {
			t.Errorf("at 600px %s{%s} lacks %s", selector, rule, want)
		}
	}
	if rule, _ := ruleFor(phone, ".gl-list .gl-goal"); !strings.Contains(rule, "flex-basis:100%") {
		t.Errorf("at 600px the Goal cell .gl-list .gl-goal{%s} shares its line with the Owner rather than heading the row", rule)
	}
	outside := strings.Replace(page, phone, "", 1)
	for _, label := range []string{".gl-dates::before", ".gl-checkin::before"} {
		if strings.Contains(outside, label) {
			t.Errorf("%s labels the cell outside the phone block, so the desktop table shows it too", label)
		}
	}
}

// goalsStyleSheetIn is the stylesheet a Goals page carries itself: the text
// of its <style> elements, in order.
func goalsStyleSheetIn(root *html.Node) string {
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

// The Goal list filter bar opens with the Scope chip, the shared filter chip
// in its on state with its key muted, inside the bar that lays its chips out
// as the form's own row (TestGoalListScopesToMyOrg).
func TestGoalListScopesToMyOrgStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "vera@example.com"), scopeOrgURL(ts.URL, "/goals", org.Vera))
	bar := findByTestID(t, page, "goal-filter-bar")
	if !flatFrameHasClass(bar, "gl-bar") {
		t.Errorf("the filter bar isn't a .gl-bar: %s", renderNode(bar))
	}
	chip := scopeSelectIn(t, page, "goal-scope-filter").Parent
	if !flatFrameHasClass(chip, "filter-chip") || !flatFrameHasClass(chip, "on") {
		t.Errorf("the Scope chip isn't a .filter-chip.on: %s", renderNode(chip))
	}
	if key := firstElementChild(chip); key == nil || !flatFrameHasClass(key, "chip-key") {
		t.Errorf("the Scope chip's key isn't a .chip-key: %s", renderNode(chip))
	}
	if decls, ok := ruleFor(goalsStyleSheetIn(page), ".gl-bar"); !ok || !strings.Contains(decls, "display:contents") {
		t.Errorf("the Goal list's .gl-bar{%s} doesn't lay its chips into the form's row", decls)
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	for sel, want := range map[string]string{
		".filter-chip.on": "background:var(--color-primary-subtle)",
		".chip-key":       "color:var(--color-ink-muted)",
	} {
		if decls, ok := ruleFor(css, sel); !ok || !strings.Contains(decls, want) {
			t.Errorf("app.css %s{%s} doesn't declare %s", sel, decls, want)
		}
	}
}

// On a coarse pointer the Goal page's group heads' small links — Manage,
// Edit, + Add — are 44px tall, as DESIGN.md's accessibility rule asks; on a
// fine pointer they keep their size (#404).
func TestGoalPageGroupHeadLinksAre44pxOnCoarsePointer(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Grow revenue", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	heads := findAll(page, func(n *html.Node) bool { return flatFrameHasClass(n, "gp-group-head") })
	if len(heads) == 0 {
		t.Fatalf("the Goal page has no .gp-group-head")
	}
	links := findAll(heads[0], func(n *html.Node) bool { return n.Data == "a" })
	if len(links) == 0 || attrOf(links[0], "class") != "small" {
		t.Fatalf("the group head's link isn't its small link: %s", renderNode(heads[0]))
	}
	css := goalsStyleSheetIn(page)
	coarse := strings.Join(mediaBlocks(css, "@media (pointer:coarse)"), "}")
	if rule, ok := ruleFor(coarse, ".gp-group-head>a"); !ok || !strings.Contains(rule, "min-height:44px") {
		t.Errorf("on a coarse pointer the group head's link isn't 44px tall: %q", rule)
	}
	if rule, ok := ruleFor(between(t, css, ".gp-group-head{", "@media"), ".gp-group-head>a"); ok && strings.Contains(rule, "min-height") {
		t.Errorf("on a fine pointer the group head's link is made taller: %q", rule)
	}
}

// No CSS order redraws the Goal list's filter bar out of its markup order, so
// Tab follows it as it is drawn (TestGoalFilterBarTabOrderFollowsItsDrawnOrder,
// #404): no rule for the form, its row, More filters' panel or Clear's slot
// sets order.
func TestGoalFilterBarTabOrderFollowsItsDrawnOrderStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	h.CreateGoal(sam, "Grow revenue", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, fmt.Sprintf("%s/goals?value=%d", ts.URL, pillar.Values[0].ID))
	for id, class := range map[string]string{
		"goal-filters":            "gl-filters",
		"goal-filter-bar":         "gl-bar",
		"more-filters-panel":      "gl-more",
		"goal-filters-clear-slot": "gl-clear",
	} {
		if n := findByTestID(t, page, id); !flatFrameHasClass(n, class) {
			t.Errorf("the filter bar's %s isn't a .%s: %s", id, class, renderNode(n))
		}
	}
	bar := regexp.MustCompile(`\.gl-(filters|bar|more|clear)\b`)
	order := regexp.MustCompile(`(^|;)\s*order:`)
	for _, rule := range strings.Split(between(t, goalsStyleSheetIn(page), ".gl-filters{", ""), "}") {
		at := strings.LastIndex(rule, "{")
		if at < 0 {
			continue
		}
		if bar.MatchString(rule[:at]) && order.MatchString(rule[at+1:]) {
			t.Errorf("a filter bar rule redraws it out of markup order: %s}", strings.TrimSpace(rule))
		}
	}
}

// The Goal list's header is the page's header strip, its count muted, the
// List / Table switch a segmented control, and New goal a primary button
// (DESIGN.md § Goal list; TestGoalListHeaderOpensProposeFormThatSwapsTheList).
func TestGoalListHeaderOpensProposeFormThatSwapsTheListStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals")
	head := findByTestID(t, page, "goals-head")
	layouts := findAll(head, func(n *html.Node) bool { return n.Data == "nav" && attrOf(n, "aria-label") == "Layout" })
	if len(layouts) != 1 {
		t.Fatalf("header has %d Layout switches, want 1: %s", len(layouts), renderNode(head))
	}
	for _, c := range []struct {
		what    string
		n       *html.Node
		classes []string
	}{
		{"header", head, []string{"page-header", "gl-ph"}},
		{"count", findByTestID(t, head, "goal-count"), []string{"ph-count"}},
		{"Layout switch", layouts[0], []string{"gl-seg"}},
		{"New goal", findByTestID(t, head, "new-goal"), []string{"btn", "primary", "sm"}},
	} {
		for _, class := range c.classes {
			if !flatFrameHasClass(c.n, class) {
				t.Errorf("the header's %s isn't a .%s: %s", c.what, class, renderNode(c.n))
			}
		}
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	for sel, want := range map[string]string{
		".page-header": "border-bottom:1px solid var(--color-border)",
		".ph-count":    "color:var(--color-ink-muted)",
		".btn.primary": "background:var(--color-primary-strong)",
	} {
		if decls, ok := ruleFor(css, sel); !ok || !strings.Contains(decls, want) {
			t.Errorf("app.css %s{%s} doesn't declare %s", sel, decls, want)
		}
	}
	if decls, ok := ruleFor(goalsStyleSheetIn(page), ".gl-seg"); !ok || !strings.Contains(decls, "display:inline-flex") {
		t.Errorf("the Goal list's .gl-seg{%s} doesn't lay its segments in a row", decls)
	}
}

// The List / Table switch shows its chosen segment by a canvas fill and a
// strong primary edge, 3:1 on the chip track in light (WCAG 1.4.11), since the
// fill alone is 1.13:1 (DESIGN.md § Segmented controls). Every segment
// reserves the edge as a transparent border, as the cadence's do, so choosing
// one shifts nothing, and its touch height counts the border.
func TestGoalListLayoutSwitchEdgesTheChosenSegment(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/goals")
	list, table := findByTestID(t, page, "layout-list"), findByTestID(t, page, "layout-table")
	if nav := list.Parent; nav.Data != "nav" || !flatFrameHasClass(nav, "gl-seg") || table.Parent != nav {
		t.Errorf("the List and Table segments aren't links in the .gl-seg switch: %s", renderNode(nav))
	}
	if attrOf(list, "aria-current") != "page" || hasAttr(table, "aria-current") {
		t.Errorf("on the list, List isn't the one segment chosen: %s %s", renderNode(list), renderNode(table))
	}
	style := between(t, goalsStyleSheetIn(page), ".gl-seg{", "")
	for sel, want := range map[string][]string{
		".gl-seg a":                    {"box-sizing:border-box", "border:1px solid transparent"},
		".gl-seg a[aria-current=page]": {"background:var(--color-canvas)", "border-color:var(--color-primary-strong)", "color:var(--color-ink)"},
	} {
		rule, ok := ruleFor(style, sel)
		if !ok {
			t.Errorf("the style block has no %s rule", sel)
		}
		for _, decl := range want {
			if !strings.Contains(rule, decl) {
				t.Errorf("%s declares %q, want %s", sel, rule, decl)
			}
		}
	}
	if rule, _ := ruleFor(style, ".gl-seg a[aria-current=page]"); strings.Contains(rule, "border:") || strings.Contains(rule, "border-width") || strings.Contains(rule, "padding") {
		t.Errorf("choosing a layout resizes its segment: %s", rule)
	}
}

// assertGoalPageCarriesClass fails unless n carries each of classes.
func assertGoalPageCarriesClass(t *testing.T, what string, n *html.Node, classes ...string) {
	t.Helper()
	for _, class := range classes {
		if !flatFrameHasClass(n, class) {
			t.Errorf("%s isn't a .%s: %s", what, class, renderNode(n))
		}
	}
}

// The Goal page's title is 28px over a 16px lead, the delivery date's dates
// each wrap whole, never mid-date (#180), the rail sits 300px wide beside a
// column of up to 860px behind a 1px left edge and stacks between the title
// and the column at 900px and below (#674), Check in is the primary button, and a Dimension value is a
// tag (DESIGN.md § Properties rail).
func TestGoalPageRailHoldsTheGoalsPropertiesStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	growth := h.CreateDimension(ada, "Pillar", "Growth").Values[0]
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, growth)
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Later than planned.", "Swap vendors.", testsupport.Epoch.AddDate(0, 2, 0))
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	main := findByTestID(t, root, "goal-main")
	assertGoalPageCarriesClass(t, "the head", findByTestID(t, root, "goal-head"), "gp-head")
	assertGoalPageCarriesClass(t, "Why it matters", findByTestID(t, root, "goal-why-it-matters"), "gp-lead")
	assertGoalPageCarriesClass(t, "the delivery date", findByTestID(t, root, "goal-delivery-date"), "gp-dates")
	assertGoalPageCarriesClass(t, "the rail", findByTestID(t, root, "goal-sidebar"), "gp-rail")
	assertGoalPageCarriesClass(t, "the document column", main, "gp-doc")
	assertGoalPageCarriesClass(t, "the body around the column and rail", main.Parent, "gp-body")
	assertGoalPageCarriesClass(t, "Check in", findByTestID(t, root, "checkin-link"), "btn", "primary", "sm")
	assertGoalPageCarriesClass(t, "the Dimension value", findByTestID(t, root, "goal-dimension-value"), "tag")

	css := goalsStyleSheetIn(root)
	assertGoalsRuleDeclares(t, css, ".gp-head h1", "font-size:28px")
	assertGoalsRuleDeclares(t, css, ".gp-lead", "font-size:16px")
	assertGoalsRuleDeclares(t, css, ".gp-dates>*", "white-space:nowrap")
	assertGoalsRuleDeclares(t, css, ".gp-rail", "border-left:1px solid var(--color-border)")
	assertGoalsRuleDeclares(t, css, ".gp-body", "grid-template-columns:minmax(0,1fr) 300px")
	assertGoalsRuleDeclares(t, css, ".gp-doc", "max-width:860px")
	assertGoalsRuleDeclares(t, mediaBlock(t, css, "@media (max-width:900px)"), ".gp-body", "grid-template-columns:minmax(0,1fr)")
	appCSS := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	assertGoalsRuleDeclares(t, appCSS, ".btn.primary", "background:var(--color-primary-strong)")
	assertGoalsRuleDeclares(t, appCSS, ".tag", "border-radius:var(--radius-full)")
}

// Above 900px the grid puts the head and the column on the left, the head
// over the column, and runs the rail down the right the page's full height,
// so the page looks as it did when the head sat inside the column. At 900px
// and below it drops those placements and stacks the three in DOM order:
// head, rail, column, the rail between hairlines (#674).
func TestGoalPageGridPlacesHeadRailAndColumnStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)

	css := goalsStyleSheetIn(getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)))
	assertGoalsRuleDeclares(t, css, ".gp-body", "grid-template-columns:minmax(0,1fr) 300px", "grid-template-rows:auto 1fr")
	assertGoalsRuleDeclares(t, css, ".gp-head", "grid-column:1", "grid-row:1", "max-width:860px", "margin:0 auto", "padding:32px 28px 0")
	assertGoalsRuleDeclares(t, css, ".gp-doc", "grid-column:1", "grid-row:2", "max-width:860px", "padding:40px 28px 64px", "gap:40px")
	assertGoalsRuleDeclares(t, css, ".gp-rail", "grid-column:2", "grid-row:1/-1", "border-left:1px solid var(--color-border)")

	narrow := mediaBlock(t, css, "@media (max-width:900px)")
	assertGoalsRuleDeclares(t, narrow, ".gp-body", "grid-template-columns:minmax(0,1fr)", "grid-template-rows:none")
	for _, sel := range []string{".gp-head", ".gp-doc", ".gp-rail"} {
		assertGoalsRuleDeclares(t, narrow, sel, "grid-column:auto", "grid-row:auto")
	}
	assertGoalsRuleDeclares(t, narrow, ".gp-head", "max-width:none", "padding:20px 16px 0")
	assertGoalsRuleDeclares(t, narrow, ".gp-rail", "border-left:0", "border-top:1px solid var(--color-border)", "border-bottom:1px solid var(--color-border)")
}

// A Goal with no Health says so in muted text in its rail.
func TestGoalPageRailShowsOnlyWhatTheGoalHasStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Tidy the backlog", "Nobody can find anything.")
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	_, props := goalRailPropertiesIn(t, root)
	health := props["Health"]
	if health == nil {
		t.Fatalf("the rail has no Health row")
	}
	muted := findAll(health, func(n *html.Node) bool { return n != health && textOf(n) == "No Health" })
	if len(muted) != 1 {
		t.Fatalf("the Health row doesn't set No Health apart: %s", renderNode(health))
	}
	assertGoalPageCarriesClass(t, "No Health", muted[0], "muted")
	assertGoalsRuleDeclares(t, getBody(t, http.DefaultClient, ts.URL+"/static/app.css"), ".muted", "color:var(--color-ink-muted)")
}

// At phone width the Goal page's header strip wraps its actions onto their own
// line rather than squeezing the breadcrumb to nothing, and the breadcrumb's
// titles end in an ellipsis rather than widening the page. Wrapped, the
// actions stay flush right: the More menu opens leftward from its button's
// right edge, and a left-aligned button would push it off-screen (#42).
func TestGoalPageHeaderWrapsActionsAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "A to-ce the API tier", "Callers need one front door.")
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	assertGoalPageCarriesClass(t, "the header strip", findByTestID(t, root, "goal-page-header"), "gp-ph")
	assertGoalPageCarriesClass(t, "the header's actions", findByTestID(t, root, "goal-actions"), "gp-actions")
	crumbs := findByTestID(t, root, "goal-breadcrumb")
	for _, title := range findAll(crumbs, func(n *html.Node) bool { return attrOf(n, "aria-current") == "page" }) {
		assertGoalPageCarriesClass(t, "the breadcrumb's title", title, "gp-crumb")
	}
	css := goalsStyleSheetIn(root)
	if phone, ok := ruleFor(strings.Join(mediaBlocks(css, "@media (max-width:600px)"), "\n"), ".gp-ph"); !ok || !strings.Contains(phone, "flex-wrap:wrap") {
		t.Errorf("the header strip doesn't wrap its actions at phone width: .gp-ph{%s}", phone)
	}
	assertGoalsRuleDeclares(t, css, ".gp-crumb", "text-overflow:ellipsis")
	assertGoalsRuleDeclares(t, css, ".gp-actions", "margin-left:auto")
}

// Each Milestone date, the current one and each struck slip, wraps as a whole,
// never mid-date ("2026-04-" over "02") at phone width, while the breaks between
// the dates still wrap (#172): the dates in a .gp-dates cell keep to one line,
// the cell itself doesn't.
func TestGoalPageMilestoneDatesDoNotBreakOverHTTPStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	section := findByTestID(t, root, "goal-milestones")
	assertGoalPageCarriesClass(t, "the Milestone's date cell", goalMilestoneDateCellIn(t, section, findByTestID(t, section, "goal-milestone")), "gp-dates")
	css := goalsStyleSheetIn(root)
	assertGoalsRuleDeclares(t, css, ".gp-dates>*", "white-space:nowrap")
	if rule, ok := ruleFor(css, ".gp-dates"); ok && strings.Contains(rule, "nowrap") {
		t.Errorf("the whole date cell is kept on one line, so its slips can't wrap: .gp-dates{%s}", rule)
	}
}

// A Metric is a row of the grid its name, sparkline and current value sit in,
// the current value in tabular figures.
func TestGoalPageMetricRowsShowASparklineStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	ctx := t.Context()
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	if _, err := h.Service.MarkGoalDated(ctx, goal.Owner.ID, goal.ID, testsupport.Epoch.AddDate(0, 6, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	metric, err := h.Service.AddMetric(ctx, goal.Owner.ID, domain.AddMetricInput{
		GoalID: goal.ID, Name: "p95 latency", Unit: "ms", Direction: domain.MetricDown,
		Baseline: 1200, Target: 400, TargetDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.ActivateGoal(ctx, goal.Owner.ID, goal.ID); err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Faster.",
		Readings: []domain.MetricReadingInput{{MetricID: metric.ID, Value: 800}},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	card := findByTestID(t, root, "goal-metric")
	assertGoalPageCarriesClass(t, "the Metric", card, "gp-metric")
	assertGoalPageCarriesClass(t, "the current value", findByTestID(t, card, "metric-current"), "num")
	assertGoalsRuleDeclares(t, goalsStyleSheetIn(root), ".gp-metric", "display:grid")
	assertGoalsRuleDeclares(t, getBody(t, http.DefaultClient, ts.URL+"/static/app.css"), ".num", "font-variant-numeric:tabular-nums")
}

// A Proposed Goal's checklist tints an unmet item's ○ mark in the neutral
// muted ink and a met item's ✓ in Green's ink, never in Red's: each item is a
// .gp-check, a met one .done too, its mark its first span.
func TestProposedGoalChecklistMarksUnmetItemsToDoStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	items := goalActivationChecklistItemsIn(t, page)
	if len(items) == 0 {
		t.Fatalf("the checklist has no items")
	}
	for _, li := range items {
		done := attrOf(li, "data-done") == "true"
		if !flatFrameHasClass(li, "gp-check") || flatFrameHasClass(li, "done") != done {
			t.Errorf("checklist item done=%v isn't a .gp-check marked .done only when done: %s", done, renderNode(li))
		}
		if mark := goalFirstContentOf(li); mark == nil || mark.Data != "span" || textOf(mark) != map[bool]string{true: "✓", false: "○"}[done] {
			t.Errorf("checklist item's first span isn't its mark: %s", renderNode(li))
		}
	}
	css := goalsStyleSheetIn(page)
	assertGoalsRuleDeclares(t, css, ".gp-check>span:first-child", "color:var(--color-ink-muted)")
	assertGoalsRuleDeclares(t, css, ".gp-check.done>span:first-child", "color:var(--health-g-ink)")
	if regexp.MustCompile(`\.gp-check[^{}]*\{[^}]*--health-r-ink`).MatchString(css) {
		t.Errorf("the checklist still uses Red's ink")
	}
}

// Each section of the Goal page's document column is a 13/600 heading over a
// --color-border hairline, with its rows ruled by hairlines too, and none sits
// in a box or under a rule of its own: the space between sections groups them
// (DESIGN.md § Goal page sections).
func TestGoalPageSectionsAreHeadingsOverHairlineRows(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	page := goalPageWithEverySection(t, h, ts)

	for _, tc := range []struct{ tag, testID string }{
		{"section", "goal-checkins"},
		{"section", "goal-metrics"},
		{"section", "goal-milestones"},
		{"section", "goal-highlights"},
		{"section", "goal-draft-highlights"},
		{"section", "goal-action-items"},
		{"section", "goal-comments"},
		{"div", "goal-history"},
	} {
		block := findByTestID(t, page, tc.testID)
		if block.Data != tc.tag || !flatFrameHasClass(block, "gp-section") || flatFrameHasClass(block, "ruled") || flatFrameHasClass(block, "card") {
			t.Errorf("%s is not a plain section: class=%q", tc.testID, attrOf(block, "class"))
		}
	}
	if main := findByTestID(t, page, "goal-main"); !flatFrameHasClass(main, "gp-doc") {
		t.Errorf("the document column isn't a .gp-doc: class=%q", attrOf(main, "class"))
	}
	css := goalsStyleSheetIn(page)
	for _, sel := range []string{".gp-section>h2", ".gp-section>.gp-group-head"} {
		assertGoalsRuleDeclares(t, css, sel, "border-bottom:1px solid var(--color-border)")
	}
	assertGoalsRuleDeclares(t, css, ".gp-doc h2", "font-size:var(--text-h2)", "font-weight:600")
	assertGoalsRuleDeclares(t, css, ".gp-rows>li", "border-bottom:1px solid var(--color-border)")
	lists := findAll(findByTestID(t, page, "goal-highlights"), func(n *html.Node) bool { return n.Data == "ul" })
	if len(lists) == 0 || !flatFrameHasClass(lists[0], "gp-rows") {
		t.Errorf("the Highlights are not rows")
	}
}

// The Why it matters' What goes here? button reads as the muted explanation
// text it opens, a .explain, at the lead's secondary size, a .gp-lead-help.
func TestGoalPageExplainsWhyItMattersAndTopLevelOnActivationStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	g := h.ActiveGoal(ada, "Grow revenue", "It pays for everything.")

	page := getPage(t, signInClient(t, ts.URL, ada.Email), goalPageURL(ts.URL, g))
	button := buttonNamed(page, "What goes here?")
	if button == nil {
		t.Fatalf("the Goal page has no What goes here? button")
	}
	for _, class := range []string{"explain", "gp-lead-help"} {
		if !flatFrameHasClass(button, class) {
			t.Errorf("What goes here? isn't a .%s: class=%q", class, attrOf(button, "class"))
		}
	}
	assertGoalsRuleDeclares(t, goalsStyleSheetIn(page), ".gp-lead-help", "font-size:var(--text-secondary)")
	assertGoalsRuleDeclares(t, getBody(t, http.DefaultClient, ts.URL+"/static/app.css"), ".explain", "color:var(--color-ink-muted)")
}

// A Goal list row shows a value of a Retired Dimension as the same outlined
// pill, a .tag, as any other value.
func TestRetiredDimensionLeavesGoalListFilterAndGroupingOverHTTPStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	h.AssignGoalValue(alpha, pillar.Values[0])
	if err := h.Service.RetireDimension(t.Context(), boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	ts := newServer(t, h)

	row := goalListRowOf(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals"), alpha)
	tag := findByTestID(t, row, "goal-value-tag")
	if textOf(tag) != "Growth" || !flatFrameHasClass(tag, "tag") {
		t.Errorf("Alpha's Growth isn't a .tag: %s", renderNode(tag))
	}
	assertGoalsRuleDeclares(t, getBody(t, http.DefaultClient, ts.URL+"/static/app.css"), ".tag", "border-radius:var(--radius-full)", "border-style:solid")
}

// On a narrow screen the Goal table scrolls sideways inside its own
// container, the .table-scroll straight around it, rather than widening the
// page.
func TestGoalTableHasNoTotalsRowAndIsntGroupedStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.AssignGoalValue(h.CreateGoal(sam, "Alpha", "It matters."), pillar.Values[0])
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, fmt.Sprintf("%s/goals?layout=table&group=%d", ts.URL, pillar.ID))
	table := findByTestID(t, page, "goal-table")
	first := table.Parent.FirstChild
	for first != nil && first.Type != html.ElementNode {
		first = first.NextSibling
	}
	if scroll := table.Parent; scroll.Data != "div" || !flatFrameHasClass(scroll, "table-scroll") || first != table {
		t.Errorf("the Goal table isn't straight inside its own .table-scroll container: %s", renderNode(scroll))
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if decls, ok := ruleFor(css, ".table-scroll"); !ok || !strings.Contains(decls, "overflow-x:auto") {
		t.Errorf("app.css .table-scroll{%s} doesn't scroll sideways", decls)
	}
}

// At 600px and below the Goal table's scrolling container fades the edge it
// can scroll toward (#686): canvas layers attached local cover fixed
// --color-border-strong fades at whichever end is reached, so a fade shows
// only while there is more to that side. Every column stays, the frame stays
// flat, and wider screens and the other .table-scroll tables are left alone.
// Edit values' container is the same one.
func TestGoalTableScrollFadesItsEdgeAtPhoneWidthStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Ship the phone table", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/goals?layout=table", "/goals?layout=table&edit=1"} {
		root := getPage(t, client, ts.URL+path)
		scroll := findByTestID(t, root, "goal-table-scroll")
		var children []*html.Node
		for c := scroll.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				children = append(children, c)
			}
		}
		if scroll.Data != "div" || !flatFrameHasClass(scroll, "table-scroll") || len(children) != 1 || attrOf(children[0], "data-testid") != "goal-table" {
			t.Errorf("%s: goal-table-scroll isn't a .table-scroll div holding only the Goal table: %s", path, renderNode(scroll))
		}
	}

	css := homeStyleSheetIn(getPage(t, client, ts.URL+"/goals?layout=table"))
	phone := mediaBlocks(css, "@media (max-width:600px)")
	var fade string
	for _, block := range phone {
		if decls, ok := ruleFor(block, ".gl-table .table-scroll"); ok {
			fade = decls
		}
	}
	for _, want := range []string{"local", "scroll", "var(--color-canvas)", "var(--color-border-strong)", "linear-gradient("} {
		if !strings.Contains(fade, want) {
			t.Errorf("at 600px .gl-table .table-scroll{%s} lacks %s", fade, want)
		}
	}
	for _, unwanted := range []string{"box-shadow", "--shadow-pop", "rgba("} {
		if strings.Contains(fade, unwanted) {
			t.Errorf("at 600px .gl-table .table-scroll{%s} isn't flat: it has %s", fade, unwanted)
		}
	}
	outside := css
	for _, block := range phone {
		outside = strings.Replace(outside, block, "", 1)
	}
	if decls, ok := ruleFor(outside, ".gl-table .table-scroll"); ok && strings.Contains(decls, "background") {
		t.Errorf("above 600px .gl-table .table-scroll{%s} has a background", decls)
	}
}

// An Active Goal's Incomplete flag is a quiet status: an .lc alert in the
// neutral chip's surface, not Check-in overdue's dashed .st (CONTEXT.md:
// Incomplete).
func TestGoalPageFlagsIncompleteStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	h.SetDimensionRequired(boss, h.CreateDimension(boss, "Pillar", "Growth"), true)
	ts := newServer(t, h)

	flag := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal)), "goal-incomplete")
	if !flatFrameHasClass(flag, "lc") || flatFrameHasClass(flag, "st") {
		t.Errorf("Incomplete flag isn't an .lc, quieter than Check-in overdue's .st: %s", renderNode(flag))
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	lc, ok := ruleFor(css, ".lc")
	if !ok || !strings.Contains(lc, "background:var(--color-surface-alt)") || strings.Contains(lc, "dashed") {
		t.Errorf("app.css .lc{%s} isn't the quiet neutral surface", lc)
	}
	if st, ok := ruleFor(css, ".st"); !ok || !strings.Contains(st, "border-style:dashed") {
		t.Errorf("app.css .st{%s} lost the dashed edge the .lc flag is quieter than", st)
	}
}

// The Goal page header's Check in is the primary action, filled in the
// primary colour, and No change beside it a quiet one, with no fill or edge of
// its own (TestGoalPageHeaderActionsOverHTTP).
func TestGoalPageHeaderActionsOverHTTPStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	ts := newServer(t, h)

	actions := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal)), "goal-actions")
	if link := findByTestID(t, actions, "checkin-link"); !flatFrameHasClass(link, "btn") || !flatFrameHasClass(link, "primary") {
		t.Errorf("Check in is not a primary button: %s", renderNode(link))
	}
	noChange := buttonNamed(findByTestID(t, actions, "no-change-checkin"), "Check in: no change")
	if noChange == nil || !flatFrameHasClass(noChange, "btn") || !flatFrameHasClass(noChange, "quiet") {
		t.Errorf("No change is not a quiet button: %s", renderNode(actions))
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	for sel, want := range map[string]string{
		".btn.primary": "background:var(--color-primary-strong)",
		".btn.quiet":   "background:transparent",
	} {
		if decls, ok := ruleFor(css, sel); !ok || !strings.Contains(decls, want) {
			t.Errorf("app.css %s{%s} doesn't declare %s", sel, decls, want)
		}
	}
}

// The filter bar's no-script Apply is a full-size button, and the Columns
// form's Apply a small one (TestGoalListApplyButtonOnlyWithoutJavaScript).
func TestGoalListApplyButtonOnlyWithoutJavaScriptStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	page := getBody(t, client, ts.URL+"/goals?layout=table")
	filters := findByTestID(t, goalPageWithoutScript(t, page), "goal-filters")
	if apply := buttonNamed(filters, "Apply"); apply == nil || attrOf(apply, "class") != "btn" {
		t.Errorf("the filter bar's Apply is not a plain .btn: %s", renderNode(filters))
	}
	columns := findByTestID(t, parsePage(t, page), "goal-columns")
	if apply := buttonNamed(columns, "Apply"); apply == nil || !flatFrameHasClass(apply, "btn") || !flatFrameHasClass(apply, "sm") {
		t.Errorf("the Columns form's Apply is not a .btn.sm: %s", renderNode(columns))
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	for sel, want := range map[string]string{
		".btn":    "height:40px",
		".btn.sm": "height:32px",
	} {
		if decls, ok := ruleFor(css, sel); !ok || !strings.Contains(decls, want) {
			t.Errorf("app.css %s{%s} doesn't declare %s", sel, decls, want)
		}
	}
}

// The Delegates group, among the rail's properties, heads itself with a small
// muted .label, the rest with a plain h3: the .label rule sets its size and
// ink (TestGoalPageSidebarLinksOpenTheirFormInPlaceOverHTTP).
func TestGoalPageSidebarLinksOpenTheirFormInPlaceOverHTTPStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.CreateDimension(ada, "Pillar", "Growth")
	h.CreateField(ada, "Budget", domain.FieldNumber, "USD")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))

	for _, tc := range []struct {
		group, heading string
		label          bool
	}{
		{"goal-parents", "Contributes to", false},
		{"goal-delegates", "Delegates", true},
		{"goal-dimensions", "Dimensions", false},
		{"goal-field-block", "Fields", false},
	} {
		heading := headingNamed(findByTestID(t, page, tc.group), tc.heading)
		if heading == nil || heading.Data != "h3" {
			t.Errorf("%s has no h3 reading %s", tc.group, tc.heading)
			continue
		}
		if got := len(heading.Attr) > 0; got != tc.label || tc.label && (len(heading.Attr) != 1 || !flatFrameHasClass(heading, "label")) {
			t.Errorf("%s's heading is %s, want a .label: %v", tc.group, renderNode(heading), tc.label)
		}
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if decls, ok := ruleFor(css, ".label"); !ok || !strings.Contains(decls, "font-size:var(--text-label)") || !strings.Contains(decls, "color:var(--color-ink-muted)") {
		t.Errorf("app.css .label{%s} doesn't set a small muted label", decls)
	}
}

// A linked Goal's Health and its title sit side by side in a .gp-link-head,
// which lays them out in a row, never a column
// (TestGoalPageLinkedGoalsLeadWithTheirHealth).
func TestGoalPageLinkedGoalsLeadWithTheirHealthStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	children := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, f.parent)), "goal-children")
	parents := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, f.yellow)), "goal-parents")
	for _, link := range []*html.Node{linkTo(children, goalHref(f.yellow.ID)), linkTo(parents, goalHref(f.parent.ID))} {
		if link == nil || !flatFrameHasClass(link.Parent, "gp-link-head") {
			t.Fatalf("a linked Goal's title doesn't sit in a .gp-link-head:\n%s\n%s", renderNode(children), renderNode(parents))
		}
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if rule, _ := ruleFor(css, ".gp-link-head"); !strings.Contains(rule, "display:flex") || strings.Contains(rule, "column") {
		t.Errorf("a linked Goal's Health and title don't sit on one line: .gp-link-head{%s}", rule)
	}
}

// The goal form's date warnings sit on their own line in the neutral ink,
// not the danger ink a refusal takes (#675).
func TestGoalFormInputWarningIsNeutralStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	page := getBody(t, client, ts.URL+"/goals/new")

	rule := cssRule(t, page, ".gf-input-warning")
	for _, want := range []string{"display:block", "font-size:var(--text-secondary)", "color:var(--color-ink-2)"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".gf-input-warning{%s} lacks %s", rule, want)
		}
	}
	for _, unwanted := range []string{"danger", "border", "outline"} {
		if strings.Contains(rule, unwanted) {
			t.Errorf(".gf-input-warning{%s} has %s, want it neutral", rule, unwanted)
		}
	}
}

// Finishing a saved Goal, its Title is text in the Title input's heading
// style, 28/1.2 at 600, with no underline or box to look editable (#673).
func TestDefineGoalTitleTextIsHeadingStyleStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/define", ts.URL, g.ID))

	title := findByTestID(t, parsePage(t, page), "goal-form-title")
	if !slices.Contains(strings.Fields(attrOf(title, "class")), "gf-title-text") {
		t.Errorf("the Title isn't styled by .gf-title-text: %s", renderNode(title))
	}
	rule := cssRule(t, page, ".gf-what .gf-title-text")
	for _, want := range []string{"font-size:28px", "line-height:1.2", "font-weight:600", "letter-spacing:-0.02em", "border:0"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".gf-title-text{%s} lacks %s", rule, want)
		}
	}
	if strings.Contains(rule, "border-bottom") {
		t.Errorf(".gf-title-text{%s} is underlined like an input", rule)
	}
}

// The Contributes to picker's error is styled as the goal form's field errors
// are, on Suggest a parent and the Goal page's Contribute to a Goal too,
// which don't carry the goal form's styles: the picker brings its own
// .gf-input-error rule (#656).
func TestParentPickerErrorIsInTheDangerInkStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	for _, tc := range []struct {
		name, who, opened, action string
	}{
		{"Suggest a parent", "pat@example.com", fmt.Sprintf("/goals/%d/suggest-parent", goal.ID), fmt.Sprintf("/goals/%d/suggest-parent", goal.ID)},
		{"Contribute to a Goal", "sam@example.com", fmt.Sprintf("/goals/%d?open=parent-link", goal.ID), fmt.Sprintf("/goals/%d/links", goal.ID)},
	} {
		client := signInClient(t, ts.URL, tc.who)
		for what, root := range map[string]*html.Node{
			"opened":  getPage(t, client, ts.URL+tc.opened),
			"refused": parsePage(t, postFormBody(t, client, ts.URL+tc.action, url.Values{"parent_id": {""}})),
		} {
			css := homeStyleSheetIn(root)
			if _, ok := ruleFor(css, ".gf-form"); ok {
				t.Errorf("%s %s carries the goal form's styles, so the picker's own aren't what's tested", tc.name, what)
			}
			decls, ok := ruleFor(css, ".gf-input-error")
			if !ok {
				t.Errorf("%s %s has no .gf-input-error rule", tc.name, what)
				continue
			}
			for _, want := range []string{"display:block", "font-size:var(--text-secondary)", "color:var(--color-danger-ink)"} {
				if !strings.Contains(decls, want) {
					t.Errorf("%s %s: .gf-input-error{%s} lacks %s", tc.name, what, decls, want)
				}
			}
			if what != "refused" {
				continue
			}
			e := findByTestID(t, findByTestID(t, root, "parent-picker"), "input-error")
			if !slices.Contains(strings.Fields(attrOf(e, "class")), "gf-input-error") {
				t.Errorf("%s: the picker's error %q has class %q, want gf-input-error", tc.name, textOf(e), attrOf(e, "class"))
			}
		}
	}
}
