package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// oneOfEachKind arranges an Active Goal with one History entry of each kind on
// different days: its Why it matters on Fri 2 Jan 2026 (its creation), a Field set on
// Tue 6 Jan, a Check-in that slips the delivery date on Thu 8 Jan and a
// Handoff to Pat started on Sun 11 Jan.
func oneOfEachKind(t *testing.T, h *testsupport.Harness) domain.Goal {
	t.Helper()
	ctx := context.Background()
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	pat := h.SignInNamed("pat@example.com", "Pat Next")
	boss := h.SignIn("boss@example.com") // an Admin: the harness is built with boss@example.com
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "")

	h.Clock.Advance(4 * 24 * time.Hour)
	h.SetGoalField(sam, goal, budget, "200")

	h.Clock.Advance(2 * 24 * time.Hour)
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthYellow,
		Status:             "Vendor is late.",
		PathToGreen:        "Swap vendors.",
		PathTargetDate:     testsupport.Epoch.AddDate(0, 2, 0),
		DeliveryDate:       goal.DeliveryDate.AddDate(0, 0, 14),
		DeliveryDateReason: "Vendor API delayed two weeks.",
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}

	h.Clock.Advance(3 * 24 * time.Hour)
	if _, err := h.Service.StartHandoff(ctx, domain.StartHandoffInput{GoalID: goal.ID, ToOwnerID: pat.ID, ActorID: sam.ID}); err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	return goal
}

// historyEntries splits a Goal page's History block into its entries, in the
// order they show.
func historyEntries(history string) []string {
	return strings.Split(history, `data-testid="history-entry"`)[1:]
}

// historyBlock is the Goal page's History block, up to the sidebar.
func historyBlock(t *testing.T, page string) string {
	t.Helper()
	return between(t, page, `data-testid="goal-history"`, "<aside")
}

// valueEntries are the value changes on the Goal page's History timeline under
// the Values chip, newest first, with entities unescaped.
func valueEntries(t *testing.T, client *http.Client, goalURL string) []string {
	t.Helper()
	return filteredEntries(t, client, goalURL, "values", "value")
}

// ownershipEntries are the Handoffs and Admin Reassigns on the Goal page's
// History timeline under the Ownership chip, newest first.
func ownershipEntries(t *testing.T, client *http.Client, goalURL string) []string {
	t.Helper()
	return filteredEntries(t, client, goalURL, "ownership", "ownership")
}

// filteredEntries are the entries the Goal page's History lists under filter,
// up to its first two pages, each checked to be of kind, with entities
// unescaped.
func filteredEntries(t *testing.T, client *http.Client, goalURL, filter, kind string) []string {
	t.Helper()
	var out []string
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL+"?history="+filter+"&shown=25"))) {
		if got := entryKind(t, e); got != kind {
			t.Errorf("the %s chip lists a %s entry", filter, got)
		}
		out = append(out, html.UnescapeString(e))
	}
	return out
}

// entryKind is the kind an entry says it is.
func entryKind(t *testing.T, entry string) string {
	t.Helper()
	_, rest, ok := strings.Cut(entry, `data-kind="`)
	if !ok {
		t.Fatalf("entry has no kind: %s", entry)
	}
	kind, _, _ := strings.Cut(rest, `"`)
	return kind
}

// A Goal's History is one list, newest first, of every kind of entry, each
// week under a "Week of" heading in the org's calendar, and it is open on
// load: nothing in it sits behind an outer disclosure.
func TestGoalHistoryIsOneTimelineNewestFirstUnderWeekHeadings(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	goal := oneOfEachKind(t, h)
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	history := historyBlock(t, page)
	if strings.Contains(history, "<details") && strings.Index(history, "<details") < strings.Index(history, `data-testid="history-entry"`) {
		t.Errorf("the History block opens behind a disclosure:\n%s", history)
	}

	weeks := strings.Split(history, `data-testid="history-week"`)[1:]
	if len(weeks) != 2 {
		t.Fatalf("History has %d weeks, want 2:\n%s", len(weeks), history)
	}
	for i, want := range []struct {
		heading string
		kinds   []string
	}{
		{"Week of Jan 5", []string{"ownership", "checkin", "value"}},
		{"Week of Dec 29, 2025", []string{"why-it-matters"}},
	} {
		if heading := between(t, weeks[i], "<h3", "</h3>"); !strings.Contains(heading, want.heading) {
			t.Errorf("week %d is headed %q, want %q", i, heading, want.heading)
		}
		var kinds []string
		for _, e := range strings.Split(weeks[i], `data-testid="history-entry"`)[1:] {
			kinds = append(kinds, entryKind(t, e))
		}
		if strings.Join(kinds, ",") != strings.Join(want.kinds, ",") {
			t.Errorf("week %q lists %v, want %v", want.heading, kinds, want.kinds)
		}
	}
}

// History's weeks and times are the org's calendar: a Check-in on Monday 5
// Jan at 03:00 UTC is Sunday evening in Los Angeles, so in an org there it
// falls in the week of 29 Dec with the Goal's creation.
func TestGoalHistoryWeeksAreInTheOrgsTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Clock.Set(time.Date(2026, 1, 5, 3, 0, 0, 0, time.UTC))
	h.Checkin(sam, goal.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	ts := httptest.NewServer(web.NewServer(domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))))
	t.Cleanup(ts.Close)

	history := historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal)))
	weeks := strings.Split(history, `data-testid="history-week"`)[1:]
	if len(weeks) != 1 || !strings.Contains(weeks[0], "Week of Dec 29, 2025") {
		t.Fatalf("History is not one week of Dec 29, 2025 in Los Angeles:\n%s", history)
	}
	if checkin := historyEntries(history)[0]; !strings.Contains(checkin, "Jan 4, 19:00") {
		t.Errorf("the Check-in doesn't show its Los Angeles time:\n%s", checkin)
	}
}

// historyChips reads the History block's filter chips as each label mapped to
// its count and address.
func historyChips(t *testing.T, history string) map[string]struct{ count, href string } {
	t.Helper()
	chips := map[string]struct{ count, href string }{}
	for _, chip := range strings.Split(between(t, history, `data-testid="history-chips"`, "</nav>"), `data-testid="history-chip"`)[1:] {
		label := strings.TrimSpace(between(t, chip, ">", "<span")[1:])
		count := between(t, chip, `class="tl-count">`, "</span>")[len(`class="tl-count">`):]
		_, href, _ := strings.Cut(chip, `href="`)
		href, _, _ = strings.Cut(href, `"`)
		chips[label] = struct{ count, href string }{count, html.UnescapeString(href)}
	}
	return chips
}

// Each filter chip counts what it lists and, followed, reloads the page with
// the filter in the address, the History block in view and only that kind
// listed; Date Slips lists the Check-ins that carry one, and All lists
// everything again.
func TestGoalHistoryChipsCountAndFilter(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	goal := oneOfEachKind(t, h)
	h.Clock.Advance(time.Hour)
	h.Checkin(goal.Owner, goal.ID, domain.HealthGreen, "Vendor swapped.", "", time.Time{})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	chips := historyChips(t, historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal))))
	for _, tc := range []struct {
		label, count string
		kinds        []string
		mark         string // a fact only the listed entries carry
	}{
		{"Check-ins", "2", []string{"checkin", "checkin"}, ""},
		{"Date Slips", "1", []string{"checkin"}, "Vendor API delayed two weeks."},
		{"Why it matters", "1", []string{"why-it-matters"}, ""},
		{"Ownership", "1", []string{"ownership"}, ""},
		{"Values", "1", []string{"value"}, ""},
		{"All", "5", []string{"checkin", "ownership", "checkin", "value", "why-it-matters"}, ""},
	} {
		chip, ok := chips[tc.label]
		if !ok {
			t.Errorf("no %q chip among %v", tc.label, chips)
			continue
		}
		if chip.count != tc.count {
			t.Errorf("%q chip counts %s, want %s", tc.label, chip.count, tc.count)
		}
		if !strings.HasSuffix(chip.href, "#history") {
			t.Errorf("%q chip doesn't bring the History block into view: %s", tc.label, chip.href)
		}
		history := historyBlock(t, getBody(t, client, ts.URL+chip.href))
		var kinds []string
		for _, e := range historyEntries(history) {
			kinds = append(kinds, entryKind(t, e))
		}
		if strings.Join(kinds, ",") != strings.Join(tc.kinds, ",") {
			t.Errorf("following %q lists %v, want %v", tc.label, kinds, tc.kinds)
		}
		if tc.mark != "" && !strings.Contains(history, tc.mark) {
			t.Errorf("following %q doesn't list %q", tc.label, tc.mark)
		}
		if current := between(t, history, `data-filter="`+filterOf(chip.href)+`"`, ">"); !strings.Contains(current, `aria-current="page"`) {
			t.Errorf("following %q doesn't mark its chip current: %s", tc.label, current)
		}
	}
}

// History's filter chips are controls, so their edges reach WCAG's 3:1 in
// light (1.4.11): a resting chip's is --color-border-strong, as every chip
// edge (3.33:1 on the canvas), and the current chip's --color-primary-strong,
// as every chosen option's (5.47:1), since --color-border reaches only 1.17:1
// and --color-primary 2.49:1 (DESIGN.md § History timeline, Deviations 6).
func TestGoalHistoryChipsEdgesReachThreeToOne(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	goal := oneOfEachKind(t, h)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, goalPageURL(ts.URL, goal))
	for sel, want := range map[string]string{
		".tl-chip":               "border:1px solid var(--color-border-strong)",
		".tl-chip[aria-current]": "border-color:var(--color-primary-strong)",
	} {
		if rule := cssRule(t, page, sel); !strings.Contains(rule, want) {
			t.Errorf("%s declares %q, want %s", sel, rule, want)
		}
	}
}

// filterOf is the History filter an address asks for.
func filterOf(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return u.Query().Get("history")
}

// historyEarlier is the address History's "Show earlier" link goes to.
func historyEarlier(t *testing.T, history string) string {
	t.Helper()
	_, href, ok := strings.Cut(between(t, history, `data-testid="history-earlier"`, ">"), `href="`)
	if !ok {
		t.Fatalf("no Show earlier link in:\n%s", history)
	}
	href, _, _ = strings.Cut(href, `"`)
	return html.UnescapeString(href)
}

// History opens on the latest 5 entries, and "Show earlier" reloads the page
// with 20 more under the same filter, the History block in view; once
// everything shows, it is gone.
func TestGoalHistoryShowsFiveAndShowEarlierAddsTwentyMoreKeepingTheFilter(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	for i := 1; i <= 44; i++ {
		h.Clock.Advance(time.Hour)
		h.Checkin(sam, goal.ID, domain.HealthGreen, fmt.Sprintf("Update %d.", i), "", time.Time{})
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	statuses := func(history string) []string {
		var out []string
		for _, e := range historyEntries(history) {
			if entryKind(t, e) == "why-it-matters" {
				out = append(out, "Why it matters")
				continue
			}
			out = append(out, between(t, e, "Update ", ".")[len("Update "):])
		}
		return out
	}

	// All: 45 entries, the latest 5 first.
	history := historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)))
	if got := statuses(history); len(got) != 5 || got[0] != "44" || got[4] != "40" {
		t.Fatalf("History first shows %v, want Updates 44 down to 40", got)
	}
	history = historyBlock(t, getBody(t, client, ts.URL+historyEarlier(t, history)))
	if got := statuses(history); len(got) != 25 || got[24] != "20" {
		t.Fatalf("Show earlier shows %v, want Updates 44 down to 20", got)
	}
	history = historyBlock(t, getBody(t, client, ts.URL+historyEarlier(t, history)))
	if got := statuses(history); len(got) != 45 || got[44] != "Why it matters" {
		t.Fatalf("Show earlier again shows %v, want everything down to the Why it matters", got)
	}
	if strings.Contains(history, `data-testid="history-earlier"`) {
		t.Errorf("Show earlier stays once everything shows")
	}

	// Check-ins: Show earlier keeps the filter.
	history = historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)+"?history=checkins"))
	link := historyEarlier(t, history)
	if filterOf(link) != "checkins" || !strings.HasSuffix(link, "#history") {
		t.Errorf("Show earlier drops the filter or the History block: %s", link)
	}
	history = historyBlock(t, getBody(t, client, ts.URL+link))
	if got := statuses(history); len(got) != 25 || slices.Contains(got, "Why it matters") {
		t.Errorf("Show earlier under Check-ins shows %v, want 25 Check-ins", got)
	}
}

// A Goal with 8 History entries opens on 5, with "Show earlier" asking for 25;
// following it shows all 8 and no link. A missing, bad or non-positive count
// shows the first page.
func TestGoalHistoryOfEightShowsFiveThenAll(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	for i := 1; i <= 7; i++ {
		h.Clock.Advance(time.Hour)
		h.Checkin(sam, goal.ID, domain.HealthGreen, fmt.Sprintf("Update %d.", i), "", time.Time{})
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	history := historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)))
	if n := len(historyEntries(history)); n != 5 {
		t.Fatalf("History first shows %d entries, want 5", n)
	}
	link := historyEarlier(t, history)
	if want := fmt.Sprintf("/goals/%d?shown=25#history", goal.ID); link != want {
		t.Errorf("Show earlier goes to %s, want %s", link, want)
	}
	history = historyBlock(t, getBody(t, client, ts.URL+link))
	if n := len(historyEntries(history)); n != 8 {
		t.Errorf("Show earlier shows %d entries, want 8", n)
	}
	if strings.Contains(history, `data-testid="history-earlier"`) {
		t.Errorf("Show earlier stays once everything shows")
	}
	for _, shown := range []string{"", "?shown=x", "?shown=0", "?shown=-3"} {
		history := historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)+shown))
		if n := len(historyEntries(history)); n != 5 {
			t.Errorf("%q shows %d entries, want the first page of 5", shown, n)
		}
	}
}

// A Check-in on the timeline shows what it recorded: its Health and status, the
// Date Slips of the delivery date and of a Milestone with their reasons, its
// Path to Green, a Lifecycle change, and who wrote it for whom, by Name. Its
// readings and explanation wait behind a disclosure on the entry. A Check-in
// with no Health takes the neutral Check-in marker.
func TestGoalHistoryCheckinShowsWhatItRecorded(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	dee := h.SignInNamed("dee@example.com", "Dee Delegate")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.AddDelegate(sam, dee, goal.ID)
	latency, err := h.Service.AddMetric(ctx, domain.AddMetricInput{
		GoalID: goal.ID, Name: "p95 latency", Unit: "ms", Direction: domain.MetricDown,
		Baseline: 1000, Target: 400, TargetDate: testsupport.Epoch.AddDate(0, 6, 0),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	red := h.ActiveChildOf(sam, goal, "Red work", "Red why it matters.")
	h.Checkin(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", testsupport.Epoch.AddDate(0, 1, 0))
	ms, err := h.Service.ListMilestones(ctx, goal.ID)
	if err != nil || len(ms) != 1 {
		t.Fatalf("ListMilestones: %v %v", ms, err)
	}
	beta := ms[0]

	h.Clock.Advance(24 * time.Hour)
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           dee.ID,
		Health:             domain.HealthYellow,
		Status:             "Vendor is late.",
		PathToGreen:        "Swap vendors.",
		PathTargetDate:     testsupport.Epoch.AddDate(0, 2, 0),
		Explanation:        "The Red child is a stretch item.",
		Readings:           []domain.MetricReadingInput{{MetricID: latency.ID, Value: 800}},
		DeliveryDate:       goal.DeliveryDate.AddDate(0, 0, 14),
		DeliveryDateReason: "Vendor API delayed two weeks.",
		Milestones: []domain.MilestoneChangeInput{{
			MilestoneID: beta.ID, TargetDate: beta.TargetDate.AddDate(0, 0, 10), DateReason: "Design review moved.",
		}},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	h.Clock.Advance(24 * time.Hour)
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: goal.ID, AuthorID: sam.ID, Status: "Pausing.",
		Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Team moved to the payments incident.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}
	ts := newServer(t, h)

	entries := historyEntries(historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))))
	onHold, slipped := entries[0], entries[1]

	if !strings.Contains(onHold, `<span class="tl-mark tl-checkin" aria-hidden="true">`) {
		t.Errorf("a Check-in with no Health lacks the neutral Check-in marker:\n%s", onHold)
	}
	if lc := between(t, onHold, `data-testid="checkin-lifecycle"`, "</span>"); !strings.Contains(lc, "Active → On Hold") || !strings.Contains(lc, "Team moved to the payments incident.") {
		t.Errorf("the On Hold Check-in lacks its Lifecycle change: %s", lc)
	}

	if !strings.Contains(slipped, `<span class="tl-mark y" aria-hidden="true"><span class="dot"></span></span>`) {
		t.Errorf("the Yellow Check-in's marker lacks its Health's shape:\n%s", slipped)
	}
	slips := between(t, slipped, `data-testid="entry-slips"`, "</ul>")
	for _, want := range []string{
		"<strong>Delivery date</strong>: <del>" + fmtDay(goal.DeliveryDate) + "</del> " + fmtDay(goal.DeliveryDate.AddDate(0, 0, 14)),
		"Vendor API delayed two weeks.",
		"<strong>Milestone Beta</strong>: <del>" + fmtDay(beta.TargetDate) + "</del> " + fmtDay(beta.TargetDate.AddDate(0, 0, 10)),
		"Design review moved.",
	} {
		if !strings.Contains(slips, want) {
			t.Errorf("the Check-in's Date Slips lack %q:\n%s", want, slips)
		}
	}
	for _, want := range []string{
		"Yellow", "Vendor is late.", "Path to Green: Swap vendors. (by Mar 2)",
		"by " + shownAs(dee.Email, "Dee Delegate") + " for " + shownAs(sam.Email, "Sam Owner"),
	} {
		if !strings.Contains(slipped, want) {
			t.Errorf("the Check-in lacks %q:\n%s", want, slipped)
		}
	}
	detail := between(t, slipped, `<details data-testid="checkin-detail"`, "</details>")
	if strings.Contains(openTag(detail), "open") {
		t.Errorf("the Check-in's detail is open on load: %s", openTag(detail))
	}
	for _, want := range []string{"p95 latency: ", "800 ms", "The Red child is a stretch item."} {
		if !strings.Contains(detail, want) {
			t.Errorf("the Check-in's detail lacks %q:\n%s", want, detail)
		}
	}
}

// fmtDay is a date in the current year as the pages write it, "Jul 2".
func fmtDay(d time.Time) string { return d.Format("Jan 2") }

// Every fact the five old history sections showed is on the timeline, with
// people by Name: the Why it matters text and who wrote it, a Handoff's from, to,
// outcome and who started it, a value change and who made it, a Check-in's
// Health, status, Path to Green and author, and the Date Slip it carried.
func TestGoalHistoryKeepsEveryFactOfTheOldSections(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	goal := oneOfEachKind(t, h)
	ts := newServer(t, h)

	entries := historyEntries(historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))))
	if len(entries) != 4 {
		t.Fatalf("History lists %d entries, want 4", len(entries))
	}
	sam, pat := shownAs("sam@example.com", "Sam Owner"), shownAs("pat@example.com", "Pat Next")
	for i, want := range [][]string{
		{">Handoff<", "Jan 11, 15:04", sam + " → " + pat, "pending", "started by " + sam},
		{">Check-in<", "Jan 8, 15:04", "Yellow", "Vendor is late.", "Path to Green: Swap vendors. (by Mar 2)", "by " + sam,
			"<strong>Delivery date</strong>: <del>Jul 2</del> Jul 16", "Vendor API delayed two weeks."},
		{">Value<", "Jan 6, 15:04", "Budget: set to 200", "by " + sam},
		{">Why it matters<", "Jan 2, 15:04", "Customers wait too long.", "by " + sam},
	} {
		for _, part := range want {
			if !strings.Contains(entries[i], part) {
				t.Errorf("entry %d lacks %q:\n%s", i, part, entries[i])
			}
		}
	}
}

// History needs no script: its chips and Show earlier are plain links to the
// Goal page, and nothing in it waits on htmx or a script to show.
func TestGoalHistoryWorksWithoutJavaScript(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	for i := range 21 {
		h.Clock.Advance(time.Hour)
		h.Checkin(sam, goal.ID, domain.HealthGreen, fmt.Sprintf("Update %d.", i), "", time.Time{})
	}
	ts := newServer(t, h)

	history := historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal)))
	for _, banned := range []string{"<script", "hx-", " onclick="} {
		if strings.Contains(history, banned) {
			t.Errorf("History relies on %q:\n%s", banned, history)
		}
	}
	links := strings.Split(between(t, history, `data-testid="history-chips"`, "</nav>"), "<a ")[1:]
	links = append(links, between(t, history, `<a data-testid="history-earlier"`, "</a>"))
	if len(links) != 11 {
		t.Fatalf("History has %d chip and Show earlier links, want 11", len(links))
	}
	for _, link := range links {
		_, href, _ := strings.Cut(link, `href="`)
		if !strings.HasPrefix(href, fmt.Sprintf("/goals/%d", goal.ID)) {
			t.Errorf("a History control isn't a plain link to the Goal page: %s", link)
		}
	}
}

// healthCell is one cell of a Goal page's Health strip: its state (g, y, r,
// no-checkin or blank) and its text equivalent.
type healthCell struct{ state, text string }

// healthCells reads the Health strip on a Goal page, oldest cell first.
func healthCells(t *testing.T, page string) []healthCell {
	t.Helper()
	strip := between(t, page, `data-testid="health-strip"`, "</figure>")
	var out []healthCell
	for _, cell := range strings.Split(strip, `data-testid="health-cell"`)[1:] {
		_, state, _ := strings.Cut(cell, `data-state="`)
		state, _, _ = strings.Cut(state, `"`)
		text := between(t, cell, `<span class="sr-only">`, "</span>")[len(`<span class="sr-only">`):]
		out = append(out, healthCell{state, html.UnescapeString(text)})
	}
	return out
}

// Above its History, a weekly Goal's page shows its Health over the last 11
// weeks, oldest first: each week's Health, and a dashed "no Check-in" cell for
// a week it went without one. Each cell says in text what it shows, and the
// strip sums itself up in one line.
func TestGoalPageShowsHealthStripAboveHistory(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	for week := 0; week < 11; week++ {
		if week > 0 {
			h.Clock.Advance(7 * 24 * time.Hour)
		}
		switch week {
		case 3, 7:
		case 5:
			h.Checkin(sam, goal.ID, domain.HealthYellow, "Slipping.", "Add a reviewer.", h.Clock.Now().AddDate(0, 0, 14))
		default:
			h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
		}
	}
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	if strings.Index(page, `data-testid="health-strip"`) > strings.Index(page, `data-testid="history-chips"`) {
		t.Errorf("the Health strip isn't above the History list")
	}
	cells := healthCells(t, page)
	if len(cells) != 11 {
		t.Fatalf("strip has %d cells, want 11: %v", len(cells), cells)
	}
	monday := time.Date(2025, time.December, 29, 0, 0, 0, 0, time.UTC)
	for i, c := range cells {
		heading := "Week of " + monday.AddDate(0, 0, 7*i).Format("Jan 2")
		if i == 0 {
			heading += ", 2025"
		}
		want := healthCell{"g", heading + ": Green"}
		switch i {
		case 3, 7:
			want = healthCell{"no-checkin", heading + ": no Check-in"}
		case 5:
			want = healthCell{"y", heading + ": Yellow"}
		}
		if c != want {
			t.Errorf("cell %d = %+v, want %+v", i, c, want)
		}
	}
	summary := html.UnescapeString(between(t, page, `data-testid="health-strip-summary"`, "</"))
	if !strings.Contains(summary, "Last 11 weeks: 8 Green, 1 Yellow, 2 with no Check-in") {
		t.Errorf("strip summary = %q", summary)
	}
}

// The weeks before a Goal became Active and those it spent On Hold are blank,
// not dashed: each says why in its text, and the summary counts them apart from
// the weeks with no Check-in.
func TestHealthStripCellsAreBlankWhereNoCheckinWasOwed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	// Activated on Friday 2 Jan, in the week of 29 Dec: the 3rd period from the
	// right once the clock reaches the week of 12 Jan.
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Clock.Advance(7 * 24 * time.Hour)
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: goal.ID, AuthorID: sam.ID, Status: "Pausing.", Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Waiting on legal.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}
	h.Clock.Advance(7 * 24 * time.Hour)
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	cells := healthCells(t, page)
	if len(cells) != 11 {
		t.Fatalf("strip has %d cells, want 11: %v", len(cells), cells)
	}
	for i, c := range cells[:8] {
		if c.state != "blank" || !strings.HasSuffix(c.text, ": not yet Active") {
			t.Errorf("cell %d before activation = %+v, want blank, not yet Active", i, c)
		}
	}
	for i, want := range []healthCell{
		{"no-checkin", "Week of Dec 29, 2025: no Check-in"},
		{"blank", "Week of Jan 5: On Hold"},
		{"blank", "Week of Jan 12: On Hold"},
	} {
		if c := cells[8+i]; c != want {
			t.Errorf("cell %d = %+v, want %+v", 8+i, c, want)
		}
	}
	for _, cell := range strings.Split(between(t, page, `data-testid="health-strip"`, "</figure>"), `data-testid="health-cell"`)[1:] {
		if strings.Contains(cell, `class="dot"`) {
			t.Errorf("a cell with no Health carries a Health's dot: %s", cell)
		}
	}
	summary := html.UnescapeString(between(t, page, `data-testid="health-strip-summary"`, "</"))
	if !strings.Contains(summary, "Last 11 weeks: 1 with no Check-in, 10 not Active.") {
		t.Errorf("strip summary = %q", summary)
	}
}

// The week in progress can't have been missed. A weekly Goal checked in last
// Friday shows this week, on Monday, as not yet due: its own cell, not dashed,
// and counted apart from the weeks with no Check-in.
func TestHealthStripShowsTheWeekInProgressAsNotYetDue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	// Friday 2 Jan 2026, in the week of 29 Dec.
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(3 * 24 * time.Hour)
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	cells := healthCells(t, page)
	if len(cells) != 11 {
		t.Fatalf("strip has %d cells, want 11: %v", len(cells), cells)
	}
	if want := (healthCell{"g", "Week of Dec 29, 2025: Green"}); cells[9] != want {
		t.Errorf("last week's cell = %+v, want %+v", cells[9], want)
	}
	if want := (healthCell{"not-yet-due", "Week of Jan 5: not yet due"}); cells[10] != want {
		t.Errorf("this week's cell = %+v, want %+v", cells[10], want)
	}
	if strings.Contains(between(t, page, `data-state="not-yet-due"`, "</li>"), `class="dot"`) {
		t.Errorf("the not yet due cell carries a Health's dot")
	}
	// An open slot waiting to be filled: an input's solid edge with no fill,
	// unlike a Health's fill or the dashed edge of a week with no Check-in.
	if rule := cssRule(t, page, ".hs-cell.not-yet-due"); !strings.Contains(rule, "border:1px solid var(--color-border-strong)") || strings.Contains(rule, "dashed") {
		t.Errorf("the not yet due cell's style is %q, want a 1px solid --color-border-strong edge", rule)
	}
	summary := html.UnescapeString(between(t, page, `data-testid="health-strip-summary"`, "</"))
	if !strings.Contains(summary, "Last 11 weeks: 1 Green, 1 not yet due, 9 not Active.") {
		t.Errorf("strip summary = %q", summary)
	}
}

// A week ends how its last Check-in leaves the Goal: a Green Check-in followed
// by one that puts it On Hold leaves the week blank, and the week in progress
// is blank too, not "not yet due", since nobody owes a Check-in on paused work.
func TestHealthStripWeekEndingOnHoldIsBlank(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Clock.Advance(7 * 24 * time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: goal.ID, AuthorID: sam.ID, Status: "Pausing.", Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Waiting on legal.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}
	h.Clock.Advance(7 * 24 * time.Hour)
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	cells := healthCells(t, page)
	if len(cells) != 11 {
		t.Fatalf("strip has %d cells, want 11: %v", len(cells), cells)
	}
	for i, want := range []healthCell{
		{"no-checkin", "Week of Dec 29, 2025: no Check-in"},
		{"blank", "Week of Jan 5: On Hold"},
		{"blank", "Week of Jan 12: On Hold"},
	} {
		if c := cells[8+i]; c != want {
			t.Errorf("cell %d = %+v, want %+v", 8+i, c, want)
		}
	}
	summary := html.UnescapeString(between(t, page, `data-testid="health-strip-summary"`, "</"))
	if !strings.Contains(summary, "Last 11 weeks: 1 with no Check-in, 10 not Active.") {
		t.Errorf("strip summary = %q", summary)
	}
}

// A Goal on a 14-day cadence has 14-day periods, each named by its first and
// last days.
func TestHealthStripPeriodsFollowTheGoalsCadence(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	if _, err := h.Service.SetCadence(context.Background(), goal.ID, 14); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	h.Clock.Advance(7 * 24 * time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	cells := healthCells(t, page)
	if len(cells) != 11 {
		t.Fatalf("strip has %d cells, want 11: %v", len(cells), cells)
	}
	if want := (healthCell{"g", "Dec 29, 2025 – Jan 11: Green"}); cells[10] != want {
		t.Errorf("current cell = %+v, want %+v", cells[10], want)
	}
	if want := (healthCell{"blank", "Dec 15, 2025 – Dec 28, 2025: not yet Active"}); cells[9] != want {
		t.Errorf("cell before = %+v, want %+v", cells[9], want)
	}
	summary := html.UnescapeString(between(t, page, `data-testid="health-strip-summary"`, "</"))
	if !strings.Contains(summary, "Last 11 periods of 14 days: 1 Green, 10 not Active.") {
		t.Errorf("strip summary = %q", summary)
	}
}

// A Goal that has never been Active has no strip.
func TestProposedGoalPageHasNoHealthStrip(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Ship v2", "Customers wait too long.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	if strings.Contains(page, `data-testid="health-strip"`) {
		t.Errorf("a Proposed Goal's page shows a Health strip")
	}
}

// replanCheckin arranges an Active Goal titled title with Milestones Beta and
// Rollout to 50% (added from the Goal page, so not in a Check-in), then a Green
// Check-in on it that, with replan, adds GA on gaDate, marks Beta Done and
// removes Rollout to 50% as descoped. Without replan it changes no Milestone.
func replanCheckin(t *testing.T, h *testsupport.Harness, sam domain.Account, title string, replan bool) (goal domain.Goal, gaDate time.Time) {
	t.Helper()
	ctx := context.Background()
	goal = h.ActiveGoal(sam, title, "Customers wait too long.")
	ms, err := h.Service.ListMilestones(ctx, goal.ID)
	if err != nil || len(ms) != 1 {
		t.Fatalf("ListMilestones: %v %v", ms, err)
	}
	rollout, err := h.Service.AddMilestone(ctx, domain.AddMilestoneInput{
		GoalID: goal.ID, Name: "Rollout to 50%", TargetDate: goal.DeliveryDate.AddDate(0, 0, -14),
	})
	if err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	gaDate = goal.DeliveryDate.AddDate(0, 0, -7)
	in := domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Re-planned."}
	if replan {
		in.Milestones = []domain.MilestoneChangeInput{
			{MilestoneID: ms[0].ID, Status: domain.MilestoneDone},
			{MilestoneID: rollout.ID, Status: domain.MilestoneRemoved, RemovedReason: "descoped"},
		}
		in.NewMilestones = []domain.NewMilestoneInput{{Name: "GA", TargetDate: gaDate}}
	}
	if _, err := h.Service.SubmitCheckin(ctx, in); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	return goal, gaDate
}

// A Check-in's entry lists the Milestones it added, marked Done or removed,
// with the removal's reason, beside its Date Slips; the Date Slips chip still
// counts only the Check-ins that slipped a date.
func TestGoalHistoryCheckinShowsItsMilestoneChanges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	goal, gaDate := replanCheckin(t, h, sam, "Ship v2", true)
	ts := newServer(t, h)

	history := historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal)))
	checkin := historyEntries(history)[0]
	if got := entryKind(t, checkin); got != "checkin" {
		t.Fatalf("the newest entry is a %s, want the Check-in", got)
	}
	got := milestoneChangeLines(t, checkin)
	want := []string{
		"Added Milestone GA (" + fmtDay(gaDate) + ")",
		"Marked Beta Done",
		"Removed Rollout to 50%: descoped",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the Check-in's Milestone changes = %q, want %q", got, want)
	}
	if strings.Contains(checkin, `data-testid="entry-slips"`) {
		t.Errorf("a Check-in that slipped no date lists Date Slips:\n%s", checkin)
	}
	if c := historyChips(t, history)["Date Slips"]; c.count != "0" {
		t.Errorf("the Date Slips chip counts %s, want 0", c.count)
	}
}

// milestoneChangeLines returns the text of each Milestone change a Check-in
// entry lists, in order.
func milestoneChangeLines(t *testing.T, checkin string) []string {
	t.Helper()
	changes := html.UnescapeString(between(t, checkin, `data-testid="entry-milestone-changes"`, "</ul>"))
	var lines []string
	for _, li := range strings.Split(changes, `data-testid="milestone-change">`)[1:] {
		text, _, _ := strings.Cut(li, "</li>")
		lines = append(lines, text)
	}
	return lines
}

// Renaming the Milestones a Check-in added, marked Done and removed leaves its
// entry naming them as they were then.
func TestGoalHistoryCheckinKeepsMilestoneNamesThroughARename(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	goal, gaDate := replanCheckin(t, h, sam, "Ship v2", true)
	ms, err := h.Service.ListMilestones(context.Background(), goal.ID)
	if err != nil || len(ms) != 3 {
		t.Fatalf("ListMilestones: %v %v", ms, err)
	}
	renames := map[string]string{"GA": "Launch", "Beta": "Preview", "Rollout to 50%": "Half rollout"}
	for _, m := range ms {
		if _, err := h.Service.EditMilestone(context.Background(), domain.EditMilestoneInput{
			MilestoneID: m.ID, Name: renames[m.Name], TargetDate: m.TargetDate,
		}); err != nil {
			t.Fatalf("EditMilestone %q: %v", m.Name, err)
		}
	}
	ts := newServer(t, h)

	history := historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal)))
	checkin := historyEntries(history)[0]
	if got := entryKind(t, checkin); got != "checkin" {
		t.Fatalf("the newest entry is a %s, want the Check-in", got)
	}
	want := []string{
		"Added Milestone GA (" + fmtDay(gaDate) + ")",
		"Marked Beta Done",
		"Removed Rollout to 50%: descoped",
	}
	if got := milestoneChangeLines(t, checkin); !slices.Equal(got, want) {
		t.Errorf("after the renames the Check-in's Milestone changes = %q, want %q", got, want)
	}
}

// A Check-in that marked GA Done and moved Beta's date, after both are
// renamed, still reads "Marked GA Done", while its Date Slip names the
// Milestone as it is now.
func TestGoalHistoryCheckinKeepsItsMilestoneNameBesideADateSlipThroughARename(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	ms, err := h.Service.ListMilestones(ctx, goal.ID)
	if err != nil || len(ms) != 1 || ms[0].Name != "Beta" {
		t.Fatalf("ListMilestones: %v %v, want just Beta", ms, err)
	}
	beta := ms[0]
	ga, err := h.Service.AddMilestone(ctx, domain.AddMilestoneInput{
		GoalID: goal.ID, Name: "GA", TargetDate: goal.DeliveryDate.AddDate(0, 0, -7),
	})
	if err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	moved := beta.TargetDate.AddDate(0, 0, 10)
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "GA is out; Beta slips.",
		Milestones: []domain.MilestoneChangeInput{
			{MilestoneID: ga.ID, Status: domain.MilestoneDone},
			{MilestoneID: beta.ID, TargetDate: moved, DateReason: "Design review moved."},
		},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	for _, rename := range []struct {
		m    domain.Milestone
		name string
		date time.Time
	}{{ga, "Launch", ga.TargetDate}, {beta, "Preview", moved}} {
		if _, err := h.Service.EditMilestone(ctx, domain.EditMilestoneInput{
			MilestoneID: rename.m.ID, Name: rename.name, TargetDate: rename.date,
		}); err != nil {
			t.Fatalf("EditMilestone %q: %v", rename.m.Name, err)
		}
	}
	ts := newServer(t, h)

	checkin := historyEntries(historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))))[0]
	if got := entryKind(t, checkin); got != "checkin" {
		t.Fatalf("the newest entry is a %s, want the Check-in", got)
	}
	if got, want := milestoneChangeLines(t, checkin), []string{"Marked GA Done"}; !slices.Equal(got, want) {
		t.Errorf("after the renames the Check-in's Milestone changes = %q, want %q", got, want)
	}
	slips := between(t, checkin, `data-testid="entry-slips"`, "</ul>")
	if want := "<strong>Milestone Preview</strong>: <del>" + fmtDay(beta.TargetDate) + "</del> " + fmtDay(moved); !strings.Contains(slips, want) {
		t.Errorf("the Check-in's Date Slip lacks %q:\n%s", want, slips)
	}
	if strings.Contains(slips, "Beta") {
		t.Errorf("the Check-in's Date Slip names Beta by its old name:\n%s", slips)
	}
}

// A Check-in from before Milestone changes were recorded has none to show, so
// its entry renders exactly as one that changed no Milestone: its twin, made at
// the same moment with the same Health and status, on a Goal set up the same.
func TestGoalHistoryCheckinWithoutRecordedChangesRendersAsBefore(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	older, _ := replanCheckin(t, h, sam, "Ship v2", true)
	twin, _ := replanCheckin(t, h, sam, "Ship v3", false)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	checkinEntry := func(goal domain.Goal) string {
		t.Helper()
		for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)))) {
			if entryKind(t, e) == "checkin" {
				return e
			}
		}
		t.Fatalf("no Check-in on %s's History", goal.Title)
		return ""
	}
	if !strings.Contains(checkinEntry(older), `data-testid="entry-milestone-changes"`) {
		t.Fatal("the replanning Check-in shows no Milestone changes before they are deleted")
	}

	if _, err := h.DB.Exec(`DELETE FROM milestone_changes WHERE checkin_id IN (SELECT id FROM checkins WHERE goal_id = ?)`, older.ID); err != nil {
		t.Fatalf("delete milestone changes: %v", err)
	}
	if got, want := checkinEntry(older), checkinEntry(twin); got != want {
		t.Errorf("a Check-in without recorded changes renders\n%s\nwant, as one that changed no Milestone,\n%s", got, want)
	}
}

// Through the Check-in form, a Check-in that fails validation records none of
// its Milestone changes; resubmitted valid, its entry shows all three. The
// Goal's Milestone Churn counts the same either way, and the same again once
// the recorded changes are gone, as for an older Check-in.
func TestCheckinFormRecordsMilestoneChangesOnlyWhenValid(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	ms, err := h.Service.ListMilestones(ctx, goal.ID)
	if err != nil || len(ms) != 1 {
		t.Fatalf("ListMilestones: %v %v", ms, err)
	}
	beta := ms[0]
	rollout, err := h.Service.AddMilestone(ctx, domain.AddMilestoneInput{
		GoalID: goal.ID, Name: "Rollout to 50%", TargetDate: goal.DeliveryDate.AddDate(0, 0, -14),
	})
	if err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	form := url.Values{
		"health":             {domain.HealthYellow}, // with no Path to Green
		"status":             {"Re-planned."},
		"new_milestone_name": {"GA"},
		"new_milestone_date": {"2026-05-01"},
		fmt.Sprintf("milestone_status_%d", beta.ID):            {domain.MilestoneDone},
		fmt.Sprintf("milestone_status_%d", rollout.ID):         {domain.MilestoneRemoved},
		fmt.Sprintf("milestone_removed_reason_%d", rollout.ID): {"descoped"},
	}
	checkinURL := fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID)
	churn := func(page string) string {
		t.Helper()
		return between(t, page, `data-testid="goal-milestone-churn">`, "<")[len(`data-testid="goal-milestone-churn">`):]
	}

	postForm(t, client, checkinURL, form)
	page := getBody(t, client, goalPageURL(ts.URL, goal))
	if strings.Contains(historyBlock(t, page), `data-testid="milestone-change"`) {
		t.Errorf("a rejected Check-in shows Milestone changes:\n%s", historyBlock(t, page))
	}
	if got := churn(page); got != "1" {
		t.Errorf("churn after the rejection = %s, want 1 (Rollout to 50%% added while Active)", got)
	}

	form.Set("health", domain.HealthGreen)
	if resp := postForm(t, client, checkinURL, form); resp.StatusCode != http.StatusOK {
		t.Fatalf("valid Check-in: status %d", resp.StatusCode)
	}
	page = getBody(t, client, goalPageURL(ts.URL, goal))
	entries := historyEntries(historyBlock(t, page))
	changes := html.UnescapeString(between(t, entries[0], `data-testid="entry-milestone-changes"`, "</ul>"))
	for _, want := range []string{"Added Milestone GA (May 1)", "Marked Beta Done", "Removed Rollout to 50%: descoped"} {
		if !strings.Contains(changes, want) {
			t.Errorf("the Check-in's Milestone changes lack %q:\n%s", want, changes)
		}
	}
	if got := churn(page); got != "3" {
		t.Errorf("churn = %s, want 3 (Rollout to 50%% and GA added, Rollout to 50%% removed)", got)
	}

	if _, err := h.DB.Exec(`DELETE FROM milestone_changes`); err != nil {
		t.Fatalf("delete milestone changes: %v", err)
	}
	if got := churn(getBody(t, client, goalPageURL(ts.URL, goal))); got != "3" {
		t.Errorf("churn without recorded changes = %s, want still 3", got)
	}
}

// Each Parent suggestion is a History entry saying who suggested which parent,
// then its outcome and when, or that it's still open. They're listed under
// All and Links only.
func TestGoalHistoryShowsEachParentSuggestionAndItsOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	goal := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	suggest := func(title string) domain.ParentSuggestion {
		t.Helper()
		s, err := h.Service.SuggestParent(ctx, pat.ID, goal.ID, h.CreateGoal(pat, title, "It matters.").ID, "")
		if err != nil {
			t.Fatalf("SuggestParent: %v", err)
		}
		return s
	}
	declined := suggest("Reduce outages")
	h.Clock.Advance(24 * time.Hour) // Sat 3 Jan
	if _, err := h.Service.DeclineParentSuggestion(ctx, sam.ID, declined.ID); err != nil {
		t.Fatalf("DeclineParentSuggestion: %v", err)
	}
	accepted := suggest("Cut costs")
	h.Clock.Advance(24 * time.Hour) // Sun 4 Jan
	if _, err := h.Service.AcceptParentSuggestion(ctx, sam.ID, accepted.ID); err != nil {
		t.Fatalf("AcceptParentSuggestion: %v", err)
	}
	open := suggest("Grow revenue")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	var got []string
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)+"?shown=25"))) {
		if entryKind(t, e) == "parent-suggestion" {
			got = append(got, html.UnescapeString(e))
		}
	}
	for i, want := range []struct {
		parent  domain.ParentSuggestion
		outcome string
	}{
		{open, "Still open"},
		{accepted, "Accepted Jan 4, 15:04"},
		{declined, "Declined Jan 3, 15:04"},
	} {
		if i >= len(got) {
			t.Fatalf("History has %d suggestions, want 3", len(got))
		}
		for _, fact := range []string{"Parent suggestion", "Pat Lee", "suggested", navTo(want.parent.Parent.ID), want.outcome} {
			if !strings.Contains(got[i], fact) {
				t.Errorf("suggestion %d lacks %q:\n%s", i, fact, got[i])
			}
		}
	}

	for _, filter := range []string{"checkins", "date-slips", "why-it-matters", "title", "ownership", "values"} {
		page := getBody(t, client, goalPageURL(ts.URL, goal)+"?history="+filter)
		if strings.Contains(historyBlock(t, page), `data-kind="parent-suggestion"`) {
			t.Errorf("the %s chip lists a suggestion", filter)
		}
	}
}

// Each Nudge is a History entry saying who nudged for a Check-in, listed under
// Check-ins and All and under no other chip.
func TestGoalHistoryShowsEachNudge(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	goal := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * 24 * time.Hour)
	if _, err := h.Service.Nudge(context.Background(), pat.ID, goal.ID); err != nil {
		t.Fatalf("Nudge: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := goalPageURL(ts.URL, goal)

	nudgesUnder := func(filter string) []string {
		t.Helper()
		var out []string
		for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL+"?history="+filter))) {
			if entryKind(t, e) == "nudge" {
				out = append(out, html.UnescapeString(e))
			}
		}
		return out
	}
	for _, filter := range []string{"", "checkins"} {
		nudges := nudgesUnder(filter)
		if len(nudges) != 1 {
			t.Fatalf("History under %q lists %d Nudges, want 1", filter, len(nudges))
		}
		e := nudges[0]
		for _, fact := range []string{"Nudge", "Pat Lee", "nudged for a Check-in", `class="tl-mark tl-nudge"`} {
			if !strings.Contains(e, fact) {
				t.Errorf("the Nudge under %q lacks %q:\n%s", filter, fact, e)
			}
		}
	}
	for _, filter := range []string{"date-slips", "why-it-matters", "title", "ownership", "values"} {
		if n := len(nudgesUnder(filter)); n != 0 {
			t.Errorf("the %s chip lists %d Nudges", filter, n)
		}
	}
}

// entryText is an entry's text as it reads, spaces collapsed: a link's tags
// are dropped, and every other tag is a space.
func entryText(entry string) string {
	text := regexp.MustCompile(`</?a\b[^>]*>`).ReplaceAllString(entry, "")
	text = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(text, " ")
	return strings.Join(strings.Fields(html.UnescapeString(text)), " ")
}

// A link's request, acceptance, removal and the Undo of that removal are each
// an entry in both Goals' History, newest first, saying who did it and phrased
// for the Goal it's on, with the other Goal linked.
func TestGoalHistoryShowsLinkChangesOnBothGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	patClient := signInClient(t, ts.URL, "pat@example.com")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	h.Clock.Advance(time.Hour)
	if resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/links", ts.URL, child.ID), url.Values{
		"parent_id": {fmt.Sprint(parent.ID)},
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("request link: status %d", resp.StatusCode)
	}
	pending, err := h.Service.PendingLinkRequests(context.Background(), pat.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("PendingLinkRequests = %+v, %v", pending, err)
	}
	h.Clock.Advance(time.Hour)
	if resp := postForm(t, patClient, fmt.Sprintf("%s/links/%d/accept", ts.URL, pending[0].ID), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("accept link: status %d", resp.StatusCode)
	}
	h.Clock.Advance(time.Hour)
	resp := postForm(t, patClient, fmt.Sprintf("%s/links/%d/remove", ts.URL, pending[0].ID), url.Values{"goal_id": {fmt.Sprint(parent.ID)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove link: status %d", resp.StatusCode)
	}
	landed := readBody(t, resp)
	h.Clock.Advance(time.Minute)
	if resp := postForm(t, patClient, ts.URL+undoAction(t, landed), toastFields(t, landed)); resp.StatusCode != http.StatusOK {
		t.Fatalf("undo removal: status %d", resp.StatusCode)
	}

	for _, side := range []struct {
		goal, other domain.Goal
		want        []string
	}{
		{child, parent, []string{
			"Undid: No longer contributes to Reduce outages by Pat Lee",
			"No longer contributes to Reduce outages by Pat Lee",
			"Reduce outages accepted this as a contributor by Pat Lee",
			"Requested to contribute to Reduce outages by Sam Owner",
		}},
		{parent, child, []string{
			"Undid: Migrate displays no longer contributes to this by Pat Lee",
			"Migrate displays no longer contributes to this by Pat Lee",
			"Accepted Migrate displays as a contributor by Pat Lee",
			"Migrate displays asked to contribute to this by Sam Owner",
		}},
	} {
		var got []string
		for _, e := range historyEntries(historyBlock(t, getBody(t, samClient, goalPageURL(ts.URL, side.goal)))) {
			if entryKind(t, e) != "link" {
				continue
			}
			if !strings.Contains(e, navTo(side.other.ID)) {
				t.Errorf("on %s, an entry doesn't link to %s:\n%s", side.goal.Title, side.other.Title, e)
			}
			got = append(got, entryText(e))
		}
		if len(got) != len(side.want) {
			t.Fatalf("on %s, link entries = %q, want %d", side.goal.Title, got, len(side.want))
		}
		for i, want := range side.want {
			if !strings.Contains(got[i], want) {
				t.Errorf("on %s, entry %d = %q, want it to say %q", side.goal.Title, i, got[i], want)
			}
		}
	}
}

// linkEntries are the link changes on g's History, newest first, as they read.
func linkEntries(t *testing.T, client *http.Client, base string, g domain.Goal) []string {
	t.Helper()
	var out []string
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalPageURL(base, g)))) {
		if entryKind(t, e) == "link" {
			out = append(out, entryText(e))
		}
	}
	return out
}

// A rejected request, the Undo of the rejection and a link made without a
// request read on each side too: the child's Owner linking it under their own
// Goal shows as it contributing.
func TestGoalHistoryPhrasesRejectionsAndLinksForEachSide(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	own := h.CreateGoal(sam, "Fix kiosks", "Kiosks fail often.")
	h.RequestLink(sam, own, child, "")
	link := h.RequestLink(sam, child, parent, "")
	h.Clock.Advance(time.Hour)
	rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
	if err != nil {
		t.Fatalf("RejectLink: %v", err)
	}
	h.Clock.Advance(time.Minute)
	if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken); err != nil {
		t.Fatalf("RestoreLinkRequest: %v", err)
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, side := range []struct {
		goal domain.Goal
		want []string
	}{
		{child, []string{
			"Undid: Reduce outages declined the request to contribute to it by Pat Lee",
			"Reduce outages declined the request to contribute to it by Pat Lee",
			"Requested to contribute to Reduce outages by Sam Owner",
			"Fix kiosks contributes to this by Sam Owner",
		}},
		{parent, []string{
			"Undid: Declined Migrate displays's request to contribute to this by Pat Lee",
			"Declined Migrate displays's request to contribute to this by Pat Lee",
			"Migrate displays asked to contribute to this by Sam Owner",
		}},
		{own, []string{"Contributes to Migrate displays by Sam Owner"}},
	} {
		got := linkEntries(t, client, ts.URL, side.goal)
		if len(got) != len(side.want) {
			t.Fatalf("on %s, link entries = %q, want %d", side.goal.Title, got, len(side.want))
		}
		for i, want := range side.want {
			if !strings.Contains(got[i], want) {
				t.Errorf("on %s, entry %d = %q, want it to say %q", side.goal.Title, i, got[i], want)
			}
		}
	}
}

// The Links chip, after Ownership, lists a Goal's link changes and its Parent
// suggestions and nothing else, and counts them; no other chip but All lists
// either.
func TestGoalHistoryLinksChipListsLinkChangesAndParentSuggestions(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	goal := oneOfEachKind(t, h)
	pat := h.SignIn("pat@example.com")
	h.Clock.Advance(time.Hour)
	h.RequestLink(goal.Owner, goal, h.CreateGoal(goal.Owner, "Grow revenue", "It matters."), "")
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.SuggestParent(context.Background(), pat.ID, goal.ID, h.CreateGoal(pat, "Cut costs", "It matters.").ID, ""); err != nil {
		t.Fatalf("SuggestParent: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := goalPageURL(ts.URL, goal)

	history := historyBlock(t, getBody(t, client, goalURL))
	var labels []string
	for _, chip := range strings.Split(between(t, history, `data-testid="history-chips"`, "</nav>"), `data-testid="history-chip"`)[1:] {
		labels = append(labels, strings.TrimSpace(between(t, chip, ">", "<span")[1:]))
	}
	if want := []string{"All", "Check-ins", "Date Slips", "Why it matters", "Title", "Ownership", "Links", "Values", "Action Items", "Comments"}; !slices.Equal(labels, want) {
		t.Errorf("chips = %v, want %v", labels, want)
	}
	if got := historyChips(t, history)["Links"]; got.count != "2" || filterOf(got.href) != "links" {
		t.Errorf("Links chip = %+v, want 2 to ?history=links", got)
	}

	var kinds []string
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL+"?history=links"))) {
		kinds = append(kinds, entryKind(t, e))
	}
	if want := []string{"parent-suggestion", "link"}; !slices.Equal(kinds, want) {
		t.Errorf("?history=links lists %v, want %v", kinds, want)
	}

	for _, filter := range []string{"checkins", "date-slips", "why-it-matters", "title", "ownership", "values"} {
		for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL+"?history="+filter))) {
			if kind := entryKind(t, e); kind == "link" || kind == "parent-suggestion" {
				t.Errorf("the %s chip lists a %s entry", filter, kind)
			}
		}
	}
}

// A Check-in's History entry lists the Draft Highlights it discarded under
// "Discarded Draft Highlights", each with its kind and note, for everyone who
// can see the History; one that discarded none shows nothing extra, and one
// deleted by hand appears nowhere (CONTEXT.md: Draft Highlight).
func TestGoalHistoryCheckinListsTheDraftHighlightsItDiscarded(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "Nothing to discard.", "", time.Time{})
	h.Clock.Advance(time.Hour)
	kept := h.LogDraftHighlight(sam, goal.ID, domain.HighlightAccomplishment, "Kept as a Highlight.")
	vendor := h.LogDraftHighlight(sam, goal.ID, "", "Vendor may raise prices.")
	runbook := h.LogDraftHighlight(sam, goal.ID, domain.HighlightMiss, "Runbook was late.")
	deleted := h.LogDraftHighlight(sam, goal.ID, domain.HighlightInsight, "Deleted by hand.")
	if _, err := h.Service.DeleteDraftHighlight(context.Background(), sam.ID, deleted.ID); err != nil {
		t.Fatalf("DeleteDraftHighlight: %v", err)
	}
	h.CheckinWithHighlights(sam, goal.ID,
		domain.HighlightInput{DraftHighlightID: kept.ID, Kind: kept.Kind, Note: kept.Note},
		domain.HighlightInput{DraftHighlightID: vendor.ID, LeftOut: true},
		domain.HighlightInput{DraftHighlightID: runbook.ID, Kind: runbook.Kind, Note: ""},
	)

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), goalPageURL(ts.URL, goal))
	entries := historyEntries(historyBlock(t, page))
	discarding, earlier := entries[0], entries[1]
	block := between(t, discarding, `data-testid="entry-discarded-drafts"`, "</ul>")
	if !strings.Contains(block, "Discarded Draft Highlights") {
		t.Errorf("the discarded Draft Highlights aren't headed as such:\n%s", block)
	}
	want := []string{"No kind: Vendor may raise prices.", "Miss: Runbook was late."}
	if got := elementTexts(block, "li", "discarded-draft-highlight"); !slices.Equal(got, want) {
		t.Errorf("discarded = %q, want %q", got, want)
	}
	for _, absent := range []string{"Kept as a Highlight.", "Deleted by hand."} {
		if strings.Contains(block, absent) {
			t.Errorf("the discarded list shows %q", absent)
		}
	}
	if strings.Contains(earlier, "entry-discarded-drafts") || strings.Contains(earlier, "Discarded Draft Highlights") {
		t.Errorf("a Check-in that discarded none shows a discarded list:\n%s", earlier)
	}
	if strings.Contains(page, "Deleted by hand.") {
		t.Errorf("a Draft Highlight deleted by hand appears on the Goal page")
	}
}

// A Milestone added from the Goal page while the Goal is Active is its own
// History entry, saying who added which Milestone with its date, listed under
// Check-ins and All and under no other chip. One added while the Goal is
// Proposed is part of planning it, and has none.
func TestGoalHistoryShowsAMilestoneAddedOutsideACheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dana := h.SignInNamed("dana@example.com", "Dana Reyes")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	h.AddDelegate(sam, dana, goal.ID)
	h.Clock.Advance(3 * 24 * time.Hour)
	ts := newServer(t, h)
	postForm(t, signInClient(t, ts.URL, "dana@example.com"), fmt.Sprintf("%s/goals/%d/milestones", ts.URL, goal.ID),
		url.Values{"name": {"GA"}, "target_date": {"2026-05-01"}})
	client := signInClient(t, ts.URL, "sam@example.com")
	postForm(t, client, fmt.Sprintf("%s/goals/%d/milestones", ts.URL, proposed.ID),
		url.Values{"name": {"Pricing page"}, "target_date": {"2026-04-01"}})

	milestonesUnder := func(g domain.Goal, filter string) []string {
		t.Helper()
		var out []string
		for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalPageURL(ts.URL, g)+"?history="+filter))) {
			if entryKind(t, e) == "milestone" {
				out = append(out, e)
			}
		}
		return out
	}
	for _, filter := range []string{"", "checkins"} {
		added := milestonesUnder(goal, filter)
		if len(added) != 1 {
			t.Fatalf("History under %q lists %d Milestone additions, want 1", filter, len(added))
		}
		e := added[0]
		text := entryText(e)
		for _, fact := range []string{
			"Milestone " + h.Clock.Now().Format("Jan 2, 15:04"),
			"Dana Reyes dana@example.com added Milestone GA (May 1)",
		} {
			if !strings.Contains(text, fact) {
				t.Errorf("the addition under %q lacks %q:\n%s", filter, fact, text)
			}
		}
		if !strings.Contains(e, `class="tl-mark tl-milestone"`) {
			t.Errorf("the addition under %q has no marker of its own:\n%s", filter, e)
		}
	}
	for _, filter := range []string{"date-slips", "why-it-matters", "title", "ownership", "links", "values"} {
		if n := len(milestonesUnder(goal, filter)); n != 0 {
			t.Errorf("the %s chip lists %d Milestone additions", filter, n)
		}
	}
	if n := len(milestonesUnder(proposed, "")); n != 0 {
		t.Errorf("a Proposed Goal's History lists %d Milestone additions, want none", n)
	}
}

// Defining a Goal, new on the New goal page or Proposed on its define page,
// adds its Milestones as part of planning it: they are listed, and its
// History has no entry for them.
func TestDefiningAGoalLeavesNoMilestoneHistoryOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, client, ts.URL+"/goals/new", url.Values{
		"title": {"Cut checkout latency"}, "why_it_matters": {"Shoppers abandon slow carts."},
		"milestones[0].name": {"Profile checkout"}, "milestones[0].date": {"2026-11-02"},
	})
	created := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("New goal answered %d", resp.StatusCode)
	}
	resp = definePost(t, client, ts.URL, proposed, func(f url.Values) {
		f.Set("milestones[0].name", "Pricing page")
		f.Set("milestones[0].date", "2026-04-01")
	})
	defined := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("define answered %d", resp.StatusCode)
	}

	for name, tc := range map[string]struct{ page, milestone string }{
		"new":      {created, "Profile checkout"},
		"proposed": {defined, "Pricing page"},
	} {
		rows := elementTexts(pageElement(t, tc.page, "section", "goal-milestones"), "tr", "goal-milestone")
		if !slices.ContainsFunc(rows, func(r string) bool { return strings.Contains(r, tc.milestone) }) {
			t.Errorf("%s Goal's Milestones read %q, want %s", name, rows, tc.milestone)
		}
		for _, e := range historyEntries(historyBlock(t, tc.page)) {
			if entryKind(t, e) == "milestone" {
				t.Errorf("%s Goal's History has a Milestone entry:\n%s", name, e)
			}
		}
	}
}

// Each change to a Goal's title is a History entry, listed and counted under
// the Title chip, which sits after Why it matters and lists nothing else, and under
// All; the Why it matters chip doesn't list it (#281).
func TestGoalHistoryTitleChipListsOnlyTitleChanges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	goal := oneOfEachKind(t, h)
	sam := goal.Owner
	for _, title := range []string{"Ship v2 by spring", "Ship v2 by summer"} {
		h.Clock.Advance(time.Hour)
		if _, err := h.Service.EditTitle(context.Background(), goal.ID, title, sam.ID); err != nil {
			t.Fatalf("EditTitle: %v", err)
		}
	}
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.EditWhyItMatters(context.Background(), goal.ID, "Customers wait far too long.", sam.ID); err != nil {
		t.Fatalf("EditWhyItMatters: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := goalPageURL(ts.URL, goal)

	chips := historyChips(t, historyBlock(t, getBody(t, client, goalURL)))
	if got := chips["Title"]; got.count != "2" || filterOf(got.href) != "title" {
		t.Errorf("Title chip = %+v, want 2 to ?history=title", got)
	}
	if got := chips["Why it matters"]; got.count != "2" {
		t.Errorf("Why it matters chip counts %s, want 2: the first Why it matters and its edit", got.count)
	}
	if got := chips["All"]; got.count != "7" {
		t.Errorf("All chip counts %s, want 7: the four of each kind, the two title changes and the Why it matters edit", got.count)
	}

	titles := filteredEntries(t, client, goalURL, "title", "title")
	want := []string{"Ship v2 by spring → Ship v2 by summer", "Ship v2 → Ship v2 by spring"}
	if len(titles) != len(want) {
		t.Fatalf("?history=title lists %d entries, want %d", len(titles), len(want))
	}
	for i, w := range want {
		if !strings.Contains(titles[i], w) {
			t.Errorf("title entry %d doesn't read %q:\n%s", i, w, titles[i])
		}
	}
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL+"?history=why-it-matters"))) {
		if kind := entryKind(t, e); kind != "why-it-matters" {
			t.Errorf("the Why it matters chip lists a %s entry", kind)
		}
	}
}

// noChangeCheckin submits a No change Check-in on goal as author, failing the
// test on error.
func noChangeCheckin(t *testing.T, h *testsupport.Harness, author domain.Account, goal domain.Goal) domain.Checkin {
	t.Helper()
	c, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, author.ID)
	if err != nil {
		t.Fatalf("SubmitNoChangeCheckin: %v", err)
	}
	return c
}

// foldedCount is how many Check-ins an entry folds, or "" for a full entry.
func foldedCount(entry string) string {
	_, rest, ok := strings.Cut(openTagOf(entry), `data-folded="`)
	if !ok {
		return ""
	}
	n, _, _ := strings.Cut(rest, `"`)
	return n
}

// openTagOf is the rest of an entry's opening tag, after its test ID.
func openTagOf(entry string) string {
	tag, _, _ := strings.Cut(entry, ">")
	return tag
}

// A Check-in followed by three No change Check-ins shows in full, under one
// "No change ×3" entry at the newest one's place: a Check-in entry with the
// Health it repeats, the newest one's time as its one time element, and a
// disclosure listing each folded Check-in's time and who wrote it for whom.
// The Check-ins chip still counts all four.
func TestGoalHistoryFoldsNoChangeCheckinsIntoOneEntry(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignInNamed("sam@example.com", "Sam Owner")
	dee := h.SignInNamed("dee@example.com", "Dee Delegate")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.AddDelegate(sam, dee, goal.ID)
	h.Clock.Advance(24 * time.Hour) // Sat 3 Jan
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Vendor is late.", "Swap vendors.", testsupport.Epoch.AddDate(0, 2, 0))
	for _, who := range []domain.Account{sam, dee, sam} { // Sat 10, 17 and 24 Jan
		h.Clock.Advance(7 * 24 * time.Hour)
		noChangeCheckin(t, h, who, goal)
	}
	ts := newServer(t, h)

	history := historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal)))
	entries := historyEntries(history)
	if len(entries) != 3 {
		t.Fatalf("History lists %d entries, want the folded one, the full Check-in and the Why it matters:\n%s", len(entries), history)
	}
	folded, full := entries[0], entries[1]
	if kind, n := entryKind(t, folded), foldedCount(folded); kind != "checkin" || n != "3" {
		t.Errorf("the latest entry is a %s folding %q, want a Check-in folding 3:\n%s", kind, n, folded)
	}
	if text := entryText(folded); !strings.Contains(text, "No change ×3") || !strings.Contains(text, "Yellow") {
		t.Errorf("the folded entry reads %q, want No change ×3 and its Yellow", text)
	}
	if !strings.Contains(folded, `<span class="tl-mark y" aria-hidden="true">`) {
		t.Errorf("the folded entry lacks the Yellow marker:\n%s", folded)
	}
	if times := regexp.MustCompile(`<time[ >]`).FindAllString(folded, -1); len(times) != 1 {
		t.Errorf("the folded entry has %d time elements, want 1:\n%s", len(times), folded)
	}
	if at := between(t, folded, "<time", "</time>"); !strings.Contains(at, `datetime="2026-01-24T15:04:05Z"`) || !strings.Contains(at, "Jan 24, 15:04") {
		t.Errorf("the folded entry is dated %s, want its newest Check-in's Jan 24, 15:04", at)
	}
	listed := between(t, folded, `data-testid="folded-checkins"`, "</details>")
	samBy, deeBy := "by "+shownAs(sam.Email, "Sam Owner"), "by "+shownAs(dee.Email, "Dee Delegate")+" for "+shownAs(sam.Email, "Sam Owner")
	lines := strings.Split(listed, "<li")[1:]
	if len(lines) != 3 {
		t.Fatalf("the disclosure lists %d Check-ins, want 3:\n%s", len(lines), listed)
	}
	for i, want := range []struct{ at, by string }{
		{"Jan 24, 15:04", samBy},
		{"Jan 17, 15:04", deeBy},
		{"Jan 10, 15:04", samBy},
	} {
		if !strings.Contains(lines[i], want.at) || !strings.Contains(lines[i], want.by) {
			t.Errorf("folded Check-in %d lacks %q %q:\n%s", i, want.at, want.by, lines[i])
		}
	}
	if foldedCount(full) != "" || !strings.Contains(full, "Vendor is late.") || !strings.Contains(full, "Jan 3, 15:04") {
		t.Errorf("the repeated Check-in doesn't show in full:\n%s", full)
	}
	weeks := strings.Split(history, `data-testid="history-week"`)[1:]
	if len(weeks) != 2 || !strings.Contains(weeks[0], "Week of Jan 19") || !strings.Contains(weeks[0], `data-folded="3"`) {
		t.Errorf("the folded entry isn't under its newest Check-in's week, Jan 19:\n%s", history)
	}
	if chips := historyChips(t, history); chips["Check-ins"].count != "4" || chips["All"].count != "5" {
		t.Errorf("chips count %s Check-ins and %s in all, want 4 and 5", chips["Check-ins"].count, chips["All"].count)
	}
}

// A single No change after a Check-in reads "No change", without a count.
func TestGoalHistoryFoldsASingleNoChange(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Clock.Advance(24 * time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(7 * 24 * time.Hour)
	noChangeCheckin(t, h, sam, goal)
	ts := newServer(t, h)

	entries := historyEntries(historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))))
	if len(entries) != 3 || foldedCount(entries[0]) != "1" {
		t.Fatalf("History doesn't lead with one folded No change: %d entries", len(entries))
	}
	if text := entryText(entries[0]); !strings.Contains(text, "No change") || strings.Contains(text, "×") {
		t.Errorf("the folded entry reads %q, want No change without a count", text)
	}
}

// foldedCounts are how many Check-ins each entry on the Goal's first page of
// History folds, "" for a full entry.
func foldedCounts(t *testing.T, client *http.Client, goalURL string) []string {
	t.Helper()
	var out []string
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL))) {
		out = append(out, foldedCount(e))
	}
	return out
}

// A Check-in that records something more than the one before it — a Date
// Slip, a Highlight, a Milestone change or a Metric reading that differs from
// the one before — shows in full and ends the run of repeats: the No change
// before it and the one after it each fold alone. A reading equal to the one
// before ends nothing, and all three fold under the first Check-in.
func TestGoalHistoryCheckinThatRecordsMoreEndsTheRun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		more   func(in *domain.SubmitCheckinInput, goal domain.Goal, metric domain.Metric)
		folded []string
	}{
		{"date slip", func(in *domain.SubmitCheckinInput, goal domain.Goal, _ domain.Metric) {
			in.DeliveryDate, in.DeliveryDateReason = goal.DeliveryDate.AddDate(0, 0, 14), "Vendor API delayed."
		}, []string{"1", "", "1", "", ""}},
		{"highlight", func(in *domain.SubmitCheckinInput, _ domain.Goal, _ domain.Metric) {
			in.Highlights = []domain.HighlightInput{{Kind: domain.HighlightAccomplishment, Note: "Beta shipped."}}
		}, []string{"1", "", "1", "", ""}},
		{"milestone change", func(in *domain.SubmitCheckinInput, _ domain.Goal, _ domain.Metric) {
			in.NewMilestones = []domain.NewMilestoneInput{{Name: "GA", TargetDate: testsupport.Epoch.AddDate(0, 3, 0)}}
		}, []string{"1", "", "1", "", ""}},
		{"different reading", func(in *domain.SubmitCheckinInput, _ domain.Goal, m domain.Metric) {
			in.Readings = []domain.MetricReadingInput{{MetricID: m.ID, Value: 900}}
		}, []string{"1", "", "1", "", ""}},
		{"equal reading", func(in *domain.SubmitCheckinInput, _ domain.Goal, m domain.Metric) {
			in.Readings = []domain.MetricReadingInput{{MetricID: m.ID, Value: 800}}
		}, []string{"3", "", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			ctx := context.Background()
			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
			latency, err := h.Service.AddMetric(ctx, domain.AddMetricInput{
				GoalID: goal.ID, Name: "p95 latency", Unit: "ms", Direction: domain.MetricDown,
				Baseline: 1000, Target: 400, TargetDate: testsupport.Epoch.AddDate(0, 6, 0),
			})
			if err != nil {
				t.Fatalf("AddMetric: %v", err)
			}
			checkin := func(in domain.SubmitCheckinInput) {
				t.Helper()
				h.Clock.Advance(7 * 24 * time.Hour)
				in.GoalID, in.AuthorID, in.Health, in.Status = goal.ID, sam.ID, domain.HealthYellow, "Vendor is late."
				in.PathToGreen, in.PathTargetDate = "Swap vendors.", testsupport.Epoch.AddDate(0, 3, 0)
				if _, err := h.Service.SubmitCheckin(ctx, in); err != nil {
					t.Fatalf("SubmitCheckin: %v", err)
				}
			}
			checkin(domain.SubmitCheckinInput{Readings: []domain.MetricReadingInput{{MetricID: latency.ID, Value: 800}}})
			h.Clock.Advance(7 * 24 * time.Hour)
			noChangeCheckin(t, h, sam, goal)
			var more domain.SubmitCheckinInput
			tc.more(&more, goal, latency)
			checkin(more)
			h.Clock.Advance(7 * 24 * time.Hour)
			noChangeCheckin(t, h, sam, goal)
			ts := newServer(t, h)

			got := foldedCounts(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
			if !slices.Equal(got, tc.folded) {
				t.Errorf("History's entries fold %q, want %q", got, tc.folded)
			}
		})
	}
}

// Any other entry between two identical Check-ins keeps them apart: with a
// Nudge between them, neither folds.
func TestGoalHistoryNudgeBetweenIdenticalCheckinsKeepsThemApart(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	lee := h.SignIn("lee@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Clock.Advance(24 * time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(10 * 24 * time.Hour) // Check-in overdue
	if _, err := h.Service.Nudge(context.Background(), lee.ID, goal.ID); err != nil {
		t.Fatalf("Nudge: %v", err)
	}
	h.Clock.Advance(24 * time.Hour)
	noChangeCheckin(t, h, sam, goal)
	ts := newServer(t, h)

	client := signInClient(t, ts.URL, "sam@example.com")
	for _, filter := range []string{"", "?history=checkins"} {
		got := foldedCounts(t, client, goalPageURL(ts.URL, goal)+filter)
		if want := map[string][]string{"": {"", "", "", ""}, "?history=checkins": {"", "", ""}}[filter]; !slices.Equal(got, want) {
			t.Errorf("History%s folds %q, want %q", filter, got, want)
		}
	}
}

// A folded run takes one of the first page's 5 places: a Check-in, three No
// change Check-ins and four Check-ins after them open on the four and the
// folded run, with Show earlier leading to the full Check-in and the Why it matters.
func TestGoalHistoryFoldedRunIsOneOfTheFirstFive(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Clock.Advance(time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	for range 3 {
		h.Clock.Advance(time.Hour)
		noChangeCheckin(t, h, sam, goal)
	}
	for i := 1; i <= 4; i++ {
		h.Clock.Advance(time.Hour)
		h.Checkin(sam, goal.ID, domain.HealthGreen, fmt.Sprintf("Update %d.", i), "", time.Time{})
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	history := historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)))
	if got, want := foldedCounts(t, client, goalPageURL(ts.URL, goal)), []string{"", "", "", "", "3"}; !slices.Equal(got, want) {
		t.Fatalf("History first folds %q, want %q", got, want)
	}
	history = historyBlock(t, getBody(t, client, ts.URL+historyEarlier(t, history)))
	if n := len(historyEntries(history)); n != 7 {
		t.Errorf("Show earlier shows %d entries, want 7", n)
	}
	if strings.Contains(history, `data-testid="history-earlier"`) {
		t.Errorf("Show earlier stays once everything shows")
	}
	if chips := historyChips(t, history); chips["All"].count != "9" {
		t.Errorf("the All chip counts %s, want 9", chips["All"].count)
	}
}

// Whether Check-ins fold depends on what the filter lists: two identical
// Check-ins with a Why it matters revision between them show in full under All, and
// fold under Check-ins, where the revision isn't listed.
func TestGoalHistoryFoldsAcrossEntriesTheFilterLeavesOut(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Clock.Advance(time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.EditWhyItMatters(context.Background(), goal.ID, "Customers leave.", sam.ID); err != nil {
		t.Fatalf("EditWhyItMatters: %v", err)
	}
	h.Clock.Advance(time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	if got, want := foldedCounts(t, client, goalPageURL(ts.URL, goal)), []string{"", "", "", ""}; !slices.Equal(got, want) {
		t.Errorf("History under All folds %q, want %q", got, want)
	}
	if got, want := foldedCounts(t, client, goalPageURL(ts.URL, goal)+"?history=checkins"), []string{"1", ""}; !slices.Equal(got, want) {
		t.Errorf("History under Check-ins folds %q, want %q", got, want)
	}
}

// Show earlier counts places, not Check-ins: a Check-in and five No change
// Check-ins after it are seven entries but three places, all on the first page.
func TestGoalHistoryShowEarlierCountsFoldedRunsOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Clock.Advance(time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	for range 5 {
		h.Clock.Advance(time.Hour)
		noChangeCheckin(t, h, sam, goal)
	}
	ts := newServer(t, h)

	history := historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal)))
	if n := len(historyEntries(history)); n != 3 {
		t.Errorf("History shows %d entries, want 3", n)
	}
	if strings.Contains(history, `data-testid="history-earlier"`) {
		t.Errorf("History offers Show earlier with everything shown")
	}
}

// timelineActionItemGoal arranges an Active Goal, Launch in EU, owned by Ana
// Author, and a published Report, MBR, that covers it, so Action Items about
// it can be raised there.
func timelineActionItemGoal(h *testsupport.Harness) (author domain.Account, goal domain.Goal, pub domain.Publication) {
	author = h.SignInNamed("ana@example.com", "Ana Author")
	goal = h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID}})
	return author, goal, h.PublishReport(author, def)
}

// timelineRaiseActionItem raises an Action Item on pub as author, for owner,
// due on Sun 1 Feb 2026, about goalID, or the whole Report for 0.
func timelineRaiseActionItem(t *testing.T, h *testsupport.Harness, author domain.Account, pub domain.Publication, owner domain.Account, text string, goalID int64) domain.ActionItem {
	t.Helper()
	item, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: goalID, Text: text, OwnerID: owner.ID, DueDate: testsupport.Epoch.AddDate(0, 0, 30),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	return item
}

// An Action Item about the Goal is three entries in its History, newest
// first: closed by its owner with the note, reassigned from one owner to
// another, and raised for the owner it was raised for, with its due date and
// the Report it came from. Each says who did it.
func TestGoalHistoryShowsAnActionItemRaisedReassignedAndClosed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author, goal, pub := timelineActionItemGoal(h)
	sam := h.SignInNamed("sam@example.com", "Sam First")
	pat := h.SignInNamed("pat@example.com", "Pat Second")
	ctx := context.Background()
	h.Clock.Advance(time.Hour)
	item := timelineRaiseActionItem(t, h, author, pub, sam, "Book the launch venue.", goal.ID)
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.ReassignActionItem(ctx, author.ID, item.ID, pat.ID); err != nil {
		t.Fatalf("ReassignActionItem: %v", err)
	}
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.CloseActionItem(ctx, pat.ID, item.ID, "Booked the Lisbon hall."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	ts := newServer(t, h)

	var items []string
	for _, e := range historyEntries(historyBlock(t, getBody(t, signInClient(t, ts.URL, "ana@example.com"), goalPageURL(ts.URL, goal)))) {
		if entryKind(t, e) == "action-item" {
			items = append(items, e)
		}
	}
	if len(items) != 3 {
		t.Fatalf("History lists %d Action Item entries, want 3: %q", len(items), items)
	}
	pubLink := fmt.Sprintf(`href="/reports/%d/publications/%d#action-items"`, pub.DefinitionID, pub.ID)
	for i, tc := range []struct {
		facts []string
		not   []string
	}{
		{facts: []string{"Action Item closed", "Book the launch venue.", "Booked the Lisbon hall.", "by Pat Second", `class="tl-mark tl-action-item"`}},
		{facts: []string{"Action Item reassigned", "Book the launch venue.", "Sam First", "→", "Pat Second", "by Ana Author"}},
		{facts: []string{"Action Item raised", "Book the launch venue.", "for Sam First", "due Feb 1", "from MBR", pubLink, "by Ana Author"}, not: []string{"Pat Second", "pat@example.com"}},
	} {
		text := entryText(items[i])
		for _, fact := range tc.facts {
			if !strings.Contains(text, fact) && !strings.Contains(items[i], fact) {
				t.Errorf("Action Item entry %d lacks %q:\n%s", i, fact, items[i])
			}
		}
		for _, wrong := range tc.not {
			if strings.Contains(items[i], wrong) {
				t.Errorf("Action Item entry %d names %q, not who it was raised for:\n%s", i, wrong, items[i])
			}
		}
	}
}

// Action Item entries made at one instant read newest item first, and each
// item's own events in the order they happen: its closing above its
// reassignments, latest first, above its raising.
func TestGoalHistoryOrdersSameInstantActionItemEvents(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author, goal, pub := timelineActionItemGoal(h)
	sam := h.SignInNamed("sam@example.com", "Sam First")
	pat := h.SignInNamed("pat@example.com", "Pat Second")
	ctx := context.Background()
	one := timelineRaiseActionItem(t, h, author, pub, sam, "Item one.", goal.ID)
	two := timelineRaiseActionItem(t, h, author, pub, sam, "Item two.", goal.ID)
	if _, err := h.Service.ReassignActionItem(ctx, author.ID, two.ID, pat.ID); err != nil {
		t.Fatalf("ReassignActionItem: %v", err)
	}
	if _, err := h.Service.CloseActionItem(ctx, pat.ID, two.ID, "Done."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	for _, to := range []domain.Account{pat, sam} {
		if _, err := h.Service.ReassignActionItem(ctx, author.ID, one.ID, to.ID); err != nil {
			t.Fatalf("ReassignActionItem: %v", err)
		}
	}
	ts := newServer(t, h)

	items := filteredEntries(t, signInClient(t, ts.URL, "ana@example.com"), goalPageURL(ts.URL, goal), "action-items", "action-item")
	want := []struct{ event, item, to string }{
		{"Action Item closed", "Item two.", ""},
		{"Action Item reassigned", "Item two.", "→ Pat Second"},
		{"Action Item raised", "Item two.", ""},
		{"Action Item reassigned", "Item one.", "→ Sam First"},
		{"Action Item reassigned", "Item one.", "→ Pat Second"},
		{"Action Item raised", "Item one.", ""},
	}
	if len(items) != len(want) {
		t.Fatalf("?history=action-items lists %d entries, want %d: %q", len(items), len(want), items)
	}
	for i, w := range want {
		text := entryText(items[i])
		for _, fact := range []string{w.event, w.item, w.to} {
			if !strings.Contains(text, fact) {
				t.Errorf("Action Item entry %d lacks %q: %s", i, fact, text)
			}
		}
	}
}

// The Action Items chip lists only the Action Items' entries, and the other
// chips leave them out; with none, it says so.
func TestGoalHistoryActionItemsChipListsOnlyActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author, goal, pub := timelineActionItemGoal(h)
	h.Clock.Advance(time.Hour)
	h.Checkin(author, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(time.Hour)
	item := timelineRaiseActionItem(t, h, author, pub, author, "Book the launch venue.", goal.ID)
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.CloseActionItem(context.Background(), author.ID, item.ID, "Booked."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	quiet := h.ActiveGoal(author, "Hire a team", "We need people.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "ana@example.com")
	goalURL := goalPageURL(ts.URL, goal)

	if got := historyChips(t, historyBlock(t, getBody(t, client, goalURL)))["Action Items"]; got.count != "2" || filterOf(got.href) != "action-items" {
		t.Errorf("Action Items chip = %+v, want 2 to ?history=action-items", got)
	}
	items := filteredEntries(t, client, goalURL, "action-items", "action-item")
	if len(items) != 2 || !strings.Contains(items[0], "Action Item closed") || !strings.Contains(items[1], "Action Item raised") {
		t.Errorf("?history=action-items lists %q, want the closing and the raising", items)
	}
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL+"?history=checkins"))) {
		if kind := entryKind(t, e); kind == "action-item" {
			t.Errorf("the Check-ins chip lists an Action Item:\n%s", e)
		}
	}

	empty := historyBlock(t, getBody(t, client, goalPageURL(ts.URL, quiet)+"?history=action-items"))
	if got := entryText(between(t, empty, `data-testid="no-history"`, "</p>")); !strings.Contains(got, "No Action Items yet.") {
		t.Errorf("the Action Items chip on a Goal with none reads %q, want No Action Items yet.", got)
	}
}

// Only Action Items about the Goal are in its History: not one about another
// Goal, nor one about the whole Report.
func TestGoalHistoryLeavesOutActionItemsAboutOthers(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignInNamed("ana@example.com", "Ana Author")
	goal := h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	other := h.ActiveGoal(author, "Hire a team", "We need people.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID, other.ID}})
	pub := h.PublishReport(author, def)
	pat := h.SignInNamed("pat@example.com", "Pat Second")
	h.Clock.Advance(time.Hour)
	for _, goalID := range []int64{other.ID, 0} {
		item := timelineRaiseActionItem(t, h, author, pub, author, "Not this Goal's.", goalID)
		if _, err := h.Service.ReassignActionItem(context.Background(), author.ID, item.ID, pat.ID); err != nil {
			t.Fatalf("ReassignActionItem: %v", err)
		}
		if _, err := h.Service.CloseActionItem(context.Background(), pat.ID, item.ID, "Done."); err != nil {
			t.Fatalf("CloseActionItem: %v", err)
		}
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "ana@example.com")

	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)))) {
		if entryKind(t, e) == "action-item" {
			t.Errorf("History lists another's Action Item:\n%s", e)
		}
	}
	if n := len(filteredEntries(t, client, goalPageURL(ts.URL, other), "action-items", "action-item")); n != 3 {
		t.Errorf("the other Goal's History lists %d Action Item entries, want 3", n)
	}
}

// Every Comment and reply about the Goal, on the Goal itself or on a Report,
// is an entry in its History, newest first, saying who wrote it; one made on
// a Report links to the Goal's block there. The Comments chip lists exactly
// those (CONTEXT.md: Comment).
func TestGoalHistoryShowsEveryCommentOnTheGoalOrAReport(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author, goal, pub := timelineActionItemGoal(h)
	pat := h.SignInNamed("pat@example.com", "Pat Reader")
	ctx := context.Background()
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.AddComment(ctx, pat.ID, pub.ID, goal.ID, "Why did the vendor slip?"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	h.Clock.Advance(time.Hour)
	onGoal, err := h.Service.AddGoalComment(ctx, pat.ID, goal.ID, "Is the EU date still real?")
	if err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.ReplyToComment(ctx, author.ID, onGoal.ID, "Yes, as of today."); err != nil {
		t.Fatalf("ReplyToComment: %v", err)
	}
	h.Clock.Advance(time.Hour)
	h.Checkin(author, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "ana@example.com")
	goalURL := goalPageURL(ts.URL, goal)

	var comments []string
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL))) {
		if entryKind(t, e) == "comment" {
			comments = append(comments, e)
		}
	}
	if len(comments) != 3 {
		t.Fatalf("History lists %d Comment entries, want 3: %q", len(comments), comments)
	}
	pubLink := fmt.Sprintf(`href="/reports/%d/publications/%d#goal-%d"`, pub.DefinitionID, pub.ID, goal.ID)
	for i, tc := range []struct {
		label string
		facts []string
		not   []string
	}{
		{"Reply", []string{"Yes, as of today.", "by Ana Author"}, []string{" in MBR", "/reports/"}},
		{"Comment", []string{"Is the EU date still real?", "by Pat Reader", `class="tl-mark tl-comment"`}, []string{" in MBR", "/reports/"}},
		{"Comment", []string{"Why did the vendor slip?", "by Pat Reader", "· in MBR", pubLink}, nil},
	} {
		text := entryText(comments[i])
		if got := between(t, comments[i], `<span class="tl-kind">`, "</span>")[len(`<span class="tl-kind">`):]; got != tc.label {
			t.Errorf("Comment entry %d is labelled %q, want %q", i, got, tc.label)
		}
		for _, fact := range tc.facts {
			if !strings.Contains(text, fact) && !strings.Contains(comments[i], fact) {
				t.Errorf("Comment entry %d lacks %q:\n%s", i, fact, comments[i])
			}
		}
		for _, wrong := range tc.not {
			if strings.Contains(comments[i], wrong) {
				t.Errorf("Comment entry %d names %q, though it was made on the Goal:\n%s", i, wrong, comments[i])
			}
		}
	}

	if got := historyChips(t, historyBlock(t, getBody(t, client, goalURL)))["Comments"]; got.count != "3" || filterOf(got.href) != "comments" {
		t.Errorf("Comments chip = %+v, want 3 to ?history=comments", got)
	}
	if listed := filteredEntries(t, client, goalURL, "comments", "comment"); len(listed) != 3 {
		t.Errorf("?history=comments lists %d entries, want the 3 Comments", len(listed))
	}
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL+"?history=checkins"))) {
		if kind := entryKind(t, e); kind == "comment" {
			t.Errorf("the Check-ins chip lists a Comment:\n%s", e)
		}
	}

	quiet := h.ActiveGoal(author, "Hire a team", "We need people.")
	empty := historyBlock(t, getBody(t, client, goalPageURL(ts.URL, quiet)+"?history=comments"))
	if got := entryText(between(t, empty, `data-testid="no-history"`, "</p>")); !strings.Contains(got, "No Comments yet.") {
		t.Errorf("the Comments chip on a Goal with none reads %q, want No Comments yet.", got)
	}
}

// A Check-in that asks for help says so in its History entry, and one
// identical to the one before but for the ask doesn't fold into "No change":
// ticking or unticking the box is news.
func TestGoalHistoryShowsAnAskAndKeepsItFromFolding(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	path := testsupport.Epoch.AddDate(0, 2, 0)
	h.Clock.Advance(24 * time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Vendor is late.", "Swap vendors.", path)
	h.Clock.Advance(7 * 24 * time.Hour)
	h.AskForHelp(sam, goal.ID, domain.HealthYellow, "Vendor is late.", "Swap vendors.", path)
	h.Clock.Advance(7 * 24 * time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Vendor is late.", "Swap vendors.", path)
	ts := newServer(t, h)

	entries := historyEntries(historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))))
	if len(entries) != 4 {
		t.Fatalf("History lists %d entries, want three full Check-ins and the Why it matters", len(entries))
	}
	for i, wantAsk := range []bool{false, true, false} {
		if n := foldedCount(entries[i]); n != "" {
			t.Errorf("entry %d folds %s Check-ins, want it in full", i, n)
		}
		if got := strings.Contains(entries[i], `<p data-testid="checkin-asks-for-help">Asks for help</p>`); got != wantAsk {
			t.Errorf("entry %d shows Asks for help = %v, want %v:\n%s", i, got, wantAsk, entries[i])
		}
	}
}

// Under the Date Slips filter, a Check-in that moved the back-to-Green date
// later lists that slip as the Back to Green date's, its old date struck and
// its reason given (CONTEXT.md: Date Slip).
func TestGoalHistoryListsABackToGreenSlip(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Checkin(sam, goal.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))
	h.Clock.Advance(time.Hour)
	h.CheckinMovingPath(sam, goal.ID, domain.HealthRed, "Still blocked.", "Escalate.", time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC), "The vendor is out until March.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	chip, ok := historyChips(t, historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal))))["Date Slips"]
	if !ok || chip.count != "1" {
		t.Fatalf("Date Slips chip = %+v, want it counting 1", chip)
	}
	entries := historyEntries(historyBlock(t, getBody(t, client, ts.URL+chip.href)))
	if len(entries) != 1 {
		t.Fatalf("Date Slips lists %d entries, want 1", len(entries))
	}
	slips := between(t, entries[0], `data-testid="entry-slips"`, "</ul>")
	for _, want := range []string{"<strong>Back to Green date</strong>: <del>Mar 2</del> Mar 16", "The vendor is out until March."} {
		if !strings.Contains(slips, want) {
			t.Errorf("the Check-in's Date Slips lack %q:\n%s", want, slips)
		}
	}
}
