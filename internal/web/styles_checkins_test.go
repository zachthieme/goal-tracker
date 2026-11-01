package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

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

// A Check-in refused for a missing Health edges each Health choice in the
// danger ink, 1px as an invalid textarea is, while its hidden radio is invalid
// and until a Health is picked; the edge sits outside the choice's own border,
// so the picked choice's edge and the focus ring still show (#695).
func TestCheckinMissingHealthEdgesItsChoicesStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")
	page, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{"status": {"Fine."}})

	radios := formFields(parsePage(t, page), "health")
	if len(radios) != 3 {
		t.Fatalf("the form has %d Health radios, want 3", len(radios))
	}
	for _, r := range radios {
		label := r.Node.Parent
		if attrOf(r.Node, "aria-invalid") != "true" || label == nil || label.Data != "label" ||
			!slices.Contains(strings.Fields(attrOf(label, "class")), "badge") ||
			label.Parent == nil || !slices.Contains(strings.Fields(attrOf(label.Parent, "class")), "health-toggle") {
			t.Errorf("Health radio %q isn't an invalid radio in a .health-toggle label.badge", attrOf(r.Node, "value"))
		}
	}
	styles := between(t, page, ".checkin-page{", "</style>")
	const sel = ".health-toggle:not(:has(input:checked)) label.badge:has(input[aria-invalid=true])"
	if decls, ok := ruleFor(styles, sel); !ok || !strings.Contains(decls, "box-shadow:0 0 0 1px var(--color-danger-ink)") {
		t.Errorf("%s{%s} doesn't edge the choice 1px in var(--color-danger-ink)", sel, decls)
	}
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if !strings.Contains(css, "textarea[aria-invalid=true],select[aria-invalid=true]{border-color:var(--color-danger-ink)}") ||
		!strings.Contains(css, "textarea,select{font:inherit;color:var(--color-ink);background:var(--color-surface);border:1px solid") {
		t.Errorf("an invalid textarea is no longer edged 1px in var(--color-danger-ink); match the Health choices' edge to it")
	}
}

// A refused Lifecycle choice is edged (ticket #707). Its radios are native, and
// a native radio shows no border, so the shared invalid border can't mark them;
// nor can a ring on the radio, which Chromium draws square whatever its
// border-radius. So while none is picked, each choice with an invalid radio
// takes a 1px outline in the danger ink, the width of the Health choices'
// edge, set off 2px with rounded corners. Picking one drops it. Every choice
// keeps a little inline padding, refused or not, so the edge clears its radio
// and its text and the choices don't move as it comes and goes (ticket #711).
func TestCheckinRefusedLifecycleEdgesItsChoicesStyles(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")
	page, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen}, "status": {"Fine."}, "lifecycle": {domain.LifecycleProposed},
	})

	radios := formFields(parsePage(t, page), "lifecycle")
	if len(radios) == 0 {
		t.Fatal("the refused form has no Lifecycle radios")
	}
	for _, r := range radios {
		label := r.Node.Parent
		if r.Type != "radio" || attrOf(r.Node, "aria-invalid") != "true" || label == nil || label.Data != "label" ||
			label.Parent == nil || !slices.Contains(strings.Fields(attrOf(label.Parent, "class")), "lifecycle-choices") {
			t.Errorf("Lifecycle choice %q isn't an invalid radio in a .lifecycle-choices label", r.Label)
		}
	}
	styles := between(t, page, ".checkin-page{", "</style>")
	const sel = ".lifecycle-choices:not(:has(input:checked)) label:has(input[aria-invalid=true])"
	decls, ok := ruleFor(styles, sel)
	if !ok || !strings.Contains(decls, "outline:1px solid var(--color-danger-ink)") || !strings.Contains(decls, "outline-offset:2px") ||
		!strings.Contains(decls, "border-radius:var(--radius-sm)") {
		t.Errorf("%s{%s} doesn't edge the choice 1px in var(--color-danger-ink), set off 2px with var(--radius-sm) corners", sel, decls)
	}
	const base = ".lifecycle-choices label"
	decls, _ = ruleFor(styles, base)
	m := regexp.MustCompile(`(?:^|;)\s*padding-inline:\s*(\d+)px\s*(?:;|$)`).FindStringSubmatch(decls)
	if m == nil || m[1] == "0" {
		t.Errorf("%s{%s} gives the choice no padding-inline of some px, refused or not, so the edge sits against its text", base, decls)
	}
}
