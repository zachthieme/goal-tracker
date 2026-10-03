package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// oneOfEachKind arranges an Active Goal with one History entry of each kind on
// different days: its So What on Fri 2 Jan 2026 (its creation), a Field set on
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
// each checked to be of kind, with entities unescaped.
func filteredEntries(t *testing.T, client *http.Client, goalURL, filter, kind string) []string {
	t.Helper()
	var out []string
	for _, e := range historyEntries(historyBlock(t, getBody(t, client, goalURL+"?history="+filter))) {
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
		{"Week of 5 Jan", []string{"ownership", "checkin", "value"}},
		{"Week of 29 Dec 2025", []string{"so-what"}},
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
	if len(weeks) != 1 || !strings.Contains(weeks[0], "Week of 29 Dec 2025") {
		t.Fatalf("History is not one week of 29 Dec 2025 in Los Angeles:\n%s", history)
	}
	if checkin := historyEntries(history)[0]; !strings.Contains(checkin, "Sun 4 Jan 19:00") {
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
		{"So What", "1", []string{"so-what"}, ""},
		{"Ownership", "1", []string{"ownership"}, ""},
		{"Values", "1", []string{"value"}, ""},
		{"All", "5", []string{"checkin", "ownership", "checkin", "value", "so-what"}, ""},
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

// filterOf is the History filter an address asks for.
func filterOf(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return u.Query().Get("history")
}

// With more than 20 entries History shows the latest 20, and "Show earlier"
// reloads the page with 20 more under the same filter, the History block in
// view; once everything shows, it is gone.
func TestGoalHistoryShowsTwentyAndShowEarlierAddsTwentyMoreKeepingTheFilter(t *testing.T) {
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
			if entryKind(t, e) == "so-what" {
				out = append(out, "So What")
				continue
			}
			out = append(out, between(t, e, "Update ", ".")[len("Update "):])
		}
		return out
	}
	earlier := func(history string) string {
		t.Helper()
		_, href, ok := strings.Cut(between(t, history, `data-testid="history-earlier"`, ">"), `href="`)
		if !ok {
			t.Fatalf("no Show earlier link in:\n%s", history)
		}
		href, _, _ = strings.Cut(href, `"`)
		return html.UnescapeString(href)
	}

	// All: 45 entries, the latest 20 first.
	history := historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)))
	if got := statuses(history); len(got) != 20 || got[0] != "44" || got[19] != "25" {
		t.Fatalf("History first shows %v, want Updates 44 down to 25", got)
	}
	history = historyBlock(t, getBody(t, client, ts.URL+earlier(history)))
	if got := statuses(history); len(got) != 40 || got[39] != "5" {
		t.Fatalf("Show earlier shows %v, want Updates 44 down to 5", got)
	}
	history = historyBlock(t, getBody(t, client, ts.URL+earlier(history)))
	if got := statuses(history); len(got) != 45 || got[44] != "So What" {
		t.Fatalf("Show earlier again shows %v, want everything down to the So What", got)
	}
	if strings.Contains(history, `data-testid="history-earlier"`) {
		t.Errorf("Show earlier stays once everything shows")
	}

	// Check-ins: Show earlier keeps the filter.
	history = historyBlock(t, getBody(t, client, goalPageURL(ts.URL, goal)+"?history=checkins"))
	link := earlier(history)
	if filterOf(link) != "checkins" || !strings.HasSuffix(link, "#history") {
		t.Errorf("Show earlier drops the filter or the History block: %s", link)
	}
	history = historyBlock(t, getBody(t, client, ts.URL+link))
	if got := statuses(history); len(got) != 40 || slices.Contains(got, "So What") {
		t.Errorf("Show earlier under Check-ins shows %v, want 40 Check-ins", got)
	}
}

// A Check-in on the timeline shows what it recorded: its Health and status, the
// Date Slips of the delivery date and of a Milestone with their reasons, its
// Path to Green, a Lifecycle change, and who wrote it for whom, by Name. Its
// readings and explanation wait behind a disclosure on the entry. A Check-in
// with no Health takes the neutral Check-in marker.
func TestGoalHistoryCheckinShowsWhatItRecorded(t *testing.T) {
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
	red := h.ActiveChildOf(sam, goal, "Red work", "Red so what.")
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
		"Yellow", "Vendor is late.", "Path to Green: Swap vendors. (by 2026-03-02)",
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

// fmtDay is a date as the pages write it.
func fmtDay(d time.Time) string { return d.Format("2006-01-02") }

// Every fact the five old history sections showed is on the timeline, with
// people by Name: the So What text and who wrote it, a Handoff's from, to,
// outcome and who started it, a value change and who made it, a Check-in's
// Health, status, Path to Green and author, and the Date Slip it carried.
func TestGoalHistoryKeepsEveryFactOfTheOldSections(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	goal := oneOfEachKind(t, h)
	ts := newServer(t, h)

	entries := historyEntries(historyBlock(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))))
	if len(entries) != 4 {
		t.Fatalf("History lists %d entries, want 4", len(entries))
	}
	sam, pat := shownAs("sam@example.com", "Sam Owner"), shownAs("pat@example.com", "Pat Next")
	for i, want := range [][]string{
		{">Handoff<", "Sun 11 Jan 15:04", sam + " → " + pat, "pending", "started by " + sam},
		{">Check-in<", "Thu 8 Jan 15:04", "Yellow", "Vendor is late.", "Path to Green: Swap vendors. (by 2026-03-02)", "by " + sam,
			"<strong>Delivery date</strong>: <del>2026-07-02</del> 2026-07-16", "Vendor API delayed two weeks."},
		{">Value<", "Tue 6 Jan 15:04", "Budget: set to 200", "by " + sam},
		{">So What<", "Fri 2 Jan 15:04", "Customers wait too long.", "by " + sam},
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
	if len(links) != 7 {
		t.Fatalf("History has %d chip and Show earlier links, want 7", len(links))
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
		heading := "Week of " + monday.AddDate(0, 0, 7*i).Format("2 Jan")
		if i == 0 {
			heading += " 2025"
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
		{"no-checkin", "Week of 29 Dec 2025: no Check-in"},
		{"blank", "Week of 5 Jan: On Hold"},
		{"blank", "Week of 12 Jan: On Hold"},
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
	if want := (healthCell{"g", "Week of 29 Dec 2025: Green"}); cells[9] != want {
		t.Errorf("last week's cell = %+v, want %+v", cells[9], want)
	}
	if want := (healthCell{"not-yet-due", "Week of 5 Jan: not yet due"}); cells[10] != want {
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
		{"no-checkin", "Week of 29 Dec 2025: no Check-in"},
		{"blank", "Week of 5 Jan: On Hold"},
		{"blank", "Week of 12 Jan: On Hold"},
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
	if want := (healthCell{"g", "29 Dec 2025 – 11 Jan: Green"}); cells[10] != want {
		t.Errorf("current cell = %+v, want %+v", cells[10], want)
	}
	if want := (healthCell{"blank", "15 Dec 2025 – 28 Dec 2025: not yet Active"}); cells[9] != want {
		t.Errorf("cell before = %+v, want %+v", cells[9], want)
	}
	summary := html.UnescapeString(between(t, page, `data-testid="health-strip-summary"`, "</"))
	if !strings.Contains(summary, "Last 11 periods of 14 days: 1 Green, 10 not Active.") {
		t.Errorf("strip summary = %q", summary)
	}
}

// A Goal that has never been Active has no strip.
func TestProposedGoalPageHasNoHealthStrip(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Ship v2", "Customers wait too long.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	if strings.Contains(page, `data-testid="health-strip"`) {
		t.Errorf("a Proposed Goal's page shows a Health strip")
	}
}
