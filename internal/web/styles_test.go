package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

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
	for _, child := range []struct {
		goal  domain.Goal
		badge string
	}{
		{f.yellow, `<span class="badge y"><span class="dot"></span>Yellow</span>`},
		{f.green, `<span class="badge g"><span class="dot"></span>Green</span>`},
	} {
		link := fmt.Sprintf(`<a href="/goals/%d">%s</a>`, child.goal.ID, child.goal.Title)
		at := strings.Index(list, link)
		if at < 0 {
			t.Errorf("children list has no link %s; list:\n%s", link, list)
			continue
		}
		item, _, _ := strings.Cut(list[at:], "</li>")
		if !strings.Contains(item, child.badge) {
			t.Errorf("%s shows no %s; item:\n%s", child.goal.Title, child.badge, item)
		}
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
	if line := pageElement(t, page, "p", "checkin-form-rollup"); !strings.Contains(line, `<span class="badge r"><span class="dot"></span>Red</span>`) {
		t.Errorf("rollup line doesn't show Red as a badge; line:\n%s", line)
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
		`<span class="badge y"><span class="dot"></span>Yellow</span>`,
		`<span class="muted">back to Green by Mar 2</span>`,
	} {
		if !strings.Contains(last, want) {
			t.Errorf("Last time lacks %s:\n%s", want, last)
		}
	}
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
