package web_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

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
