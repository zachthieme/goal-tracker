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

// A Stale Goal's Health in Check-ins due is its last Check-in's, so it reads
// as last reported: a muted badge naming just the Health, with "as of" how
// long ago beside it, still flagged Stale. A due Goal that isn't Stale yet
// keeps its full-colour badge and no "as of".
func TestHomeDatesStaleGoalsHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	stale := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, stale.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(day)
	dueSoon := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	setCadence(t, h, dueSoon, 14)
	h.Checkin(sam, dueSoon.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(8 * day)

	due := pageElement(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "ul", "home-due")

	row := homeRow(t, due, stale)
	badge := tagAround(t, row, `data-testid="home-due-health"`)
	if !hasClass(badge, "as-of") {
		t.Errorf("Stale Goal's Health badge is not muted: %s", badge)
	}
	if !regexp.MustCompile(`data-testid="home-due-health"[^>]*><span class="dot"></span>\s*Green\s*</span>`).MatchString(row) {
		t.Errorf("Stale Goal's Health badge does not read just Green:\n%s", row)
	}
	if got := elementTexts(row, "span", "home-due-as-of"); len(got) != 1 || got[0] != "as of 9 days ago" {
		t.Errorf("Stale Goal's Health is dated %q, want as of 9 days ago:\n%s", got, row)
	}
	if !strings.Contains(row, `data-testid="home-due-stale"`) {
		t.Errorf("Stale Goal is no longer flagged Stale:\n%s", row)
	}

	row = homeRow(t, due, dueSoon)
	if hasClass(tagAround(t, row, `data-testid="home-due-health"`), "as-of") || strings.Contains(row, `data-testid="home-due-as-of"`) {
		t.Errorf("due Goal that isn't Stale has its Health dated:\n%s", row)
	}
}

// A Stale Goal never checked in on has no reported Health to date: its badge
// reads "No Health yet", unmuted, with no "as of", since its days count from
// activation.
func TestHomeLeavesNoHealthYetUndated(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(9 * day)

	row := homeRow(t, pageElement(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "ul", "home-due"), silent)
	if !strings.Contains(row, `data-testid="home-due-stale"`) {
		t.Fatalf("Goal 9 days past activation on a weekly cadence isn't Stale:\n%s", row)
	}
	badge := tagAround(t, row, `data-testid="home-due-health"`)
	if hasClass(badge, "as-of") || !strings.Contains(row, "No Health yet") {
		t.Errorf("Goal with no Check-in doesn't read No Health yet, unmuted: %s", row)
	}
	if strings.Contains(row, `data-testid="home-due-as-of"`) {
		t.Errorf("Goal with no Check-in has its Health dated:\n%s", row)
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
	if !strings.Contains(yours, `<a href="/goals?org=mine">Goals you own or help with →</a>`) {
		t.Errorf("Your goals does not link to the Goals sam owns or helps with:\n%s", yours)
	}
	if strings.Contains(yours, `href="/goals"`) {
		t.Errorf("Your goals still links to every Goal in the org:\n%s", yours)
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

// "Your goals" lists the viewer's Red and Yellow Goals under a "Not Green"
// heading, Red first, so the list doesn't read as all their Goals. With only
// Green Goals there is neither heading nor list.
func TestHomeHeadsNotGreenGoals(t *testing.T) {
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

	yours := pageElement(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "section", "home-your-goals")
	heading := strings.Index(yours, `<h4 class="label">Not Green</h4>`)
	first, second := strings.Index(yours, navTo(red.ID)), strings.Index(yours, navTo(yellow.ID))
	if heading < 0 || first < heading || second < first {
		t.Errorf("Your goals does not list Red then Yellow under a Not Green heading:\n%s", yours)
	}

	kims := pageElement(t, getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/home"), "section", "home-your-goals")
	if strings.Contains(kims, "Not Green") || strings.Contains(kims, "hm-compact") {
		t.Errorf("kim has only Green Goals but sees a Not Green list:\n%s", kims)
	}
}

// In the Not Green list a Stale Goal's Health is muted and dated as last
// reported, as in Check-ins due; a Goal checked in on lately keeps its
// full-colour badge and no "as of".
func TestHomeDatesStaleNotGreenHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	target := testsupport.Epoch.AddDate(0, 1, 0)
	stale := h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.Checkin(sam, stale.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	h.Clock.Advance(9 * day)
	fresh := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	h.Checkin(sam, fresh.ID, domain.HealthYellow, "Slipping.", "Cut scope.", target)

	yours := pageElement(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "section", "home-your-goals")

	row := homeRow(t, yours, stale)
	if badge := tagAround(t, row, `data-testid="home-goal-health"`); !hasClass(badge, "as-of") {
		t.Errorf("Stale Red Goal's Health badge is not muted: %s", badge)
	}
	if got := elementTexts(row, "span", "home-goal-as-of"); len(got) != 1 || got[0] != "as of 9 days ago" {
		t.Errorf("Stale Red Goal's Health is dated %q, want as of 9 days ago:\n%s", got, row)
	}

	row = homeRow(t, yours, fresh)
	if hasClass(tagAround(t, row, `data-testid="home-goal-health"`), "as-of") || strings.Contains(row, `data-testid="home-goal-as-of"`) {
		t.Errorf("Yellow Goal checked in today has its Health dated:\n%s", row)
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

	due := pageElement(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home"), "ul", "home-due")

	row := homeRow(t, due, overdue)
	if !strings.Contains(row, `data-testid="home-due-checkin"`) {
		t.Errorf("the overdue Goal's row has no Check in:\n%s", row)
	}
	if strings.Contains(row, "/checkins/no-change") {
		t.Errorf("the overdue Goal's row offers a No change that would be refused:\n%s", row)
	}
	row = homeRow(t, due, repeatable)
	for _, want := range []string{fmt.Sprintf(`action="/goals/%d/checkins/no-change"`, repeatable.ID), `data-testid="home-due-checkin"`} {
		if !strings.Contains(row, want) {
			t.Errorf("the repeatable Goal's row lacks %s:\n%s", want, row)
		}
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

// Every page's top bar offers Goals, the app's main list, between Home and
// Risks, with Reports after Risks.
func TestNavOffersGoalsBetweenHomeAndRisks(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/home", "/goals", "/risks", "/reports"} {
		page := getBody(t, client, ts.URL+path)
		nav := page[strings.Index(page, "<nav"):strings.Index(page, "</nav>")]
		if goals := pageElement(t, page, "a", "nav-goals"); !strings.Contains(openTag(goals), `href="/goals"`) {
			t.Errorf("on %s, the Goals nav item does not link to /goals: %s", path, goals)
		}
		home := strings.Index(nav, `data-testid="nav-home"`)
		goals := strings.Index(nav, `data-testid="nav-goals"`)
		risks := strings.Index(nav, `data-testid="nav-risks"`)
		reports := strings.Index(nav, `data-testid="nav-reports"`)
		if home < 0 || goals < home || risks < goals || reports < risks {
			t.Errorf("on %s, the nav items do not read Home, Goals, Risks, Reports:\n%s", path, nav)
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
		page := getBody(t, client, ts.URL+path)
		if brand := tagAround(t, page, `class="brand"`); attr(brand, "href") != "/home" {
			t.Errorf("on %s, the brand link does not lead to /home: %s", path, brand)
		}
	}
}

// The Goals nav item carries no count, even when the viewer owns Red and Stale
// Goals that Home and Risks count.
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
	for _, path := range []string{"/home", "/goals", "/risks"} {
		page := getBody(t, client, ts.URL+path)
		if home := pageElement(t, page, "a", "nav-home"); !strings.Contains(home, `class="count needs-you"`) {
			t.Fatalf("on %s, Home counts nothing, so the fixture has nothing for Goals to count: %s", path, home)
		}
		if goals := pageElement(t, page, "a", "nav-goals"); strings.Contains(goals, "<span") {
			t.Errorf("on %s, the Goals nav item carries a count: %s", path, goals)
		}
	}
}

// An Admin's top bar reads Home, Goals, Risks, Reports, Admin.
func TestNavOrderForAnAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	h.SignIn("ada@example.com")
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "ada@example.com"), ts.URL+"/home")

	nav := page[strings.Index(page, "<nav"):strings.Index(page, "</nav>")]
	var order []string
	for _, m := range regexp.MustCompile(`data-testid="(nav-[a-z]+)"`).FindAllStringSubmatch(nav, -1) {
		order = append(order, m[1])
	}
	if got, want := strings.Join(order, " "), "nav-home nav-goals nav-risks nav-reports nav-admin"; got != want {
		t.Errorf("an Admin's nav items read %q, want %q", got, want)
	}
}

// homeOrgCard is Home's My org card, failing the test when Home has none.
func homeOrgCard(t *testing.T, page string) string {
	t.Helper()
	return pageElement(t, page, "section", "home-org")
}

// Home shows a My org card only to someone with anyone under them by
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
	if page := getBody(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"); strings.Contains(page, `data-testid="home-org"`) {
		t.Errorf("Home shows My org with no Managers synced")
	}
	h.SetManager(dev, lead)
	h.SetManager(self, self)
	for _, who := range []string{"dev@example.com", "self@example.com"} {
		if page := getBody(t, signInClient(t, ts.URL, who), ts.URL+"/home"); strings.Contains(page, `data-testid="home-org"`) {
			t.Errorf("Home shows My org to %s, who has no one under them", who)
		}
	}

	page := getBody(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")
	card := homeOrgCard(t, page)
	if !strings.Contains(card, "<h3>My org</h3>") {
		t.Errorf("My org card isn't titled My org:\n%s", card)
	}
	needsYou, org, aside := strings.Index(page, `data-testid="home-needs-you"`), strings.Index(page, `data-testid="home-org"`), strings.Index(page, "<aside")
	if needsYou >= org || org >= aside {
		t.Errorf("My org isn't in the main column under Needs you: needs-you at %d, my org at %d, aside at %d", needsYou, org, aside)
	}
}

// homeOrgCounts reads the My org count badges in section, in order, as
// "kind: label", so an assertion sees both what each reads and what it is.
func homeOrgCounts(section string) []string {
	var out []string
	for rest := section; ; {
		at := strings.Index(rest, `data-testid="home-org-count"`)
		if at < 0 {
			return out
		}
		rest = rest[strings.LastIndex(rest[:at], "<"):]
		badge, after, _ := strings.Cut(rest, "</span>")
		_, label, _ := strings.Cut(badge, ">")
		out = append(out, attr(openTag(badge), "data-kind")+": "+label)
		rest = after
	}
}

// homeOrgRows are the My org card's rows, one per flagged person, in order.
func homeOrgRows(card string) []string {
	var out []string
	for rest := card; ; {
		start := strings.Index(rest, `<li data-testid="home-org-person"`)
		if start < 0 {
			return out
		}
		rest = rest[start:]
		end := strings.Index(rest, "</li>")
		out = append(out, rest[:end])
		rest = rest[end:]
	}
}

// homeOrgFixture is a lead with Ana and Ben directly under them and Cy under
// Ana. Cy's Goal is Red, Stale and past its Path to Green, Ana's is Yellow,
// and Ben's is Red.
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

// My org's total line counts, across everyone under the viewer, each signal
// that has anything, labelled so it reads without colour, in the order Red,
// Path to Green overdue, Stale, Yellow. Each person directly under the viewer
// with anything flagged gets a row with their whole Org's counts, worst off
// first, linking to Goals scoped to their Org; someone two levels down counts
// in the row of the person they're under. The card lists no Goals.
func TestHomeMyOrgCountsWhatIsInTroubleByPerson(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newHomeOrgFixture(h)

	card := homeOrgCard(t, getBody(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"))
	total := pageElement(t, card, "p", "home-org-total")
	wantTotal := []string{"red: 2 Red", "path-overdue: 1 Path to Green overdue", "stale: 1 Stale", "yellow: 1 Yellow"}
	if got := homeOrgCounts(total); !slices.Equal(got, wantTotal) {
		t.Errorf("total line = %v, want %v", got, wantTotal)
	}
	for _, want := range []string{`data-kind="red" class="badge r"`, `data-kind="path-overdue" class="badge st"`, `data-kind="stale" class="badge st"`, `data-kind="yellow" class="badge y"`} {
		if !strings.Contains(total, want) {
			t.Errorf("total line lacks %s:\n%s", want, total)
		}
	}

	rows := homeOrgRows(card)
	if len(rows) != 2 {
		t.Fatalf("My org has %d rows, want Ana's and Ben's:\n%s", len(rows), card)
	}
	for i, want := range []struct {
		who    domain.Account
		counts []string
	}{
		{f.ana, []string{"red: 1 Red", "path-overdue: 1 Path to Green overdue", "stale: 1 Stale", "yellow: 1 Yellow"}},
		{f.ben, []string{"red: 1 Red"}},
	} {
		if !strings.Contains(rows[i], shownAs(want.who.Email, want.who.Name)) {
			t.Errorf("row %d isn't %s's:\n%s", i, want.who.Name, rows[i])
		}
		if got := homeOrgCounts(rows[i]); !slices.Equal(got, want.counts) {
			t.Errorf("%s's row counts %v, want %v", want.who.Name, got, want.counts)
		}
		if href := fmt.Sprintf(`href="/goals?org=%d"`, want.who.ID); !strings.Contains(rows[i], href) {
			t.Errorf("%s's row doesn't link to %s:\n%s", want.who.Name, href, rows[i])
		}
	}
	if strings.Contains(card, "Cy") {
		t.Errorf("Cy, two levels down, appears on the card:\n%s", card)
	}
	for _, href := range []string{fmt.Sprintf(`href="/goals?org=%d"`, f.lead.ID), fmt.Sprintf(`href="/risks?org=%d"`, f.lead.ID)} {
		if !strings.Contains(card, href) {
			t.Errorf("My org doesn't link on with %s:\n%s", href, card)
		}
	}
	for _, g := range f.goals {
		if strings.Contains(card, g.Title) {
			t.Errorf("My org lists the Goal %q:\n%s", g.Title, card)
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

			card := homeOrgCard(t, getBody(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"))
			if rows := homeOrgRows(card); len(rows) != 2 {
				t.Errorf("My org has %d rows, want only the 2 flagged people's", len(rows))
			}
			if strings.Contains(card, "home-org-all-clear") {
				t.Errorf("My org says everyone is all clear while some are flagged:\n%s", card)
			}
			if tc.want == "" {
				if strings.Contains(card, "home-org-clear") {
					t.Errorf("My org counts others all clear when there are none:\n%s", card)
				}
				return
			}
			clear := pageElement(t, card, "p", "home-org-clear")
			if !strings.Contains(clear, ">"+tc.want) || !strings.Contains(openTag(clear), "muted") {
				t.Errorf("all-clear line = %s, want a muted %q", clear, tc.want)
			}
			if rows := strings.Index(card, "home-org-person"); rows > strings.Index(card, "home-org-clear") {
				t.Errorf("the all-clear line comes before the rows:\n%s", card)
			}
		})
	}
}

// With nothing flagged anywhere under the viewer, My org says so in one line,
// with no total and no rows, and still links on to the Org's Goals and Risks.
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

	card := homeOrgCard(t, getBody(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home"))
	allClear := pageElement(t, card, "p", "home-org-all-clear")
	if !strings.Contains(allClear, ">Everyone in My org is all clear.") || !strings.Contains(openTag(allClear), "muted") {
		t.Errorf("all-clear line = %s, want a muted %q", allClear, "Everyone in My org is all clear.")
	}
	for _, gone := range []string{"home-org-total", "home-org-person", "home-org-clear\"", "home-org-count"} {
		if strings.Contains(card, gone) {
			t.Errorf("My org with nothing flagged shows %s:\n%s", gone, card)
		}
	}
	for testID, href := range map[string]string{
		"home-org-goals-link": fmt.Sprintf("/goals?org=%d", lead.ID),
		"home-org-risks-link": fmt.Sprintf("/risks?org=%d", lead.ID),
	} {
		if link := pageElement(t, card, "a", testID); attr(openTag(link), "href") != href {
			t.Errorf("%s = %s, want href %s", testID, link, href)
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

	rows := homeOrgRows(homeOrgCard(t, getBody(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")))
	if len(rows) != 2 {
		t.Fatalf("My org has %d rows, want Ana's and Ben's", len(rows))
	}
	if !strings.Contains(rows[1], shownAs(f.ben.Email, f.ben.Name)) || !strings.Contains(rows[1], `data-testid="home-org-departed"`) {
		t.Errorf("Ben's row isn't marked departed:\n%s", rows[1])
	}
	if strings.Contains(rows[0], "home-org-departed") {
		t.Errorf("Ana's row is marked departed:\n%s", rows[0])
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

	page := getBody(t, signInClient(t, ts.URL, "lead@example.com"), ts.URL+"/home")
	homeOrgCard(t, page)
	if !strings.Contains(page, `data-testid="home-caught-up"`) || !strings.Contains(page, "You're all caught up.") {
		t.Errorf("Needs you isn't caught up with only the Org's Goals Red")
	}
	if summary := pageElement(t, page, "p", "home-summary"); !strings.Contains(summary, "0 things need you") {
		t.Errorf("summary = %s, want 0 things need you", summary)
	}
	if home := pageElement(t, page, "a", "nav-home"); strings.Contains(home, "count") {
		t.Errorf("Home's nav item counts the Org's Goals: %s", home)
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
	if _, err := h.DB.Exec(`ALTER TABLE milestones RENAME TO gone_milestones`); err != nil {
		t.Fatalf("rename milestones: %v", err)
	}

	resp, err := client.Get(ts.URL + "/home")
	if err != nil {
		t.Fatalf("GET /home: %v", err)
	}
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}
