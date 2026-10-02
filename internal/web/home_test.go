package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const day = 24 * time.Hour

// setCadence sets how often a Check-in is expected on g, failing the test on
// error.
func setCadence(t *testing.T, h *testsupport.Harness, g domain.Goal, days int) {
	t.Helper()
	if _, err := h.Service.SetCadence(context.Background(), g.ID, days); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
}

// homeRow returns the row of a Home section that links to g, failing the test
// when the section has none.
func homeRow(t *testing.T, section string, g domain.Goal) string {
	t.Helper()
	at := strings.Index(section, navTo(g.ID))
	if at < 0 {
		t.Fatalf("section has no row for %q:\n%s", g.Title, section)
	}
	start := strings.LastIndex(section[:at], "<li")
	end := strings.Index(section[at:], "</li>")
	return section[start : at+end]
}

// A Stale Goal the viewer Owns is listed under "Check in on these", saying how
// long since its last Check-in against its cadence, flagged Stale, with a
// one-click No change and a Check in link. Fresh Goals and other people's
// Stale Goals aren't listed.
func TestHomeListsStaleGoalToCheckInOn(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	stale := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, stale, 14)
	h.Checkin(sam, stale.ID, domain.HealthYellow, "Slipping.", "Cut scope.", testsupport.Epoch.AddDate(0, 1, 0))
	others := h.ActiveGoal(kim, "Grow revenue", "It pays for everything.")
	h.Clock.Advance(16 * day)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	due := pageElement(t, page, "ul", "home-due")
	row := homeRow(t, due, stale)
	for _, want := range []string{
		"Last check-in 16 days ago on a 14-day cadence",
		`data-testid="home-due-stale"`,
		`data-testid="home-due-health"`,
		fmt.Sprintf(`action="/goals/%d/checkins/no-change"`, stale.ID),
		`data-testid="home-due-checkin"`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("Stale Goal's row lacks %s:\n%s", want, row)
		}
	}
	for _, g := range []domain.Goal{fresh, others} {
		if strings.Contains(due, navTo(g.ID)) {
			t.Errorf("Check in on these lists %q, which isn't due for sam:\n%s", g.Title, due)
		}
	}
}

// A Goal that would go Stale before next week's reminder is listed too, by the
// same rule as the reminder email, but not flagged Stale. A Goal with a week
// to spare isn't listed, and a Goal with no Check-in to repeat offers no No
// change.
func TestHomeListsGoalDueBeforeNextReminder(t *testing.T) {
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	due := pageElement(t, page, "ul", "home-due")
	row := homeRow(t, due, dueSoon)
	if !strings.Contains(row, "Last check-in 8 days ago on a 14-day cadence") {
		t.Errorf("due-soon Goal's row does not say why it's listed:\n%s", row)
	}
	if strings.Contains(row, `data-testid="home-due-stale"`) {
		t.Errorf("due-soon Goal is flagged Stale before it is:\n%s", row)
	}
	if strings.Contains(due, navTo(spare.ID)) {
		t.Errorf("Check in on these lists %q, which has a week to spare:\n%s", spare.Title, due)
	}
}

// A Goal never checked in on has no previous Check-in to repeat, so its row
// offers only Check in, not No change.
func TestHomeOffersNoChangeOnlyWithAPreviousCheckin(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	row := homeRow(t, pageElement(t, page, "ul", "home-due"), silent)
	if strings.Contains(row, "no-change") {
		t.Errorf("Goal with no Check-in offers No change:\n%s", row)
	}
	if !strings.Contains(row, `data-testid="home-due-checkin"`) {
		t.Errorf("Goal with no Check-in offers no Check in link:\n%s", row)
	}
}

// A Delegate checks in for the Owner, so a due Goal they're a Delegate on is
// listed once, under "Check-ins due", tagged with whose it is. The sidebar
// links to the Delegate page with how many Goals they're a Delegate on, in
// place of a card listing them again; someone with none sees no such link.
func TestHomeListsDelegatedGoalsOnce(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	other := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.AddDelegate(sam, dee, other.ID)
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "dee@example.com"), ts.URL+"/home")

	row := homeRow(t, pageElement(t, page, "ul", "home-due"), g)
	if !strings.Contains(row, `data-testid="home-due-for" class="tag">for `+shownAs("sam@example.com", "sam")) {
		t.Errorf("delegated Goal's row is not tagged with its Owner:\n%s", row)
	}
	if n := strings.Count(page, navTo(g.ID)); n != 1 {
		t.Errorf("delegated Goal shows %d times on Home, want once", n)
	}
	if strings.Contains(page, `data-testid="home-delegated"`) {
		t.Errorf("Home still has a Delegated to you card")
	}
	link := pageElement(t, page, "a", "home-delegate-link")
	if !strings.Contains(link, `href="/delegates"`) || !strings.Contains(link, `<span class="num">2</span>`) {
		t.Errorf("Delegate link does not lead to /delegates counting dee's 2 Goals: %s", link)
	}

	samPage := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	if strings.Contains(samPage, `data-testid="home-delegate-link"`) {
		t.Errorf("sam is a Delegate on nothing but sees the Delegate link")
	}
	if strings.Contains(homeRow(t, pageElement(t, samPage, "ul", "home-due"), g), `data-testid="home-due-for"`) {
		t.Errorf("sam's own Goal is tagged as someone else's")
	}
}

// Home's Check in goes straight to the Goal's Check-in page, whether the
// viewer Owns the Goal or is a Delegate on it, and following it shows the
// Check-in form (#102).
func TestHomeCheckInOpensCheckinForm(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	h.Clock.Advance(10 * day)

	for _, who := range []string{"sam@example.com", "dee@example.com"} {
		client := signInClient(t, ts.URL, who)
		row := homeRow(t, pageElement(t, getBody(t, client, ts.URL+"/home"), "ul", "home-due"), g)
		href := attr(tagAround(t, row, `data-testid="home-due-checkin"`), "href")
		if want := fmt.Sprintf("/goals/%d/checkin", g.ID); href != want {
			t.Errorf("%s: Home's Check in links to %q, want %q:\n%s", who, href, want, row)
			continue
		}
		if page := getBody(t, client, ts.URL+href); !strings.Contains(page, `data-testid="checkin-form"`) {
			t.Errorf("%s: following Check in shows no Check-in form:\n%s", who, page)
		}
	}
}

// A link request waiting on the viewer as the parent's Owner, and a Handoff
// waiting on them as the proposed new Owner, are listed under "Waiting on
// you", each with Accept and a Reject that submits at once. Someone with
// nothing waiting and nothing due sees each section's empty state.
func TestHomeListsWhatIsWaitingOnYou(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	child := h.ActiveGoal(kim, "Ship search", "People can't find things.")
	link := h.RequestLink(kim, child, parent, "")
	handed := h.ActiveGoal(kim, "Cut churn", "Customers leave.")
	handoff, err := h.Service.StartHandoffByEmail(context.Background(), handed.ID, "sam@example.com", kim.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	waiting := pageElement(t, page, "ul", "home-waiting")
	for _, tc := range []struct {
		what   string
		row    string
		action string
	}{
		{"link request", homeRow(t, waiting, child), fmt.Sprintf("/links/%d", link.ID)},
		{"Handoff", homeRow(t, waiting, handed), fmt.Sprintf("/handoffs/%d", handoff.ID)},
	} {
		if !strings.Contains(tc.row, `action="`+tc.action+`/accept"`) {
			t.Errorf("%s has no Accept:\n%s", tc.what, tc.row)
		}
		assertSubmitsAtOnce(t, tc.what+"'s Reject", tagAround(t, tc.row, `action="`+tc.action+`/reject"`))
	}

	kimPage := getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/home")
	if !strings.Contains(kimPage, `data-testid="home-waiting-empty"`) {
		t.Errorf("kim has nothing waiting but sees no empty state:\n%s", kimPage)
	}
	// kim's Goals were activated today, so none is due yet.
	if empty := pageElement(t, kimPage, "p", "home-due-empty"); !strings.Contains(empty, "You're all caught up.") {
		t.Errorf("kim has nothing to check in on but sees %s", empty)
	}
}

// The sidebar counts the viewer's own Active Goals by Health and lists the Red
// and Yellow ones; Goals they're only a Delegate on aren't theirs to count.
// The heading says how many things need them, beside a New goal link.
func TestHomeSummarizesYourGoals(t *testing.T) {
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	for testID, want := range map[string]string{
		"home-count-green":  "1",
		"home-count-yellow": "1",
		"home-count-red":    "1",
	} {
		if got := pageElement(t, page, "span", testID); !strings.HasSuffix(got, ">"+want) {
			t.Errorf("%s = %s, want %s", testID, got, want)
		}
	}
	yours := pageElement(t, page, "section", "home-your-goals")
	for _, g := range []domain.Goal{yellow, red} {
		if !strings.Contains(yours, navTo(g.ID)) {
			t.Errorf("Your goals does not list %q:\n%s", g.Title, yours)
		}
	}
	for _, g := range []domain.Goal{green, notMine} {
		if strings.Contains(yours, navTo(g.ID)) {
			t.Errorf("Your goals lists %q, which isn't one of sam's Red or Yellow Goals:\n%s", g.Title, yours)
		}
	}
	if !strings.Contains(yours, `href="/goals"`) {
		t.Errorf("Your goals does not link to all of sam's Goals:\n%s", yours)
	}

	// All four Goals were last checked in 2 days ago on a weekly cadence, so
	// each is due before next week's reminder.
	if got := pageElement(t, page, "p", "home-summary"); !strings.Contains(got, "4 things need you") {
		t.Errorf("summary = %s, want 4 things need you", got)
	}
	if head := pageElement(t, page, "header", "home-head"); !strings.Contains(head, ">New goal</a>") {
		t.Errorf("heading has no New goal link:\n%s", head)
	}
}

// Every page's top bar leads with Home, counting what needs the viewer — the
// Goals to check in on plus what's waiting on them — and showing no count when
// nothing does. Its count is a pill of its own, set apart from the top bar's
// other counts, because it means something needs the person. Home replaces the
// pending-links, pending-handoffs, and delegated nav items, whose pages stay.
func TestNavCountsWhatNeedsYou(t *testing.T) {
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
		page := getBody(t, samClient, ts.URL+path)
		nav := page[strings.Index(page, "<nav"):strings.Index(page, "</nav>")]
		home := pageElement(t, page, "a", "nav-home")
		if !strings.Contains(home, `href="/home"`) || !strings.Contains(home, `<span class="count needs-you">3</span>`) {
			t.Errorf("on %s, Home does not count sam's 2 Goals to check in on and 1 link request in its needs-you pill: %s", path, home)
		}
		if first := strings.Index(nav, `data-testid="nav-`); first != strings.Index(nav, `data-testid="nav-home"`) {
			t.Errorf("on %s, Home is not the first nav item:\n%s", path, nav)
		}
		for _, gone := range []string{"nav-pending-links", "nav-pending-handoffs", "nav-delegated-goals"} {
			if strings.Contains(nav, gone) {
				t.Errorf("on %s, the top bar still has %s", path, gone)
			}
		}
	}

	// kim checked in today and nothing is waiting on them.
	kimPage := getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/goals")
	if home := pageElement(t, kimPage, "a", "nav-home"); strings.Contains(home, "count") {
		t.Errorf("Home shows a count with nothing needing kim: %s", home)
	}
}

// Home's one-button forms — a due Goal's No change, a link request's Accept and
// Reject — sit inline through the shared class rather than a style attribute.
func TestHomeInlineFormsComeFromTheSharedClass(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	stale := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, stale, 14)
	h.Checkin(sam, stale.ID, domain.HealthYellow, "Slipping.", "Cut scope.", testsupport.Epoch.AddDate(0, 1, 0))
	h.Clock.Advance(16 * day)
	link := h.RequestLink(kim, h.ActiveGoal(kim, "Cut churn", "Customers leave."), stale, "")
	client := signInClient(t, ts.URL, "sam@example.com")
	css := getBody(t, client, ts.URL+"/static/app.css")

	page := getBody(t, client, ts.URL+"/home")
	for _, action := range []string{
		fmt.Sprintf("/goals/%d/checkins/no-change", stale.ID),
		fmt.Sprintf("/links/%d/accept", link.ID),
		fmt.Sprintf("/links/%d/reject", link.ID),
	} {
		assertStyledBy(t, tagAround(t, page, `action="`+action+`"`), css, "inline-form", "display:inline")
	}
}

// Home's No change on a Green Goal whose Milestone has gone overdue is refused;
// the person lands on that Goal's Check-in form with the reason, whether the
// button posted plainly or through htmx, and nothing is recorded (#103).
func TestHomeNoChangeRefusedLandsOnCheckinForm(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := overdueGreenGoal(h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	row := homeRow(t, pageElement(t, getBody(t, client, ts.URL+"/home"), "ul", "home-due"), goal)
	action := attr(tagAround(t, row, `action="/goals/`), "action")
	if want := fmt.Sprintf("/goals/%d/checkins/no-change", goal.ID); action != want {
		t.Fatalf("Home's No change posts to %q, want %q:\n%s", action, want, row)
	}

	for _, hx := range []bool{false, true} {
		resp, page := postNoChange(t, client, ts.URL+action, hx)
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

// Home's New goal goes straight to the Goal list with its propose form open and
// the Title focused, by the address alone so it works without script (#93).
func TestHomeNewGoalOpensProposeForm(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")

	head := pageElement(t, getBody(t, client, ts.URL+"/home"), "header", "home-head")
	link := tagAround(t, head, ">New goal<")
	href := html.UnescapeString(attr(link, "href"))
	if !strings.HasPrefix(href, "/goals?") {
		t.Fatalf("Home's New goal links to %q, want the Goal list with its form open:\n%s", href, head)
	}
	if !proposeFormOpen(t, getBody(t, client, ts.URL+href)) {
		t.Errorf("following New goal shows the propose form closed")
	}
}
