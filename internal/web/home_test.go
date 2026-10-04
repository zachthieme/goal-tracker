package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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
	t.Parallel()

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

	due := pageElement(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "ul", "home-due")

	// weekly is 5 days past its 7-day cadence; monthly is 24 days into 28.
	first, second := strings.Index(due, navTo(weekly.ID)), strings.Index(due, navTo(monthly.ID))
	if first < 0 || second < 0 || first > second {
		t.Errorf("Check-ins due does not list %q, 5 days overdue, before %q, due in 4:\n%s", weekly.Title, monthly.Title, due)
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

	page := getBody(t, client, ts.URL+"/home")
	requests := pageElement(t, page, "ul", "home-requests")
	if got := pageElement(t, page, "p", "home-summary"); !strings.Contains(got, "2 things need you") {
		t.Errorf("summary = %s, want 2 things need you for the two Requests", got)
	}

	if older, newer := strings.Index(requests, navTo(handed.ID)), strings.Index(requests, navTo(child.ID)); older < 0 || newer < 0 || older > newer {
		t.Errorf("Requests does not list the day-old Handoff before today's link request:\n%s", requests)
	}
	for _, tc := range []struct {
		what   string
		row    string
		action string
	}{
		{"link request", homeRow(t, requests, child), fmt.Sprintf("/links/%d", link.ID)},
		{"Handoff", homeRow(t, requests, handed), fmt.Sprintf("/handoffs/%d", handoff.ID)},
	} {
		accept := between(t, tc.row, `action="`+tc.action+`/accept"`, "</form>")
		if !strings.Contains(accept, `<button type="submit" class="btn sm">Accept</button>`) {
			t.Errorf("%s has no outlined Accept:\n%s", tc.what, tc.row)
		}
		reject := between(t, tc.row, `action="`+tc.action+`/reject"`, "</form>")
		if !strings.Contains(reject, `<button type="submit" class="btn quiet sm">Reject</button>`) {
			t.Errorf("%s has no Reject text button:\n%s", tc.what, tc.row)
		}
		assertSubmitsAtOnce(t, tc.what+"'s Reject", tagAround(t, tc.row, `action="`+tc.action+`/reject"`))
	}

	postForm(t, client, ts.URL+fmt.Sprintf("/links/%d/accept", link.ID), nil)
	if parents := h.ParentsOf(child); len(parents) != 1 || parents[0].ID != parent.ID {
		t.Errorf("Home's Accept did not link %q under %q: parents = %v", child.Title, parent.Title, parents)
	}
	postForm(t, client, ts.URL+fmt.Sprintf("/handoffs/%d/reject", handoff.ID), nil)
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

	for _, tc := range []struct {
		who, has, lacks string
	}{
		{"sam@example.com", `data-testid="home-due"`, `data-testid="home-requests"`},
		{"kim@example.com", `data-testid="home-requests"`, `data-testid="home-due"`},
	} {
		list := pageElement(t, getBody(t, signInClient(t, ts.URL, tc.who), ts.URL+"/home"), "section", "home-needs-you")
		if !strings.Contains(list, tc.has) {
			t.Errorf("%s: Needs you lacks %s:\n%s", tc.who, tc.has, list)
		}
		for _, gone := range []string{tc.lacks, `data-testid="home-caught-up"`} {
			if strings.Contains(list, gone) {
				t.Errorf("%s: Needs you shows %s:\n%s", tc.who, gone, list)
			}
		}
	}
	if !strings.Contains(pageElement(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "section", "home-needs-you"), navTo(due.ID)) {
		t.Errorf("sam's due Goal is missing from Needs you")
	}

	// pat's Goal was activated today, and nothing waits on them.
	list := pageElement(t, getBody(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/home"), "section", "home-needs-you")
	if n := strings.Count(list, "<li"); n != 0 {
		t.Errorf("pat has nothing needing them but Needs you lists %d rows:\n%s", n, list)
	}
	for _, gone := range []string{"Check-ins due", "Requests"} {
		if strings.Contains(list, gone) {
			t.Errorf("pat's Needs you heads an empty %s subgroup:\n%s", gone, list)
		}
	}
	if strings.Count(list, "You're all caught up") != 1 || !strings.Contains(list, `data-testid="home-caught-up"`) {
		t.Errorf("pat's Needs you does not show one caught-up line:\n%s", list)
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

	row := homeRow(t, pageElement(t, getBody(t, client, ts.URL+"/home"), "ul", "home-requests"), handed)

	if strings.Contains(row, "/accept") {
		t.Errorf("Handoff with Delegates offers Accept on Home without the choice of whom to keep:\n%s", row)
	}
	review := tagAround(t, row, `data-testid="home-request-review"`)
	if href := attr(review, "href"); href != "/handoffs" {
		t.Fatalf("Review links to %q, want /handoffs:\n%s", href, row)
	}
	if !strings.Contains(row, ">Review</a>") {
		t.Errorf("Review link does not read Review:\n%s", row)
	}
	if !strings.Contains(row, fmt.Sprintf(`action="/handoffs/%d/reject"`, handoff.ID)) {
		t.Errorf("Handoff with Delegates offers no Reject:\n%s", row)
	}
}

// The sidebar counts the viewer's own Active Goals by Health and lists the Red
// and Yellow ones; Goals they're only a Delegate on aren't theirs to count.
// The heading says how many things need them, beside a New goal link.
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

// "Your goals" draws the viewer's own Active Goals by Health as a segmented
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	bar := pageElement(t, page, "div", "home-health-bar")
	if got := strings.Join(regexp.MustCompile(`<span class="(g|y|r)"`).FindAllString(bar, -1), ""); got != `<span class="g"<span class="g"<span class="r"` {
		t.Errorf("bar's units = %s, want two Green then one Red:\n%s", got, bar)
	}
	if label := attr(tagAround(t, bar, `data-testid="home-health-bar"`), "aria-label"); label != "2 Green, 0 Yellow, 1 Red" {
		t.Errorf("bar's label = %q, want its counts", label)
	}
	for testID, want := range map[string]string{
		"home-count-green":  "2",
		"home-count-yellow": "0",
		"home-count-red":    "1",
	} {
		if got := pageElement(t, page, "span", testID); !strings.HasSuffix(got, ">"+want) {
			t.Errorf("%s = %s, want %s", testID, got, want)
		}
	}

	kimPage := getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/home")
	if !strings.Contains(kimPage, `data-testid="home-health-bar"`) {
		t.Errorf("kim's Yellow Goal draws no bar")
	}
	pat := getBody(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/home")
	if strings.Contains(pat, `data-testid="home-health-bar"`) {
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	bar := pageElement(t, page, "div", "home-health-bar")
	if got := strings.Join(regexp.MustCompile(`<span class="(g|y|r)"`).FindAllString(bar, -1), ""); got != `<span class="g"` {
		t.Errorf("bar's units = %s, want only the one Green:\n%s", got, bar)
	}
	if label := attr(tagAround(t, bar, `data-testid="home-health-bar"`), "aria-label"); label != "1 Green, 0 Yellow, 0 Red" {
		t.Errorf("bar's label = %q, want only the Active Goal counted", label)
	}
}

// Every page's top bar leads with Home, counting what needs the viewer — the
// Goals to check in on plus what's waiting on them — and showing no count when
// nothing does. Its count is a pill of its own, set apart from the top bar's
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
	t.Parallel()

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
	t.Parallel()

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

// Home's New goal links to the New goal page, which opens with the Title
// focused, so it works without script.
func TestHomeNewGoalOpensProposeForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")

	head := pageElement(t, getBody(t, client, ts.URL+"/home"), "header", "home-head")
	link := tagAround(t, head, ">New goal<")
	if href := attr(link, "href"); href != "/goals/new" {
		t.Fatalf("Home's New goal links to %q, want /goals/new:\n%s", href, head)
	}
	form := pageElement(t, getBody(t, client, ts.URL+attr(link, "href")), "form", "goal-form")
	if title := tagAround(t, form, `name="title"`); !strings.Contains(title, " autofocus") {
		t.Errorf("following New goal doesn't focus the Title: %s", title)
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
	page := getBody(t, client, ts.URL+"/home")
	if got := pageElement(t, page, "p", "home-summary"); !strings.Contains(got, "1 thing needs you") {
		t.Errorf("summary = %s, want 1 thing needs you for the suggestion", got)
	}
	row := homeRow(t, pageElement(t, page, "ul", "home-requests"), goal)
	for _, want := range []string{"Pat Lee", navTo(parent.ID), "displays cause outages"} {
		if !strings.Contains(row, want) {
			t.Errorf("the row lacks %q:\n%s", want, row)
		}
	}
	base := fmt.Sprintf("/parent-suggestions/%d", s.ID)
	for _, action := range []string{"/decline", "/accept"} {
		form := between(t, row, `action="`+base+action+`"`, "</form>")
		if !strings.Contains(form, `name="from" value="home"`) {
			t.Errorf("Home's %s doesn't say it came from Home:\n%s", action, form)
		}
	}

	resp := postForm(t, client, ts.URL+base+"/accept", url.Values{"from": {"home"}})
	page = readBody(t, resp)
	if resp.Request.URL.Path != "/home" {
		t.Errorf("accepting landed on %s, want /home", resp.Request.URL.Path)
	}
	if strings.Contains(page, `data-testid="home-requests"`) {
		t.Errorf("Home still lists the accepted suggestion:\n%s", page)
	}
	if pending, _ := h.Service.PendingLinkRequests(context.Background(), kim.ID); len(pending) != 1 || pending[0].Child.ID != goal.ID {
		t.Errorf("kim's requests = %+v, want the Goal's link", pending)
	}
}
