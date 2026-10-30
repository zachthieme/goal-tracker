package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const day = 24 * time.Hour

// setCadence sets how often a Check-in is expected on g, failing the test on
// error.
func setCadence(t *testing.T, h *testsupport.Harness, g domain.Goal, days int) {
	t.Helper()
	if _, err := h.Service.SetCadence(context.Background(), g.Owner.ID, g.ID, days); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
}

// homeGoalPath is the address of g's page, where a Home row's title links.
func homeGoalPath(g domain.Goal) string {
	return fmt.Sprintf("/goals/%d", g.ID)
}

// homeLinksTo reports whether anything under root links to g's page.
func homeLinksTo(root *html.Node, g domain.Goal) bool {
	return linkTo(root, homeGoalPath(g)) != nil
}

// homeRowIn returns the row of a Home section that links to g: the <li>
// holding the link, failing the test when the section has none.
func homeRowIn(t *testing.T, section *html.Node, g domain.Goal) *html.Node {
	t.Helper()
	for n := linkTo(section, homeGoalPath(g)); n != nil && within(n, section); n = n.Parent {
		if n.Type == html.ElementNode && n.Data == "li" {
			return n
		}
	}
	t.Fatalf("section has no row for %q:\n%s", g.Title, renderNode(section))
	return nil
}

// homeFormTo returns the first form under root posting to action, or nil.
func homeFormTo(root *html.Node, action string) *html.Node {
	if forms := findAll(root, func(n *html.Node) bool { return n.Data == "form" && attrOf(n, "action") == action }); len(forms) > 0 {
		return forms[0]
	}
	return nil
}

// homeFormsEndingIn returns every form under root whose action ends in tail.
func homeFormsEndingIn(root *html.Node, tail string) []*html.Node {
	return findAll(root, func(n *html.Node) bool { return n.Data == "form" && strings.HasSuffix(attrOf(n, "action"), tail) })
}

// homeClasses are the classes n carries, in order.
func homeClasses(n *html.Node) []string { return strings.Fields(attrOf(n, "class")) }

// homeIsHeading reports whether n is a heading, h1 to h6.
func homeIsHeading(n *html.Node) bool {
	return len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6'
}

// homeOneElement returns the one element under root that match reports,
// failing the test, which names it what, unless there is exactly one.
func homeOneElement(t *testing.T, root *html.Node, what string, match func(*html.Node) bool) *html.Node {
	t.Helper()
	found := findAll(root, match)
	if len(found) != 1 {
		t.Fatalf("page has %d %s, want 1:\n%s", len(found), what, renderNode(root))
	}
	return found[0]
}

// homeMainIn is the page's <main>.
func homeMainIn(t *testing.T, root *html.Node) *html.Node {
	t.Helper()
	return homeOneElement(t, root, "<main>s", func(n *html.Node) bool { return n.Data == "main" })
}

// homePageHeaderIn is the page's header strip, over the page title.
func homePageHeaderIn(t *testing.T, root *html.Node) *html.Node {
	t.Helper()
	return homeOneElement(t, root, "header strips", func(n *html.Node) bool { return flatFrameHasClass(n, "page-header") })
}

// homeNavIn is the sidebar's main navigation.
func homeNavIn(t *testing.T, root *html.Node) *html.Node {
	t.Helper()
	return homeOneElement(t, root, "Main navs", func(n *html.Node) bool { return n.Data == "nav" && attrOf(n, "aria-label") == "Main" })
}

// homeNavCounts are the counts a sidebar nav item shows, each as its classes
// and then what it reads ("count needs-you: 3"), so an assertion sees both
// what it reads and whether it is the needs-you pill.
func homeNavCounts(item *html.Node) []string {
	var out []string
	for _, c := range findAll(item, func(n *html.Node) bool { return flatFrameHasClass(n, "count") }) {
		out = append(out, attrOf(c, "class")+": "+textOf(c))
	}
	return out
}

// needsYouHeading reads the Needs you heading, "" when Home has none.
func needsYouHeading(root *html.Node) string {
	if got := findAllByTestID(root, "home-needs-you-heading"); len(got) == 1 && got[0].Data == "h2" {
		return textOf(got[0])
	}
	return ""
}

// homeShowsPerson reports whether root shows the person emailAddr by label,
// on a collapsed control that discloses their email, hidden beside it, with
// the email on hover too (CONTEXT.md: Name).
func homeShowsPerson(root *html.Node, emailAddr, label string) bool {
	for _, p := range findAll(root, func(n *html.Node) bool { return flatFrameHasClass(n, "person") }) {
		buttons := findAll(p, func(n *html.Node) bool { return n.Data == "button" })
		emails := findAll(p, func(n *html.Node) bool { return flatFrameHasClass(n, "person-email") })
		if len(buttons) != 1 || len(emails) != 1 {
			continue
		}
		b := buttons[0]
		if textOf(b) == label && attrOf(b, "title") == emailAddr && attrOf(b, "type") == "button" &&
			flatFrameHasClass(b, "disclose") && attrOf(b, "aria-expanded") == "false" &&
			isHiddenElement(emails[0]) && runningText(emails[0]) == emailAddr {
			return true
		}
	}
	return false
}

// The Needs you heading carries how many things need the viewer, in muted
// text after its name, so the page title doesn't say it again; with nothing
// waiting it reads plain "Needs you" (#377, #391).
func TestHomeNeedsYouHeadingCarriesTheCount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.Clock.Advance(10 * day)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	if got := needsYouHeading(page); got != "Needs you 2" {
		t.Errorf("Needs you heading = %q, want %q", got, "Needs you 2")
	}
	if hasTestID(page, "home-summary") || strings.Contains(textOf(page), "things need you") {
		t.Errorf("the page head still says how many things need sam: %q", textOf(findByTestID(t, page, "home-head")))
	}

	pat := getPage(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/home")
	if got := needsYouHeading(pat); got != "Needs you" {
		t.Errorf("pat's Needs you heading = %q, want %q", got, "Needs you")
	}
}

// Home is one surface with no card, Needs you included: Needs you, My org and
// the viewer's Goals that aren't Green are plain sections, under the page
// header's "Home" and its New goal, and the page title, Your week, over
// today's date and the Your goals line, counting by Health and linking on
// (#377, #391).
func TestHomeIsOneSurfaceWithNoCard(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeOrgFixture(h)
	mine := h.ActiveGoal(f.lead, "Hire", "We need people.")
	h.Checkin(f.lead, mine.ID, domain.HealthRed, "No offers.", "Raise the band.", h.Clock.Now().AddDate(0, 1, 0))

	page := getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")
	main := homeMainIn(t, page)

	if cards := findAll(page, func(n *html.Node) bool { return flatFrameHasClass(n, "card") }); len(cards) != 0 {
		t.Errorf("Home has %d cards, want none: %s", len(cards), renderNode(cards[0]))
	}
	if asides := findAll(main, func(n *html.Node) bool { return n.Data == "aside" }); len(asides) != 0 {
		t.Errorf("Home still has a side column:\n%s", renderNode(asides[0]))
	}

	strip := homePageHeaderIn(t, page)
	if got := textOf(strip); got != "Home New goal" {
		t.Errorf("page header reads %q, want Home beside New goal", got)
	}
	if newGoal := linkTo(strip, "/goals/new"); newGoal == nil || !flatFrameHasClass(newGoal, "primary") {
		t.Errorf("New goal isn't the page header's primary action:\n%s", renderNode(strip))
	}

	head := findByTestID(t, page, "home-head")
	if got := textOf(head); got != "Your week Monday, Jan 12 Your goals: 0 Green · 0 Yellow · 1 Red · Goals you own or help with →" {
		t.Errorf("page title reads %q, want Your week over the date and the Your goals line", got)
	}
	times := findAll(head, func(n *html.Node) bool { return n.Data == "time" })
	if title := headingNamed(head, "Your week"); title == nil || title.Data != "h1" || len(times) != 1 || attrOf(times[0], "datetime") != "2026-01-12" {
		t.Errorf("Your week isn't the page title over today's date:\n%s", renderNode(head))
	}
	if link := linkTo(head, "/goals?org=mine"); link == nil || textOf(link) != "Goals you own or help with →" {
		t.Errorf("page head's Your goals line doesn't link to the Goals the lead owns or helps with:\n%s", renderNode(head))
	}
	if notGreen := findByTestID(t, main, "home-not-green"); !homeLinksTo(notGreen, mine) {
		t.Errorf("the lead's Red Goal isn't listed as not Green:\n%s", renderNode(notGreen))
	}
}

// Home's outline reads Your week, then a section heading (h2) for each of
// Needs you, My org and Your Yellow or Red goals, so no heading skips a level
// (#386). Each heading has its count after its name: My org counts the people
// it lists, and Your Yellow or Red goals its Goals (#391). How they look is
// TestHomeSectionsAreHeadedH2Styles's.
func TestHomeSectionsAreHeadedH2(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeOrgFixture(h)
	mine := h.ActiveGoal(f.lead, "Hire", "We need people.")
	h.Checkin(f.lead, mine.ID, domain.HealthRed, "No offers.", "Raise the band.", h.Clock.Now().AddDate(0, 1, 0))

	main := homeMainIn(t, getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"))

	for _, testID := range []string{"home-needs-you", "home-org", "home-not-green"} {
		section := findByTestID(t, main, testID)
		if headings := findAll(section, homeIsHeading); len(headings) == 0 || headings[0].Data != "h2" {
			t.Errorf("%s's first heading isn't an h2:\n%s", testID, renderNode(section))
		}
	}
	var firstH2, firstH3 *html.Node
	for _, heading := range findAll(main, homeIsHeading) {
		if heading.Data == "h2" && firstH2 == nil {
			firstH2 = heading
		}
		if heading.Data == "h3" && firstH3 == nil {
			firstH3 = heading
		}
	}
	if firstH2 == nil || (firstH3 != nil && precedes(firstH3, firstH2)) {
		t.Errorf("an h3 comes before Home's first h2:\n%s", renderNode(main))
	}

	for testID, want := range map[string]string{
		"home-org":       "My org 2",
		"home-not-green": "Your Yellow or Red goals 1",
	} {
		if heading := headingNamed(findByTestID(t, main, testID), want); heading == nil || heading.Data != "h2" {
			t.Errorf("%s isn't headed %q", testID, want)
		}
	}
}

// A Check-in overdue Goal the viewer Owns is listed under "Check-ins due",
// saying how long since its last Check-in against its cadence, flagged Check-in
// overdue, with a one-click No change and a Check in link. Fresh Goals and
// other people's Check-in overdue Goals aren't listed.
func TestHomeListsCheckinOverdueGoalToCheckInOn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	checkinOverdue := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, checkinOverdue, 14)
	h.Checkin(sam, checkinOverdue.ID, domain.HealthYellow, "Slipping.", "Cut scope.", testsupport.Epoch.AddDate(0, 1, 0))
	others := h.ActiveGoal(kim, "Grow revenue", "It pays for everything.")
	h.Clock.Advance(16 * day)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	due := findByTestID(t, page, "home-due")
	row := homeRowIn(t, due, checkinOverdue)
	for _, testID := range []string{"home-due-checkin-overdue", "home-due-health", "home-due-checkin"} {
		if !hasTestID(row, testID) {
			t.Errorf("Check-in overdue Goal's row lacks %s:\n%s", testID, renderNode(row))
		}
	}
	if form := homeFormTo(row, fmt.Sprintf("/goals/%d/checkins/no-change", checkinOverdue.ID)); form == nil || buttonNamed(form, "No change") == nil {
		t.Errorf("Check-in overdue Goal's row offers no No change:\n%s", renderNode(row))
	}
	if got := textOf(findByTestID(t, row, "home-due-status")); !strings.Contains(got, "last check-in 16 days ago · every 14 days") {
		t.Errorf("Check-in overdue Goal's status line = %q, want how long since its last Check-in against its cadence", got)
	}
	for _, g := range []domain.Goal{fresh, others} {
		if homeLinksTo(due, g) {
			t.Errorf("Check in on these lists %q, which isn't due for sam:\n%s", g.Title, renderNode(due))
		}
	}
}

// A row in Check-ins due leads with the Goal's Health pill, then its title,
// then one meta line: how long since its last Check-in, its cadence and, when
// it applies, its Check-in overdue mark. The age is said once, and No change
// and Check in sit after it (#377, #391). How the row looks is
// TestHomeDueRowLeadsWithItsHealthStyles's.
func TestHomeDueRowLeadsWithItsHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	checkinOverdue := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, checkinOverdue, 14)
	h.Checkin(sam, checkinOverdue.ID, domain.HealthYellow, "Slipping.", "Cut scope.", testsupport.Epoch.AddDate(0, 1, 0))
	h.Clock.Advance(16 * day)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	row := homeRowIn(t, findByTestID(t, page, "home-due"), checkinOverdue)
	health, title, status := findByTestID(t, row, "home-due-health"), linkTo(row, homeGoalPath(checkinOverdue)), findByTestID(t, row, "home-due-status")
	noChange, checkIn := buttonNamed(row, "No change"), findByTestID(t, row, "home-due-checkin")
	if noChange == nil || !precedes(health, title) || !precedes(title, status) || !precedes(status, noChange) || !precedes(noChange, checkIn) {
		t.Errorf("row doesn't read Health, title, meta line, then No change and Check in:\n%s", renderNode(row))
	}
	assertHealthChip(t, health, "Yellow")
	if got := textOf(status); got != "last check-in 16 days ago · every 14 days Check-in overdue" {
		t.Errorf("row's meta line = %q, want its age, cadence and Check-in overdue mark", got)
	}
	if n := strings.Count(textOf(row), "16 days ago"); n != 1 {
		t.Errorf("row says the Goal's age %d times, want once: %q", n, textOf(row))
	}
}

// A Goal that would go Check-in overdue before next week's reminder is listed
// too, by the same rule as the reminder email, but not flagged Check-in
// overdue. A Goal with a week to spare isn't listed, and a Goal with no
// Check-in to repeat offers no No change.
func TestHomeListsGoalDueBeforeNextReminder(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dueSoon := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, dueSoon, 14)
	h.Checkin(sam, dueSoon.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(6 * day)
	spare := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	setCadence(t, h, spare, 14)
	h.Checkin(sam, spare.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(2 * day)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	due := findByTestID(t, page, "home-due")
	row := homeRowIn(t, due, dueSoon)
	if got := textOf(findByTestID(t, row, "home-due-status")); got != "last check-in 8 days ago · every 14 days" {
		t.Errorf("due-soon Goal's row does not say why it's listed: %q", got)
	}
	if hasTestID(row, "home-due-checkin-overdue") {
		t.Errorf("due-soon Goal is flagged Check-in overdue before it is:\n%s", renderNode(row))
	}
	if homeLinksTo(due, spare) {
		t.Errorf("Check in on these lists %q, which has a week to spare:\n%s", spare.Title, renderNode(due))
	}
}

// A Check-in overdue Goal's Health in Check-ins due is its last Check-in's, so
// it reads as last reported: a muted badge naming just the Health, dated by the
// "last check-in" beside it, still flagged Check-in overdue, and with no "as
// of" to say the age again. A due Goal that isn't Check-in overdue yet keeps
// its full-colour badge.
func TestHomeDatesCheckinOverdueGoalsHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	checkinOverdue := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, checkinOverdue.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(day)
	dueSoon := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	setCadence(t, h, dueSoon, 14)
	h.Checkin(sam, dueSoon.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(8 * day)

	due := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "home-due")

	row := homeRowIn(t, due, checkinOverdue)
	badge := findByTestID(t, row, "home-due-health")
	if !flatFrameHasClass(badge, "as-of") {
		t.Errorf("Check-in overdue Goal's Health badge is not muted: %s", renderNode(badge))
	}
	assertHealthChip(t, badge, "Green")
	if got := textOf(findByTestID(t, row, "home-due-status")); got != "last check-in 9 days ago · every 7 days Check-in overdue" {
		t.Errorf("Check-in overdue Goal's Health is dated %q, want by its last Check-in 9 days ago", got)
	}
	if !hasTestID(row, "home-due-checkin-overdue") {
		t.Errorf("Check-in overdue Goal is no longer flagged Check-in overdue:\n%s", renderNode(row))
	}

	row = homeRowIn(t, due, dueSoon)
	if flatFrameHasClass(findByTestID(t, row, "home-due-health"), "as-of") {
		t.Errorf("due Goal that isn't Check-in overdue has its Health muted:\n%s", renderNode(row))
	}
}

// A Check-in overdue Goal never checked in on has no reported Health to date:
// its badge reads "No Health yet", unmuted, and its days count from activation.
func TestHomeLeavesNoHealthYetUndated(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(9 * day)

	row := homeRowIn(t, findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "home-due"), silent)
	if !hasTestID(row, "home-due-checkin-overdue") {
		t.Fatalf("Goal 9 days past activation on a weekly cadence isn't Check-in overdue:\n%s", renderNode(row))
	}
	badge := findByTestID(t, row, "home-due-health")
	if flatFrameHasClass(badge, "as-of") || textOf(badge) != "No Health yet" {
		t.Errorf("Goal with no Check-in doesn't read No Health yet, unmuted: %s", renderNode(badge))
	}
	if got := textOf(findByTestID(t, row, "home-due-status")); got != "no check-in since activation 9 days ago · every 7 days Check-in overdue" {
		t.Errorf("Goal with no Check-in has status line %q, want its days counted from activation", got)
	}
}

// "Check-ins due" lists the most overdue Goal first: how far past its cadence
// it is, not how long since its last update, so a long cadence doesn't push a
// Goal ahead of one already further behind.
func TestHomeListsMostOverdueFirst(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	monthly := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	setCadence(t, h, monthly, 28)
	h.Clock.Advance(12 * day)
	weekly := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(12 * day)

	due := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "home-due")

	// weekly is 5 days past its 7-day cadence; monthly is 24 days into 28.
	first, second := linkTo(due, homeGoalPath(weekly)), linkTo(due, homeGoalPath(monthly))
	if first == nil || second == nil || !precedes(first, second) {
		t.Errorf("Check-ins due does not list %q, 5 days overdue, before %q, due in 4:\n%s", weekly.Title, monthly.Title, renderNode(due))
	}
}

// A Goal never checked in on has no previous Check-in to repeat, so its row
// offers only Check in, not No change.
func TestHomeOffersNoChangeOnlyWithAPreviousCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(10 * day)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	row := homeRowIn(t, findByTestID(t, page, "home-due"), silent)
	if buttonNamed(row, "No change") != nil || len(homeFormsEndingIn(row, "/no-change")) != 0 {
		t.Errorf("Goal with no Check-in offers No change:\n%s", renderNode(row))
	}
	if !hasTestID(row, "home-due-checkin") {
		t.Errorf("Goal with no Check-in offers no Check in link:\n%s", renderNode(row))
	}
}

// A Delegate checks in for the Owner, so a due Goal they're a Delegate on is
// listed once, under "Check-ins due", tagged with whose it is. The Your goals
// line links to the Delegate page with how many Goals they're a Delegate on,
// in place of a card listing them again; someone with none sees no such link.
func TestHomeListsDelegatedGoalsOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	other := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.AddDelegate(sam, dee, other.ID)
	h.Clock.Advance(10 * day)

	page := getPage(t, signInClient(t, ts.URL, "dee@example.com"), ts.URL+"/home")

	row := homeRowIn(t, findByTestID(t, page, "home-due"), g)
	if tag := findByTestID(t, row, "home-due-for"); textOf(tag) != "for sam" || !homeShowsPerson(tag, "sam@example.com", "sam") {
		t.Errorf("delegated Goal's row is not tagged with its Owner: %s", renderNode(tag))
	}
	if n := len(findAll(page, func(n *html.Node) bool { return n.Data == "a" && attrOf(n, "href") == homeGoalPath(g) })); n != 1 {
		t.Errorf("delegated Goal shows %d times on Home, want once", n)
	}
	if hasTestID(page, "home-delegated") {
		t.Errorf("Home still has a Delegated to you card")
	}
	link := findByTestID(t, page, "home-delegate-link")
	nums := findAll(link, func(n *html.Node) bool { return flatFrameHasClass(n, "num") })
	if attrOf(link, "href") != "/delegates" || len(nums) != 1 || textOf(nums[0]) != "2" || textOf(link) != "Goals delegated to you 2 →" {
		t.Errorf("Delegate link does not lead to /delegates counting dee's 2 Goals: %s", renderNode(link))
	}

	samPage := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	if hasTestID(samPage, "home-delegate-link") {
		t.Errorf("sam is a Delegate on nothing but sees the Delegate link")
	}
	if hasTestID(homeRowIn(t, findByTestID(t, samPage, "home-due"), g), "home-due-for") {
		t.Errorf("sam's own Goal is tagged as someone else's")
	}
}

// Home's Check in goes straight to the Goal's Check-in page, whether the
// viewer Owns the Goal or is a Delegate on it, and following it shows the
// Check-in form (#102).
func TestHomeCheckInOpensCheckinForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	h.Clock.Advance(10 * day)

	for _, who := range []string{"sam@example.com", "dee@example.com"} {
		client := signInClient(t, ts.URL, who)
		row := homeRowIn(t, findByTestID(t, getPage(t, client, ts.URL+"/home"), "home-due"), g)
		checkIn := findByTestID(t, row, "home-due-checkin")
		href := attrOf(checkIn, "href")
		if want := fmt.Sprintf("/goals/%d/checkin", g.ID); href != want || textOf(checkIn) != "Check in" {
			t.Errorf("%s: Home's Check in links to %q, want %q: %s", who, href, want, renderNode(checkIn))
			continue
		}
		if page := getPage(t, client, ts.URL+href); !hasTestID(page, "checkin-form") {
			t.Errorf("%s: following Check in shows no Check-in form:\n%s", who, renderNode(page))
		}
	}
}

// A link request names the contributing Goal's Owner in brackets, and keeps
// the "(" with their Name, so a narrow window can't leave it ending a line;
// their email still discloses beside the Name (#642). A Handoff waiting
// alongside shares the row's test id, so the row is the one badged Link
// request.
func TestHomeLinkRequestOwnerKeepsTheirOpenBracketOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignInNamed("kim@example.com", "Kim Park")
	handed := h.ActiveGoal(kim, "Cut churn", "Customers leave.")
	if _, err := h.Service.StartHandoffByEmail(context.Background(), handed.ID, "sam@example.com", kim.ID); err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	parent := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	child := h.ActiveGoal(kim, "Ship search", "People can't find things.")
	h.RequestLink(kim, child, parent, "")
	client := signInClient(t, ts.URL, "sam@example.com")

	var row *html.Node
	for _, r := range findAllByTestID(getPage(t, client, ts.URL+"/home"), "home-request") {
		badges := findAll(r, func(n *html.Node) bool { return flatFrameHasClass(n, "badge") })
		if len(badges) > 0 && textOf(badges[0]) == "Link request" {
			row = r
		}
	}
	if row == nil {
		t.Fatal("Home lists no row badged Link request")
	}
	what := findAll(row, func(n *html.Node) bool { return flatFrameHasClass(n, "muted") })
	if len(what) != 1 {
		t.Fatalf("the link request has no muted line naming the Owner:\n%s", renderNode(row))
	}
	if got, want := strings.Join(strings.Fields(runningText(what[0])), " "), "(Kim Park) wants to contribute to"; got != want {
		t.Errorf("the link request reads %q, want %q", got, want)
	}
	assertNameKeepsItsOpenBracket(t, row, "Kim Park")
	if !homeShowsPerson(row, "kim@example.com", "Kim Park") {
		t.Errorf("the link request doesn't show Kim Park as a Name that discloses their email:\n%s", renderNode(row))
	}
}

// A Handoff waiting on the viewer as the proposed new Owner and a link request
// waiting on them as the parent's Owner are listed under "Requests", oldest
// first whatever their kind. Each offers Accept as an outlined button and
// Reject as a text button that submits at once, and both do what they say.
func TestHomeListsRequestsOldestFirst(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	handed := h.ActiveGoal(kim, "Cut churn", "Customers leave.")
	handoff, err := h.Service.StartHandoffByEmail(context.Background(), handed.ID, "sam@example.com", kim.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	h.Clock.Advance(day)
	parent := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	child := h.ActiveGoal(kim, "Ship search", "People can't find things.")
	link := h.RequestLink(kim, child, parent, "")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/home")
	requests := findByTestID(t, page, "home-requests")
	if got := needsYouHeading(page); got != "Needs you 2" {
		t.Errorf("Needs you heading = %q, want %q for the two Requests", got, "Needs you 2")
	}

	if older, newer := linkTo(requests, homeGoalPath(handed)), linkTo(requests, homeGoalPath(child)); older == nil || newer == nil || !precedes(older, newer) {
		t.Errorf("Requests does not list the day-old Handoff before today's link request:\n%s", renderNode(requests))
	}
	for _, tc := range []struct {
		what   string
		row    *html.Node
		action string
	}{
		{"link request", homeRowIn(t, requests, child), fmt.Sprintf("/links/%d", link.ID)},
		{"Handoff", homeRowIn(t, requests, handed), fmt.Sprintf("/handoffs/%d", handoff.ID)},
	} {
		for _, b := range []struct {
			name, look string
			classes    []string
		}{
			{"Accept", "outlined", []string{"btn", "sm"}},
			{"Reject", "text", []string{"btn", "quiet", "sm"}},
		} {
			form := homeFormTo(tc.row, tc.action+"/"+strings.ToLower(b.name))
			var button *html.Node
			if form != nil {
				button = buttonNamed(form, b.name)
			}
			if button == nil || !slices.Contains(submitButtons(form), button) || !slices.Equal(homeClasses(button), b.classes) {
				t.Errorf("%s has no %s %s button:\n%s", tc.what, b.look, b.name, renderNode(tc.row))
			}
		}
		assertSubmitsAtOnce(t, tc.what+"'s Reject", renderNode(tc.row), tc.action+"/reject")
	}

	postFormStatus(t, client, ts.URL+fmt.Sprintf("/links/%d/accept", link.ID), nil)
	if parents := h.ParentsOf(child); len(parents) != 1 || parents[0].ID != parent.ID {
		t.Errorf("Home's Accept did not link %q under %q: parents = %v", child.Title, parent.Title, parents)
	}
	postFormStatus(t, client, ts.URL+fmt.Sprintf("/handoffs/%d/reject", handoff.ID), nil)
	if pending, _ := h.Service.PendingHandoffs(context.Background(), sam.ID); len(pending) != 0 {
		t.Errorf("Home's Reject left the Handoff pending")
	}
}

// A "Needs you" subgroup with nothing in it is left out, and with nothing at
// all the list shows a single caught-up line.
func TestHomeOmitsEmptySubgroups(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	pat := h.SignIn("pat@example.com")
	due := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(10 * day)
	parent := h.ActiveGoal(kim, "Grow revenue", "It pays for everything.")
	h.RequestLink(pat, h.ActiveGoal(pat, "Cut churn", "Customers leave."), parent, "")

	needsYou := func(who string) *html.Node {
		return findByTestID(t, getPage(t, signInClient(t, ts.URL, who), ts.URL+"/home"), "home-needs-you")
	}
	for _, tc := range []struct {
		who, has, lacks string
	}{
		{"sam@example.com", "home-due", "home-requests"},
		{"kim@example.com", "home-requests", "home-due"},
	} {
		list := needsYou(tc.who)
		if !hasTestID(list, tc.has) {
			t.Errorf("%s: Needs you lacks %s:\n%s", tc.who, tc.has, renderNode(list))
		}
		for _, gone := range []string{tc.lacks, "home-caught-up"} {
			if hasTestID(list, gone) {
				t.Errorf("%s: Needs you shows %s:\n%s", tc.who, gone, renderNode(list))
			}
		}
	}
	if !homeLinksTo(needsYou("sam@example.com"), due) {
		t.Errorf("sam's due Goal is missing from Needs you")
	}

	// pat's Goal was activated today, and nothing waits on them.
	list := needsYou("pat@example.com")
	if n := len(findAll(list, func(n *html.Node) bool { return n.Data == "li" })); n != 0 {
		t.Errorf("pat has nothing needing them but Needs you lists %d rows:\n%s", n, renderNode(list))
	}
	for _, gone := range []string{"Check-ins due", "Requests"} {
		if headingNamed(list, gone) != nil || strings.Contains(textOf(list), gone) {
			t.Errorf("pat's Needs you heads an empty %s subgroup:\n%s", gone, renderNode(list))
		}
	}
	if strings.Count(textOf(list), "You're all caught up") != 1 || !hasTestID(list, "home-caught-up") {
		t.Errorf("pat's Needs you does not show one caught-up line:\n%s", renderNode(list))
	}
}

// Accepting a Handoff of a Goal with Delegates means choosing which to keep, so
// Home offers Review, leading to the Pending handoffs page where that choice is
// made (TestHomeHandoffReviewListsDelegatesToKeep), in place of Accept. Reject
// still decides it from Home.
func TestHomeOffersReviewForHandoffWithDelegates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	dee := h.SignIn("dee@example.com")
	handed := h.ActiveGoal(kim, "Cut churn", "Customers leave.")
	h.AddDelegate(kim, dee, handed.ID)
	handoff, err := h.Service.StartHandoffByEmail(context.Background(), handed.ID, "sam@example.com", kim.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	row := homeRowIn(t, findByTestID(t, getPage(t, client, ts.URL+"/home"), "home-requests"), handed)

	if buttonNamed(row, "Accept") != nil || len(homeFormsEndingIn(row, "/accept")) != 0 {
		t.Errorf("Handoff with Delegates offers Accept on Home without the choice of whom to keep:\n%s", renderNode(row))
	}
	review := findByTestID(t, row, "home-request-review")
	if href := attrOf(review, "href"); href != "/handoffs" {
		t.Fatalf("Review links to %q, want /handoffs:\n%s", href, renderNode(row))
	}
	if got := textOf(review); got != "Review" {
		t.Errorf("Review link reads %q, want Review", got)
	}
	if form := homeFormTo(row, fmt.Sprintf("/handoffs/%d/reject", handoff.ID)); form == nil || buttonNamed(form, "Reject") == nil {
		t.Errorf("Handoff with Delegates offers no Reject:\n%s", renderNode(row))
	}
}

// The Your goals line counts the viewer's own Active Goals by Health, and the
// section after My org lists the Red and Yellow ones; Goals they're only a
// Delegate on aren't theirs to count. Needs you's heading says how many things
// need them, and the page header has a New goal link.
func TestHomeSummarizesYourGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	target := testsupport.Epoch.AddDate(0, 1, 0)
	green := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	yellow := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Cut scope.", target)
	red := h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Checkin(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	notMine := h.ActiveGoal(kim, "Hire", "We need people.")
	h.Checkin(kim, notMine.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	h.AddDelegate(kim, sam, notMine.ID)
	h.Clock.Advance(2 * day)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	for testID, want := range map[string]string{
		"home-count-green":  "1",
		"home-count-yellow": "1",
		"home-count-red":    "1",
	} {
		if got := textOf(findByTestID(t, page, testID)); got != want {
			t.Errorf("%s = %q, want %s", testID, got, want)
		}
	}
	yours := findByTestID(t, page, "home-not-green")
	for _, g := range []domain.Goal{yellow, red} {
		if !homeLinksTo(yours, g) {
			t.Errorf("Your goals does not list %q:\n%s", g.Title, renderNode(yours))
		}
	}
	for _, g := range []domain.Goal{green, notMine} {
		if homeLinksTo(yours, g) {
			t.Errorf("Your goals lists %q, which isn't one of sam's Red or Yellow Goals:\n%s", g.Title, renderNode(yours))
		}
	}
	line := findByTestID(t, page, "home-your-goals")
	if link := linkTo(line, "/goals?org=mine"); link == nil || textOf(link) != "Goals you own or help with →" {
		t.Errorf("Your goals does not link to the Goals sam owns or helps with:\n%s", renderNode(line))
	}
	if linkTo(line, "/goals") != nil {
		t.Errorf("Your goals still links to every Goal in the org:\n%s", renderNode(line))
	}

	// All four Goals were last checked in 2 days ago on a weekly cadence, so
	// each is due before next week's reminder.
	if got := needsYouHeading(page); got != "Needs you 4" {
		t.Errorf("Needs you heading = %q, want %q", got, "Needs you 4")
	}
	if strip := homePageHeaderIn(t, page); len(findAll(strip, func(n *html.Node) bool { return n.Data == "a" && textOf(n) == "New goal" })) == 0 {
		t.Errorf("page header has no New goal link:\n%s", renderNode(strip))
	}
}

// Home lists the viewer's Red and Yellow Goals under a "Your Yellow or Red
// goals" heading, Red first, so the list doesn't read as all their Goals. With
// only Green Goals there is no such section.
func TestHomeHeadsYellowOrRedGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	target := testsupport.Epoch.AddDate(0, 1, 0)
	yellow := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Cut scope.", target)
	red := h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Checkin(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	green := h.ActiveGoal(kim, "Ship search", "People can't find things.")
	h.Checkin(kim, green.ID, domain.HealthGreen, "On track.", "", time.Time{})

	yours := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "home-not-green")
	heading := headingNamed(yours, "Your Yellow or Red goals 2")
	first, second := linkTo(yours, homeGoalPath(red)), linkTo(yours, homeGoalPath(yellow))
	if heading == nil || heading.Data != "h2" || first == nil || second == nil || !precedes(heading, first) || !precedes(first, second) {
		t.Errorf("Your goals does not list Red then Yellow under a Yellow or Red heading:\n%s", renderNode(yours))
	}

	if kims := getPage(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/home"); hasTestID(kims, "home-not-green") || strings.Contains(textOf(kims), "Yellow or Red") {
		t.Errorf("kim has only Green Goals but sees a Yellow or Red list:\n%s", renderNode(kims))
	}
}

// In the Not Green list a Check-in overdue Goal's Health is muted and dated as
// last reported, as in Check-ins due; a Goal checked in on lately keeps its
// full-colour badge and no "as of".
func TestHomeDatesCheckinOverdueNotGreenHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	target := testsupport.Epoch.AddDate(0, 1, 0)
	checkinOverdue := h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Checkin(sam, checkinOverdue.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	h.Clock.Advance(9 * day)
	fresh := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.Checkin(sam, fresh.ID, domain.HealthYellow, "Slipping.", "Cut scope.", target)

	yours := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "home-not-green")

	row := homeRowIn(t, yours, checkinOverdue)
	if badge := findByTestID(t, row, "home-goal-health"); !flatFrameHasClass(badge, "as-of") {
		t.Errorf("Check-in overdue Red Goal's Health badge is not muted: %s", renderNode(badge))
	}
	if got := textOf(findByTestID(t, row, "home-goal-as-of")); got != "as of 9 days ago" {
		t.Errorf("Check-in overdue Red Goal's Health is dated %q, want as of 9 days ago", got)
	}

	row = homeRowIn(t, yours, fresh)
	if flatFrameHasClass(findByTestID(t, row, "home-goal-health"), "as-of") || hasTestID(row, "home-goal-as-of") {
		t.Errorf("Yellow Goal checked in today has its Health dated:\n%s", renderNode(row))
	}
}

// homeHealthBarUnits reads the Health distribution bar's units, in order, by
// the Health each draws ("g", "y" or "r").
func homeHealthBarUnits(bar *html.Node) []string {
	var units []string
	for _, unit := range findAll(bar, func(n *html.Node) bool { return n.Data == "span" }) {
		units = append(units, attrOf(unit, "class"))
	}
	return units
}

// The Your goals line draws the viewer's own Active Goals by Health as a segmented
// bar, one unit a Goal, Green then Yellow then Red, labelled with each count
// so it reads without colour. It counts Goals, not Metrics (ADR 0003), and
// leaves out Goals with no Health yet and those the viewer is only a Delegate
// on. Someone with no Goal to count sees no bar.
func TestHomeDrawsHealthDistributionBar(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	target := testsupport.Epoch.AddDate(0, 1, 0)
	for _, title := range []string{"Ship search", "Grow revenue"} {
		g := h.ActiveGoal(sam, title, "It matters.")
		h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	}
	red := h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Checkin(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	h.ActiveGoal(sam, "Hire", "We need people.")
	notMine := h.ActiveGoal(kim, "Open an office", "Closer to customers.")
	h.Checkin(kim, notMine.ID, domain.HealthYellow, "Slipping.", "Cut scope.", target)
	h.AddDelegate(kim, sam, notMine.ID)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	bar := findByTestID(t, page, "home-health-bar")
	if got := homeHealthBarUnits(bar); !slices.Equal(got, []string{"g", "g", "r"}) {
		t.Errorf("bar's units = %q, want two Green then one Red", got)
	}
	if label := attrOf(bar, "aria-label"); label != "2 Green, 0 Yellow, 1 Red" {
		t.Errorf("bar's label = %q, want its counts", label)
	}
	for testID, want := range map[string]string{
		"home-count-green":  "2",
		"home-count-yellow": "0",
		"home-count-red":    "1",
	} {
		if got := textOf(findByTestID(t, page, testID)); got != want {
			t.Errorf("%s = %q, want %s", testID, got, want)
		}
	}

	if kimPage := getPage(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/home"); !hasTestID(kimPage, "home-health-bar") {
		t.Errorf("kim's Yellow Goal draws no bar")
	}
	if pat := getPage(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/home"); hasTestID(pat, "home-health-bar") {
		t.Errorf("pat has no Goals but sees a distribution bar")
	}
}

// The Health distribution bar counts only Active Goals: a Proposed Goal, and a
// Done or Cancelled one whose last Health before closing was Yellow or Red,
// leave it alone.
func TestHomeHealthBarLeavesOutGoalsThatAreNotActive(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	target := testsupport.Epoch.AddDate(0, 1, 0)
	green := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.CreateGoal(sam, "Hire", "We need people.")
	done := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.Checkin(sam, done.ID, domain.HealthYellow, "Slipping.", "Cut scope.", target)
	closeGoal(t, h, sam, done, domain.LifecycleDone)
	cancelled := h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Checkin(sam, cancelled.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	closeGoal(t, h, sam, cancelled, domain.LifecycleCancelled)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	bar := findByTestID(t, page, "home-health-bar")
	if got := homeHealthBarUnits(bar); !slices.Equal(got, []string{"g"}) {
		t.Errorf("bar's units = %q, want only the one Green", got)
	}
	if label := attrOf(bar, "aria-label"); label != "1 Green, 0 Yellow, 0 Red" {
		t.Errorf("bar's label = %q, want only the Active Goal counted", label)
	}
}

// Every page's sidebar leads with Home, counting what needs the viewer — the
// Goals to check in on plus what's waiting on them — and showing no count when
// nothing does. Its count is a pill of its own, set apart from the sidebar's
// other counts, because it means something needs the person. Home replaces the
// pending-links, pending-handoffs, and delegated nav items, whose pages stay.
func TestNavCountsWhatNeedsYou(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.Checkin(sam, parent.ID, domain.HealthGreen, "On track.", "", time.Time{})
	child := h.ActiveGoal(kim, "Ship search", "People can't find things.")
	h.Checkin(kim, child.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.RequestLink(kim, child, parent, "")
	h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Clock.Advance(10 * day)
	h.Checkin(kim, child.ID, domain.HealthGreen, "On track.", "", time.Time{})

	samClient := signInClient(t, ts.URL, "sam@example.com")
	for _, path := range []string{"/goals", "/home", "/reports"} {
		page := getPage(t, samClient, ts.URL+path)
		nav := homeNavIn(t, page)
		home := findByTestID(t, page, "nav-home")
		if attrOf(home, "href") != "/home" || !slices.Equal(homeNavCounts(home), []string{"count needs-you: 3"}) {
			t.Errorf("on %s, Home does not count sam's 2 Goals to check in on and 1 link request in its needs-you pill: %s", path, renderNode(home))
		}
		if items := findAll(nav, func(n *html.Node) bool { return strings.HasPrefix(attrOf(n, "data-testid"), "nav-") }); len(items) == 0 || items[0] != home {
			t.Errorf("on %s, Home is not the first nav item:\n%s", path, renderNode(nav))
		}
		for _, gone := range []string{"nav-pending-links", "nav-pending-handoffs", "nav-delegated-goals"} {
			if hasTestID(nav, gone) {
				t.Errorf("on %s, the sidebar still has %s", path, gone)
			}
		}
	}

	// kim checked in today and nothing is waiting on them.
	kimPage := getPage(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/goals")
	if home := findByTestID(t, kimPage, "nav-home"); len(homeNavCounts(home)) != 0 {
		t.Errorf("Home shows a count with nothing needing kim: %s", renderNode(home))
	}
}

// Home's one-button forms — a due Goal's No change, a link request's Accept and
// Reject — each hold the one button that sends them. How they sit inline is
// TestHomeInlineFormsComeFromTheSharedClassStyles's.
func TestHomeInlineFormsComeFromTheSharedClass(t *testing.T) {
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

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	for action, label := range map[string]string{
		fmt.Sprintf("/goals/%d/checkins/no-change", checkinOverdue.ID): "No change",
		fmt.Sprintf("/links/%d/accept", link.ID):                       "Accept",
		fmt.Sprintf("/links/%d/reject", link.ID):                       "Reject",
	} {
		form := homeFormTo(page, action)
		if form == nil {
			t.Errorf("Home has no form posting to %s", action)
			continue
		}
		if buttons := submitButtons(form); len(buttons) != 1 || textOf(buttons[0]) != label {
			t.Errorf("the form posting to %s doesn't hold one %s button: %s", action, label, renderNode(form))
		}
	}
}

// Home offers a due Goal's No change only where it would be recorded: a Green
// Goal whose Milestone has gone overdue gets only its Check in, while a due
// Goal a No change would repeat keeps its No change (#235).
func TestHomeOffersNoChangeOnlyWhereItWouldBeRecorded(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	overdue := overdueGreenGoal(h, sam)
	repeatable := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, repeatable, 14)
	h.Checkin(sam, repeatable.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(16 * day)

	due := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "home-due")

	row := homeRowIn(t, due, overdue)
	if !hasTestID(row, "home-due-checkin") {
		t.Errorf("the overdue Goal's row has no Check in:\n%s", renderNode(row))
	}
	if buttonNamed(row, "No change") != nil || len(homeFormsEndingIn(row, "/checkins/no-change")) != 0 {
		t.Errorf("the overdue Goal's row offers a No change that would be refused:\n%s", renderNode(row))
	}
	row = homeRowIn(t, due, repeatable)
	if form := homeFormTo(row, fmt.Sprintf("/goals/%d/checkins/no-change", repeatable.ID)); form == nil || buttonNamed(form, "No change") == nil {
		t.Errorf("the repeatable Goal's row lacks its No change:\n%s", renderNode(row))
	}
	if !hasTestID(row, "home-due-checkin") {
		t.Errorf("the repeatable Goal's row lacks its Check in:\n%s", renderNode(row))
	}
}

// A No change on a Green Goal whose Milestone has gone overdue is refused, as
// one sent from Home's row before the Milestone went overdue would be; the
// person lands on that Goal's Check-in form with the reason, whether it posted
// plainly or through htmx, and nothing is recorded (#103).
func TestHomeNoChangeRefusedLandsOnCheckinForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := overdueGreenGoal(h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")
	action := fmt.Sprintf("/goals/%d/checkins/no-change", goal.ID)

	for _, hx := range []bool{false, true} {
		resp, page := postNoChange(t, client, ts.URL+action, hx)
		defer func() { _ = resp.Body.Close() }()
		want := http.StatusUnprocessableEntity
		if hx {
			want = http.StatusOK
		}
		if resp.StatusCode != want {
			t.Errorf("hx=%v: status = %d, want %d", hx, resp.StatusCode, want)
		}
		assertNoChangeRefusalOnForm(t, page, `Milestone &#34;Beta&#34; is overdue`)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("a refused No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// Home's New goal, in its page header, links to the New goal page, which opens
// with the Title focused, so it works without script.
func TestHomeNewGoalOpensProposeForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")

	strip := homePageHeaderIn(t, getPage(t, client, ts.URL+"/home"))
	links := findAll(strip, func(n *html.Node) bool { return n.Data == "a" && textOf(n) == "New goal" })
	if len(links) != 1 || attrOf(links[0], "href") != "/goals/new" {
		t.Fatalf("Home's New goal doesn't link to /goals/new:\n%s", renderNode(strip))
	}
	form := findByTestID(t, getPage(t, client, ts.URL+attrOf(links[0], "href")), "goal-form")
	if title := formField(t, form, "title"); !hasAttr(title.Node, "autofocus") {
		t.Errorf("following New goal doesn't focus the Title: %s", renderNode(title.Node))
	}
}

// A Parent suggestion on a Goal the person Owns is one of Home's Requests,
// counted in what needs them: who suggested which parent, with the note, and
// Decline and Accept, each returning to Home. Accepting requests the link.
func TestHomeListsParentSuggestionsToDecide(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	kim := h.SignIn("kim@example.com")
	goal := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	parent := h.CreateGoal(kim, "Reduce outages", "Outages cost trust.")
	s, err := h.Service.SuggestParent(context.Background(), pat.ID, goal.ID, parent.ID, "displays cause outages")
	if err != nil {
		t.Fatalf("SuggestParent: %v", err)
	}

	client := signInClient(t, ts.URL, "sam@example.com")
	page := getPage(t, client, ts.URL+"/home")
	if got := needsYouHeading(page); got != "Needs you 1" {
		t.Errorf("Needs you heading = %q, want %q for the suggestion", got, "Needs you 1")
	}
	row := homeRowIn(t, findByTestID(t, page, "home-requests"), goal)
	for _, want := range []string{"Pat Lee", "displays cause outages"} {
		if !strings.Contains(textOf(row), want) {
			t.Errorf("the row lacks %q: %q", want, textOf(row))
		}
	}
	if !homeLinksTo(row, parent) {
		t.Errorf("the row doesn't link to the suggested parent:\n%s", renderNode(row))
	}
	base := fmt.Sprintf("/parent-suggestions/%d", s.ID)
	for action, label := range map[string]string{"/decline": "Decline", "/accept": "Accept"} {
		form := homeFormTo(row, base+action)
		if form == nil || buttonNamed(form, label) == nil {
			t.Errorf("the row offers no %s:\n%s", label, renderNode(row))
			continue
		}
		if got := formSubmission(form).Get("return"); got != "/home" {
			t.Errorf("Home's %s returns to %q, want /home: %s", label, got, renderNode(form))
		}
	}

	resp := postForm(t, client, ts.URL+base+"/accept", url.Values{"return": {"/home"}})
	defer func() { _ = resp.Body.Close() }()
	landed := parsePage(t, readBody(t, resp))
	if resp.Request.URL.Path != "/home" {
		t.Errorf("accepting landed on %s, want /home", resp.Request.URL.Path)
	}
	if hasTestID(landed, "home-requests") {
		t.Errorf("Home still lists the accepted suggestion:\n%s", renderNode(findByTestID(t, landed, "home-requests")))
	}
	if pending, _ := h.Service.PendingLinkRequests(context.Background(), kim.ID); len(pending) != 1 || pending[0].Child.ID != goal.ID {
		t.Errorf("kim's requests = %+v, want the Goal's link", pending)
	}
}

// Every page's sidebar offers Goals, the app's main list, between Home and
// Follow-ups, with Reports after Follow-ups.
func TestNavOffersGoalsBetweenHomeAndFollowUps(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/home", "/goals", "/follow-ups", "/reports"} {
		nav := homeNavIn(t, getPage(t, client, ts.URL+path))
		if goals := findByTestID(t, nav, "nav-goals"); attrOf(goals, "href") != "/goals" {
			t.Errorf("on %s, the Goals nav item does not link to /goals: %s", path, renderNode(goals))
		}
		home, goals := findByTestID(t, nav, "nav-home"), findByTestID(t, nav, "nav-goals")
		followUps, reports := findByTestID(t, nav, "nav-follow-ups"), findByTestID(t, nav, "nav-reports")
		if !precedes(home, goals) || !precedes(goals, followUps) || !precedes(followUps, reports) {
			t.Errorf("on %s, the nav items do not read Home, Goals, Follow-ups, Reports:\n%s", path, renderNode(nav))
		}
	}
}

// The brand link leads to Home, the front door, on every page.
func TestBrandLinkLeadsHome(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/home", "/goals", "/reports"} {
		page := getPage(t, client, ts.URL+path)
		brand := findAll(page, func(n *html.Node) bool { return n.Data == "a" && textOf(n) == "Goal Tracker" })
		if len(brand) != 1 || attrOf(brand[0], "href") != "/home" {
			t.Errorf("on %s, the brand link does not lead to /home", path)
		}
	}
}

// The Goals nav item carries no count, even when the viewer owns Red and
// Check-in overdue Goals that Home and Follow-ups count.
func TestNavGoalsCarriesNoCount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	red := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.Checkin(sam, red.ID, domain.HealthRed, "Behind.", "Hire two reps.", h.Clock.Now().AddDate(0, 1, 0))
	h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Clock.Advance(10 * day)

	client := signInClient(t, ts.URL, "sam@example.com")
	for _, path := range []string{"/home", "/goals", "/follow-ups"} {
		page := getPage(t, client, ts.URL+path)
		if home := findByTestID(t, page, "nav-home"); len(homeNavCounts(home)) == 0 {
			t.Fatalf("on %s, Home counts nothing, so the fixture has nothing for Goals to count: %s", path, renderNode(home))
		}
		if goals := findByTestID(t, page, "nav-goals"); textOf(goals) != "Goals" || len(findAll(goals, func(n *html.Node) bool { return n.Data == "span" })) != 0 {
			t.Errorf("on %s, the Goals nav item carries a count: %s", path, renderNode(goals))
		}
	}
}

// An Admin's sidebar reads Home, Goals, Follow-ups, Reports, Admin.
func TestNavOrderForAnAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	h.SignIn("ada@example.com")
	ts := newServer(t, h)
	nav := homeNavIn(t, getPage(t, signInClient(t, ts.URL, "ada@example.com"), ts.URL+"/home"))

	var order []string
	for _, item := range findAll(nav, func(n *html.Node) bool { return strings.HasPrefix(attrOf(n, "data-testid"), "nav-") }) {
		order = append(order, attrOf(item, "data-testid"))
	}
	if got, want := strings.Join(order, " "), "nav-home nav-goals nav-follow-ups nav-reports nav-admin"; got != want {
		t.Errorf("an Admin's nav items read %q, want %q", got, want)
	}
}

// homeOrgSection is Home's My org section, failing the test when Home has none.
func homeOrgSection(t *testing.T, page *html.Node) *html.Node {
	t.Helper()
	return findByTestID(t, page, "home-org")
}

// Home shows a My org section only to someone with anyone under them by
// Manager: not when no directory has synced any Managers, not to someone with
// no one under them, and not to someone who manages only themselves. It sits
// in the main column, under Needs you.
func TestHomeShowsMyOrgOnlyToSomeoneWithPeopleUnderThem(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	lead := h.SignIn("lead@example.com")
	dev := h.SignIn("dev@example.com")
	self := h.SignIn("self@example.com")
	if page := getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"); hasTestID(page, "home-org") {
		t.Errorf("Home shows My org with no Managers synced")
	}
	h.SetManager(dev, lead)
	res := h.SyncDirectory(domain.DirectoryPerson{Email: self.Email, ManagerEmail: self.Email})
	if got, want := testsupport.CycleEmails(res.Cycles), [][]string{{"self@example.com"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("the sync's Cycles = %q, want %q", got, want)
	}
	for _, who := range []string{"dev@example.com", "self@example.com"} {
		if page := getPage(t, signInClient(t, ts.URL, who), ts.URL+"/home"); hasTestID(page, "home-org") {
			t.Errorf("Home shows My org to %s, who has no one under them", who)
		}
	}

	page := getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")
	card := homeOrgSection(t, page)
	if heading := headingNamed(card, "My org"); heading == nil || heading.Data != "h2" {
		t.Errorf("My org section isn't titled My org:\n%s", renderNode(card))
	}
	if !precedes(findByTestID(t, page, "home-needs-you"), card) {
		t.Errorf("My org isn't under Needs you")
	}
}

// homeOrgCountsIn reads the My org count badges under root, in order, as
// "kind: label", so an assertion sees both what each reads and what it is.
func homeOrgCountsIn(root *html.Node) []string {
	var out []string
	for _, badge := range findAllByTestID(root, "home-org-count") {
		out = append(out, attrOf(badge, "data-kind")+": "+textOf(badge))
	}
	return out
}

// homeOrgRowsIn are the My org section's rows, one per flagged person, in order.
func homeOrgRowsIn(card *html.Node) []*html.Node {
	return findAllByTestID(card, "home-org-person")
}

// homeOrgFixture is a lead with Ana and Ben directly under them and Cy under
// Ana. Cy's Goal is Red, Check-in overdue and past its Path to Green, Ana's is
// Yellow, and Ben's is Red.
type homeOrgFixture struct {
	lead, ana, ben, cy domain.Account
	goals              []domain.Goal
}

func newHomeOrgFixture(h *testsupport.Harness) homeOrgFixture {
	f := homeOrgFixture{
		lead: h.SignInNamed("lead@example.com", "Lee Lead"),
		ana:  h.SignInNamed("ana@example.com", "Ana"),
		ben:  h.SignInNamed("ben@example.com", "Ben"),
		cy:   h.SignInNamed("cy@example.com", "Cy"),
	}
	h.SetManager(f.ana, f.lead)
	h.SetManager(f.ben, f.lead)
	h.SetManager(f.cy, f.ana)
	troubled := h.ActiveGoal(f.cy, "Ship search", "People can't find things.")
	h.Checkin(f.cy, troubled.ID, domain.HealthRed, "Blocked.", "Escalate.", testsupport.Epoch.AddDate(0, 0, 3))
	h.Clock.Advance(10 * day)
	far := h.Clock.Now().AddDate(0, 1, 0)
	slipping := h.ActiveGoal(f.ana, "Cut costs", "Spend is up.")
	h.Checkin(f.ana, slipping.ID, domain.HealthYellow, "Slipping.", "Renegotiate.", far)
	blocked := h.ActiveGoal(f.ben, "Move offices", "The lease ends.")
	h.Checkin(f.ben, blocked.ID, domain.HealthRed, "No site.", "Widen the search.", far)
	f.goals = []domain.Goal{troubled, slipping, blocked}
	return f
}

// orgPersonLink is the link a My org row's person is shown by, to their Org
// on Goals, failing the test when the row has none.
func orgPersonLink(t *testing.T, row *html.Node) *html.Node {
	t.Helper()
	return findByTestID(t, row, "home-org-person-link")
}

// In My org each person's row is one link to their Org on Goals: their
// initials, their Name, then their counts, so no row repeats a "Their org on
// Goals" link, and the total line is plain text, set apart from the rows'
// pills and chips (#377, #391).
func TestHomeMyOrgNamesLinkToTheirOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeOrgFixture(h)

	card := homeOrgSection(t, getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"))
	rows := homeOrgRowsIn(card)
	if len(rows) != 2 {
		t.Fatalf("My org has %d rows, want Ana's and Ben's:\n%s", len(rows), renderNode(card))
	}
	for i, want := range []struct {
		who      domain.Account
		initials string
		reads    string
	}{
		{f.ana, "A", "A Ana 1 Red 1 Path to Green overdue 1 Check-in overdue 1 Yellow"},
		{f.ben, "B", "B Ben 1 Red"},
	} {
		link := orgPersonLink(t, rows[i])
		if href := attrOf(link, "href"); href != fmt.Sprintf("/goals?org=%d", want.who.ID) {
			t.Errorf("%s's row links to %q, want their org on Goals", want.who.Name, href)
		}
		if got := textOf(link); got != want.reads {
			t.Errorf("%s's row link reads %q, want %q, their initials, Name and counts", want.who.Name, got, want.reads)
		}
		lead := link.FirstChild
		for lead != nil && lead.Type != html.ElementNode {
			lead = lead.NextSibling
		}
		if lead == nil || textOf(lead) != want.initials || attrOf(lead, "aria-hidden") != "true" || !slices.Equal(homeClasses(lead), []string{"avatar", "hm-avatar"}) {
			t.Errorf("%s's row doesn't lead with their initials:\n%s", want.who.Name, renderNode(link))
		}
		for _, count := range findAllByTestID(rows[i], "home-org-count") {
			if !within(count, link) {
				t.Errorf("%s's counts sit outside their row's link:\n%s", want.who.Name, renderNode(rows[i]))
			}
		}
	}
	if strings.Contains(textOf(card), "Their org on Goals") {
		t.Errorf("My org still links each row to Their org on Goals:\n%s", renderNode(card))
	}

	total := findByTestID(t, card, "home-org-total")
	if got := textOf(total); got != "2 Red · 1 Path to Green overdue · 1 Check-in overdue · 1 Yellow" {
		t.Errorf("total line reads %q", got)
	}
	if badges := findAll(total, func(n *html.Node) bool { return flatFrameHasClass(n, "badge") }); len(badges) != 0 {
		t.Errorf("total line is chips, not text:\n%s", renderNode(total))
	}
}

// My org's total line counts, across everyone under the viewer, each signal
// that has anything, labelled so it reads without colour, in the order Red,
// Path to Green overdue, Check-in overdue, Yellow. Each person directly under
// the viewer with anything flagged gets a row with their whole Org's counts as
// badges, most urgent first, their name linking to Goals scoped to their Org;
// someone two levels down counts in the row of the person they're under. The
// section lists no Goals.
func TestHomeMyOrgCountsWhatIsInTroubleByPerson(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeOrgFixture(h)

	card := homeOrgSection(t, getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"))
	total := findByTestID(t, card, "home-org-total")
	wantTotal := []string{"red: 2 Red", "path-overdue: 1 Path to Green overdue", "checkin-overdue: 1 Check-in overdue", "yellow: 1 Yellow"}
	if got := homeOrgCountsIn(total); !slices.Equal(got, wantTotal) {
		t.Errorf("total line = %v, want %v", got, wantTotal)
	}

	rows := homeOrgRowsIn(card)
	if len(rows) != 2 {
		t.Fatalf("My org has %d rows, want Ana's and Ben's:\n%s", len(rows), renderNode(card))
	}
	var anaBadges []string
	for _, badge := range findAllByTestID(rows[0], "home-org-count") {
		anaBadges = append(anaBadges, attrOf(badge, "data-kind")+": "+attrOf(badge, "class"))
	}
	if want := []string{"red: badge r", "path-overdue: badge st", "checkin-overdue: badge st", "yellow: badge y"}; !slices.Equal(anaBadges, want) {
		t.Errorf("Ana's row's badges = %q, want %q", anaBadges, want)
	}
	for i, want := range []struct {
		who    domain.Account
		counts []string
	}{
		{f.ana, []string{"red: 1 Red", "path-overdue: 1 Path to Green overdue", "checkin-overdue: 1 Check-in overdue", "yellow: 1 Yellow"}},
		{f.ben, []string{"red: 1 Red"}},
	} {
		if !strings.Contains(textOf(orgPersonLink(t, rows[i])), want.who.Name) {
			t.Errorf("row %d isn't %s's:\n%s", i, want.who.Name, renderNode(rows[i]))
		}
		if got := homeOrgCountsIn(rows[i]); !slices.Equal(got, want.counts) {
			t.Errorf("%s's row counts %v, want %v", want.who.Name, got, want.counts)
		}
		if href := fmt.Sprintf("/goals?org=%d", want.who.ID); linkTo(rows[i], href) == nil {
			t.Errorf("%s's row doesn't link to %s:\n%s", want.who.Name, href, renderNode(rows[i]))
		}
	}
	if strings.Contains(textOf(card), "Cy") {
		t.Errorf("Cy, two levels down, appears on the section:\n%s", renderNode(card))
	}
	for _, href := range []string{fmt.Sprintf("/goals?org=%d", f.lead.ID), fmt.Sprintf("/follow-ups?org=%d", f.lead.ID)} {
		if linkTo(card, href) == nil {
			t.Errorf("My org doesn't link on to %s:\n%s", href, renderNode(card))
		}
	}
	for _, g := range f.goals {
		if strings.Contains(textOf(card), g.Title) || homeLinksTo(card, g) {
			t.Errorf("My org lists the Goal %q:\n%s", g.Title, renderNode(card))
		}
	}
}

// People directly under the viewer with nothing flagged get no row of their
// own, only one line counting them after the rows; it's left out when there
// are none.
func TestHomeMyOrgCountsThoseAllClearInOneLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		clear int
		want  string
	}{
		{0, ""},
		{1, "1 other all clear"},
		{2, "2 others all clear"},
	} {
		t.Run(strconv.Itoa(tc.clear), func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			ts := newServer(t, h)
			f := newHomeOrgFixture(h)
			for i := range tc.clear {
				fine := h.SignIn(fmt.Sprintf("fine%d@example.com", i))
				h.SetManager(fine, f.lead)
				g := h.ActiveGoal(fine, "Run payroll", "People get paid.")
				h.Checkin(fine, g.ID, domain.HealthGreen, "Fine.", "", time.Time{})
			}

			card := homeOrgSection(t, getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"))
			rows := homeOrgRowsIn(card)
			if len(rows) != 2 {
				t.Fatalf("My org has %d rows, want only the 2 flagged people's", len(rows))
			}
			if hasTestID(card, "home-org-all-clear") {
				t.Errorf("My org says everyone is all clear while some are flagged:\n%s", renderNode(card))
			}
			if tc.want == "" {
				if hasTestID(card, "home-org-clear") {
					t.Errorf("My org counts others all clear when there are none:\n%s", renderNode(card))
				}
				return
			}
			clear := findByTestID(t, card, "home-org-clear")
			if textOf(clear) != tc.want || !flatFrameHasClass(clear, "muted") {
				t.Errorf("all-clear line = %s, want a muted %q", renderNode(clear), tc.want)
			}
			if !precedes(rows[len(rows)-1], clear) {
				t.Errorf("the all-clear line comes before the rows:\n%s", renderNode(card))
			}
		})
	}
}

// With nothing flagged anywhere under the viewer, My org says so in one line,
// with no total and no rows, and still links on to the Org's Goals and Follow-ups.
func TestHomeMyOrgSaysWhenEveryoneIsAllClear(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	lead := h.SignIn("lead@example.com")
	for _, who := range []string{"ana@example.com", "ben@example.com"} {
		p := h.SignIn(who)
		h.SetManager(p, lead)
		g := h.ActiveGoal(p, "Run payroll", "People get paid.")
		h.Checkin(p, g.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	}
	own := h.ActiveGoal(lead, "Set strategy", "No direction.")
	h.Checkin(lead, own.ID, domain.HealthRed, "Stuck.", "Offsite.", h.Clock.Now().AddDate(0, 1, 0))

	card := homeOrgSection(t, getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"))
	allClear := findByTestID(t, card, "home-org-all-clear")
	if textOf(allClear) != "Everyone in My org is all clear." || !flatFrameHasClass(allClear, "muted") {
		t.Errorf("all-clear line = %s, want a muted %q", renderNode(allClear), "Everyone in My org is all clear.")
	}
	for _, gone := range []string{"home-org-total", "home-org-person", "home-org-clear", "home-org-count"} {
		if hasTestID(card, gone) {
			t.Errorf("My org with nothing flagged shows %s:\n%s", gone, renderNode(card))
		}
	}
	for _, c := range []struct{ testID, href, label string }{
		{"home-org-goals-link", fmt.Sprintf("/goals?org=%d", lead.ID), "My org on Goals →"},
		{"home-org-follow-ups-link", fmt.Sprintf("/follow-ups?org=%d", lead.ID), "My org on Follow-ups →"},
	} {
		if link := findByTestID(t, card, c.testID); attrOf(link, "href") != c.href || textOf(link) != c.label {
			t.Errorf("%s = %s, want %q linking to %s", c.testID, renderNode(link), c.label, c.href)
		}
	}
}

// A Departed person directly under the viewer keeps their row, marked
// departed, while their Ownerless Goal stays flagged where they were.
func TestHomeMyOrgMarksADepartedPerson(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ts := newServer(t, h)
	admin := h.SignIn("admin@example.com")
	f := newHomeOrgFixture(h)
	if err := h.Service.MarkDeparted(context.Background(), admin.ID, f.ben.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	rows := homeOrgRowsIn(homeOrgSection(t, getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")))
	if len(rows) != 2 {
		t.Fatalf("My org has %d rows, want Ana's and Ben's", len(rows))
	}
	if !strings.Contains(textOf(orgPersonLink(t, rows[1])), f.ben.Name) || !hasTestID(rows[1], "home-org-departed") {
		t.Errorf("Ben's row isn't marked departed:\n%s", renderNode(rows[1]))
	}
	if hasTestID(rows[0], "home-org-departed") {
		t.Errorf("Ana's row is marked departed:\n%s", renderNode(rows[0]))
	}
}

// My org adds nothing to what needs the viewer: with no Check-ins due and no
// requests, they're all caught up however much is Red under them, and the top
// bar's Home item carries no count.
func TestHomeMyOrgAddsNothingToNeedsYou(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	newHomeOrgFixture(h)

	page := getPage(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")
	homeOrgSection(t, page)
	if caughtUp := findAllByTestID(page, "home-caught-up"); len(caughtUp) != 1 || textOf(caughtUp[0]) != "You're all caught up." {
		t.Errorf("Needs you isn't caught up with only the Org's Goals Red")
	}
	if got := needsYouHeading(page); got != "Needs you" {
		t.Errorf("Needs you heading = %q, want %q", got, "Needs you")
	}
	if home := findByTestID(t, page, "nav-home"); len(homeNavCounts(home)) != 0 {
		t.Errorf("Home's nav item counts the Org's Goals: %s", renderNode(home))
	}
}

// Home whose due Goal can't be asked whether a No change would be recorded
// answers 500, as any failure to load Home does, rather than offering a No
// change it couldn't check (#235).
func TestHomeFailsWhenNoChangeCantBeChecked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(8 * day)
	client := signInClient(t, ts.URL, "sam@example.com")
	h.FailReads("milestones")

	resp, err := ctxGet(t, client, ts.URL+"/home")
	if err != nil {
		t.Fatalf("GET /home: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// homeActionItemFixture is a Report, MBR, on a Goal its author Owns, published
// so Action Items can be raised on it for sam, who has nothing else waiting.
type homeActionItemFixture struct {
	h      *testsupport.Harness
	author domain.Account
	sam    domain.Account
	goal   domain.Goal
	def    domain.ReportDefinition
	pub    domain.Publication
}

func newHomeActionItemFixture(h *testsupport.Harness) homeActionItemFixture {
	f := homeActionItemFixture{h: h, author: h.SignIn("author@example.com"), sam: h.SignIn("sam@example.com")}
	f.goal = h.ActiveGoal(f.author, "Launch in EU", "Expand the market.")
	f.def = h.SaveReportDefinition(f.author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{f.goal.ID}})
	f.pub = h.PublishReport(f.author, f.def)
	return f
}

// raise raises an Action Item on the fixture's publication for owner, due in
// dueIn days (negative for overdue), about goalID, or the whole Report for 0.
func (f homeActionItemFixture) raise(owner domain.Account, text string, dueIn int, goalID int64) domain.ActionItem {
	f.h.T.Helper()
	item, err := f.h.Service.RaiseActionItem(context.Background(), f.author.ID, domain.RaiseActionItemInput{
		PublicationID: f.pub.ID, GoalID: goalID, Text: text, OwnerID: owner.ID, DueDate: f.h.Clock.Now().AddDate(0, 0, dueIn),
	})
	if err != nil {
		f.h.T.Fatalf("RaiseActionItem: %v", err)
	}
	return item
}

// homeActionItemRowsIn returns each row of Home's Action Items, in order, or
// none when Home shows no such group.
func homeActionItemRowsIn(page *html.Node) []*html.Node {
	lists := findAllByTestID(page, "home-action-items")
	if len(lists) == 0 {
		return nil
	}
	return findAllByTestID(lists[0], "home-action-item")
}

// Home's Needs you lists the reader's open Action Items under their own
// heading, soonest due first so an overdue one leads, flagged Overdue. Each
// row names its Report and when it's due, and one about a Goal links to the
// Goal page's Action Items and names the Goal. How a row looks is
// TestHomeListsYourOpenActionItemsStyles's.
func TestHomeListsYourOpenActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeActionItemFixture(h)
	f.raise(f.sam, "Send the vendor plan.", 7, 0)
	f.raise(f.sam, "Explain the slip.", -2, f.goal.ID)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	needs := findByTestID(t, page, "home-needs-you")
	if heading := headingNamed(needs, "Action Items"); heading == nil || heading.Data != "h3" {
		t.Errorf("Needs you has no Action Items heading:\n%s", renderNode(needs))
	}
	rows := homeActionItemRowsIn(page)
	if len(rows) != 2 {
		t.Fatalf("Home lists %d Action Items, want 2:\n%s", len(rows), renderNode(needs))
	}
	overdue, later := rows[0], rows[1]
	if !strings.Contains(textOf(overdue), "Explain the slip.") {
		t.Errorf("the overdue Action Item is not listed first:\n%s", renderNode(needs))
	}
	chips := findAll(overdue, func(n *html.Node) bool { return n.Data == "span" && flatFrameHasClass(n, "badge") })
	var chipsRead []string
	for _, chip := range chips {
		chipsRead = append(chipsRead, attrOf(chip, "class")+": "+textOf(chip))
	}
	if want := []string{"badge lc: Action Item", "badge r: Overdue"}; !slices.Equal(chipsRead, want) {
		t.Errorf("overdue Action Item's chips = %q, want %q", chipsRead, want)
	}
	if !hasTestID(overdue, "home-action-item-overdue") {
		t.Errorf("overdue Action Item isn't flagged Overdue:\n%s", renderNode(overdue))
	}
	if link := linkTo(overdue, fmt.Sprintf("/goals/%d#action-items", f.goal.ID)); link == nil || textOf(link) != "Explain the slip." {
		t.Errorf("overdue Action Item's row doesn't lead to its Goal's Action Items:\n%s", renderNode(overdue))
	}
	for _, want := range []string{"Launch in EU", "due " + h.Clock.Now().AddDate(0, 0, -2).Format("Jan 2, 2006")} {
		if !strings.Contains(textOf(overdue), want) {
			t.Errorf("overdue Action Item's row lacks %q: %q", want, textOf(overdue))
		}
	}
	if !hasTestID(overdue, "close-action-item") {
		t.Errorf("overdue Action Item's row offers no Close:\n%s", renderNode(overdue))
	}
	if hasTestID(later, "home-action-item-overdue") {
		t.Errorf("an Action Item due next week is flagged Overdue:\n%s", renderNode(later))
	}
	for _, row := range rows {
		if !strings.Contains(textOf(row), "MBR") {
			t.Errorf("Action Item's row does not name its Report: %q", textOf(row))
		}
	}
}

// An Action Item about the whole Report has no Goal page to lead to, so its
// row on Home links to the Action Items on the publication it was raised on.
func TestHomeActionItemAboutTheReportLinksToItsPublication(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeActionItemFixture(h)
	f.raise(f.sam, "Send the vendor plan.", 7, 0)

	rows := homeActionItemRowsIn(getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"))
	if len(rows) != 1 {
		t.Fatalf("Home lists %d Action Items, want 1", len(rows))
	}
	if want := fmt.Sprintf("/reports/%d/publications/%d#action-items", f.def.ID, f.pub.ID); linkTo(rows[0], want) == nil {
		t.Errorf("whole-Report Action Item's row doesn't link to %s:\n%s", want, renderNode(rows[0]))
	}
	if homeLinksTo(rows[0], f.goal) {
		t.Errorf("whole-Report Action Item's row links to a Goal:\n%s", renderNode(rows[0]))
	}
}

// Open Action Items count toward what needs the reader, in the summary under
// the page title and the sidebar's Home count, and someone whose only open
// work is one isn't told they're caught up.
func TestHomeCountsOpenActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeActionItemFixture(h)
	f.raise(f.sam, "Send the vendor plan.", 7, 0)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	if got := needsYouHeading(page); got != "Needs you 1" {
		t.Errorf("Needs you heading = %q, want %q for the Action Item", got, "Needs you 1")
	}
	if home := findByTestID(t, page, "nav-home"); !slices.Equal(homeNavCounts(home), []string{"count needs-you: 1"}) {
		t.Errorf("sidebar's Home does not count the Action Item: %s", renderNode(home))
	}
	if hasTestID(page, "home-caught-up") {
		t.Errorf("Home says sam is caught up with an Action Item open")
	}
}

// The owner closes an Action Item from its row on Home with a note, and comes
// back to Home, where it's gone. How the close form is laid out is
// TestHomeClosesAnActionItemStyles's.
func TestHomeClosesAnActionItem(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeActionItemFixture(h)
	item := f.raise(f.sam, "Send the vendor plan.", 7, 0)
	client := signInClient(t, ts.URL, "sam@example.com")

	rows := homeActionItemRowsIn(getPage(t, client, ts.URL+"/home"))
	if len(rows) != 1 {
		t.Fatalf("Home lists %d Action Items, want 1", len(rows))
	}
	form := findByTestID(t, rows[0], "close-action-item")
	action := attrOf(form, "action")
	if want := fmt.Sprintf("/action-items/%d/close", item.ID); action != want {
		t.Fatalf("row's close form posts to %q, want %q", action, want)
	}
	if back := formField(t, form, "return"); back.Type != "hidden" || back.Value != "/home" {
		t.Errorf("row's close form does not return to Home: %s", renderNode(form))
	}

	resp := postForm(t, noRedirects(client), ts.URL+action, url.Values{"note": {"Sent."}, "return": {"/home"}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if got := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || got != "/home" {
		t.Errorf("closing from Home: %d to %q, want 303 to /home", resp.StatusCode, got)
	}
	if rows := homeActionItemRowsIn(getPage(t, client, ts.URL+"/home")); len(rows) != 0 {
		t.Errorf("Home still lists the closed Action Item: %q", textsOf(rows))
	}
}

// Home lists only the reader's open Action Items: not one they've closed, nor
// one someone else owns.
func TestHomeLeavesOutClosedAndOthersActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeActionItemFixture(h)
	kim := h.SignIn("kim@example.com")
	closed := f.raise(f.sam, "Send the vendor plan.", 7, 0)
	if _, err := h.Service.CloseActionItem(context.Background(), f.sam.ID, closed.ID, "Sent."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	f.raise(kim, "Explain the slip.", -2, f.goal.ID)
	f.raise(f.sam, "Book the review.", 3, 0)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	rows := homeActionItemRowsIn(page)
	if len(rows) != 1 || !strings.Contains(textOf(rows[0]), "Book the review.") {
		t.Errorf("Home lists %d Action Items, want only sam's open one: %q", len(rows), textsOf(rows))
	}
	for _, gone := range []string{"Send the vendor plan.", "Explain the slip."} {
		if strings.Contains(textOf(page), gone) {
			t.Errorf("Home shows %q, which isn't sam's to close", gone)
		}
	}
}

// An open Ask for help is listed on its addressee's Home under Needs you, in
// an "Asks for help" subgroup after Check-ins due: Red first, each row leading
// to the Goal's Comments to answer and naming the Owner who asked. It counts in
// the summary and the sidebar. It isn't listed for the Owner who asked, nor for
// a Manager further up while the first one hasn't left, and an answer doesn't
// clear it: only the Owner's next Check-in that unticks the box, or is Green,
// does.
func TestHomeListsAsksForHelpAddressedToTheViewer(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	mia := h.SignInNamed("mia@example.com", "Mia Chen")
	vp := h.SignIn("vp@example.com")
	h.SetManager(sam, mia)
	h.SetManager(mia, vp)
	yellow := h.ActiveGoal(sam, "Alpha yellow", "Why it matters.")
	h.AskForHelp(sam, yellow.ID, domain.HealthYellow, "Short a reviewer.", "Borrow one.", pathDate)
	red := h.ActiveGoal(sam, "Zeta red", "Why it matters.")
	h.AskForHelp(sam, red.ID, domain.HealthRed, "Blocked.", "Need budget.", pathDate)
	miaClient := signInClient(t, ts.URL, "mia@example.com")

	page := getPage(t, miaClient, ts.URL+"/home")
	needsYou := findByTestID(t, page, "home-needs-you")
	if heading := headingNamed(needsYou, "Asks for help"); heading == nil || heading.Data != "h3" {
		t.Errorf("Needs you has no Asks for help subgroup:\n%s", renderNode(needsYou))
	}
	asks := findByTestID(t, page, "home-asks")
	rows := findAllByTestID(asks, "home-ask")
	if len(rows) != 2 {
		t.Fatalf("Asks for help lists %d rows, want 2:\n%s", len(rows), renderNode(asks))
	}
	for i, g := range []struct {
		goal   domain.Goal
		health string
	}{{red, domain.HealthRed}, {yellow, domain.HealthYellow}} {
		row := rows[i]
		if link := linkTo(row, fmt.Sprintf("/goals/%d#comments", g.goal.ID)); link == nil || textOf(link) != g.goal.Title {
			t.Errorf("row %d doesn't link %q to its Comments:\n%s", i, g.goal.Title, renderNode(row))
		}
		assertHealthChip(t, healthChipIn(t, row), g.health)
		if !strings.Contains(shownTextOf(row), "from Sam Owner") || !homeShowsPerson(row, sam.Email, "Sam Owner") {
			t.Errorf("row %d doesn't name the Owner who asked:\n%s", i, renderNode(row))
		}
	}
	if got := needsYouHeading(page); got != "Needs you 2" {
		t.Errorf("Needs you heading = %q, want %q for the two asks", got, "Needs you 2")
	}
	if home := findByTestID(t, getPage(t, miaClient, ts.URL+"/goals"), "nav-home"); !slices.Equal(homeNavCounts(home), []string{"count needs-you: 2"}) {
		t.Errorf("sidebar's Home = %s, want a count of 2", renderNode(home))
	}

	for _, who := range []string{"sam@example.com", "vp@example.com"} {
		if page := getPage(t, signInClient(t, ts.URL, who), ts.URL+"/home"); hasTestID(page, "home-asks") || hasTestID(page, "home-ask") {
			t.Errorf("%s's Home lists an ask addressed to Mia", who)
		}
	}

	if _, err := h.Service.AddGoalComment(context.Background(), mia.ID, red.ID, "Take Lee for two weeks."); err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	if got := len(findAllByTestID(getPage(t, miaClient, ts.URL+"/home"), "home-ask")); got != 2 {
		t.Errorf("after Mia's answer Home lists %d asks, want both still open", got)
	}

	h.Checkin(sam, red.ID, domain.HealthRed, "Still blocked.", "Need budget.", pathDate)
	h.Checkin(sam, yellow.ID, domain.HealthGreen, "Fixed.", "", time.Time{})
	if page := getPage(t, miaClient, ts.URL+"/home"); hasTestID(page, "home-asks") || hasTestID(page, "home-ask") || strings.Contains(textOf(page), "Asks for help") {
		t.Errorf("Home still lists asks the Owner closed:\n%s", renderNode(findByTestID(t, page, "home-needs-you")))
	}
}
