package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
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

	_, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)

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

// The submit bar, its summary, Cancel and Submit check-in carry the classes
// its rules select, quiet Cancel beside a primary Submit check-in; the old
// checkin-submit row is gone (ticket #367).
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
	if !strings.Contains(bar, fmt.Sprintf(`<a class="btn quiet" href="/goals/%d">Cancel</a>`, goal.ID)) {
		t.Errorf("the submit bar has no quiet Cancel back to the Goal:\n%s", bar)
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
// field it scrolls to clear of it. At 600px and below Cancel hides, the
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
	for _, want := range []string{"flex:1", "color:var(--color-ink-2)", "font-size:14px"} {
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
		".checkin-summary":                 "font-size:13px",
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

// The column sticks beside the form on a wide screen as a quiet rail, and on
// a phone drops Last time and Dates, which the form's ghost text already
// covers (ticket #366), leaving Contributed to by with no rule above it.
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
	if rule, _ := ruleFor(styles, ".checkin-context"); !strings.Contains(rule, "font-size:13px") {
		t.Errorf(".checkin-context{%s} isn't in small text", rule)
	}
	phone := mediaBlock(t, styles, "@media (max-width:600px)")
	// Each phone rule's selector outranks the base rule it overrides
	// (.checkin-context>section, 0,1,1; ...>section+section, 0,2,1), so it
	// wins (#370).
	for _, sel := range []string{".checkin-context>.checkin-context-last", ".checkin-context>.checkin-context-dates"} {
		if rule, ok := ruleFor(phone, sel); !ok || !strings.Contains(rule, "display:none") {
			t.Errorf("at 600px %s{%s} isn't hidden", sel, rule)
		}
	}
	if rule, ok := ruleFor(phone, ".checkin-context>section.checkin-context-children"); !ok ||
		!strings.Contains(rule, "border-top:none") || !strings.Contains(rule, "padding-top:0") {
		t.Errorf("at 600px .checkin-context>section.checkin-context-children{%s} keeps its top rule", rule)
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
// wide enough to hold them. Under 1100px of the page's content width — a
// container query on the page body, so the sidebar counts — the narrow column
// moves under the main one at full width (#473).
func TestReportDraftColumnsStackUnder1100pxStyles(t *testing.T) {
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
	narrow := mediaBlock(t, styles, "@container rp-draft (max-width:1100px)")
	if decls, ok := ruleFor(narrow, ".rp-draft-page"); !ok || !strings.Contains(decls, "grid-template-columns:minmax(0,1fr)") {
		t.Errorf("under 1100px .rp-draft-page{%s} isn't one full-width column", decls)
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
// titles at 16px, and its block labels recede, 12px and muted. The
// Exceptions count stays beside its heading, smaller and muted, and the
// narrative's Insights, Accomplishments and Misses read at 15px (#475).
func TestReportHeadingScaleStyles(t *testing.T) {
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
	if decls, ok := ruleFor(css, ".label"); !ok || !strings.Contains(decls, "font-size:12px") || !strings.Contains(decls, "color:var(--color-ink-muted)") {
		t.Errorf(".label{%s} isn't a 12px muted label", decls)
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
			".rp-exceptions>h2>.num":       {"font-size:14px"},
			".rp-exceptions>h2+.rp-block":  {"border-top:none"},
			".rp-card-head h3":             {"font-size:16px", "font-weight:600"},
			".rp-narrative h3.label":       {"font-size:15px", "font-weight:600"},
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
