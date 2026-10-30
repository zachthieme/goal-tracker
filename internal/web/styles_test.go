package web_test

import (
	"context"
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

// The tests here check the pages' style rules, and the classes those rules
// select, in one place, so a page's behaviour tests (checkins_test.go) don't
// depend on them. How a page looks in a browser is the Playwright suite's
// (e2e/).

// The Path to Green fieldset stays in the form, hidden by CSS alone while
// Green is picked.
func TestCheckinPathToGreenHidesWhileGreenPicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Slipping.", "Add a second on-call.", pathDate)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	if !strings.Contains(page, `:has(input[name=health][value=Green]:checked) .path-to-green:not(.has-error)`) {
		t.Errorf("no CSS rule hides the Path to Green while Green is picked; body:\n%s", page)
	}
}

// A Path to Green error keeps the fieldset shown whatever Health is picked:
// the fieldset holding it is marked has-error, which the rule hiding it while
// Green is picked leaves alone.
func TestCheckinPathToGreenFieldShownOnError(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":           {domain.HealthGreen},
		"status":           {"Fine."},
		"path_target_date": {"not a date"},
	})
	field := pageElement(t, body, "fieldset", "path-to-green-field")
	if !strings.Contains(field, "has-error") {
		t.Errorf("Path to Green fieldset holding an error is not marked to stay shown; fieldset:\n%s", field)
	}

	// So is the one a refused move of the back-to-Green date comes back with,
	// its error beside the reason.
	red := h.ActiveGoal(sam, "Cut churn", "Keep customers.")
	h.Checkin(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, red.ID), url.Values{
		"health":           {domain.HealthRed},
		"status":           {"Still blocked."},
		"path_to_green":    {"Escalate."},
		"path_target_date": {"2026-06-29"},
	})
	defer func() { _ = resp.Body.Close() }()
	body = between(t, readBody(t, resp), `id="checkin-form"`, "</form>")
	if fieldset := tagAround(t, body, `data-testid="path-to-green-field"`); !hasClass(fieldset, "has-error") {
		t.Errorf("the Path to Green isn't marked as holding the error: %s", fieldset)
	}
}

// The Check-in page's No change sits in the page's header, not in a card of
// its own, and where it is unavailable says why in muted text (#235).
func TestCheckinPageNoChangeCardStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	repeatable := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, repeatable.ID, domain.HealthGreen, "On track.", "", time.Time{})
	overdue := overdueGreenGoal(h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, repeatable.ID))
	if tag := tagAround(t, page, `data-testid="no-change-card"`); strings.Contains(tag, `class="card"`) {
		t.Errorf("No change is still a card: %s", tag)
	}
	c := between(t, getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, overdue.ID)), `data-testid="no-change-card"`, `data-testid="checkin-form"`)
	if unavailable := tagAround(t, c, `data-testid="no-change-unavailable"`); !hasClass(unavailable, "muted") {
		t.Errorf("the card's reason is not muted: %s", unavailable)
	}
}

// A refused No change's reason, at the top of the Check-in form, is styled as
// an error, as the import page's error flash is, not left as plain text (#235).
func TestNoChangeRefusalIsStyledAsAnError(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := overdueGreenGoal(h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)
	defer func() { _ = resp.Body.Close() }()

	form := between(t, page, `data-testid="checkin-form"`, `data-testid="checkin-health"`)
	if tag := tagAround(t, form, `data-testid="checkin-error"`); attr(tag, "class") != "alert r" {
		t.Errorf("the form's top error has class %q, want %q: %s", attr(tag, "class"), "alert r", tag)
	}
}

// The off-target notice at the top of the form is styled as a Yellow alert,
// and it and the warning hide, by CSS alone, while another Health than Green
// is picked: changing the Health answers the question.
func TestCheckinOffTargetWarningHidesUnlessGreenPicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetIdleGoal(t, h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	const notGreen = ".checkin-form:not(:has(input[name=health][value=Green]:checked))"
	if got := cssRule(t, page, notGreen+" .checkin-off-target, "+notGreen+" .checkin-off-target-notice"); got != "display:none" {
		t.Errorf("the rule hiding the warning while Green isn't picked declares %q, want display:none", got)
	}

	body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Old pool nearly drained."},
	})
	tag := openTag(pageElement(t, body, "p", "checkin-off-target-notice"))
	if !hasClass(tag, "alert") || !hasClass(tag, "y") || !hasClass(tag, "checkin-off-target-notice") {
		t.Errorf("notice tag = %s, want classes alert y and checkin-off-target-notice", tag)
	}
}

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

// The Check-in page's Contributed to by list and its Rolled-up Health show
// each Health as a badge, the count of children that set it as the badge's
// muted count line; a Goal with no children keeps the plain Check-in page, its
// column shown for every Goal (ticket #364, #366).
func TestCheckinPageChildrenStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)
	lone := h.ActiveGoal(sam, "Ship it", "It matters.")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, f.parent.ID))
	list := pageElement(t, page, "section", "checkin-children")
	if !strings.Contains(list, `<h3 class="checkin-rail-head">Contributed to by</h3>`) {
		t.Errorf("children list is not headed by a rail head; list:\n%s", list)
	}
	children := parsePage(t, list)
	for _, child := range []struct {
		goal   domain.Goal
		health string
	}{
		{f.yellow, "Yellow"},
		{f.green, "Green"},
	} {
		link := linkTo(children, fmt.Sprintf("/goals/%d", child.goal.ID))
		if link == nil {
			t.Errorf("children list has no link to %s; list:\n%s", child.goal.Title, list)
			continue
		}
		assertHealthChip(t, healthChipIn(t, link.Parent), child.health)
	}
	if line := pageElement(t, page, "p", "checkin-form-rollup"); !strings.Contains(line, `<span class="muted">1 of 2 Yellow</span>`) {
		t.Errorf("rollup line doesn't show the count as the badge's muted count line; line:\n%s", line)
	}

	if page := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, lone.ID)); !strings.Contains(page, `<div class="checkin-page">`) {
		t.Errorf("a Goal with no children doesn't keep the plain Check-in page; body:\n%s", page)
	}
}

// The form's Rolled-up Health shows Red as a badge (ticket #364).
func TestCheckinFormRolledUpPanelStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red why it matters.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, parent.ID))
	doc := parsePage(t, page)
	assertHealthChip(t, findByTestID(t, doc, "checkin-form-rollup-health"), "Red")
	// Its word, read out, doesn't run into the label before it or the text
	// after it.
	if got := runningText(findByTestID(t, doc, "checkin-form-rollup")); !strings.HasPrefix(got, "Rolled-up Health: Red ") {
		t.Errorf("rollup line runs %q, want it to read Rolled-up Health: Red, then its count", got)
	}
}

// A refused Check-in's field errors sit on their own line in the danger ink,
// as the Goal form's input errors do (#669).
func TestCheckinFieldErrorIsInTheDangerInkStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")
	page, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{"health": {domain.HealthYellow}})

	for _, e := range findAllByTestID(parsePage(t, page), "checkin-error") {
		if !strings.Contains(" "+attrOf(e, "class")+" ", " checkin-input-error ") {
			t.Errorf("field error %q has class %q, want checkin-input-error", textOf(e), attrOf(e, "class"))
		}
	}
	rule := cssRule(t, page, ".checkin-input-error")
	for _, want := range []string{"display:block", "font-size:var(--text-secondary)", "color:var(--color-danger-ink)"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".checkin-input-error{%s} lacks %s", rule, want)
		}
	}
}

// The children list's rules live in app.css, shared by the Goal page and the
// Check-in page, so the two lists can't drift; the Goal page no longer carries
// its own copy.
func TestChildrenListStylesAreShared(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)

	rules := []string{
		".gp-list{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:8px}",
		".gp-link{display:flex;flex-direction:column;gap:4px}",
		".gp-link-meta{display:flex;align-items:center;gap:8px;flex-wrap:wrap}",
		".gp-link-meta form{margin:0 0 0 auto}",
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	goalPage := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, f.parent.ID))
	for _, rule := range rules {
		if !strings.Contains(css, rule) {
			t.Errorf("app.css lacks %s", rule)
		}
		if strings.Contains(goalPage, rule) {
			t.Errorf("Goal page's inline styles still carry %s", rule)
		}
	}
}

// The Check-in page is a form in the flat frame: its page header strip reads
// Check in under the Goal's title, and nothing on it is a card, whether it has
// a No change to offer and Contributed to by to show or not (DESIGN.md §
// Check-in page).
func TestCheckinPageIsAFlatFormStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)
	h.Checkin(sam, f.parent.ID, domain.HealthYellow, "Slipping.", "Fix the paywall.", pathDate)
	lone := h.ActiveGoal(sam, "Ship it", "It matters.")
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, g := range []domain.Goal{f.parent, lone} {
		page := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, g.ID))
		assertFlatFormFrame(t, page, g.Title, "Check in")
		if crumb := tagAround(t, page, `class="ph-parent"`); attr(crumb, "href") != fmt.Sprintf("/goals/%d", g.ID) {
			t.Errorf("%s: the header strip's breadcrumb doesn't link to the Goal: %s", g.Title, crumb)
		}
	}
}

// The Check-in page separates its sections by whitespace, not boxes: the Path
// to Green is no longer filled Yellow or Red, and no fieldset is boxed, while
// the Path to Green still hides while Green is picked (ticket #364).
func TestCheckinPageSectionsAreNotBoxed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	for _, gone := range []string{
		".checkin-form:has(input[name=health][value=Yellow]:checked) .path-to-green",
		".checkin-form:has(input[name=health][value=Red]:checked) .path-to-green",
		".checkin-form fieldset{border:1px",
	} {
		if strings.Contains(page, gone) {
			t.Errorf("styles still carry %s", gone)
		}
	}
	if !strings.Contains(page, `.checkin-form:has(input[name=health][value=Green]:checked) .path-to-green:not(.has-error){display:none}`) {
		t.Errorf("styles no longer hide the Path to Green while Green is picked")
	}
}

// The submit bar, its summary, Back to Goal and Submit check-in carry the
// classes its rules select, a quiet Back to Goal beside a primary Submit
// check-in; the old checkin-submit row is gone (ticket #367).
func TestCheckinSubmitBarClasses(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	bar := between(t, between(t, page, `data-testid="checkin-form"`, "</form>"), `<div data-testid="checkin-submit-bar"`, "")
	if !strings.HasPrefix(bar, `<div data-testid="checkin-submit-bar" class="checkin-submit-bar">`) {
		t.Errorf("the submit bar's opening tag = %s", openTag(bar))
	}
	if summary := tagAround(t, bar, `data-testid="checkin-summary"`); attr(summary, "class") != "checkin-summary" {
		t.Errorf("the summary = %s, want class checkin-summary", summary)
	}
	if !strings.Contains(bar, fmt.Sprintf(`<a class="btn quiet" href="/goals/%d">Back to Goal</a>`, goal.ID)) {
		t.Errorf("the submit bar has no quiet Back to Goal link:\n%s", bar)
	}
	if !strings.Contains(bar, `<button type="submit" class="btn primary">Submit check-in</button>`) {
		t.Errorf("the submit bar has no primary Submit check-in:\n%s", bar)
	}
	if strings.Contains(page, `class="checkin-submit"`) {
		t.Errorf("the page still has the old checkin-submit row")
	}
}

// The submit bar sticks just above the bottom of the window while the form runs
// past it, a floating surface (DESIGN.md § System: Surfaces are for what
// floats): bordered, rounded and cast by --shadow-pop, and the page keeps a
// field it scrolls to clear of it. At 600px and below Back to Goal hides, the
// summary shrinks and Submit check-in grows to a thumb's size (ticket #367).
func TestCheckinSubmitBarFloatsAtTheWindowsBottom(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	bar := cssRule(t, page, ".checkin-submit-bar")
	for _, want := range []string{
		"position:sticky", "bottom:16px", "z-index:10",
		"background:var(--color-surface)", "border:1px solid var(--color-border)",
		"border-radius:var(--radius-lg)", "box-shadow:var(--shadow-pop)",
		"padding:12px 16px", "display:flex", "align-items:center", "gap:16px", "flex-wrap:wrap",
	} {
		if !strings.Contains(bar, want) {
			t.Errorf(".checkin-submit-bar declares %q, missing %s", bar, want)
		}
	}
	if strings.Contains(bar, "#") {
		t.Errorf(".checkin-submit-bar names a colour by hex: %q", bar)
	}
	summary := cssRule(t, page, ".checkin-summary")
	for _, want := range []string{"flex:1", "color:var(--color-ink-2)", "font-size:var(--text-body)"} {
		if !strings.Contains(summary, want) {
			t.Errorf(".checkin-summary declares %q, missing %s", summary, want)
		}
	}
	if part := cssRule(t, page, ".checkin-summary>*"); !strings.Contains(part, "white-space:nowrap") {
		t.Errorf("the summary's parts break inside themselves rather than wrap whole: %q", part)
	}
	if submit := cssRule(t, page, ".checkin-submit-bar .btn.primary"); !strings.Contains(submit, "height:44px") {
		t.Errorf("Submit check-in isn't 44px tall: %q", submit)
	}
	if pad := cssRule(t, page, "html:has(.checkin-submit-bar)"); !strings.Contains(pad, "scroll-padding-bottom:") {
		t.Errorf("the page doesn't keep a scrolled-to field clear of the bar: %q", pad)
	}

	phone := map[string]string{
		".checkin-submit-bar .btn.quiet":   "display:none",
		".checkin-summary":                 "font-size:var(--text-secondary)",
		".checkin-submit-bar .btn.primary": "height:48px",
	}
	for sel, want := range phone {
		found := false
		for _, block := range mediaBlocks(page, "@media (max-width:600px)") {
			if decls, ok := ruleFor(block, sel); ok && strings.Contains(decls, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("at 600px and below %s doesn't declare %s", sel, want)
		}
	}
}

// The column's sections are headed by rail heads, Last time showing the
// Health as a badge and the back-to-Green date muted (ticket #366).
func TestCheckinPageContextClasses(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "The paywall fix slipped a week.", "Ship the fix behind a flag.", testsupport.Epoch.AddDate(0, 2, 0))

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	aside := pageElement(t, page, "aside", "checkin-context")
	last := pageElement(t, aside, "section", "checkin-last-time")
	for _, want := range []string{
		`<h3 class="checkin-rail-head">Last time · Jan 2</h3>`,
		`<span class="muted">back to Green by Mar 2</span>`,
	} {
		if !strings.Contains(last, want) {
			t.Errorf("Last time lacks %s:\n%s", want, last)
		}
	}
	assertHealthChip(t, healthChipIn(t, parsePage(t, last)), "Yellow")
	if dates := pageElement(t, aside, "section", "checkin-context-dates"); !strings.Contains(dates, `<h3 class="checkin-rail-head">Dates</h3>`) {
		t.Errorf("the Dates section isn't headed by a rail head:\n%s", dates)
	}
}

// The column sticks beside the form on a wide screen as a quiet rail. On a
// phone it drops Dates (ticket #366) but keeps Last time, as the form's fields
// start empty (#657), and Contributed to by keeps its rule under Last time;
// a rail with neither is hidden.
func TestCheckinPageContextStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	styles := between(t, page, ".checkin-page{", "</style>")
	if rule, ok := ruleFor(styles, ".checkin-context"); !ok || !strings.Contains(rule, "position:sticky") {
		t.Errorf(".checkin-context{%s} isn't sticky", rule)
	}
	// It reads as a quiet rail: behind a hairline, in small text, headed by
	// small muted labels (DESIGN.md § Check-in page's column).
	for sel, want := range map[string]string{
		".checkin-context":   "border-left:1px solid var(--color-border)",
		".checkin-rail-head": "color:var(--color-ink-muted)",
	} {
		if rule, ok := ruleFor(styles, sel); !ok || !strings.Contains(rule, want) {
			t.Errorf("%s{%s} doesn't declare %s", sel, rule, want)
		}
	}
	if rule, _ := ruleFor(styles, ".checkin-context"); !strings.Contains(rule, "font-size:var(--text-secondary)") {
		t.Errorf(".checkin-context{%s} isn't in small text", rule)
	}
	// At 900px and below the rail follows the form, so a hairline above sets
	// it off from the form, where one below did when it came first, running
	// the page's full width (#674).
	narrow := mediaBlock(t, styles, "@media (max-width:900px)")
	if rule, ok := ruleFor(narrow, ".checkin-context"); !ok ||
		!strings.Contains(rule, "border-top:1px solid var(--color-border)") || !strings.Contains(rule, "padding:24px 0 0") ||
		!strings.Contains(rule, "align-self:stretch") || strings.Contains(rule, "border-bottom") {
		t.Errorf("at 900px .checkin-context{%s} isn't ruled off from the form above it", rule)
	}
	phone := mediaBlock(t, styles, "@media (max-width:600px)")
	// Each phone rule's selector outranks the base rule it overrides
	// (.checkin-context>section, 0,1,1; ...>section+section, 0,2,1), so it
	// wins (#370).
	if rule, ok := ruleFor(phone, ".checkin-context>.checkin-context-dates"); !ok || !strings.Contains(rule, "display:none") {
		t.Errorf("at 600px .checkin-context>.checkin-context-dates{%s} isn't hidden", rule)
	}
	if rule, ok := ruleFor(phone, ".checkin-context:not(:has(.checkin-context-last)):not(:has(.checkin-context-children))"); !ok || !strings.Contains(rule, "display:none") {
		t.Errorf("at 600px a rail with neither Last time nor children isn't hidden: {%s}", rule)
	}
	for _, block := range mediaBlocks(styles, "@media (max-width:600px)") {
		for _, sel := range []string{
			".checkin-context>.checkin-context-last",
			".checkin-context:not(:has(.checkin-context-children))",
			".checkin-context>section.checkin-context-children",
		} {
			if rule, ok := ruleFor(block, sel); ok {
				t.Errorf("at 600px %s{%s} still hides Last time or drops the children's rule", sel, rule)
			}
		}
	}
	// Under Last time, Contributed to by keeps the hairline every section after
	// the first has.
	if rule, ok := ruleFor(styles, ".checkin-context>section+section"); !ok || !strings.Contains(rule, "border-top:1px solid var(--color-border)") {
		t.Errorf(".checkin-context>section+section{%s} isn't ruled off from the section above", rule)
	}
}

// The add bar, and the dates and Lifecycle sections' headings, show only once
// script has run, as their class checkin-js-only has them, on the page and on
// a refused submit's re-render (ticket #365).
func TestCheckinAddBarShowsOnlyWithScript(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	if open := openTag(between(t, page, `<div data-testid="checkin-add-bar"`, `data-testid="checkin-context"`)); !slices.Contains(strings.Fields(attr(open, "class")), "checkin-js-only") {
		t.Errorf("the bar shows without script: %s", open)
	}
	body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {""},
	})
	for name, html := range map[string]string{"page": page, "re-render": body} {
		for section, heading := range map[string]string{
			"checkin-dates-section":     "checkin-dates-heading",
			"checkin-lifecycle-section": "checkin-lifecycle-heading",
		} {
			details := between(t, html, `<details data-testid="`+section+`"`, "</details>")
			if tag := tagAround(t, details, `data-testid="`+heading+`"`); !slices.Contains(strings.Fields(attr(tag, "class")), "checkin-js-only") {
				t.Errorf("%s: %s = %s, want class checkin-js-only", name, heading, tag)
			}
		}
	}
}

// Without script the add bar and the sections' headings hide, and the sections
// stay collapsed <details>, rows divided by hairlines; once script marks the
// page checkin-js, a closed section and every summary hide, so a section shows
// once opened. Neither the bar nor a section is a box: the bar is a plain row
// over a hairline. On a phone the bar's buttons keep to one line that scrolls
// sideways (ticket #365, DESIGN.md § Check-in form's optional sections).
func TestCheckinAddBarStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	styles := between(t, page, ".checkin-page{", "</style>")
	for sel, want := range map[string]string{
		".checkin-page:not(.checkin-js) .checkin-js-only": "display:none",
		".checkin-js .checkin-more:not([open])":           "display:none",
		".checkin-js .checkin-more>summary":               "display:none",
		".checkin-more~.checkin-more":                     "border-top:1px solid var(--color-border)",
		".checkin-add-bar":                                "border-bottom:1px solid var(--color-border)",
		".checkin-add-bar .btn[data-used]":                "background:var(--color-surface-alt)",
	} {
		if rule, ok := ruleFor(styles, sel); !ok || !strings.Contains(rule, want) {
			t.Errorf("%s{%s} doesn't declare %s", sel, rule, want)
		}
	}
	for _, sel := range []string{".checkin-add-bar", ".checkin-more", ".checkin-rollup"} {
		if rule, _ := ruleFor(styles, sel); strings.Contains(rule, "background") || strings.Contains(rule, "border-radius") || strings.Contains(rule, "border:") {
			t.Errorf("%s{%s} is a box", sel, rule)
		}
	}
	phone := mediaBlocks(styles, "@media (max-width:600px)")
	for sel, want := range map[string]string{
		".checkin-add-buttons":      "overflow-x:auto",
		".checkin-add-buttons .btn": "flex:none",
		".checkin-add-divider":      "display:none",
	} {
		found := false
		for _, block := range phone {
			if rule, ok := ruleFor(block, sel); ok && strings.Contains(rule, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("at 600px %s doesn't declare %s", sel, want)
		}
	}
}

// A filter chip is in its on state, filled with the primary tint and its key
// shown as set, when it is marked on, when its select has a value other than
// the blank "any" option chosen (the Follow-ups Team chip on
// ?value=<Platform>), or when its checkbox is ticked (#433, #438).
func TestFilterChipOnStateStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	for _, sel := range []string{
		".filter-chip.on",
		`.filter-chip:has(option:checked:not([value=""]))`,
		".filter-chip:has(input:checked)",
	} {
		if decls, ok := ruleFor(css, sel); !ok || !strings.Contains(decls, "background:var(--color-primary-subtle)") {
			t.Errorf("app.css %s{%s} isn't filled with --color-primary-subtle", sel, decls)
		}
	}
	keySet := `.filter-chip:has(option:checked:not([value=""])) .chip-key-set`
	if decls, ok := ruleFor(css, keySet); !ok || !strings.Contains(decls, "display:inline") {
		t.Errorf("app.css %s{%s} doesn't show the set key", keySet, decls)
	}
}

// Health's shape is drawn by .dot inside a Health's .r, .y or .g: Red a
// stop-sign octagon, Yellow a yield triangle pointing down, Green a circle.
// Each place that draws it scales the one shape set by --dot-size rather than
// drawing its own (#452).
func TestHealthDotShapes(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	for sel, want := range map[string]string{
		".r .dot": "clip-path:polygon(30% 0,70% 0,100% 30%,100% 70%,70% 100%,30% 100%,0 70%,0 30%)",
		".y .dot": "clip-path:polygon(0 0,100% 0,50% 100%)",
		".g .dot": "border-radius:50%",
	} {
		if decls, ok := ruleFor(css, sel); !ok || !strings.Contains(decls, want) {
			t.Errorf("app.css %s{%s} doesn't declare %s", sel, decls, want)
		}
	}
	for _, sel := range []string{".dot", ".r .dot", ".y .dot"} {
		if decls, ok := ruleFor(css, sel); !ok || !strings.Contains(decls, "var(--dot-size)") {
			t.Errorf("app.css %s{%s} isn't sized by --dot-size", sel, decls)
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

// Home's dated pill, for a Goal whose Check-in is overdue, keeps its Health's
// fill, ink and shape, marked stale only by a dashed edge in that ink (#452).
func TestHomeDatedPillKeepsItsHealthColour(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(9 * 24 * time.Hour)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	if badge := tagAround(t, page, `data-testid="home-due-health"`); !hasClass(badge, "as-of") || !hasClass(badge, "g") {
		t.Fatalf("the Check-in overdue Goal's pill isn't a dated Green pill, so this test checks nothing: %s", badge)
	}
	decls, ok := ruleFor(page, ".hm-list .badge.as-of")
	if !ok || !strings.Contains(decls, "border:1px dashed currentColor") {
		t.Errorf(".hm-list .badge.as-of{%s} isn't edged in a 1px dashed line of its Health's ink", decls)
	}
	for _, prop := range []string{"background", "color:"} {
		if strings.Contains(strings.ReplaceAll(decls, "currentColor", ""), prop) {
			t.Errorf(".hm-list .badge.as-of{%s} overrides its Health's %s", decls, strings.TrimSuffix(prop, ":"))
		}
	}
}

// The Red and Yellow pills counting a person's Org on My org draw the shared
// Health shape (.dot) before their label rather than shapes of their own; the
// total line above them is plain text (#452).
func TestHomeMyOrgCountsDrawTheSharedShape(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	newHomeOrgFixture(h)

	page := getBody(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")
	var counts []*html.Node
	for _, row := range findAllByTestID(parsePage(t, page), "home-org-person") {
		counts = append(counts, findAllByTestID(row, "home-org-count")...)
	}
	if len(counts) == 0 {
		t.Fatalf("My org has no person's counts, so this test checks nothing:\n%s", page)
	}
	for _, count := range counts {
		kind := attrOf(count, "data-kind")
		dots := findAll(count, func(n *html.Node) bool { return slices.Contains(strings.Fields(attrOf(n, "class")), "dot") })
		if want := kind == "red" || kind == "yellow"; want != (len(dots) == 1) {
			t.Errorf("the %s count holds %d .dot: %s", kind, len(dots), renderNode(count))
		}
	}
	if strings.Contains(between(t, page, ".hm-org-row{", "</style>"), "::before") {
		t.Errorf("Home draws My org's Health shapes with ::before rather than the shared .dot")
	}
}

// My org's linked count pills are at least 24px tall, as a target, though
// Home's other pills are 22px: .hm-list>li>a.badge outranks .hm-list .badge,
// and a min-height outlasts its height. A row is no longer one target, so
// hovering it fills no background (#678).
func TestHomeMyOrgCountLinksAreTargetsStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	newHomeOrgFixture(h)

	page := getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")
	rows := findAllByTestID(page, "home-org-person")
	if len(rows) == 0 {
		t.Fatalf("My org has no rows, so this test checks nothing")
	}
	for _, count := range findAllByTestID(rows[0], "home-org-count") {
		li := count.Parent
		if count.Data != "a" || !flatFrameHasClass(count, "badge") || li != rows[0] || li.Data != "li" || !flatFrameHasClass(li.Parent, "hm-list") {
			t.Errorf("the count isn't a .badge link directly in an .hm-list row, so .hm-list>li>a.badge misses it: %s", renderNode(count))
		}
	}
	css := homeStyleSheetIn(page)
	assertHomeRuleDeclares(t, css, ".hm-list .badge", "height:22px")
	assertHomeRuleDeclares(t, css, ".hm-list>li>a.badge", "min-height:24px")
	if decls, _ := ruleFor(css, ".hm-list .badge"); strings.Contains(decls, "min-height") {
		t.Errorf(".hm-list .badge{%s} caps the linked pills' min-height", decls)
	}
	if decls, _ := ruleFor(css, ".hm-org-row:hover"); strings.Contains(decls, "background") {
		t.Errorf(".hm-org-row:hover{%s} fills the row, which is no longer one target", decls)
	}
}

// A link request's note on Home is set apart by its indent and a 1px neutral
// rule, not a coloured side stripe, on a line of its own under the request
// (#67, #677).
func TestHomeLinkRequestNoteIsSetApartByANeutralRuleStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	h.RequestLink(sam, h.CreateGoal(sam, "Migrate displays", "Displays fail often."), h.CreateGoal(pat, "Reduce outages", "Outages cost trust."), "Displays drive most outages.")

	page := getPage(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/home")
	note := findByTestID(t, page, "home-request-note")
	assertHomeCarriesClass(t, "the link request's note", note, "hm-note")
	if !flatFrameHasClass(note.Parent, "hm-what") {
		t.Errorf("the note isn't directly in the row's .hm-what, so it doesn't take its own line there: %s", renderNode(note.Parent))
	}
	css := homeStyleSheetIn(page)
	decls, ok := ruleFor(css, ".hm-note")
	if !ok {
		t.Fatalf("Home has no .hm-note rule")
	}
	assertNeutralRule(t, decls)
	assertHomeRuleDeclares(t, css, ".hm-note", "flex-basis:100%", "margin:0", "color:var(--color-ink-muted)")
}

// homeStyleSheetIn is the stylesheet Home carries itself, its homeStyles: the
// text of the page's <style> elements, in order.
func homeStyleSheetIn(root *html.Node) string {
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

// assertHomeRuleDeclares fails unless css has a rule for sel declaring each
// of wants.
func assertHomeRuleDeclares(t *testing.T, css, sel string, wants ...string) {
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

// assertHomeCarriesClass fails unless n, which Home shows as what, carries
// each of classes.
func assertHomeCarriesClass(t *testing.T, what string, n *html.Node, classes ...string) {
	t.Helper()
	for _, class := range classes {
		if !flatFrameHasClass(n, class) {
			t.Errorf("%s doesn't carry the %s class: %s", what, class, renderNode(n))
		}
	}
}

// homeHasAncestorClassed reports whether an element around n carries class.
func homeHasAncestorClassed(n *html.Node, class string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && flatFrameHasClass(p, class) {
			return true
		}
	}
	return false
}

// Each of Home's sections is an .hm-section headed by its own h2, a
// section-heading-sized 600 heading, with its count in muted .hm-count text
// after its name (TestHomeSectionsAreHeadedH2, TestHomeHeadsYellowOrRedGoals).
func TestHomeSectionsAreHeadedH2Styles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeOrgFixture(h)
	mine := h.ActiveGoal(f.lead, "Hire", "We need people.")
	h.Checkin(f.lead, mine.ID, domain.HealthRed, "No offers.", "Raise the band.", h.Clock.Now().AddDate(0, 1, 0))

	page := getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")
	for testID, count := range map[string]string{"home-needs-you": "", "home-org": "3", "home-not-green": "1"} {
		section := findByTestID(t, page, testID)
		assertHomeCarriesClass(t, testID, section, "hm-section")
		var heading *html.Node
		for c := section.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "h2" {
				heading = c
				break
			}
		}
		if heading == nil {
			t.Errorf("%s has no h2 of its own:\n%s", testID, renderNode(section))
			continue
		}
		counts := findAll(heading, func(n *html.Node) bool { return n.Data == "span" })
		if count == "" {
			if len(counts) != 0 {
				t.Errorf("%s's heading counts nothing yet holds %d spans: %s", testID, len(counts), renderNode(heading))
			}
			continue
		}
		if len(counts) != 1 || textOf(counts[0]) != count {
			t.Errorf("%s's heading doesn't hold its count %s: %s", testID, count, renderNode(heading))
			continue
		}
		assertHomeCarriesClass(t, testID+"'s count", counts[0], "hm-count")
	}

	css := homeStyleSheetIn(page)
	assertHomeRuleDeclares(t, css, ".hm-section>h2", "font-size:var(--text-h2)", "font-weight:600")
	assertHomeRuleDeclares(t, css, ".hm-count", "color:var(--color-ink-muted)")
}

// A Check-ins due row is a 44px .hm-list row under a hairline, its meta line
// Home's muted .hm-meta and its No change and Check in 30px buttons in its
// .hm-actions (TestHomeDueRowLeadsWithItsHealth).
func TestHomeDueRowLeadsWithItsHealthStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	checkinOverdue := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, checkinOverdue, 14)
	h.Checkin(sam, checkinOverdue.ID, domain.HealthYellow, "Slipping.", "Cut scope.", testsupport.Epoch.AddDate(0, 1, 0))
	h.Clock.Advance(16 * day)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	due := findByTestID(t, page, "home-due")
	assertHomeCarriesClass(t, "Check-ins due", due, "hm-list")
	row := homeRowIn(t, due, checkinOverdue)
	if row.Parent != due {
		t.Errorf("the row isn't a list item of Check-ins due's own:\n%s", renderNode(due))
	}
	assertHomeCarriesClass(t, "the row's meta line", findByTestID(t, row, "home-due-status"), "hm-meta")
	for what, button := range map[string]*html.Node{"No change": buttonNamed(row, "Check in: no change"), "Check in": findByTestID(t, row, "home-due-checkin")} {
		if button == nil {
			t.Errorf("the row has no %s:\n%s", what, renderNode(row))
			continue
		}
		assertHomeCarriesClass(t, what, button, "btn")
		if !homeHasAncestorClassed(button, "hm-actions") {
			t.Errorf("%s isn't among the row's .hm-actions:\n%s", what, renderNode(row))
		}
	}

	css := homeStyleSheetIn(page)
	assertHomeRuleDeclares(t, css, ".hm-meta", "color:var(--color-ink-muted)")
	assertHomeRuleDeclares(t, css, ".hm-actions .btn", "height:30px")
	assertHomeRuleDeclares(t, css, ".hm-list>li", "min-height:44px", "border-top:1px solid var(--color-border)")
}

// A Delegate's due row tags whose Goal it is with a .tag, a pill like the
// row's badges (TestHomeListsDelegatedGoalsOnce).
func TestHomeListsDelegatedGoalsOnceStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	h.Clock.Advance(10 * day)

	page := getPage(t, signInClient(t, ts.URL, "dee@example.com"), ts.URL+"/home")
	tag := findByTestID(t, homeRowIn(t, findByTestID(t, page, "home-due"), g), "home-due-for")
	assertHomeCarriesClass(t, "the for tag", tag, "tag")
	assertHomeRuleDeclares(t, homeStyleSheetIn(page), ".hm-list .tag", "border-radius:var(--radius-full)")
}

// Home's one-button forms — a due Goal's No change, a link request's Accept and
// Reject — sit inline through the shared .inline-form class rather than a
// style attribute (TestHomeInlineFormsComeFromTheSharedClass).
func TestHomeInlineFormsComeFromTheSharedClassStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	checkinOverdue := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, checkinOverdue, 14)
	h.Checkin(sam, checkinOverdue.ID, domain.HealthYellow, "Slipping.", "Cut scope.", testsupport.Epoch.AddDate(0, 1, 0))
	h.Clock.Advance(16 * day)
	link := h.RequestLink(kim, h.ActiveGoal(kim, "Cut churn", "Customers leave."), checkinOverdue, "")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/home")
	for _, action := range []string{
		fmt.Sprintf("/goals/%d/checkins/no-change", checkinOverdue.ID),
		fmt.Sprintf("/links/%d/accept", link.ID),
		fmt.Sprintf("/links/%d/reject", link.ID),
	} {
		form := homeFormTo(page, action)
		if form == nil {
			t.Errorf("Home has no form posting to %s", action)
			continue
		}
		assertHomeCarriesClass(t, "the form posting to "+action, form, "inline-form")
		if hasAttr(form, "style") {
			t.Errorf("the form posting to %s carries a style attribute: %s", action, renderNode(form))
		}
	}
	assertHomeRuleDeclares(t, getBody(t, client, ts.URL+"/static/app.css"), ".inline-form", "display:inline")
}

// Home's subgroups under Needs you are headed in muted .hm-group-head text,
// and an Action Item's row puts what it's about in its .hm-what and its Close
// in its .hm-actions, the two wrapping in a row (TestHomeListsYourOpenActionItems,
// TestHomeListsAsksForHelpAddressedToTheViewer).
func TestHomeListsYourOpenActionItemsStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeActionItemFixture(h)
	f.raise(f.sam, "Explain the slip.", -2, f.goal.ID)
	sam := h.SignInNamed("sam@example.com", "Sam")
	mia := h.SignIn("mia@example.com")
	h.SetManager(sam, mia)
	asked := h.ActiveGoal(sam, "Zeta red", "Why it matters.")
	h.AskForHelp(sam, asked.ID, domain.HealthRed, "Blocked.", "Need budget.", pathDate)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	needs := findByTestID(t, page, "home-needs-you")
	if heading := headingNamed(needs, "Action Items"); heading == nil {
		t.Errorf("Needs you has no Action Items heading:\n%s", renderNode(needs))
	} else {
		assertHomeCarriesClass(t, "the Action Items heading", heading, "hm-group-head")
	}
	miaNeeds := findByTestID(t, getPage(t, signInClient(t, ts.URL, "mia@example.com"), ts.URL+"/home"), "home-needs-you")
	if heading := headingNamed(miaNeeds, "Asks for help"); heading == nil {
		t.Errorf("Mia's Needs you has no Asks for help heading:\n%s", renderNode(miaNeeds))
	} else {
		assertHomeCarriesClass(t, "the Asks for help heading", heading, "hm-group-head")
	}

	row := findByTestID(t, page, "home-action-item")
	link := linkTo(row, fmt.Sprintf("/goals/%d#action-items", f.goal.ID))
	if link == nil || link.Parent == nil {
		t.Fatalf("the Action Item's row doesn't link to it:\n%s", renderNode(row))
	}
	assertHomeCarriesClass(t, "what the row is about", link.Parent, "hm-what")
	if !homeHasAncestorClassed(findByTestID(t, row, "close-toggle"), "hm-actions") {
		t.Errorf("the row's Close isn't among its .hm-actions:\n%s", renderNode(row))
	}

	css := homeStyleSheetIn(page)
	assertHomeRuleDeclares(t, css, ".hm-group-head", "color:var(--color-ink-muted)")
	assertHomeRuleDeclares(t, css, ".hm-what", "display:flex", "flex-wrap:wrap")
	assertHomeRuleDeclares(t, css, ".hm-actions", "display:flex", "margin-left:auto")
}

// The close form on an Action Item's Home row is laid out by .rp-inline-form,
// a wrapping row of its note and Close (TestHomeClosesAnActionItem).
func TestHomeClosesAnActionItemStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeActionItemFixture(h)
	f.raise(f.sam, "Send the vendor plan.", 7, 0)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	form := findByTestID(t, findByTestID(t, page, "home-action-item"), "close-action-item")
	assertHomeCarriesClass(t, "the close form", form, "rp-inline-form")
	assertHomeRuleDeclares(t, homeStyleSheetIn(page), ".rp-inline-form", "display:flex")
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

// The Check-in page's Health choices show their colours before one is picked:
// an unpicked choice is its Health's tint with its ink and shape, and the
// picked one its full fill edged 2px in its ink (#452).
func TestCheckinHealthChoicesShowTheirColours(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	styles := between(t, page, ".checkin-page{", "</style>")
	for sel, want := range map[string]string{
		".health-toggle label.badge": "border:2px solid transparent",
		// The form's labels are --color-ink, so each choice names its ink
		// again for its word, shape and edge to take it.
		".health-toggle label.r":                          "color:var(--health-r-ink)",
		".health-toggle label.y":                          "color:var(--health-y-ink)",
		".health-toggle label.g":                          "color:var(--health-g-ink)",
		".health-toggle label.badge:has(input:checked)":   "border-color:currentColor",
		".health-toggle label.r:not(:has(input:checked))": "background:var(--health-r-tint)",
		".health-toggle label.y:not(:has(input:checked))": "background:var(--health-y-tint)",
		".health-toggle label.g:not(:has(input:checked))": "background:var(--health-g-tint)",
	} {
		if decls, ok := ruleFor(styles, sel); !ok || !strings.Contains(decls, want) {
			t.Errorf("%s{%s} doesn't declare %s", sel, decls, want)
		}
	}
	if strings.Contains(styles, "--color-surface;border-color:var(--color-border-strong)") || strings.Contains(styles, "label.badge:not(:has(input:checked)){") {
		t.Errorf("an unpicked Health choice is still drawn grey rather than in its Health's tint")
	}

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	for _, tint := range []string{"--health-r-tint:", "--health-y-tint:", "--health-g-tint:"} {
		if n := strings.Count(css, tint); n != 3 {
			t.Errorf("app.css declares %s %d times, want once in each of the light and two dark blocks", tint, n)
		}
	}
}

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

// A Health chip without its word shrinks to its shape: centred, 22px tall as
// with its word, at least 24px wide, and at least 24px tall where it is a link (the Goal table in Edit
// values). The Check-in page's Health choice keeps its 44px height, now
// showing only the shape (#453).
func TestHealthChipShrinksToItsShapeStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Slipping.", "Cut scope.", pathDate)
	client := signInClient(t, ts.URL, "sam@example.com")

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	chip := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, goal)), "goal-health")
	if !flatFrameHasClass(chip, "shape") {
		t.Fatalf("the Health chip isn't marked to shrink to its shape: %s", renderNode(chip))
	}
	rule, _ := ruleFor(css, ".badge.shape")
	for _, want := range []string{"justify-content:center", "min-width:24px", "min-height:22px"} {
		if !strings.Contains(rule, want) {
			t.Errorf("a Health chip doesn't shrink to its centred shape: .badge.shape{%s} lacks %s", rule, want)
		}
	}
	if rule, _ := ruleFor(css, "a>.badge.shape"); !strings.Contains(rule, "min-height:24px") {
		t.Errorf("a Health chip that is a link is under a 24px target: a>.badge.shape{%s}", rule)
	}

	table := getPage(t, client, ts.URL+"/goals?layout=table&edit=1")
	link := healthChipIn(t, findByTestID(t, table, "goal-table-row"))
	if link.Parent.Data != "a" {
		t.Errorf("the Goal table's Health in Edit values isn't the chip as a link: %s", renderNode(link.Parent))
	}

	checkin := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	if rule := cssRule(t, checkin, ".health-toggle label.badge"); !strings.Contains(rule, "height:44px") {
		t.Errorf("the Health choice is under 44px tall: .health-toggle label.badge{%s}", rule)
	}
	choices := findByTestID(t, parsePage(t, checkin), "checkin-health")
	labels := findAll(choices, func(n *html.Node) bool { return n.Data == "label" })
	if len(labels) != 3 {
		t.Fatalf("the Health choice has %d options, want 3: %s", len(labels), renderNode(choices))
	}
	for i, health := range []string{"Green", "Yellow", "Red"} {
		assertHealthChip(t, labels[i], health)
	}
}

// The draft's body is two columns, the Report's main column at most 780px
// beside a 300px one for its Delegates and History, 56px apart, in a page
// wide enough to hold them. Under 960px of the page's content width — a
// container query on the page body, so the sidebar counts — the narrow column
// moves under the main one at full width (#473), so a 1280px window keeps them side by side (#477).
func TestReportDraftColumnsStackUnder960pxStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	doc := parsePage(t, page)
	main, side := findByTestID(t, doc, "report-draft"), findByTestID(t, doc, "draft-side")
	body := main.Parent
	if side.Parent != body || !slices.Contains(strings.Fields(attrOf(body, "class")), "rp-draft-page") || attrOf(body.Parent, "class") != "page" {
		t.Fatalf("the main and narrow columns aren't the page body's .rp-draft-page's own: %s", renderNode(body))
	}
	if !precedes(main, side) {
		t.Errorf("the narrow column comes before the main one, so it wouldn't stack under it")
	}

	styles := between(t, page, "<style>", "</style>")
	for sel, wants := range map[string][]string{
		".page:has(>.rp-draft-page)": {"container:rp-draft/inline-size", "max-width:1200px"},
		".rp-draft-page":             {"display:grid", "grid-template-columns:minmax(0,780px) minmax(0,300px)", "gap:56px"},
		".rp-draft-page>*":           {"min-width:0"},
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

// A Goal block's labels — Why it matters, Status, Milestones, Metrics,
// Rolled-up Health and Supported by — sit in a 120px column beside what they
// label, and above it at a phone's width or in a narrow block. Its one tinted
// box is a Red or Yellow Goal's Path to Green, in that Health's tint with its
// label in the Health's ink: no other rule tints it (#475).
func TestReportBlockLabelColumnAndPathTintStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Checkin(boss, g.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), fmt.Sprintf("%s/reports/%d", ts.URL, def.ID))
	block := findByTestID(t, parsePage(t, page), "report-exception")
	for _, testID := range []string{"report-why-it-matters", "report-status", "report-milestones"} {
		n := findByTestID(t, block, testID)
		for n.Data != "dd" {
			n = n.Parent
		}
		row := n.Parent
		dt := row.FirstChild
		for dt != nil && dt.Type != html.ElementNode {
			dt = dt.NextSibling
		}
		if dl := row.Parent; dl.Data != "dl" || !slices.Contains(strings.Fields(attrOf(dl, "class")), "rp-facts") ||
			dt == nil || dt.Data != "dt" || !slices.Contains(strings.Fields(attrOf(dt, "class")), "label") {
			t.Errorf("%s isn't a labelled row of the block's .rp-facts: %s", testID, renderNode(row.Parent))
		}
	}

	styles := between(t, page, "<style>", "</style>")
	for sel, wants := range map[string][]string{
		".rp-facts>div":             {"display:grid", "grid-template-columns:120px minmax(0,1fr)"},
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
	for _, query := range []string{"@media (max-width:600px)", "@container rp-block (max-width:28rem)"} {
		if decls, ok := ruleFor(mediaBlock(t, styles, query), ".rp-facts>div"); !ok || !strings.Contains(decls, "grid-template-columns:minmax(0,1fr)") {
			t.Errorf("%s .rp-facts>div{%s} doesn't put the label above what it labels", query, decls)
		}
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
	for name, c := range map[string]struct{ url, column string }{
		"draft":       {fmt.Sprintf("%s/reports/%d", ts.URL, def.ID), "rp-main"},
		"publication": {fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID), "rp-read"},
	} {
		page := getBody(t, client, c.url)
		doc := parsePage(t, page)
		column := findAll(doc, func(n *html.Node) bool { return slices.Contains(strings.Fields(attrOf(n, "class")), c.column) })
		if len(column) != 1 {
			t.Fatalf("%s has %d .%s columns, want 1", name, len(column), c.column)
		}
		if heading := findByTestID(t, findByTestID(t, column[0], "needs-attention"), "needs-attention-count").Parent; heading.Data != "h2" || heading.Parent.Data != "section" {
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
// TestReportBlockTintsOnlyAPathToGreenOverHTTP). The tints' colours are
// TestReportBlockLabelColumnAndPathTintStyles.
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
// the print page (#229, #453, TestReportCardHeadLeadsWithItsHealthOverHTTP).
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
			assertReportsCarriesClass(t, where+"'s title", title.Parent.Parent, "rp-card-title")
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

// The print page lays a Goal block out as the screen does (#475), Why it
// matters and Status in a label column, and the Path to Green in a box tinted
// in its Goal's Health, Red or Yellow, kept when printed. An overdue Path to
// Green keeps its Health's tint, so an overdue Yellow one prints Yellow, not
// Red (#478, TestPrintPageLaysOutTheGoalBlockAsOnScreenOverHTTP).
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
		for _, id := range []string{"report-why-it-matters", "report-status"} {
			assertReportsCarriesClass(t, c.goal.Title+"'s "+id+" list", findByTestID(t, block, id).Parent.Parent, "rp-facts")
		}
		assertReportsCarriesClass(t, c.goal.Title+"'s Path to Green box", findByTestID(t, block, "report-path-box"), "rp-tint", c.tint)
	}

	style := reportsStyleSheetIn(doc)
	facts := printPageRule(t, style, ".rp-facts")
	if !strings.Contains(facts, "display: grid;") || !strings.Contains(facts, "grid-template-columns: 7.5rem") || !strings.Contains(facts, "margin: 0.3rem 0;") {
		t.Errorf("print page doesn't lay Why it matters and Status out in a label column: .rp-facts { %s }", facts)
	}
	if dd := printPageRule(t, style, ".rp-facts dd"); !strings.Contains(dd, "margin: 0;") {
		t.Errorf("print page indents a fact's value by the browser's dd margin: .rp-facts dd { %s }", dd)
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

// The type scale is four --text-* tokens on :root, at the laptop's sizes, so
// one rule can raise them on a touch screen (DESIGN.md § Typography, #513):
// body copy at 14px, secondary copy and section headings at 13px, labels at
// 12px. The base rules take their size from them.
func TestTypeScaleIsTokensStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	root := tokens(tokenBlock(t, css, ":root"))
	for token, want := range map[string]string{
		"--text-body":      "14px",
		"--text-secondary": "13px",
		"--text-label":     "12px",
		"--text-h2":        "13px",
	} {
		if got := root[token]; got != want {
			t.Errorf(":root %s is %q, want %s", token, got, want)
		}
	}
	for sel, want := range map[string]string{
		"body":      "font-size:var(--text-body)",
		"h2":        "font-size:var(--text-h2)",
		".small":    "font-size:var(--text-secondary)",
		".label,th": "font-size:var(--text-label)",
	} {
		if decls := cssRule(t, css, "\n"+sel); !strings.Contains(decls, want) {
			t.Errorf("%s{%s} doesn't declare %s", sel, decls, want)
		}
	}
	if decls := cssRule(t, css, "\nh2"); !strings.Contains(decls, "font-weight:600") {
		t.Errorf("h2{%s} isn't 600", decls)
	}
	if decls, _ := ruleFor(css, ".label"); !strings.Contains(decls, "color:var(--color-ink-muted)") {
		t.Errorf(".label{%s} doesn't recede in muted ink", decls)
	}
}

// On a touch screen, read at arm's length, the scale grows: body copy to
// 16px, secondary copy and section headings to 15px, and muted copy darkens
// to the body's ink. Labels keep their size and muted ink, so they recede by
// colour (DESIGN.md § Typography, #513).
func TestTouchScreenRaisesTheTypeScaleStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	touch := mediaBlock(t, css, "@media (pointer:coarse)")
	root, ok := ruleFor(touch, ":root")
	if !ok {
		t.Fatalf("@media (pointer:coarse) sets no :root tokens: %s", touch)
	}
	raised := tokens(root)
	for token, want := range map[string]string{
		"--text-body":      "16px",
		"--text-secondary": "15px",
		"--text-h2":        "15px",
	} {
		if got := raised[token]; got != want {
			t.Errorf("on touch %s is %q, want %s", token, got, want)
		}
	}
	if got, ok := raised["--text-label"]; ok {
		t.Errorf("on touch --text-label is raised to %s; a label keeps its size", got)
	}
	if decls, ok := ruleFor(touch, ".muted"); !ok || !strings.Contains(decls, "color:var(--color-ink-2)") {
		t.Errorf("on touch .muted{%s} isn't the body's ink", decls)
	}
	for _, sel := range []string{".label", "th"} {
		if decls, ok := ruleFor(touch, sel); ok && strings.Contains(decls, "color:") {
			t.Errorf("on touch %s{%s} is recoloured; a label recedes in muted ink", sel, decls)
		}
	}
}

// No screen style sets a 12, 13 or 14px font size of its own, so the touch
// screen's scale reaches every page: app.css and each page's style block take
// a --text-* token instead. The Report Print view keeps its pt sizes
// (DESIGN.md § Typography, #513).
func TestNoLiteralTypeScaleSizesStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	literal := regexp.MustCompile(`font-size\s*:\s*1[234]px`)
	for sheet, styles := range styleSheets(t, css) {
		if n := len(literal.FindAllString(styles, -1)); n > 0 {
			t.Errorf("%s sets %d literal 12–14px font sizes, want --text-* tokens", sheet, n)
		}
	}
}

// tokens reads a custom property written last in its rule, with no ; after
// it, as one followed by ; or the rule's }: a --text-label raised last in the
// touch screen's :root is seen (#537).
func TestStyleHelpersTokensReadAPropertyThatEndsItsRule(t *testing.T) {
	t.Parallel()

	for _, decls := range []string{"--text-body:16px;--text-label:14px", "--text-body:16px;--text-label:14px}", "--text-body:16px;--text-label: 14px ;"} {
		if got := tokens(decls); got["--text-body"] != "16px" || got["--text-label"] != "14px" {
			t.Errorf("tokens(%q) = %v, want --text-body 16px and --text-label 14px", decls, got)
		}
	}
}

// ruleFor reads every rule whose selector list names the selector, not only
// the first, so a declaration in a later rule for it is seen (#537).
func TestStyleHelpersRuleForReadsEveryRuleForTheSelector(t *testing.T) {
	t.Parallel()

	css := `.a{color:red}.b{margin:0}/* later */.c,.a{padding:4px}`
	decls, ok := ruleFor(css, ".a")
	if !ok || !strings.Contains(decls, "color:red") || !strings.Contains(decls, "padding:4px") || strings.Contains(decls, "margin") {
		t.Errorf("ruleFor(%q, .a) = %q, %t, want color:red and padding:4px, and not .b's margin", css, decls, ok)
	}
	if got := declValue(t, decls, "color"); got != "red" {
		t.Errorf("color in .a's rules %q is %q, want red", decls, got)
	}
	if decls, ok := ruleFor(css, ".d"); ok {
		t.Errorf("ruleFor(%q, .d) = %q, found, want no rule", css, decls)
	}
}

// declValue reads the value the cascade applies, the last one decls gives the
// property, so a later rule ruleFor joins overrides an earlier one (#544).
func TestStyleHelpersDeclValueReadsTheLastValueForTheProperty(t *testing.T) {
	t.Parallel()

	css := `.a{color:red}.b{margin:0}.a{color:blue}`
	decls, _ := ruleFor(css, ".a")
	if got := declValue(t, decls, "color"); got != "blue" {
		t.Errorf("color in .a's rules %q is %q, want blue, the later rule's", decls, got)
	}
}

// The sidebar is as tall as the part of the window a person can see, so the
// signed-in person at its foot shows without scrolling, even in a browser
// whose toolbars take part of the window, such as Safari on an iPad, where
// 100vh is the height with the toolbars hidden. Each viewport height reads
// dvh, after a vh fallback for a browser without dvh.
func TestSidebarFitsTheVisibleWindow(t *testing.T) {
	t.Parallel()

	ts := newServer(t, testsupport.New(t))
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	for sel, want := range map[string]string{
		".frame":                  "min-height:100vh;min-height:100dvh",
		".sidebar":                "height:100vh;height:100dvh",
		".sb-menu[open]~.sb-body": "max-height:calc(100vh - 48px);max-height:calc(100dvh - 48px)",
	} {
		assertHomeRuleDeclares(t, css, sel, want)
	}
}

// A Name in brackets keeps its "(" with it: the element holding the "(" and
// the Name doesn't break, but the Name inside it still wraps, since a person
// with no Name is shown by their email, which must fit a phone (#642).
func TestNameInBracketsKeepsItsOpenBracketStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	for sel, wants := range map[string][]string{
		".person-nobreak":           {"white-space:nowrap"},
		".person-nobreak>.disclose": {"white-space:normal", "overflow-wrap:anywhere"},
	} {
		decls, ok := ruleFor(css, sel)
		if !ok {
			t.Errorf("app.css has no %s rule", sel)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(decls, want) {
				t.Errorf("app.css %s{%s} lacks %s", sel, decls, want)
			}
		}
	}
}

// The Import page's file input error sits on its own line in the danger ink,
// as the Goal form's input errors do (#676).
func TestImportFileErrorIsInTheDangerInkStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "admin@example.com")
	page := getBody(t, admin, ts.URL+"/imports")

	rule := cssRule(t, page, ".im-input-error")
	for _, want := range []string{"display:block", "font-size:var(--text-secondary)", "color:var(--color-danger-ink)"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".im-input-error{%s} lacks %s", rule, want)
		}
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

// The person picker's results drop down under its input, over what follows,
// rather than pushing the form's buttons down (#667).
func TestPersonPickerResultsDropDownUnderItsInput(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	for sel, wants := range map[string][]string{
		".person-picker":  {"position:relative"},
		".person-results": {"position:absolute", "top:100%"},
	} {
		decls, ok := ruleFor(css, sel)
		for _, want := range wants {
			if !ok || !strings.Contains(decls, want) {
				t.Errorf("app.css %s{%s} doesn't declare %s", sel, decls, want)
			}
		}
	}
}
