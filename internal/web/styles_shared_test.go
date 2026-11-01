package web_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The style tests check the pages' style rules, and the classes those rules
// select, apart from the pages' behaviour tests, so a page's behaviour tests
// (checkins_test.go) don't depend on them. Each area's live in its own
// styles_<area>_test.go (styles_checkins_test.go, styles_goals_test.go, …);
// the tests here check rules in the shared stylesheet that more than one area
// uses, and the style helpers themselves. How a page looks in a browser is the
// Playwright suite's (e2e/).

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
