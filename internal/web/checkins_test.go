package web_test

import (
	"cmp"
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
	"unicode"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The tests here assert on what a person sees and can do on the Check-in page
// and the Goal page's status — text, fields, links, buttons and test IDs — read
// through the parser helpers in helpers_test.go, so a cosmetic change to the
// markup breaks none of them. The styles these pages carry are tested in
// styles_test.go.

// A Yellow Check-in submitted without a Path to Green comes back with the
// validation error rendered in the form, next to the Path to Green field, so the
// Owner can fix it in place (htmx). This is the validation-error smoke test.
func TestSmokeCheckinValidationErrorReachesForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	// Submit Yellow with a status but no Path to Green, as htmx would.
	body, status := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthYellow},
		"status": {"Slipping a little."},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200 so htmx swaps the form", status)
	}
	doc := parsePage(t, body)
	if !hasTestID(doc, "checkin-form") {
		t.Errorf("error response is not the re-rendered form; body:\n%s", body)
	}
	if msg := fieldError(t, doc, "path_target_date"); !strings.Contains(msg, "Path to Green") {
		t.Errorf("the error = %q, want it to mention the Path to Green", msg)
	}

	// Nothing was recorded.
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded despite the validation error: %d", len(history))
	}
}

// The one-click "no change" button records a Check-in repeating the previous
// values. This is the no-change smoke test.
func TestSmokeNoChangeCheckinButton(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	// Seed a first Check-in through the form (the happy path).
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"On track."},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit first check-in: status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Click "no change".
	resp = postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("no-change: status %d", resp.StatusCode)
	}

	history, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2 (the seeded Check-in and its no-change repeat)", len(history))
	}
	if history[0].Health != domain.HealthGreen || history[0].Status != "On track." {
		t.Errorf("no-change did not repeat the previous values: %+v", history[0])
	}

	// The Goal page shows the current Health from the latest Check-in.
	page := getPage(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if got := textOf(findByTestID(t, page, "goal-health")); got != domain.HealthGreen {
		t.Errorf("Goal page shows Health %q, want Green", got)
	}
}

// An Owner records a Metric reading and a Highlight in a Check-in through the
// web form: both are persisted, and the Goal page then shows the Metric's trend
// value against its target and the Highlight. This is the readings/Highlight
// smoke test (ticket #6).
func TestSmokeCheckinRecordsReadingAndHighlight(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")
	metric, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, domain.AddMetricInput{
		GoalID:     goal.ID,
		Name:       "p95 checkout latency",
		Unit:       "ms",
		Direction:  domain.MetricDown,
		Baseline:   1200,
		Target:     400,
		TargetDate: h.Clock.Now().AddDate(0, 6, 0),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	samClient := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":                             {domain.HealthGreen},
		"status":                             {"On track."},
		fmt.Sprintf("reading_%d", metric.ID): {"850"},
		"highlight_kind":                     {domain.HighlightAccomplishment},
		"highlight_note":                     {"Shipped the caching layer."},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit check-in: status %d", resp.StatusCode)
	}

	readings, err := h.Service.ListMetricReadings(context.Background(), metric.ID)
	if err != nil {
		t.Fatalf("ListMetricReadings: %v", err)
	}
	if len(readings) != 1 || readings[0].Value != 850 {
		t.Fatalf("readings = %+v, want one reading of 850", readings)
	}
	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	if len(highlights) != 1 || highlights[0].Note != "Shipped the caching layer." {
		t.Fatalf("highlights = %+v, want the Accomplishment just recorded", highlights)
	}

	reads := textOf(getPage(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)))
	for _, want := range []string{"850", "Shipped the caching layer.", domain.HighlightAccomplishment} {
		if !strings.Contains(reads, want) {
			t.Errorf("Goal page missing %q; it reads:\n%s", want, reads)
		}
	}
}

// An Owner moves the delivery date later in a Check-in through the web form,
// with a reason and a Yellow Health. The Goal page then shows the date history
// struck through (~~old~~ new) on the head's metadata line, the reason, and the
// slip count. This is the Date
// Slip smoke test (ticket #5).
func TestSmokeCheckinDeliveryDateSlipShownStruckThrough(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	samClient := signInClient(t, ts.URL, "sam@example.com")
	oldDate := goal.DeliveryDate.Format("Jan 2")
	newDate := goal.DeliveryDate.AddDate(0, 0, 14).Format("Jan 2")

	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":               {domain.HealthYellow},
		"status":               {"Vendor is late."},
		"path_to_green":        {"Swap vendors."},
		"path_target_date":     {"2026-06-15"},
		"delivery_date":        {goal.DeliveryDate.AddDate(0, 0, 14).Format("2006-01-02")},
		"delivery_date_reason": {"Vendor API delayed two weeks."},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit check-in: status %d", resp.StatusCode)
	}
	if slips, _ := h.Service.ListDateSlips(context.Background(), goal.ID); len(slips) != 1 {
		t.Fatalf("recorded %d Date Slips, want 1", len(slips))
	}

	page := getPage(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	delivery := findByTestID(t, findByTestID(t, page, "goal-meta"), "goal-delivery-date")
	if got, struck := textOf(delivery), struckThrough(delivery); got != oldDate+" "+newDate || !slices.Equal(struck, []string{oldDate}) {
		t.Errorf("the rail shows the delivery date as %q with %q struck through, want %s struck through, then %s", got, struck, oldDate, newDate)
	}
	if slips := textOf(findByTestID(t, findByTestID(t, page, "goal-history"), "entry-slips")); !strings.Contains(slips, "Vendor API delayed two weeks.") {
		t.Errorf("the Check-in on the History timeline is missing the Date Slip's reason: %q", slips)
	}
	if got := textOf(findByTestID(t, page, "goal-slip-count")); got != "1" {
		t.Errorf("Goal page shows a slip count of %q, want 1", got)
	}
}

// The Owner's Health cell of a Red Goal reads its Run off Green beside the
// badge ("Red · 2 wks"); a Green Goal's shows none (CONTEXT.md: Run off Green).
func TestLatestStatusShowsTheRunOffGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	red := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	green := h.ActiveGoal(sam, "Cut churn", "Keep customers.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	// Red on Mondays 5 and 12 January, read on the 12th, each moving the
	// back-to-Green date a week later with the reason that needs.
	for _, d := range []int{5, 12} {
		h.Clock.Set(time.Date(2026, time.January, d, 9, 0, 0, 0, time.UTC))
		h.CheckinMovingPath(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", h.Clock.Now().AddDate(0, 1, 0), "Escalation takes longer.")
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	cell := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, red)), "goal-health-cell")
	if got := textsOf(findAllByTestID(cell, "goal-run-off-green")); !slices.Equal(got, []string{"· 2 wks"}) {
		t.Errorf("Owner's Health cell reads its Run off Green %q, want \"· 2 wks\"; cell reads %q", got, textOf(cell))
	}
	if page := getPage(t, client, goalPageURL(ts.URL, green)); hasTestID(page, "goal-run-off-green") {
		t.Errorf("a Green Goal's page shows a Run off Green: %q", textOf(findByTestID(t, page, "goal-health-cell")))
	}
}

// The Latest status card opens with a row of cells, each a small label over a
// value: the Owner's Health, the Rolled-up Health with its Check-in
// overdue-children count when the Goal has Active children, and the Back to
// Green date when there is a Path to Green. The status, the Path to Green and
// the explanation follow.
func TestLatestStatusOpensWithCells(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	h.ActiveChildOf(sam, parent, "Silent child", "It matters.")
	h.Clock.Advance(10 * 24 * time.Hour)
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red why it matters.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID:         parent.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Slipping at this level.",
		PathToGreen:    "Cut scope.",
		PathTargetDate: pathDate,
		Explanation:    "The Red child is a stretch item.",
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	card := findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID)), "goal-checkins")
	cells := findByTestID(t, card, "goal-status-cells")
	for _, tc := range []struct{ testID, label, value string }{
		{"goal-health-cell", "Owner's Health", "Yellow"},
		{"goal-rollup-cell", "Rolled-up Health", "Red · 1 of 2 overdue"},
		{"goal-back-to-green-cell", "Back to Green by", "Jun 15"},
	} {
		label, value := statusCell(t, cells, tc.testID)
		if label != tc.label || !strings.HasPrefix(value, tc.value) {
			t.Errorf("%s cell reads %q over %q, want %q over %q", tc.testID, label, value, tc.label, tc.value)
		}
	}
	for testID, want := range map[string]string{"goal-health": "Yellow", "goal-rollup-health": "Red", "goal-rollup-checkin-overdue": "1 of 2 overdue"} {
		if got := textsOf(findAllByTestID(cells, testID)); !slices.Equal(got, []string{want}) {
			t.Errorf("cells show %s %q, want %q", testID, got, want)
		}
	}
	for _, below := range []string{"goal-status", "goal-path-to-green", "goal-rollup-explanation"} {
		if n := findAllByTestID(card, below); len(n) != 1 || !precedes(cells, n[0]) {
			t.Errorf("Latest status has no %s below its cells", below)
		}
	}

	// A childless Goal with no Path to Green shows only the Owner's Health cell.
	lone := h.ActiveGoal(sam, "Lone", "It matters.")
	h.Checkin(sam, lone.ID, domain.HealthGreen, "On track.", "", time.Time{})
	card = findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, lone.ID)), "goal-checkins")
	if got := textOf(findByTestID(t, findByTestID(t, card, "goal-status-cells"), "goal-health")); got != "Green" {
		t.Errorf("Latest status's Owner's Health cell reads %q, want Green", got)
	}
	for _, absent := range []string{"goal-rollup-cell", "goal-back-to-green-cell"} {
		if hasTestID(card, absent) {
			t.Errorf("a childless Goal with no Path to Green shows %s", absent)
		}
	}
	for _, absent := range []string{"Rolled-up Health", "Back to Green"} {
		if strings.Contains(textOf(card), absent) {
			t.Errorf("a childless Goal with no Path to Green reads %q: %q", absent, textOf(card))
		}
	}

	// A Goal with no Health keeps its message in place of the cells.
	proposed := h.CreateGoal(sam, "Proposed", "It matters.")
	card = findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, proposed.ID)), "goal-checkins")
	if hasTestID(card, "goal-status-cells") || !strings.Contains(textOf(card), "A Proposed Goal has no Health until it is Active.") {
		t.Errorf("a Proposed Goal's Latest status should say it has no Health, with no cells: %q", textOf(card))
	}
}

// statusCell reads the Latest status cell carrying testID: its label, the
// <dt>, and its value, the <dd>.
func statusCell(t *testing.T, cells *html.Node, testID string) (label, value string) {
	t.Helper()
	cell := findByTestID(t, cells, testID)
	return textOf(firstTag(t, cell, "dt")), textOf(firstTag(t, cell, "dd"))
}

// firstTag returns the first tag element under root, failing the test if
// there is none.
func firstTag(t *testing.T, root *html.Node, tag string) *html.Node {
	t.Helper()
	found := findAll(root, func(n *html.Node) bool { return n.Data == tag })
	if len(found) == 0 {
		t.Fatalf("no <%s> in:\n%s", tag, renderNode(root))
	}
	return found[0]
}

// A Green Check-in that moves the delivery date later comes back as the form
// with the error next to the delivery date, and nothing is recorded.
func TestSmokeGreenWithLaterDeliveryDateRejectedInForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, status := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":               {domain.HealthGreen},
		"status":               {"Fine."},
		"delivery_date":        {goal.DeliveryDate.AddDate(0, 0, 7).Format("2006-01-02")},
		"delivery_date_reason": {"Vendor slipped."},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200", status)
	}
	doc := parsePage(t, body)
	if msg := fieldError(t, doc, "delivery_date"); !strings.Contains(msg, "moves the delivery date later") {
		t.Errorf("delivery date field's error = %q, want the Green one", msg)
	}
	if got := formField(t, doc, "delivery_date_reason").Value; got != "Vendor slipped." {
		t.Errorf("re-rendered form's reason = %q, want the one typed", got)
	}
	if slips, _ := h.Service.ListDateSlips(context.Background(), goal.ID); len(slips) != 0 {
		t.Errorf("recorded %d Date Slips despite the rejection", len(slips))
	}
}

// Through the Check-in form an Owner slips one Milestone, removes another with
// a reason, and adds a new one, all under a Green Health (a Milestone slip that
// doesn't move the delivery date doesn't affect Health). The Goal page then
// shows the Milestone's date struck through, the removal reason, the slip count,
// and the Milestone Churn.
func TestSmokeCheckinChangesMilestones(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	ctx := context.Background()
	ms, err := h.Service.ListMilestones(ctx, goal.ID)
	if err != nil || len(ms) != 1 {
		t.Fatalf("ListMilestones = %v, %v", ms, err)
	}
	beta := ms[0]
	samClient := signInClient(t, ts.URL, "sam@example.com")

	// First Check-in adds a Docs Milestone.
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":             {domain.HealthGreen},
		"status":             {"Adding docs."},
		"new_milestone_name": {"Docs"},
		"new_milestone_date": {"2026-05-01"},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first check-in: status %d", resp.StatusCode)
	}
	ms, _ = h.Service.ListMilestones(ctx, goal.ID)
	var docs domain.Milestone
	for _, m := range ms {
		if m.Name == "Docs" {
			docs = m
		}
	}
	if docs.ID == 0 {
		t.Fatalf("the Docs Milestone was not added: %+v", ms)
	}

	// Second Check-in slips Beta and removes Docs.
	oldBeta := beta.TargetDate.Format("Jan 2")
	newBeta := beta.TargetDate.AddDate(0, 0, 10).Format("Jan 2")
	_ = resp.Body.Close()
	resp = postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Beta moves; launch holds."},
		fmt.Sprintf("milestone_date_%d", beta.ID):           {beta.TargetDate.AddDate(0, 0, 10).Format("2006-01-02")},
		fmt.Sprintf("milestone_date_reason_%d", beta.ID):    {"Design review moved."},
		fmt.Sprintf("milestone_status_%d", docs.ID):         {domain.MilestoneRemoved},
		fmt.Sprintf("milestone_removed_reason_%d", docs.ID): {"Docs folded into GA."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second check-in: status %d", resp.StatusCode)
	}

	page := getPage(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	milestones := findByTestID(t, page, "goal-milestones")
	for _, want := range []string{newBeta + " " + oldBeta, "Docs folded into GA."} {
		if !strings.Contains(textOf(milestones), want) {
			t.Errorf("Milestone list missing %q; it reads %q", want, textOf(milestones))
		}
	}
	if struck := struckThrough(milestones); !slices.Contains(struck, oldBeta) {
		t.Errorf("Milestone list strikes through %q, want Beta's old date %s", struck, oldBeta)
	}
	if slips := textOf(findByTestID(t, findByTestID(t, page, "goal-history"), "entry-slips")); !strings.Contains(slips, "Design review moved.") {
		t.Errorf("the Check-in on the History timeline is missing the Date Slip's reason: %q", slips)
	}
	for testID, want := range map[string]string{"goal-slip-count": "1", "goal-milestone-churn": "2"} {
		if got := textOf(findByTestID(t, page, testID)); got != want {
			t.Errorf("Goal page shows %s %q, want %q", testID, got, want)
		}
	}
}

// Through the Check-in form an Owner puts a Goal On Hold with a reason, then
// resumes it. The Goal page shows the Lifecycle and why, the History timeline
// shows each change, and while On Hold the Check-in page offers only to resume
// or Cancel.
func TestSmokeCheckinPutsGoalOnHoldAndResumes(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	samClient := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	resp := postForm(t, samClient, goalURL+"/checkins", url.Values{
		"health":           {domain.HealthGreen},
		"status":           {"Pausing for the reorg."},
		"lifecycle":        {domain.LifecycleOnHold},
		"lifecycle_reason": {"Team moved to the payments incident."},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put On Hold: status %d", resp.StatusCode)
	}

	page := getPage(t, samClient, goalURL)
	if got := textOf(findByTestID(t, page, "goal-lifecycle")); got != domain.LifecycleOnHold {
		t.Errorf("Goal page shows the Goal %q, want On Hold", got)
	}
	if note := textOf(findByTestID(t, page, "goal-lifecycle-note")); !strings.Contains(note, "Team moved to the payments incident.") {
		t.Errorf("Goal page missing why it is On Hold; it reads %q", note)
	}
	if entry := textOf(findByTestID(t, page, "checkin-lifecycle")); !strings.Contains(entry, "Active → On Hold") || !strings.Contains(entry, "Team moved to the payments incident.") {
		t.Errorf("History timeline missing the Lifecycle change; it reads %q", entry)
	}
	choices := findByTestID(t, getPage(t, samClient, goalURL+"/checkin"), "checkin-lifecycle-fields")
	if got := radioLabels(choices, "lifecycle"); !slices.Equal(got, []string{"Resume (Active)", "Cancel"}) {
		t.Errorf("On Hold form offers %q, want resume and Cancel only", got)
	}
	if hasTestID(page, "no-change-checkin") {
		t.Errorf("On Hold Goal offers the no-change Check-in")
	}
	_ = resp.Body.Close()

	resp = postForm(t, samClient, goalURL+"/checkins", url.Values{
		"health":    {domain.HealthGreen},
		"status":    {"Back on it."},
		"lifecycle": {domain.LifecycleActive},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resume: status %d", resp.StatusCode)
	}
	page = getPage(t, samClient, goalURL)
	if got := textOf(findByTestID(t, page, "goal-lifecycle")); got != domain.LifecycleActive {
		t.Errorf("Goal page shows the resumed Goal %q, want Active", got)
	}
	if !strings.Contains(textOf(page), "On Hold → Active") {
		t.Errorf("Check-in history missing the resume")
	}
}

// radioLabels are the labels of the radios named name under root, in order:
// the choices a person is offered.
func radioLabels(root *html.Node, name string) []string {
	var out []string
	for _, f := range formFields(root, name) {
		out = append(out, f.Label)
	}
	return out
}

// Marking a Goal Done through the form without a final value for every Metric
// comes back as the form with the error inline and the typed outcome kept; with
// the final value it succeeds, and the Goal page shows the Goal Done with its
// outcome and no Check-in form.
func TestSmokeCheckinMarksGoalDone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")
	metric, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, domain.AddMetricInput{
		GoalID:     goal.ID,
		Name:       "p95 checkout latency",
		Unit:       "ms",
		Direction:  domain.MetricDown,
		Baseline:   1200,
		Target:     400,
		TargetDate: h.Clock.Now().AddDate(0, 6, 0),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	samClient := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)
	done := url.Values{
		"health":    {domain.HealthGreen},
		"status":    {"Shipped the cache."},
		"lifecycle": {domain.LifecycleDone},
		"outcome":   {"p95 latency down to 380ms."},
	}

	body, status := postFormHX(t, samClient, goalURL+"/checkins", done)
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200", status)
	}
	doc := parsePage(t, body)
	if msg := fieldError(t, doc, fmt.Sprintf("reading_%d", metric.ID)); !strings.Contains(msg, "final value") {
		t.Errorf("reading field's error = %q, want the final-value one", msg)
	}
	if got := formField(t, doc, "outcome").Value; got != "p95 latency down to 380ms." {
		t.Errorf("re-rendered form's outcome = %q, want the one typed", got)
	}

	done.Set(fmt.Sprintf("reading_%d", metric.ID), "380")
	resp := postForm(t, samClient, goalURL+"/checkins", done)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark Done: status %d", resp.StatusCode)
	}

	page := getPage(t, samClient, goalURL)
	if got := textOf(findByTestID(t, page, "goal-lifecycle")); got != domain.LifecycleDone {
		t.Errorf("Goal page shows the Goal %q, want Done", got)
	}
	if note := textOf(findByTestID(t, page, "goal-lifecycle-note")); !strings.Contains(note, "Outcome: p95 latency down to 380ms.") {
		t.Errorf("Goal page missing the outcome; it reads %q", note)
	}
	if hasTestID(page, "checkin-form") {
		t.Errorf("a Done Goal still offers the Check-in form")
	}
}

// Cancelling a Goal through the form without a reason is rejected with a
// message that reads naturally (ticket #29).
func TestCheckinCancelWithoutReasonErrorReadsNaturally(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, status := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":    {domain.HealthGreen},
		"status":    {"s"},
		"lifecycle": {domain.LifecycleCancelled},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200", status)
	}
	if msg := textOf(findByTestID(t, parsePage(t, body), "checkin-error")); !strings.Contains(msg, "Cancelling a Goal needs a reason") {
		t.Errorf("error = %q, want it to say Cancelling a Goal needs a reason", msg)
	}
}

// A reading that isn't a number is rejected with a message naming the Metric,
// not its database id, shown next to that reading, and the typed text stays in
// the input (ticket #29).
func TestCheckinNonNumericReadingNamesTheMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Grow the product", "Growth funds the roadmap.")
	metric, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, domain.AddMetricInput{
		GoalID:     goal.ID,
		Name:       "Signups",
		Unit:       "per week",
		Direction:  domain.MetricUp,
		Baseline:   100,
		Target:     500,
		TargetDate: h.Clock.Now().AddDate(0, 6, 0),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	samClient := signInClient(t, ts.URL, "sam@example.com")

	reading := fmt.Sprintf("reading_%d", metric.ID)
	body, status := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"On track."},
		reading:  {"abc"},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200", status)
	}
	doc := parsePage(t, body)
	msg := fieldError(t, doc, reading)
	if !strings.Contains(msg, `"Signups" needs a number`) {
		t.Errorf("error = %q, want it to say \"Signups\" needs a number", msg)
	}
	if strings.Contains(msg, "metric") {
		t.Errorf("error = %q, still names the Metric by its id", msg)
	}
	if got := formField(t, doc, reading).Value; got != "abc" {
		t.Errorf("re-rendered reading = %q, want the abc typed", got)
	}
}

// The Check-in form shows each Metric's latest reading as ghost text in its
// empty reading input (ticket #29, #284); a Metric with no readings yet shows
// none.
func TestCheckinFormShowsLatestReadingsAsGhostText(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Grow the product", "Growth funds the roadmap.")
	addMetric := func(name string) domain.Metric {
		t.Helper()
		m, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, domain.AddMetricInput{
			GoalID:     goal.ID,
			Name:       name,
			Unit:       "per week",
			Direction:  domain.MetricUp,
			Baseline:   100,
			Target:     500,
			TargetDate: h.Clock.Now().AddDate(0, 6, 0),
		})
		if err != nil {
			t.Fatalf("AddMetric: %v", err)
		}
		return m
	}
	signups := addMetric("Signups")
	referrals := addMetric("Referrals")
	samClient := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	for _, reading := range []string{"120", "135.5"} {
		resp := postForm(t, samClient, goalURL+"/checkins", url.Values{
			"health":                              {domain.HealthGreen},
			"status":                              {"On track."},
			fmt.Sprintf("reading_%d", signups.ID): {reading},
		})
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("submit check-in: status %d", resp.StatusCode)
		}
	}

	readings := findByTestID(t, getPage(t, samClient, goalURL+"/checkin"), "checkin-readings")
	signupsField := formField(t, readings, fmt.Sprintf("reading_%d", signups.ID))
	if signupsField.Value != "" || signupsField.Placeholder != "135.5" {
		t.Errorf("Signups reading = %q with ghost text %q, want it empty with its latest value, 135.5, as ghost text", signupsField.Value, signupsField.Placeholder)
	}
	referralsField := formField(t, readings, fmt.Sprintf("reading_%d", referrals.ID))
	if referralsField.Value != "" || hasAttr(referralsField.Node, "placeholder") {
		t.Errorf("Referrals has no readings, so its input should be blank with no ghost text; got %q with ghost text %q", referralsField.Value, referralsField.Placeholder)
	}

	// Each Metric's row shows its unit, baseline, target and target date, its
	// last reading with progress toward the target, and labels its input for
	// a screen reader (ticket #364). The harness clock reads Jan 2, 2026.
	signupsRow := rowAround(signupsField.Node, "Signups")
	if got, want := textOf(signupsRow), "Signups per week · 100 → 500 by Jul 2 · last 135.5"; !strings.Contains(got, want) {
		t.Errorf("Signups row reads %q, want it to contain %q", got, want)
	}
	// (135.5 − 100) / (500 − 100) of the way from baseline to target.
	if bars := findAll(signupsRow, func(n *html.Node) bool { return n.Data == "progress" }); len(bars) != 1 ||
		attrOf(bars[0], "value") != "0.08875" || attrOf(bars[0], "max") != "1" || attrOf(bars[0], "aria-hidden") != "true" {
		t.Errorf("Signups row has no progress 0.08875 of the way to its target, hidden from a screen reader; row:\n%s", renderNode(signupsRow))
	}
	if signupsField.Label != "Signups current value" {
		t.Errorf("Signups input is labelled %q, want Signups current value", signupsField.Label)
	}
	referralsRow := rowAround(referralsField.Node, "Referrals")
	if got, want := textOf(referralsRow), "Referrals per week · 100 → 500 by Jul 2"; !strings.Contains(got, want) {
		t.Errorf("Referrals row reads %q, want it to contain %q", got, want)
	}
	if strings.Contains(textOf(referralsRow), "· last") || len(findAll(referralsRow, func(n *html.Node) bool { return n.Data == "progress" })) > 0 {
		t.Errorf("Referrals has no reading, so its row should show no last value or progress; row:\n%s", renderNode(referralsRow))
	}
}

// rowAround is the nearest element around field that reads text: the row
// that names the field, however it is laid out.
func rowAround(field *html.Node, text string) *html.Node {
	for p := field.Parent; p != nil; p = p.Parent {
		if strings.Contains(textOf(p), text) {
			return p
		}
	}
	return topOf(field)
}

// A Green Check-in on a Goal whose Rolled-up Health is Red, sent without an
// explanation, gets its error next to the explanation field — not under the
// Path to Green.
func TestCheckinExplanationErrorShownNextToExplanationField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red why it matters.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	samClient := signInClient(t, ts.URL, "sam@example.com")
	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, parent.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Looks fine."},
	})
	doc := parsePage(t, body)
	const want = "This Health differs from the Rolled-up Health (Red); explain why."
	if msg := fieldError(t, doc, "explanation"); !strings.HasPrefix(msg, want) {
		t.Errorf("explanation field's error = %q, want it to read %q", msg, want)
	}
	if strings.Contains(textOf(doc), "validation failed") {
		t.Errorf("page shows the internal \"validation failed\" prefix; body:\n%s", body)
	}
}

// fieldError returns what the form's one validation error says, failing
// unless it sits beside the field named name — after it, or after a radio
// group's last radio, and before the next field's label — so it is shown next
// to the field it is about. A name several fields share means the first.
func fieldError(t *testing.T, doc *html.Node, name string) string {
	t.Helper()
	fields := formFields(doc, name)
	if len(fields) == 0 {
		t.Fatalf("form has no field %s:\n%s", name, renderNode(doc))
	}
	field := fields[0]
	if field.Type == "radio" {
		field = fields[len(fields)-1]
	}
	return errorAfter(t, doc, field.Node)
}

// formTopError returns what the form's one validation error says, failing
// unless it sits at the top of the Check-in form, before its first label.
func formTopError(t *testing.T, doc *html.Node) string {
	t.Helper()
	return errorAfter(t, doc, findByTestID(t, doc, "checkin-form"))
}

// errorAfter returns what the page's one validation error says, failing unless
// it is the first thing after from, reading on, before any label.
func errorAfter(t *testing.T, doc, from *html.Node) string {
	t.Helper()
	errs := findAllByTestID(doc, "checkin-error")
	if len(errs) != 1 {
		t.Fatalf("form shows %d validation errors, want 1:\n%s", len(errs), renderNode(doc))
	}
	passed := false
	for n := range doc.Descendants() {
		switch {
		case n == from:
			passed = true
		case !passed || n.Type != html.ElementNode:
		case n == errs[0]:
			return textOf(n)
		case n.Data == "label":
			t.Fatalf("the error %q is not next to the field:\n%s", textOf(errs[0]), renderNode(doc))
		}
	}
	t.Fatalf("the error %q comes before the field:\n%s", textOf(errs[0]), renderNode(doc))
	return ""
}

// A Yellow Check-in without a Path to Green gets its error next to the Path to
// Green fields.
func TestCheckinPathToGreenErrorShownNextToPathToGreenField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthYellow},
		"status": {"Slipping a little."},
	})
	doc := parsePage(t, body)
	const want = "A Yellow Goal needs a Path to Green"
	if msg := fieldError(t, doc, "path_target_date"); !strings.HasPrefix(msg, want) {
		t.Errorf("Path to Green field's error = %q, want it to start %q", msg, want)
	}
	if strings.Contains(textOf(doc), "validation failed") {
		t.Errorf("page shows the internal \"validation failed\" prefix; body:\n%s", body)
	}
}

// A Check-in that puts the Goal On Hold without a reason gets its error next to
// the Lifecycle reason field.
func TestCheckinLifecycleReasonErrorShownNextToReasonField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":    {domain.HealthGreen},
		"status":    {"Pausing."},
		"lifecycle": {domain.LifecycleOnHold},
	})
	if msg := fieldError(t, parsePage(t, body), "lifecycle_reason"); !strings.Contains(msg, "On Hold needs a reason") {
		t.Errorf("Lifecycle reason field's error = %q, want the On Hold one", msg)
	}
}

// A Check-in that marks the Goal Done without an outcome gets its error next to
// the outcome field.
func TestCheckinOutcomeErrorShownNextToOutcomeField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":    {domain.HealthGreen},
		"status":    {"Finished."},
		"lifecycle": {domain.LifecycleDone},
	})
	if msg := fieldError(t, parsePage(t, body), "outcome"); !strings.Contains(msg, "needs an outcome") {
		t.Errorf("outcome field's error = %q, want the outcome one", msg)
	}
}

// A Check-in asking for a Lifecycle change it can't make gets its error next to
// the Lifecycle choice.
func TestCheckinLifecycleChangeErrorShownNextToLifecycleField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":    {domain.HealthGreen},
		"status":    {"Back to the drawing board."},
		"lifecycle": {domain.LifecycleProposed},
	})
	// The Lifecycle is a radio group; its error follows the last radio.
	if msg := fieldError(t, parsePage(t, body), "lifecycle"); !strings.Contains(msg, "Active Goal to") {
		t.Errorf("Lifecycle field's error = %q, want the Lifecycle change one", msg)
	}
}

// A Check-in that moves the delivery date without a reason gets its error next
// to the delivery date's reason field.
func TestCheckinDeliveryDateReasonErrorShownNextToReasonField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":           {domain.HealthYellow},
		"status":           {"Vendor is late."},
		"path_to_green":    {"Swap vendors."},
		"path_target_date": {"2026-06-15"},
		"delivery_date":    {goal.DeliveryDate.AddDate(0, 0, 14).Format("2006-01-02")},
	})
	if msg := fieldError(t, parsePage(t, body), "delivery_date_reason"); !strings.Contains(msg, "delivery date needs a reason") {
		t.Errorf("delivery date reason field's error = %q, want the Date Slip one", msg)
	}
}

// A Check-in that moves a Milestone's date without a reason gets its error next
// to that Milestone's reason field.
func TestCheckinMilestoneDateReasonErrorShownNextToReasonField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal)
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Beta moves."},
		fmt.Sprintf("milestone_date_%d", beta.ID): {beta.TargetDate.AddDate(0, 0, 10).Format("2006-01-02")},
	})
	if msg := fieldError(t, parsePage(t, body), fmt.Sprintf("milestone_date_reason_%d", beta.ID)); !strings.Contains(msg, "needs a reason") {
		t.Errorf("Milestone reason field's error = %q, want the Date Slip one", msg)
	}
}

// A Check-in that removes a Milestone without a reason gets its error next to
// that Milestone's reason-for-removing field.
func TestCheckinMilestoneRemovedReasonErrorShownNextToReasonField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal)
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Dropping the beta."},
		fmt.Sprintf("milestone_status_%d", beta.ID): {domain.MilestoneRemoved},
	})
	if msg := fieldError(t, parsePage(t, body), fmt.Sprintf("milestone_removed_reason_%d", beta.ID)); !strings.Contains(msg, "Removing Milestone") {
		t.Errorf("Milestone removal field's error = %q, want the removal one", msg)
	}
}

// A Green Check-in that leaves a Milestone overdue gets its error next to that
// Milestone's date.
func TestCheckinOverdueMilestoneErrorShownNextToMilestoneDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal)
	samClient := signInClient(t, ts.URL, "sam@example.com")

	// Pulling Beta's date into the past leaves it overdue, which rules out Green.
	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"All good."},
		fmt.Sprintf("milestone_date_%d", beta.ID):        {h.Clock.Now().AddDate(0, 0, -1).Format("2006-01-02")},
		fmt.Sprintf("milestone_date_reason_%d", beta.ID): {"It was really due yesterday."},
	})
	msg := fieldError(t, parsePage(t, body), fmt.Sprintf("milestone_date_%d", beta.ID))
	if !strings.Contains(msg, "is overdue (due Jan 1), so the Goal can't be Green.") {
		t.Errorf("Milestone date field's error = %q, want the overdue one, its date as the page shows dates", msg)
	}
	if regexp.MustCompile(`\d{4}-\d{2}-\d{2}`).MatchString(msg) {
		t.Errorf("Milestone date field's error = %q, want no ISO date", msg)
	}
}

// A Check-in adding a Milestone without a date gets its error next to that new
// Milestone's date.
func TestCheckinNewMilestoneErrorShownNextToNewMilestone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":             {domain.HealthGreen},
		"status":             {"Adding docs."},
		"new_milestone_name": {"Launch party", "Docs"},
		"new_milestone_date": {"2026-05-01", ""},
	})
	// The Docs row is the second new Milestone; the error follows its date.
	doc := parsePage(t, body)
	names, dates := formFields(doc, "new_milestone_name"), formFields(doc, "new_milestone_date")
	if len(names) < 2 || len(dates) < 2 || names[1].Value != "Docs" {
		t.Fatalf("form lost the Docs row; body:\n%s", body)
	}
	if msg := errorAfter(t, doc, dates[1].Node); !strings.Contains(msg, "needs a date") {
		t.Errorf("new Milestone's error = %q, want the missing date one", msg)
	}
}

// Two new Milestones may share a name: the error is shown by the row the
// refusal is about, not the first row its message's wording could match.
func TestCheckinNewMilestoneErrorShownNextToItsRowAmongNamesakes(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":             {domain.HealthGreen},
		"status":             {"Adding docs."},
		"new_milestone_name": {"Docs", "Docs"},
		"new_milestone_date": {"2026-05-01", ""},
	})
	doc := parsePage(t, body)
	dates := formFields(doc, "new_milestone_date")
	if len(dates) < 2 {
		t.Fatalf("form lost a new Milestone row; body:\n%s", body)
	}
	if msg := errorAfter(t, doc, dates[1].Node); !strings.Contains(msg, "needs a date") {
		t.Errorf("second Docs row's error = %q, want the missing date one", msg)
	}
}

// A Green Check-in adding a Milestone that is already overdue gets its error
// next to that new Milestone.
func TestCheckinOverdueNewMilestoneErrorShownNextToNewMilestone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":             {domain.HealthGreen},
		"status":             {"Adding docs."},
		"new_milestone_name": {"Docs"},
		"new_milestone_date": {h.Clock.Now().AddDate(0, 0, -1).Format("2006-01-02")},
	})
	if msg := fieldError(t, parsePage(t, body), "new_milestone_date"); !strings.Contains(msg, "is overdue") {
		t.Errorf("new Milestone's error = %q, want the overdue one", msg)
	}
}

// A Milestone date that isn't a date gets its error next to that Milestone's
// date.
func TestCheckinInvalidMilestoneDateErrorShownNextToMilestoneDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal)
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"All good."},
		fmt.Sprintf("milestone_date_%d", beta.ID): {"next spring"},
	})
	if msg := fieldError(t, parsePage(t, body), fmt.Sprintf("milestone_date_%d", beta.ID)); !strings.Contains(msg, "invalid date") {
		t.Errorf("Milestone date field's error = %q, want the invalid date one", msg)
	}
}

// highlightRows returns a rendered Check-in form's Highlight rows, in order.
func highlightRows(t *testing.T, doc *html.Node) []*html.Node {
	t.Helper()
	return findAllByTestID(findByTestID(t, doc, "checkin-highlight-section"), "checkin-highlight-row")
}

// An Owner flags several Highlights in one Check-in, one per row of the form,
// in any mix of kinds and with kinds repeating; each is recorded in the order
// entered, and a row left blank is ignored (CONTEXT.md: Highlight).
func TestCheckinFormRecordsSeveralHighlightsInOrder(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	_, status := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Shipped the failover."},
		"highlight_kind": {
			domain.HighlightAccomplishment, domain.HighlightMiss, "", domain.HighlightInsight, domain.HighlightAccomplishment,
		},
		"highlight_note": {
			"Zero downtime on the cutover.", "The runbook was a week late.", "", "Drills find what reviews miss.", "Retired the old pager.",
		},
	})
	if status != http.StatusOK {
		t.Fatalf("submit: status %d, want 200", status)
	}

	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	var got []string
	for _, hl := range highlights {
		got = append(got, hl.Kind+": "+hl.Note)
	}
	want := []string{
		"Accomplishment: Zero downtime on the cutover.",
		"Miss: The runbook was a week late.",
		"Insight: Drills find what reviews miss.",
		"Accomplishment: Retired the old pager.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("highlights = %q, want %q", got, want)
	}
}

// A Highlight row with a note but no kind is refused with the error in that
// row, naming it, and nothing is recorded; the rows keep what was typed.
func TestCheckinHighlightWithNoKindRefusedInItsRow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":         {domain.HealthGreen},
		"status":         {"On track."},
		"highlight_kind": {domain.HighlightInsight, ""},
		"highlight_note": {"Retries masked the root cause.", "Found a cheaper vendor."},
	})
	rows := highlightRows(t, parsePage(t, body))
	if len(rows) != 2 {
		t.Fatalf("form shows %d Highlight rows, want the 2 typed", len(rows))
	}
	if hasTestID(rows[0], "checkin-error") {
		t.Errorf("Highlight 1 shows an error: %q", textOf(rows[0]))
	}
	if errs := textsOf(findAllByTestID(rows[1], "checkin-error")); len(errs) != 1 || !strings.Contains(errs[0], "Highlight 2 needs a kind") {
		t.Errorf("Highlight 2's errors = %q, want one naming it", errs)
	}
	if got := formField(t, rows[1], "highlight_note").Value; got != "Found a cheaper vendor." {
		t.Errorf("Highlight 2's note = %q, want the one typed", got)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("checkins = %d, want none recorded", len(history))
	}
}

// A fresh Check-in form shows one empty Highlight row and an Add another
// button.
func TestCheckinFormShowsOneEmptyHighlightRow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	rows := highlightRows(t, page)
	if len(rows) != 1 {
		t.Fatalf("form shows %d Highlight rows, want 1", len(rows))
	}
	if got := formField(t, rows[0], "highlight_note").Value; got != "" {
		t.Errorf("the Highlight row's note = %q, want it empty", got)
	}
	add := buttonNamed(findByTestID(t, page, "checkin-highlight-section"), "Add another")
	if add == nil || attrOf(add, "name") != "add_highlight" {
		t.Errorf("no Add another button posting add_highlight")
	}
}

// Enter in a field submits the form by its first submit button, so the
// Check-in form's first is a hidden plain submit, ahead of Add another: Enter
// records the Check-in rather than adding a Highlight row.
func TestCheckinFormEnterSubmitsTheCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	form := findByTestID(t, page, "checkin-form")
	first := submitButtons(form)[0]
	if attrOf(first, "data-testid") != "checkin-default-submit" {
		t.Fatalf("the form's first submit, which Enter presses, isn't the default submit: %s", renderNode(first))
	}
	if hasAttr(first, "name") {
		t.Errorf("the default submit posts a name, so it isn't a plain Submit check-in: %s", renderNode(first))
	}
	if add := buttonNamed(form, "Add another"); add == nil || precedes(add, first) {
		t.Errorf("the default submit comes after Add another")
	}
}

// Add another re-renders the form with one more Highlight row, keeping
// everything typed anywhere on the form, and records nothing — over htmx and
// as a plain form post without JavaScript.
func TestCheckinAddAnotherHighlightKeepsTypedAndRecordsNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		post func(t *testing.T, client *http.Client, rawURL string, form url.Values) (string, int)
	}{
		{"htmx", postFormHX},
		{"plain", func(t *testing.T, client *http.Client, rawURL string, form url.Values) (string, int) {
			resp := postForm(t, client, rawURL, form)
			defer func() { _ = resp.Body.Close() }()
			return readBody(t, resp), resp.StatusCode
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t)
			ts := newServer(t, h)

			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
			samClient := signInClient(t, ts.URL, "sam@example.com")

			body, status := tc.post(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
				"health":         {domain.HealthYellow},
				"status":         {"Failover slipped a week."},
				"path_to_green":  {"Drill again on Friday."},
				"highlight_kind": {domain.HighlightMiss},
				"highlight_note": {"The runbook was a week late."},
				"add_highlight":  {"1"},
			})
			if status != http.StatusOK {
				t.Fatalf("add another: status %d, want 200", status)
			}
			doc := parsePage(t, body)
			if hasTestID(doc, "checkin-error") {
				t.Errorf("add another shows a validation error: %q", textOf(findByTestID(t, doc, "checkin-error")))
			}
			rows := highlightRows(t, doc)
			if len(rows) != 2 {
				t.Fatalf("form shows %d Highlight rows, want 2", len(rows))
			}
			if note, kind := formField(t, rows[0], "highlight_note"), formField(t, rows[0], "highlight_kind"); note.Value != "The runbook was a week late." || kind.Value != domain.HighlightMiss {
				t.Errorf("Highlight 1 reads %q of kind %q, want what was typed", note.Value, kind.Value)
			}
			if got := formField(t, rows[1], "highlight_note").Value; got != "" {
				t.Errorf("the added row's note = %q, want it empty", got)
			}
			if !hasAttr(findByTestID(t, doc, "checkin-highlight-section"), "open") {
				t.Errorf("the Highlight section is collapsed after Add another")
			}
			for name, typed := range map[string]string{"status": "Failover slipped a week.", "path_to_green": "Drill again on Friday."} {
				if got := formField(t, doc, name).Value; got != typed {
					t.Errorf("form's %s = %q, want %q as typed", name, got, typed)
				}
			}
			if got := checkedRadio(doc, "health"); got != domain.HealthYellow {
				t.Errorf("form's Health = %q, want Yellow as picked", got)
			}
			if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
				t.Errorf("checkins = %d, want none recorded by Add another", len(history))
			}
		})
	}
}

// A Check-in without a status gets its error next to the status field.
func TestCheckinStatusErrorShownNextToStatusField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"  "},
	})
	if msg := fieldError(t, parsePage(t, body), "status"); !strings.Contains(msg, "needs a status") {
		t.Errorf("status field's error = %q, want the missing status one", msg)
	}
}

// A Check-in with a Health that isn't Green, Yellow, or Red gets its error next
// to the Health field.
func TestCheckinHealthErrorShownNextToHealthField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {"Purple"},
		"status": {"On track."},
	})
	// Health is a radio group; its error follows the last radio.
	if msg := fieldError(t, parsePage(t, body), "health"); !strings.Contains(msg, "Health must be") {
		t.Errorf("Health field's error = %q, want the Health one", msg)
	}
}

// An error about the Check-in as a whole rather than one field — here, checking
// in on a Cancelled Goal — is shown at the top of the form.
func TestCheckinErrorAboutNoOneFieldShownAtTopOfForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.OnHoldGoal(sam, "Reduce outages", "Outages cost trust.", "Waiting on budget.")
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          goal.ID,
		AuthorID:        sam.ID,
		Status:          "No budget this year.",
		Lifecycle:       domain.LifecycleCancelled,
		LifecycleReason: "Budget cut.",
	}); err != nil {
		t.Fatalf("cancel the Goal: %v", err)
	}
	samClient := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Trying anyway."},
	})
	if msg := formTopError(t, parsePage(t, body)); !strings.Contains(msg, "Only an Active Goal") {
		t.Errorf("the form's top error = %q, want the Active Goal one", msg)
	}
}

// Add another answers as the Check-in page does: the Owner and a Delegate get
// the form back with one more Highlight row, and anyone else gets 403 (#528).
func TestCheckinAddAnotherForOwnerAndDelegatesOnly(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	h.SignIn("eve@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, goal.ID)
	submitURL := fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID)
	addAnother := url.Values{
		"health":         {domain.HealthYellow},
		"highlight_kind": {domain.HighlightMiss},
		"highlight_note": {"The runbook was a week late."},
		"add_highlight":  {"1"},
	}

	for _, tc := range []struct {
		name string
		post func(t *testing.T, client *http.Client, rawURL string, form url.Values) (string, int)
	}{
		{"htmx", postFormHX},
		{"plain", func(t *testing.T, client *http.Client, rawURL string, form url.Values) (string, int) {
			resp := postForm(t, client, rawURL, form)
			defer func() { _ = resp.Body.Close() }()
			return readBody(t, resp), resp.StatusCode
		}},
	} {
		for _, who := range []string{"sam@example.com", "tpm@example.com"} {
			body, status := tc.post(t, signInClient(t, ts.URL, who), submitURL, addAnother)
			if status != http.StatusOK {
				t.Fatalf("%s, %s: add another: status %d, want 200", tc.name, who, status)
			}
			if rows := highlightRows(t, parsePage(t, body)); len(rows) != 2 {
				t.Errorf("%s, %s: form shows %d Highlight rows, want 2", tc.name, who, len(rows))
			}
		}

		if _, status := tc.post(t, signInClient(t, ts.URL, "eve@example.com"), submitURL, addAnother); status != http.StatusForbidden {
			t.Errorf("%s: someone neither Owner nor Delegate got status %d for Add another, want 403", tc.name, status)
		}
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("checkins = %d, want none recorded by Add another", len(history))
	}
}

// The Check-in has its own page, for the Owner or a Delegate: titled "Check in",
// with a breadcrumb back to the Goal and the form. Anyone else gets 403.
func TestCheckinPageForOwnerAndDelegatesOnly(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	h.SignIn("eve@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, goal.ID)
	checkinURL := fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)

	for _, who := range []string{"sam@example.com", "tpm@example.com"} {
		page := getPage(t, signInClient(t, ts.URL, who), checkinURL)
		if h1 := headingNamed(page, "Check in"); h1 == nil || h1.Data != "h1" {
			t.Errorf("%s: page is not titled Check in", who)
		}
		if crumb := linkTo(page, fmt.Sprintf("/goals/%d", goal.ID)); crumb == nil || textOf(crumb) != "Reduce outages" {
			t.Errorf("%s: page has no breadcrumb back to the Goal", who)
		}
		if !hasTestID(page, "checkin-form") {
			t.Errorf("%s: page has no Check-in form", who)
		}
	}

	resp, err := ctxGet(t, signInClient(t, ts.URL, "eve@example.com"), checkinURL)
	if err != nil {
		t.Fatalf("GET %s: %v", checkinURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("someone neither Owner nor Delegate got status %d, want 403", resp.StatusCode)
	}
}

// The Goal page no longer embeds the Check-in form: its header links the Owner
// to the Check-in page and keeps the one-click no-change button beside it.
func TestGoalPageLinksToCheckinPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	actions := findByTestID(t, page, "goal-actions")
	if link := linkTo(actions, fmt.Sprintf("/goals/%d/checkin", goal.ID)); link == nil || textOf(link) != "Check in" {
		t.Errorf("Goal page's header does not link to the Check-in page; it reads %q", textOf(actions))
	}
	if hasTestID(page, "checkin-form") {
		t.Errorf("Goal page still embeds the Check-in form")
	}
	if !hasTestID(actions, "no-change-checkin") {
		t.Errorf("Goal page's header lost the no-change button; it reads %q", textOf(actions))
	}
}

// Health is picked from a radio group, not a select, picked from the latest
// Check-in. The Path to Green fieldset stays in the form, hidden by CSS alone
// while Green is picked (styles_test.go).
func TestCheckinPageHealthIsRadioGroup(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Slipping.", "Add a second on-call.", pathDate)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	var got []string
	for _, f := range formFields(page, "health") {
		got = append(got, fmt.Sprintf("%s %s=%s", f.Type, f.Label, f.Value))
	}
	if want := []string{"radio Green=Green", "radio Yellow=Yellow", "radio Red=Red"}; !slices.Equal(got, want) {
		t.Errorf("Health fields = %q, want the radio group %q", got, want)
	}
	if got := checkedRadio(page, "health"); got != domain.HealthYellow {
		t.Errorf("Health picked = %q, want the latest Check-in's Yellow", got)
	}
	if !hasTestID(page, "path-to-green-field") {
		t.Errorf("Path to Green fieldset left the form")
	}
}

// The Highlight, the dates and Milestones, and the Lifecycle each sit in a
// section collapsed by default, its summary line showing the current value.
func TestCheckinPageOptionalSectionsCollapsedByDefault(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	for testID, summary := range map[string]string{
		"checkin-highlight-section": "Flag a highlight",
		"checkin-dates-section":     "Change dates or milestones · delivers Jul 2 · 1 planned milestone",
		"checkin-lifecycle-section": "Pause, finish, or cancel this goal · stays Active",
	} {
		section := findByTestID(t, page, testID)
		if hasAttr(section, "open") {
			t.Errorf("%s is open on a fresh Check-in", testID)
		}
		if got := sectionSummary(t, section); !strings.Contains(got, summary) {
			t.Errorf("%s summary reads %q, want it to read %q", testID, got, summary)
		}
	}
}

// sectionSummary is what a collapsible section's summary line reads.
func sectionSummary(t *testing.T, section *html.Node) string {
	t.Helper()
	return textOf(firstTag(t, section, "summary"))
}

// The Check-in page opened with ?open=dates, as a Schedule conflict's Fix
// opens it, has its dates and Milestones section open and the others
// collapsed; any other open value, or none, leaves the dates collapsed.
func TestCheckinPageOpenDatesOpensTheDatesSection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")
	checkin := fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)

	page := getPage(t, client, checkin+"?open=dates")
	for testID, open := range map[string]bool{
		"checkin-dates-section":     true,
		"checkin-highlight-section": false,
		"checkin-lifecycle-section": false,
	} {
		if got := hasAttr(findByTestID(t, page, testID), "open"); got != open {
			t.Errorf("with ?open=dates, %s open = %v, want %v", testID, got, open)
		}
	}
	for _, query := range []string{"?open=other", ""} {
		page := getPage(t, client, checkin+query)
		if hasAttr(findByTestID(t, page, "checkin-dates-section"), "open") {
			t.Errorf("with %q, the dates section is open, want it collapsed", query)
		}
	}
}

// A collapsed section opens on a re-render when it holds the error or a value
// the reader typed, so the field at fault is never hidden.
func TestCheckinSectionOpensOnErrorOrSubmittedValue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		form    url.Values
		section string
	}{
		{"highlight error", url.Values{"highlight_note": {"Found a cheaper vendor."}}, "checkin-highlight-section"},
		{"highlight typed", url.Values{"status": {""}, "highlight_note": {"Found a cheaper vendor."}}, "checkin-highlight-section"},
		{"delivery date error", url.Values{"delivery_date": {"2026-08-01"}}, "checkin-dates-section"},
		{"new milestone typed", url.Values{"status": {""}, "new_milestone_name": {"GA"}, "new_milestone_date": {"2026-05-01"}}, "checkin-dates-section"},
		{"lifecycle error", url.Values{"lifecycle": {domain.LifecycleOnHold}}, "checkin-lifecycle-section"},
		{"lifecycle picked", url.Values{"status": {""}, "lifecycle": {domain.LifecycleDone}, "outcome": {"Shipped."}}, "checkin-lifecycle-section"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t)
			ts := newServer(t, h)

			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
			form := url.Values{"health": {domain.HealthGreen}, "status": {"On track."}}
			for k, v := range tc.form {
				form[k] = v
			}

			body, _ := postFormHX(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), form)
			doc := parsePage(t, body)
			if !hasTestID(doc, "checkin-error") {
				t.Fatalf("expected a validation error; body:\n%s", body)
			}
			if !hasAttr(findByTestID(t, doc, tc.section), "open") {
				t.Errorf("%s stays collapsed", tc.section)
			}
		})
	}
}

// The Goal page's menu leads to the Check-in page with a Lifecycle change
// picked (?lifecycle=): the Lifecycle section opens with that choice selected
// and its reason and outcome empty, ready to write.
func TestCheckinPageOpensWithLifecyclePickedFromQuery(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct{ query, picked, summary string }{
		{"On%20Hold", "Put On Hold", "Pause, finish, or cancel this goal · On Hold"},
		{"Done", "Mark Done", "Pause, finish, or cancel this goal · Done"},
		{"Cancelled", "Cancel", "Pause, finish, or cancel this goal · Cancelled"},
	} {
		page := getPage(t, samClient, fmt.Sprintf("%s/goals/%d/checkin?lifecycle=%s", ts.URL, goal.ID, tc.query))
		section := findByTestID(t, page, "checkin-lifecycle-section")
		if !hasAttr(section, "open") {
			t.Errorf("?lifecycle=%s: the Lifecycle section stays collapsed", tc.query)
		}
		if got := sectionSummary(t, section); got != tc.summary {
			t.Errorf("?lifecycle=%s: summary reads %q, want %q", tc.query, got, tc.summary)
		}
		if got := checkedRadio(section, "lifecycle"); got != tc.picked {
			t.Errorf("?lifecycle=%s: %q is picked, want %q", tc.query, got, tc.picked)
		}
		if got := formField(t, section, "lifecycle_reason").Value; got != "" {
			t.Errorf("?lifecycle=%s: the reason reads %q, want it empty", tc.query, got)
		}
		if outcome := formField(t, section, "outcome"); outcome.Type != "text" || outcome.Value != "" {
			t.Errorf("?lifecycle=%s: the outcome is a %s reading %q, want an empty text field", tc.query, outcome.Type, outcome.Value)
		}
	}
}

// Following the Goal page's Put on hold… leads to a plain Check-in: submitted
// with no reason it is refused as any On Hold Check-in is, the reason's error
// beside its field; with a reason the Goal is On Hold and History shows the
// change on that Check-in.
func TestCheckinFromPutOnHoldMenuItemIsStillACheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	more := findByTestID(t, getPage(t, samClient, goalURL), "goal-more")
	putOnHold := findAll(more, func(n *html.Node) bool { return n.Data == "a" && textOf(n) == "Put on hold…" })
	if len(putOnHold) != 1 {
		t.Fatalf("the Goal page's menu has %d Put on hold… items, want 1: %q", len(putOnHold), textOf(more))
	}
	page := getPage(t, samClient, ts.URL+attrOf(putOnHold[0], "href"))
	if picked := checkedRadio(findByTestID(t, page, "checkin-lifecycle-section"), "lifecycle"); picked != "Put On Hold" {
		t.Fatalf("Put on hold… opened the Check-in with %q picked", picked)
	}
	form := url.Values{"health": {domain.HealthGreen}, "status": {"Pausing for the reorg."}, "lifecycle": {domain.LifecycleOnHold}}

	body, _ := postFormHX(t, samClient, goalURL+"/checkins", form)
	if msg := fieldError(t, parsePage(t, body), "lifecycle_reason"); !strings.Contains(msg, "On Hold needs a reason") {
		t.Errorf("with no reason the error is %q, want the On Hold one", msg)
	}
	if got, err := h.Service.ViewGoal(t.Context(), goal.ID); err != nil || got.Lifecycle != domain.LifecycleActive {
		t.Fatalf("after a refused Check-in the Goal is %q (%v), want Active", got.Lifecycle, err)
	}

	form.Set("lifecycle_reason", "Team moved to the payments incident.")
	if status := postFormStatus(t, samClient, goalURL+"/checkins", form); status != http.StatusOK {
		t.Fatalf("put On Hold: status %d", status)
	}
	page = getPage(t, samClient, goalURL)
	if got := textOf(findByTestID(t, page, "goal-lifecycle")); got != domain.LifecycleOnHold {
		t.Errorf("Goal page shows the Goal %q, want On Hold", got)
	}
	if entry := textOf(findByTestID(t, page, "checkin-lifecycle")); !strings.Contains(entry, "Active → On Hold") || !strings.Contains(entry, "Team moved to the payments incident.") {
		t.Errorf("History missing the Lifecycle change; it reads %q", entry)
	}
}

// A ?lifecycle= the form doesn't offer from the Goal's current Lifecycle, or
// one that is the form's default, is ignored: the page renders as it does
// without it, Lifecycle section closed and the default picked. It is never
// refused.
func TestCheckinPageIgnoresLifecycleQueryItDoesNotOffer(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	active := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	onHold := h.OnHoldGoal(sam, "Grow revenue", "Revenue funds the rest.", "Waiting on budget.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct {
		goal   domain.Goal
		query  string
		picked string
	}{
		{onHold, "Done", "Resume (Active)"},
		{onHold, "On%20Hold", "Resume (Active)"},
		{onHold, "Active", "Resume (Active)"},
		{active, "bogus", "Keep Active"},
		{active, "Active", "Keep Active"},
		{active, "", "Keep Active"},
	} {
		checkinURL := fmt.Sprintf("%s/goals/%d/checkin", ts.URL, tc.goal.ID)
		plain := findByTestID(t, getPage(t, samClient, checkinURL), "checkin-form")
		form := findByTestID(t, getPage(t, samClient, checkinURL+"?lifecycle="+tc.query), "checkin-form")
		if got, want := renderNode(form), renderNode(plain); got != want {
			t.Errorf("%s ?lifecycle=%s: the form differs from the one without it:\n%s\nwant:\n%s", tc.goal.Lifecycle, tc.query, got, want)
		}
		section := findByTestID(t, form, "checkin-lifecycle-section")
		if hasAttr(section, "open") {
			t.Errorf("%s ?lifecycle=%s: the Lifecycle section is open", tc.goal.Lifecycle, tc.query)
		}
		if got := checkedRadio(section, "lifecycle"); got != tc.picked {
			t.Errorf("%s ?lifecycle=%s: %q is picked, want the default %q", tc.goal.Lifecycle, tc.query, got, tc.picked)
		}
	}
}

// When the Goal is Active and has a previous Check-in, the Check-in page opens
// with the one-click no-change card, dated from that Check-in, above the form.
// With no previous Check-in there is nothing to repeat, so there is no card.
func TestCheckinPageOffersNoChangeFirst(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")
	checkinURL := fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)

	if page := getPage(t, samClient, checkinURL); hasTestID(page, "no-change-checkin") {
		t.Errorf("a Goal with no Check-in offers the no-change card")
	}

	latest := h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	page := getPage(t, samClient, checkinURL)
	card := findByTestID(t, page, "no-change-card")
	if !precedes(card, findByTestID(t, page, "checkin-form")) {
		t.Errorf("the no-change card does not come before the form")
	}
	if want := "Nothing changed since " + latest.CreatedAt.Format("Jan 2") + "?"; !strings.Contains(textOf(card), want) {
		t.Errorf("no-change card reads %q, want it to ask %q", textOf(card), want)
	}
	if button := buttonNamed(findByTestID(t, card, "no-change-checkin"), "No change"); button == nil {
		t.Errorf("no-change card has no No change button; it reads %q", textOf(card))
	}
}

// Cancelling a Goal through the Lifecycle section asks for confirmation
// before the Check-in is sent.
func TestCheckinFormConfirmsCancellingTheGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	confirm := attrOf(findByTestID(t, page, "checkin-form"), "hx-on:htmx:confirm")
	if !strings.Contains(confirm, "value=Cancelled]:checked") || !strings.Contains(confirm, "confirm(") {
		t.Errorf("the form does not confirm a Cancel before sending; its htmx confirm handler: %q", confirm)
	}
}

// A No change that would repeat a Green after a Milestone went overdue is
// refused, and the person lands on that Goal's Check-in page with the reason at
// the top of the form, in plain words, and nothing recorded (#103).
func TestNoChangeRefusedWhileOverdueLandsOnCheckinForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Blue-green deploys for the monolith", "Deploys take the site down.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(100 * day) // past the Beta Milestone's date
	client := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	assertNoChangeRefusalOnForm(t, page, `Milestone "Beta" is overdue`)
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("a refused No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// assertNoChangeRefusalOnForm checks a refused No change came back as the
// Check-in page, inside the site's chrome, with the reason as the form's top
// error and without the internal "validation failed:" prefix. The reason may
// carry HTML entities, as the page's source does.
func assertNoChangeRefusalOnForm(t *testing.T, page, reason string) {
	t.Helper()
	reason = html.UnescapeString(reason)
	doc := parsePage(t, page)
	if !hasTestID(doc, "nav-home") {
		t.Errorf("refusal is not inside the normal page; body:\n%s", page)
	}
	if !hasTestID(doc, "checkin-form") {
		t.Fatalf("refusal does not show the Check-in form; body:\n%s", page)
	}
	if msg := formTopError(t, doc); !strings.Contains(msg, reason) {
		t.Errorf("the form's error = %q, want it to give the reason %q", msg, reason)
	}
	if strings.Contains(textOf(doc), "validation failed") {
		t.Errorf("the reason carries the internal prefix; body:\n%s", page)
	}
}

// postNoChange clicks a No change button: it posts to the button's action, as
// htmx does when hx is set, and returns the response with its body read.
func postNoChange(t *testing.T, client *http.Client, rawURL string, hx bool) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rawURL, strings.NewReader(""))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	return resp, readBody(t, resp)
}

// overdueGreenGoal arranges a Goal whose latest Check-in is Green and whose
// Beta Milestone has since gone overdue, so No change on it is refused.
func overdueGreenGoal(h *testsupport.Harness, owner domain.Account) domain.Goal {
	return overdueGoal(h, owner, domain.HealthGreen)
}

// overdueGoal arranges a Goal whose latest Check-in has health (with a Path to
// Green and its date when Yellow or Red) and whose Beta Milestone, due Apr 2,
// has since gone overdue, so No change on it is refused.
func overdueGoal(h *testsupport.Harness, owner domain.Account, health string) domain.Goal {
	goal := h.ActiveGoal(owner, "Blue-green deploys for the monolith", "Deploys take the site down.")
	path, pathTarget := "", time.Time{}
	if health != domain.HealthGreen {
		path, pathTarget = "Add a second reviewer.", pathDate
	}
	h.Checkin(owner, goal.ID, health, "On track.", path, pathTarget)
	h.Clock.Advance(100 * day) // past the Beta Milestone's date
	return goal
}

// A No change refused for an overdue Milestone reads the same on the card and,
// once refused, as the form's top error: a sentence with the due date shown as
// the page shows dates, not as ISO text (#342).
func TestNoChangeOverdueRefusalReadsTheSameOnCardAndForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := overdueGreenGoal(h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")
	const want = `Milestone "Beta" is overdue (due Apr 2): submit a Check-in to move its date, mark it Done or remove it.`

	card := findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)), "no-change-card")
	if got := textOf(findByTestID(t, card, "no-change-unavailable")); got != want {
		t.Errorf("the card's reason = %q, want %q", got, want)
	}

	resp, refused := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)
	defer func() { _ = resp.Body.Close() }()
	if got := formTopError(t, parsePage(t, refused)); got != want {
		t.Errorf("the form's top error = %q, want %q", got, want)
	}
}

// While a Milestone is overdue the Check-in page doesn't preselect the latest
// Check-in's Green — a slip is never hidden behind a Green — but leaves the
// Health to pick, with the dates and Milestones section open; so does the page
// a refused No change lands on. A Yellow or Red stays picked, the section
// open too (#342).
func TestCheckinPagePreselectsNoGreenWhileMilestoneOverdue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ health, want string }{
		{domain.HealthGreen, ""},
		{domain.HealthYellow, domain.HealthYellow},
	} {
		t.Run(tc.health, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			ts := newServer(t, h)
			sam := h.SignIn("sam@example.com")
			goal := overdueGoal(h, sam, tc.health)
			client := signInClient(t, ts.URL, "sam@example.com")

			resp, refused := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)
			defer func() { _ = resp.Body.Close() }()
			for name, page := range map[string]*html.Node{
				"Check-in page":     getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)),
				"refused No change": parsePage(t, refused),
			} {
				if got := checkedRadio(page, "health"); got != tc.want {
					t.Errorf("%s: Health checked = %q, want %q", name, got, tc.want)
				}
				if !hasAttr(findByTestID(t, page, "checkin-dates-section"), "open") {
					t.Errorf("%s: the dates and Milestones section is collapsed", name)
				}
			}
		})
	}
}

// A No change refused because a Metric is off target leaves Green picked: the
// full form can confirm that Green (#342).
func TestCheckinPageKeepsGreenWhileMetricOffTarget(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetIdleGoal(t, h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	if got := checkedRadio(getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)), "health"); got != domain.HealthGreen {
		t.Errorf("Health checked = %q, want Green", got)
	}
}

// The Check-in page's "Nothing changed since …?" card offers its No change
// only where it would be recorded. On a Goal whose Milestone has gone overdue
// it says why in place of the button, and once a No change has been refused it
// leaves the reason to the form's top error, so it shows once. On a Goal a No
// change would repeat it keeps its button (#235).
func TestCheckinPageNoChangeCardSaysWhyNoChangeIsUnavailable(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	overdue := overdueGreenGoal(h, sam)
	repeatable := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, repeatable.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")

	c := findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, overdue.ID)), "no-change-card")
	if hasTestID(c, "no-change-checkin") {
		t.Errorf("the overdue Goal's card offers a No change that would be refused: %q", textOf(c))
	}
	reason := textOf(findByTestID(t, c, "no-change-unavailable"))
	if !strings.HasPrefix(reason, `Milestone "Beta" is overdue`) || !strings.HasSuffix(reason, ".") {
		t.Errorf("the card's reason = %q, want the overdue Milestone named, as a sentence", reason)
	}

	resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, overdue.ID), false)
	defer func() { _ = resp.Body.Close() }()
	doc := parsePage(t, page)
	if c := findByTestID(t, doc, "no-change-card"); hasTestID(c, "no-change-unavailable") {
		t.Errorf("after a refused No change, the card repeats the form's error: %q", textOf(c))
	}
	if n := strings.Count(textOf(doc), `Milestone "Beta" is overdue`); n != 1 {
		t.Errorf("after a refused No change, the reason shows %d times, want once", n)
	}
	assertNoChangeRefusalOnForm(t, page, `Milestone "Beta" is overdue`)

	c = findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, repeatable.ID)), "no-change-card")
	if !hasTestID(c, "no-change-checkin") {
		t.Errorf("the repeatable Goal's card has no No change: %q", textOf(c))
	}
	if hasTestID(c, "no-change-unavailable") {
		t.Errorf("the repeatable Goal's card says No change is unavailable: %q", textOf(c))
	}
}

// A No change refused over htmx comes back as the whole Check-in page with 200,
// so htmx swaps it in, and the Check-in page's URL is pushed so the address bar
// matches what the person sees (#103).
func TestNoChangeRefusedOverHtmxSwapsInCheckinPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := overdueGreenGoal(h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), true)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("htmx status = %d, want 200 so htmx swaps the page in", resp.StatusCode)
	}
	if got, want := resp.Header.Get("HX-Push-Url"), fmt.Sprintf("/goals/%d/checkin", goal.ID); got != want {
		t.Errorf("HX-Push-Url = %q, want %q", got, want)
	}
	assertNoChangeRefusalOnForm(t, page, `Milestone "Beta" is overdue`)
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("a refused No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// A No change whose repeated Health now differs from the Rolled-up Health, with
// no explanation to carry over, lands on the form to explain (#103).
func TestNoChangeRefusedWhenRollupDiffersLandsOnCheckinForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Reliable platform", "Outages cost trust.")
	child := h.ActiveChildOf(sam, parent, "Blue-green deploys", "Deploys take the site down.")
	h.Checkin(sam, parent.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(sam, child.ID, domain.HealthRed, "Blocked.", "Unblock the pipeline.", h.Clock.Now().AddDate(0, 1, 0))
	client := signInClient(t, ts.URL, "sam@example.com")

	resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, parent.ID), false)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	assertNoChangeRefusalOnForm(t, page, "differs from the Rolled-up Health (Red)")
	if status := formField(t, parsePage(t, page), "status"); status.Value != "" || status.Placeholder != "On track." {
		t.Errorf("status = %q with ghost text %q, want empty with the last Check-in's as ghost text", status.Value, status.Placeholder)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), parent.ID); len(history) != 1 {
		t.Errorf("a refused No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// A No change on a Goal never checked in on has nothing to repeat, so it lands
// on the form to write the first Check-in (#103).
func TestNoChangeRefusedWithoutPreviousCheckinLandsOnCheckinForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, hx := range []bool{false, true} {
		resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), hx)
		defer func() { _ = resp.Body.Close() }()
		assertNoChangeRefusalOnForm(t, page, "There is no previous Check-in to repeat.")
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a refused No change recorded a Check-in: history has %d, want 0", len(history))
	}
}

// A No change on a Goal that isn't Active is refused, posted plainly or through
// htmx: an On Hold Goal lands on its Check-in form with the reason, and a
// Cancelled one on its Check-in page with the reason and no form (#109).
func TestNoChangeRefusedOnGoalNotActive(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	onHold := h.OnHoldGoal(sam, "Ship search", "People can't find things.", "Waiting on budget.")
	cancelled := closeGoal(t, h, sam, h.ActiveGoal(sam, "Cut churn", "Customers leave."), domain.LifecycleCancelled)
	client := signInClient(t, ts.URL, "sam@example.com")
	const reason = "Only an Active Goal can be checked in on."

	for _, hx := range []bool{false, true} {
		for _, goal := range []domain.Goal{onHold, cancelled} {
			resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), hx)
			defer func() { _ = resp.Body.Close() }()
			want, push := http.StatusUnprocessableEntity, ""
			if hx {
				want, push = http.StatusOK, fmt.Sprintf("/goals/%d/checkin", goal.ID)
			}
			if resp.StatusCode != want {
				t.Errorf("%s, hx=%v: status = %d, want %d", goal.Lifecycle, hx, resp.StatusCode, want)
			}
			if got := resp.Header.Get("HX-Push-Url"); got != push {
				t.Errorf("%s, hx=%v: HX-Push-Url = %q, want %q", goal.Lifecycle, hx, got, push)
			}
			if goal.Lifecycle == domain.LifecycleOnHold {
				assertNoChangeRefusalOnForm(t, page, reason)
				continue
			}
			doc := parsePage(t, page)
			if !hasTestID(doc, "nav-home") || hasTestID(doc, "checkin-form") {
				t.Errorf("Cancelled, hx=%v: want the Check-in page without a form; body:\n%s", hx, page)
			}
			if msg := textOf(findByTestID(t, doc, "checkin-error")); !strings.Contains(msg, reason) {
				t.Errorf("Cancelled, hx=%v: error = %q, want %q", hx, msg, reason)
			}
			if got := textsOf(findAllByTestID(doc, "goal-no-health")); !slices.Contains(got, "A Cancelled Goal takes no Check-ins.") {
				t.Errorf("Cancelled, hx=%v: page says %q, want it to say the Goal takes no Check-ins", hx, got)
			}
		}
	}
	for _, goal := range []domain.Goal{onHold, cancelled} {
		if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
			t.Errorf("%s: a refused No change recorded a Check-in: history has %d, want 1", goal.Lifecycle, len(history))
		}
	}
}

// Someone who may not check in on a Goal gets a 403 that says so inside the
// normal page, not bare text (#103).
func TestNoChangeByNonDelegateIsForbiddenInsideThePage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "kim@example.com")

	for _, hx := range []bool{false, true} {
		resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), hx)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("hx=%v: status = %d, want 403", hx, resp.StatusCode)
		}
		assertCheckinUnavailable(t, page, "Only the Goal's Owner or a Delegate may check in on Ship search.")
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("a forbidden No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// A No change on a Goal that doesn't exist gets a 404 that says so inside the
// normal page (#103).
func TestNoChangeOnMissingGoalIsNotFoundInsideThePage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")

	resp, page := postNoChange(t, client, ts.URL+"/goals/9999/checkins/no-change", false)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	assertCheckinUnavailable(t, page, "There is no such Goal")
}

// htmx 2 leaves an error status unswapped, so the No change button, on the
// Goal page and on the Check-in page's card, swaps any error page in itself,
// whatever its status: the click shows why it was refused instead of seeming
// to do nothing (#109, #116).
func TestNoChangeButtonSwapsInAnyErrorPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	samClient := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{fmt.Sprintf("/goals/%d", goal.ID), fmt.Sprintf("/goals/%d/checkin", goal.ID)} {
		handler := attrOf(findByTestID(t, getPage(t, samClient, ts.URL+path), "no-change-checkin"), "hx-on:htmx:response-error")
		if !strings.Contains(handler, "htmx.swap('body', event.detail.xhr.responseText") {
			t.Errorf("on %s, the No change form doesn't swap an error page in; its handler: %q", path, handler)
		}
		if before, _, _ := strings.Cut(handler, "htmx.swap"); strings.Contains(before, "status") {
			t.Errorf("on %s, the No change form swaps only some error statuses; its handler: %q", path, handler)
		}
	}

	resp, page := postNoChange(t, signInClient(t, ts.URL, "kim@example.com"), fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), true)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("forbidden: status = %d, want 403", resp.StatusCode)
	}
	assertCheckinUnavailable(t, page, "Only the Goal's Owner or a Delegate may check in on Ship search.")

	resp, page = postNoChange(t, samClient, ts.URL+"/goals/9999/checkins/no-change", true)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing: status = %d, want 404", resp.StatusCode)
	}
	assertCheckinUnavailable(t, page, "There is no such Goal")
}

// A No change that fails for want of the database gets a 500 that says so
// inside the normal page, which the button swaps in like any error (#116).
func TestNoChangeThatFailsSaysSoInsideThePage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	if _, err := h.DB.ExecContext(t.Context(), `CREATE TRIGGER fail_checkin BEFORE INSERT ON checkins
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, hx := range []bool{false, true} {
		resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), hx)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("hx=%v: status = %d, want 500", hx, resp.StatusCode)
		}
		assertCheckinUnavailable(t, page, "The No change couldn't be recorded. Try again.")
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("a failed No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// assertCheckinUnavailable checks a No change failure came back inside the
// site's chrome with a sentence saying what happened. The sentence may carry
// HTML entities, as the page's source does.
func assertCheckinUnavailable(t *testing.T, page, sentence string) {
	t.Helper()
	sentence = html.UnescapeString(sentence)
	doc := parsePage(t, page)
	if !hasTestID(doc, "nav-home") {
		t.Errorf("failure is not inside the normal page; body:\n%s", page)
	}
	if got := textOf(findByTestID(t, doc, "checkin-unavailable")); !strings.Contains(got, sentence) {
		t.Errorf("failure says %q, want %q", got, sentence)
	}
}

// A successful No change over htmx still sends the person back to the Goal
// page with HX-Redirect (#103).
func TestNoChangeOverHtmxRedirectsToGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")

	resp, _ := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), true)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := resp.Header.Get("HX-Redirect"), fmt.Sprintf("/goals/%d", goal.ID); got != want {
		t.Errorf("HX-Redirect = %q, want %q", got, want)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 2 {
		t.Errorf("history has %d, want 2 (the Check-in and its No change)", len(history))
	}
}

// The Check-in form's Highlight section opens with one pre-filled row per
// pending Draft Highlight, oldest first, each ticked to keep, posting its id
// in a hidden field and as its Keep box's value, then the usual empty row;
// every row is numbered (CONTEXT.md: Draft Highlight).
func TestCheckinFormOffersEachPendingDraftHighlightAsATickedRow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	first := h.LogDraftHighlight(sam, goal.ID, domain.HighlightAccomplishment, "Cut paging noise by half.")
	h.Clock.Advance(time.Hour)
	second := h.LogDraftHighlight(sam, goal.ID, "", "Vendor may raise prices.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	rows := highlightRows(t, page)
	if len(rows) != 3 {
		t.Fatalf("form shows %d Highlight rows, want 2 offered and 1 empty", len(rows))
	}
	for i, tc := range []struct {
		draft domain.DraftHighlight
		kind  string
	}{{first, domain.HighlightAccomplishment}, {second, ""}} {
		id := strconv.FormatInt(tc.draft.ID, 10)
		row := rows[i]
		if hidden := formField(t, row, "draft_highlight_id"); hidden.Type != "hidden" || hidden.Value != id {
			t.Errorf("row %d's id field is a %s holding %q, want a hidden field holding Draft Highlight %s", i+1, hidden.Type, hidden.Value, id)
		}
		if keep := formField(t, row, "draft_highlight_keep"); keep.Type != "checkbox" || keep.Value != id || !keep.Checked || keep.Label != "Keep" {
			t.Errorf("row %d's Keep box = %+v, want a ticked checkbox labelled Keep with value %s", i+1, keep, id)
		}
		if kind := formField(t, row, "draft_highlight_kind_"+id); kind.Type != "select" || kind.Value != tc.kind {
			t.Errorf("row %d's kind is a %s set to %q, want a select set to %q", i+1, kind.Type, kind.Value, tc.kind)
		}
		if note := formField(t, row, "draft_highlight_note_"+id); note.Type != "textarea" || note.Value != tc.draft.Note {
			t.Errorf("row %d's note is a %s reading %q, want an editable %q", i+1, note.Type, note.Value, tc.draft.Note)
		}
	}
	if got := formField(t, rows[2], "highlight_note").Value; got != "" || len(draftHighlightFields(rows[2])) > 0 {
		t.Errorf("the third row isn't the usual empty row; row:\n%s", renderNode(rows[2]))
	}
	if got := textsOf(findAllByTestID(page, "checkin-highlight-legend")); !slices.Equal(got, []string{"Highlight 1 · Draft", "Highlight 2 · Draft", "Highlight 3"}) {
		t.Errorf("legends = %q, want every row numbered, the offered ones marked Draft", got)
	}
	if !hasAttr(findByTestID(t, page, "checkin-highlight-section"), "open") {
		t.Errorf("the Highlight section is collapsed while it offers Draft Highlights")
	}
}

// draftHighlightFields are the fields under root that post a Draft Highlight.
func draftHighlightFields(root *html.Node) []*html.Node {
	return findAll(root, func(n *html.Node) bool { return strings.HasPrefix(attrOf(n, "name"), "draft_highlight") })
}

// Submitting the form with two offered rows kept, one edited, and one
// unticked records the two kept as Highlights with the text as edited, ahead
// of a typed row, and discards the unticked one with the Check-in; afterwards
// none is pending.
func TestCheckinFormKeepsEditsAndDiscardsDraftHighlights(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	a := h.LogDraftHighlight(sam, goal.ID, domain.HighlightAccomplishment, "Zero downtime on the cutover.")
	b := h.LogDraftHighlight(sam, goal.ID, "", "Vendor may raise prices.")
	c := h.LogDraftHighlight(sam, goal.ID, domain.HighlightInsight, "Drills find gaps.")
	id := func(d domain.DraftHighlight) string { return strconv.FormatInt(d.ID, 10) }

	_, status := postFormHX(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":                        {domain.HealthGreen},
		"status":                        {"On track."},
		"draft_highlight_id":            {id(a), id(b), id(c)},
		"draft_highlight_keep":          {id(a), id(c)},
		"draft_highlight_kind_" + id(a): {domain.HighlightAccomplishment},
		"draft_highlight_note_" + id(a): {"Zero downtime on the cutover."},
		"draft_highlight_kind_" + id(b): {""},
		"draft_highlight_note_" + id(b): {"Vendor may raise prices."},
		"draft_highlight_kind_" + id(c): {domain.HighlightMiss},
		"draft_highlight_note_" + id(c): {"Drills found a gap reviews missed."},
		"highlight_kind":                {domain.HighlightInsight},
		"highlight_note":                {"Typed at the Check-in."},
	})
	if status != http.StatusOK {
		t.Fatalf("submit: status %d, want 200", status)
	}

	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	var got []string
	for _, hl := range highlights {
		got = append(got, hl.Kind+": "+hl.Note)
	}
	want := []string{
		"Accomplishment: Zero downtime on the cutover.",
		"Miss: Drills found a gap reviews missed.",
		"Insight: Typed at the Check-in.",
	}
	if !slices.Equal(got, want) {
		t.Errorf("highlights = %q, want %q", got, want)
	}
	checkins, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil || len(checkins) != 1 {
		t.Fatalf("ListCheckins = %d, %v; want the one Check-in", len(checkins), err)
	}
	if d := checkins[0].DiscardedDraftHighlights; len(d) != 1 || d[0].ID != b.ID {
		t.Errorf("discarded = %v, want only the unticked one", d)
	}
	if got := pendingDraftNotes(t, h, sam, goal.ID); len(got) != 0 {
		t.Errorf("pending = %q, want none", got)
	}
}

// A kept offered row with no kind is refused in its row, named by its
// position among every row on the form; nothing is recorded, every Draft
// Highlight stays pending, and each row keeps its Keep box, kind and note as
// typed.
func TestCheckinFormRefusesAKeptDraftHighlightWithNoKindInItsRow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	a := h.LogDraftHighlight(sam, goal.ID, domain.HighlightInsight, "First.")
	b := h.LogDraftHighlight(sam, goal.ID, "", "No kind yet.")
	id := func(d domain.DraftHighlight) string { return strconv.FormatInt(d.ID, 10) }

	body, _ := postFormHX(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":                        {domain.HealthGreen},
		"status":                        {"On track."},
		"draft_highlight_id":            {id(a), id(b)},
		"draft_highlight_keep":          {id(b)},
		"draft_highlight_kind_" + id(a): {domain.HighlightInsight},
		"draft_highlight_note_" + id(a): {"First, edited."},
		"draft_highlight_kind_" + id(b): {""},
		"draft_highlight_note_" + id(b): {"No kind yet, edited."},
		"highlight_kind":                {""},
		"highlight_note":                {""},
	})
	rows := highlightRows(t, parsePage(t, body))
	if len(rows) != 3 {
		t.Fatalf("form shows %d Highlight rows, want the 3 posted", len(rows))
	}
	if hasTestID(rows[0], "checkin-error") {
		t.Errorf("Highlight 1 shows an error: %q", textOf(rows[0]))
	}
	if errs := textsOf(findAllByTestID(rows[1], "checkin-error")); len(errs) != 1 || !strings.Contains(errs[0], "Highlight 2 needs a kind") {
		t.Errorf("Highlight 2's errors = %q, want one naming it", errs)
	}
	if formField(t, rows[0], "draft_highlight_keep").Checked {
		t.Errorf("Highlight 1's Keep box came back ticked")
	}
	if !formField(t, rows[1], "draft_highlight_keep").Checked {
		t.Errorf("Highlight 2's Keep box came back unticked")
	}
	if first, second := formField(t, rows[0], "draft_highlight_note_"+id(a)).Value, formField(t, rows[1], "draft_highlight_note_"+id(b)).Value; first != "First, edited." || second != "No kind yet, edited." {
		t.Errorf("the offered rows' notes = %q and %q, want them as typed", first, second)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("checkins = %d, want none recorded", len(history))
	}
	if got := pendingDraftNotes(t, h, sam, goal.ID); !slices.Equal(got, []string{"First.", "No kind yet."}) {
		t.Errorf("pending = %q, want both still pending as logged", got)
	}
}

// A Draft Highlight posted by its draft_highlight_id alone, with no Keep, kind
// or note, is discarded with the Check-in, as an unticked Keep box discards
// it: that is all the Highlight editor's Remove leaves of a Draft item.
func TestCheckinDiscardsADraftHighlightPostedByItsIDAlone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	draft := h.LogDraftHighlight(sam, goal.ID, domain.HighlightMiss, "The failover drill slipped.")

	_, status := postFormHX(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":             {domain.HealthGreen},
		"status":             {"On track."},
		"draft_highlight_id": {strconv.FormatInt(draft.ID, 10)},
	})
	if status != http.StatusOK {
		t.Fatalf("submit: status %d, want 200", status)
	}
	if highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID); err != nil || len(highlights) != 0 {
		t.Errorf("highlights = %v, %v; want none recorded", highlights, err)
	}
	checkins, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil || len(checkins) != 1 {
		t.Fatalf("ListCheckins = %d, %v; want the one Check-in", len(checkins), err)
	}
	if d := checkins[0].DiscardedDraftHighlights; len(d) != 1 || d[0].ID != draft.ID {
		t.Errorf("discarded = %v, want the Draft Highlight", d)
	}
	if got := pendingDraftNotes(t, h, sam, goal.ID); len(got) != 0 {
		t.Errorf("pending = %q, want none", got)
	}
}

// Add another keeps every offered row's Keep box, kind and note as typed,
// adds one empty row after the typed ones, and records nothing.
func TestCheckinAddAnotherKeepsOfferedDraftHighlightsAsTyped(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	a := h.LogDraftHighlight(sam, goal.ID, domain.HighlightInsight, "First.")
	b := h.LogDraftHighlight(sam, goal.ID, "", "Second.")
	id := func(d domain.DraftHighlight) string { return strconv.FormatInt(d.ID, 10) }

	body, status := postFormHX(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":                        {domain.HealthGreen},
		"status":                        {"On track."},
		"draft_highlight_id":            {id(a), id(b)},
		"draft_highlight_keep":          {id(b)},
		"draft_highlight_kind_" + id(a): {domain.HighlightMiss},
		"draft_highlight_note_" + id(a): {"First, edited."},
		"draft_highlight_kind_" + id(b): {domain.HighlightAccomplishment},
		"draft_highlight_note_" + id(b): {"Second, edited."},
		"highlight_kind":                {domain.HighlightInsight},
		"highlight_note":                {"Typed."},
		"add_highlight":                 {"1"},
	})
	if status != http.StatusOK {
		t.Fatalf("add another: status %d, want 200", status)
	}
	rows := highlightRows(t, parsePage(t, body))
	if len(rows) != 4 {
		t.Fatalf("form shows %d Highlight rows, want 2 offered, 1 typed and 1 added", len(rows))
	}
	for i, tc := range []struct {
		draft      domain.DraftHighlight
		keep       bool
		kind, note string
	}{
		{a, false, domain.HighlightMiss, "First, edited."},
		{b, true, domain.HighlightAccomplishment, "Second, edited."},
	} {
		row := rows[i]
		if got := formField(t, row, "draft_highlight_keep").Checked; got != tc.keep {
			t.Errorf("Highlight %d's Keep box ticked = %v, want %v as posted", i+1, got, tc.keep)
		}
		kind, note := formField(t, row, "draft_highlight_kind_"+id(tc.draft)).Value, formField(t, row, "draft_highlight_note_"+id(tc.draft)).Value
		if kind != tc.kind || note != tc.note {
			t.Errorf("Highlight %d reads %q of kind %q, want %q of kind %q as typed", i+1, note, kind, tc.note, tc.kind)
		}
	}
	if typed, added := formField(t, rows[2], "highlight_note").Value, formField(t, rows[3], "highlight_note").Value; typed != "Typed." || added != "" {
		t.Errorf("the typed row reads %q and the added row %q, want Typed. and empty", typed, added)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("checkins = %d, want none recorded by Add another", len(history))
	}
	if got := pendingDraftNotes(t, h, sam, goal.ID); len(got) != 2 {
		t.Errorf("pending = %q, want both still pending", got)
	}
}

// Two kept offered rows of the same kind both become Highlights, a kept row
// whose note was blanked is discarded, and a Draft Highlight logged after the
// form opened, so not among its rows, stays pending.
func TestCheckinFormKeepsSameKindDiscardsBlankedAndLeavesUnofferedPending(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	a := h.LogDraftHighlight(sam, goal.ID, domain.HighlightAccomplishment, "Retired the old pager.")
	b := h.LogDraftHighlight(sam, goal.ID, domain.HighlightAccomplishment, "Halved alert noise.")
	c := h.LogDraftHighlight(sam, goal.ID, domain.HighlightMiss, "Runbook was late.")
	samClient := signInClient(t, ts.URL, "sam@example.com")
	form := getPage(t, samClient, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	if n := len(highlightRows(t, form)); n != 4 {
		t.Fatalf("form shows %d Highlight rows, want 3 offered and 1 empty", n)
	}
	h.LogDraftHighlight(sam, goal.ID, domain.HighlightInsight, "Logged after the form opened.")
	id := func(d domain.DraftHighlight) string { return strconv.FormatInt(d.ID, 10) }

	_, status := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":                        {domain.HealthGreen},
		"status":                        {"On track."},
		"draft_highlight_id":            {id(a), id(b), id(c)},
		"draft_highlight_keep":          {id(a), id(b), id(c)},
		"draft_highlight_kind_" + id(a): {a.Kind},
		"draft_highlight_note_" + id(a): {a.Note},
		"draft_highlight_kind_" + id(b): {b.Kind},
		"draft_highlight_note_" + id(b): {b.Note},
		"draft_highlight_kind_" + id(c): {c.Kind},
		"draft_highlight_note_" + id(c): {""},
		"highlight_kind":                {""},
		"highlight_note":                {""},
	})
	if status != http.StatusOK {
		t.Fatalf("submit: status %d, want 200", status)
	}

	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	var got []string
	for _, hl := range highlights {
		got = append(got, hl.Kind+": "+hl.Note)
	}
	if want := []string{"Accomplishment: Retired the old pager.", "Accomplishment: Halved alert noise."}; !slices.Equal(got, want) {
		t.Errorf("highlights = %q, want %q", got, want)
	}
	checkins, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil || len(checkins) != 1 {
		t.Fatalf("ListCheckins = %d, %v; want the one Check-in", len(checkins), err)
	}
	if d := checkins[0].DiscardedDraftHighlights; len(d) != 1 || d[0].ID != c.ID {
		t.Errorf("discarded = %v, want only the blanked one", d)
	}
	if got := pendingDraftNotes(t, h, sam, goal.ID); !slices.Equal(got, []string{"Logged after the form opened."}) {
		t.Errorf("pending = %q, want only the one logged after the form opened", got)
	}
}

// A No change Check-in records no Highlights and leaves pending Draft
// Highlights untouched.
func TestNoChangeCheckinLeavesDraftHighlightsPendingOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.LogDraftHighlight(sam, goal.ID, domain.HighlightInsight, "Waiting for a real Check-in.")

	resp := postForm(t, noRedirects(signInClient(t, ts.URL, "sam@example.com")), fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("No change: status %d, want 303", resp.StatusCode)
	}
	if got := pendingDraftNotes(t, h, sam, goal.ID); !slices.Equal(got, []string{"Waiting for a real Check-in."}) {
		t.Errorf("pending = %q, want the Draft Highlight untouched", got)
	}
	if highlights, _ := h.Service.ListHighlightsByGoal(context.Background(), goal.ID); len(highlights) != 0 {
		t.Errorf("highlights = %d, want none recorded", len(highlights))
	}
}

// The Check-in form starts its text, number and date fields empty, showing the
// latest Check-in's answers and each Metric's latest reading as ghost text, so
// a full Check-in is written fresh; Health stays picked from the latest
// Check-in (ticket #284).
func TestCheckinFormShowsLatestAnswersAsGhostText(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Autoscale the API tier", "Traffic spikes cost us.")
	child := h.ActiveChildOf(sam, parent, "Load tests", "Know the limits.")
	h.Checkin(sam, child.ID, domain.HealthGreen, "Done for this quarter.", "", time.Time{})
	rps := addCheckinGhostMetric(t, h, parent, "Peak requests")
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         parent.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Vendor sign-off is slower than expected.",
		PathToGreen:    "Escalate to the vendor.",
		PathTargetDate: pathDate,
		Explanation:    "The load tests miss the vendor risk.",
		Readings:       []domain.MetricReadingInput{{MetricID: rps.ID, Value: 1250.5}},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, parent.ID))
	form := findByTestID(t, page, "checkin-form")
	for name, want := range map[string]string{
		"status":        "Vendor sign-off is slower than expected.",
		"path_to_green": "Escalate to the vendor.",
		"explanation":   "The load tests miss the vendor risk.",
	} {
		field := formField(t, form, name)
		if field.Value != "" {
			t.Errorf("%s starts as %q, want empty", name, field.Value)
		}
		if field.Placeholder != want {
			t.Errorf("%s placeholder = %q, want %q", name, field.Placeholder, want)
		}
	}
	reading := formField(t, form, fmt.Sprintf("reading_%d", rps.ID))
	if reading.Value != "" {
		t.Errorf("reading starts as %q, want empty", reading.Value)
	}
	if reading.Placeholder != "1250.5" {
		t.Errorf("reading placeholder = %q, want 1250.5", reading.Placeholder)
	}
	date := formField(t, form, "path_target_date")
	if date.Value != "" {
		t.Errorf("Back to Green by starts as %q, want empty", date.Value)
	}
	if got := textsOf(findAllByTestID(form, "checkin-path-target-last")); !slices.Equal(got, []string{"Last time: Jun 15"}) {
		t.Errorf("Back to Green by hint = %q, want [Last time: Jun 15]", got)
	}
	if date.Description != "Last time: Jun 15" {
		t.Errorf("Back to Green by is described as %q, want by its hint, Last time: Jun 15", date.Description)
	}
	if got := checkedRadio(form, "health"); got != domain.HealthYellow {
		t.Errorf("Health picked = %q, want Yellow", got)
	}
}

// A first Check-in has nothing to show as ghost text: its fields carry no
// placeholder and no "Last time" hint, and it starts on Green.
func TestCheckinFormWithoutPreviousCheckinShowsNoGhostText(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Autoscale the API tier", "Traffic spikes cost us.")
	child := h.ActiveChildOf(sam, parent, "Load tests", "Know the limits.")
	h.Checkin(sam, child.ID, domain.HealthGreen, "Done for this quarter.", "", time.Time{})
	addCheckinGhostMetric(t, h, parent, "Peak requests")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, parent.ID))
	form := findByTestID(t, page, "checkin-form")
	formField(t, form, "explanation")
	for _, part := range []string{"checkin-readings", "path-to-green-field"} {
		if ghosts := findAll(findByTestID(t, form, part), func(n *html.Node) bool { return hasAttr(n, "placeholder") }); len(ghosts) > 0 {
			t.Errorf("%s shows ghost text with no previous Check-in: %s", part, renderNode(ghosts[0]))
		}
	}
	for _, name := range []string{"status", "explanation"} {
		if field := formField(t, form, name); hasAttr(field.Node, "placeholder") {
			t.Errorf("%s shows ghost text %q with no previous Check-in", name, field.Placeholder)
		}
	}
	if hasTestID(form, "checkin-path-target-last") {
		t.Errorf("form shows a Last time hint with no previous Check-in")
	}
	if got := checkedRadio(form, "health"); got != domain.HealthGreen {
		t.Errorf("a first Check-in starts on %q, want Green", got)
	}
}

// A refused Check-in, and Add another, come back with what was typed kept and
// the latest Check-in's answers still shown as ghost text, over htmx and as a
// plain post.
func TestCheckinReRenderKeepsTypedAndGhostText(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Autoscale the API tier", "Traffic spikes cost us.")
	rps := addCheckinGhostMetric(t, h, goal, "Peak requests")
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Vendor sign-off is slower than expected.",
		PathToGreen:    "Escalate to the vendor.",
		PathTargetDate: pathDate,
		Readings:       []domain.MetricReadingInput{{MetricID: rps.ID, Value: 1250.5}},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	samClient := signInClient(t, ts.URL, "sam@example.com")
	checkinsURL := fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID)
	// Red with no Path to Green is refused.
	typed := url.Values{
		"health":                          {domain.HealthRed},
		"status":                          {"The vendor went quiet."},
		fmt.Sprintf("reading_%d", rps.ID): {"900"},
	}

	addAnother := url.Values{"add_highlight": {"1"}}
	for k, v := range typed {
		addAnother[k] = v
	}

	htmxBody, _ := postFormHX(t, samClient, checkinsURL, typed)
	plainResp := postForm(t, samClient, checkinsURL, typed)
	defer func() { _ = plainResp.Body.Close() }()
	if plainResp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("plain refused post status = %d, want 422", plainResp.StatusCode)
	}
	htmxAgain, _ := postFormHX(t, samClient, checkinsURL, addAnother)
	plainAgain := postForm(t, samClient, checkinsURL, addAnother)
	defer func() { _ = plainAgain.Body.Close() }()
	for how, body := range map[string]string{
		"htmx refused":      htmxBody,
		"plain refused":     readBody(t, plainResp),
		"htmx add another":  htmxAgain,
		"plain add another": readBody(t, plainAgain),
	} {
		form := findByTestID(t, parsePage(t, body), "checkin-form")
		if refused := hasTestID(form, "checkin-error"); refused != strings.HasSuffix(how, "refused") {
			t.Fatalf("%s: form shows an error = %v; form:\n%s", how, refused, renderNode(form))
		}
		status := formField(t, form, "status")
		if status.Value != "The vendor went quiet." {
			t.Errorf("%s: status re-rendered as %q, want what was typed", how, status.Value)
		}
		if status.Placeholder != "Vendor sign-off is slower than expected." {
			t.Errorf("%s: status placeholder = %q, want the latest Check-in's", how, status.Placeholder)
		}
		if got := formField(t, form, "path_to_green").Placeholder; got != "Escalate to the vendor." {
			t.Errorf("%s: Path to Green placeholder = %q, want the latest Check-in's", how, got)
		}
		if reading := formField(t, form, fmt.Sprintf("reading_%d", rps.ID)); reading.Value != "900" || reading.Placeholder != "1250.5" {
			t.Errorf("%s: reading = %q with ghost text %q, want 900 with 1250.5", how, reading.Value, reading.Placeholder)
		}
		if got := textsOf(findAllByTestID(form, "checkin-path-target-last")); !slices.Equal(got, []string{"Last time: Jun 15"}) {
			t.Errorf("%s: Back to Green by hint = %q, want [Last time: Jun 15]", how, got)
		}
		if got := checkedRadio(form, "health"); got != domain.HealthRed {
			t.Errorf("%s: the Health picked = %q, want the Red picked", how, got)
		}
	}
}

// Ghost text is never submitted: a Check-in sent with the status left empty is
// refused though the last Check-in had one, and a reading left blank records
// no reading though the Metric was read before.
func TestCheckinGhostTextIsNotSubmitted(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Autoscale the API tier", "Traffic spikes cost us.")
	rps := addCheckinGhostMetric(t, h, goal, "Peak requests")
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Scaling to plan.",
		Readings: []domain.MetricReadingInput{{MetricID: rps.ID, Value: 1250.5}},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	samClient := signInClient(t, ts.URL, "sam@example.com")
	checkinsURL := fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID)
	blankReading := fmt.Sprintf("reading_%d", rps.ID)

	body, _ := postFormHX(t, samClient, checkinsURL, url.Values{
		"health":     {domain.HealthGreen},
		"status":     {""},
		blankReading: {""},
	})
	if msg := fieldError(t, parsePage(t, body), "status"); !strings.Contains(msg, "needs a status") {
		t.Errorf("status field's error = %q, want the status one", msg)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("an empty status recorded a Check-in: history has %d, want 1", len(history))
	}

	resp := postForm(t, samClient, checkinsURL, url.Values{
		"health":     {domain.HealthGreen},
		"status":     {"Still scaling to plan."},
		blankReading: {""},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit check-in: status %d; body:\n%s", resp.StatusCode, readBody(t, resp))
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 2 {
		t.Fatalf("history has %d Check-ins, want 2", len(history))
	}
	readings, err := h.Service.ListMetricReadings(context.Background(), rps.ID)
	if err != nil {
		t.Fatalf("ListMetricReadings: %v", err)
	}
	if len(readings) != 1 {
		t.Errorf("a blank reading recorded one: Metric has %d readings, want 1", len(readings))
	}
}

// addCheckinGhostMetric adds a Metric to goal, failing the test on error.
func addCheckinGhostMetric(t *testing.T, h *testsupport.Harness, goal domain.Goal, name string) domain.Metric {
	t.Helper()
	m, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, domain.AddMetricInput{
		GoalID:     goal.ID,
		Name:       name,
		Unit:       "per second",
		Direction:  domain.MetricUp,
		Baseline:   100,
		Target:     5000,
		TargetDate: h.Clock.Now().AddDate(0, 6, 0),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	return m
}

// offTargetIdleGoal arranges an Active Goal whose Idle capacity Metric (down,
// target 20 by Jan 30) last read 40.88, with the clock moved to Feb 2: past the
// Metric's target date, so it is off target, and before the Beta Milestone is
// due.
func offTargetIdleGoal(t *testing.T, h *testsupport.Harness, owner domain.Account) (domain.Goal, domain.Metric) {
	t.Helper()
	ctx := context.Background()
	goal := h.ActiveGoal(owner, "Reclaim idle capacity", "Idle machines cost money.")
	idle, err := h.Service.AddMetric(ctx, goal.Owner.ID, domain.AddMetricInput{
		GoalID: goal.ID, Name: "Idle capacity", Unit: "percent", Direction: domain.MetricDown,
		Baseline: 55, Target: 20, TargetDate: time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: goal.ID, AuthorID: owner.ID, Health: domain.HealthGreen, Status: "Draining the old pool.",
		Readings: []domain.MetricReadingInput{{MetricID: idle.ID, Value: 40.88}},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	h.Clock.Set(time.Date(2026, 2, 2, 15, 0, 0, 0, time.UTC))
	return goal, idle
}

// A Green Check-in while a Metric is off target records nothing: the form comes
// back as typed with a warning naming the Metric, its reading and unit, its
// target and target date, and a Confirm Green button in the form.
func TestCheckinGreenWithOffTargetMetricWarnsBeforeSaving(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, idle := offTargetIdleGoal(t, h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	reading := fmt.Sprintf("reading_%d", idle.ID)
	body, status := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Old pool nearly drained."},
		reading:  {"35.5"},
	})

	if status != http.StatusOK {
		t.Errorf("htmx status = %d, want 200 so the form swaps in", status)
	}
	doc := parsePage(t, body)
	warning := findByTestID(t, doc, "checkin-off-target")
	if !strings.Contains(textOf(warning), "Idle capacity is 35.5 percent; its target was 20 by Jan 30.") {
		t.Errorf("warning does not name the Metric, the reading entered, its target and date: %q", textOf(warning))
	}
	confirm := buttonNamed(warning, "Confirm Green")
	if confirm == nil || cmp.Or(attrOf(confirm, "type"), "submit") != "submit" || attrOf(confirm, "name") != "confirm_off_target" || attrOf(confirm, "value") != "1" {
		t.Errorf("warning offers no Confirm Green that submits the form confirmed: %s", renderNode(warning))
	}
	if !strings.Contains(textOf(warning), "A Metric is off target. Is the Goal still Green?") {
		t.Errorf("warning doesn't ask whether the Goal is still Green: %q", textOf(warning))
	}
	if got := formField(t, doc, "status").Value; got != "Old pool nearly drained." {
		t.Errorf("the status typed was lost: %q", got)
	}
	if got := formField(t, doc, reading).Value; got != "35.5" {
		t.Errorf("the reading typed was lost: %q", got)
	}
	if got := checkedRadio(doc, "health"); got != domain.HealthGreen {
		t.Errorf("the Health picked was lost: %q", got)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("an unconfirmed Green was recorded: history has %d, want 1", len(history))
	}
}

// Confirm Green posts the form again confirmed, and the Check-in records
// as Green.
func TestCheckinSaveAsGreenAnywayRecordsGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetIdleGoal(t, h, sam)
	client := noRedirects(signInClient(t, ts.URL, "sam@example.com"))

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":             {domain.HealthGreen},
		"status":             {"Missed by a little; the trend is right."},
		"confirm_off_target": {"1"},
	})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 back to the Goal", resp.StatusCode)
	}
	latest, _, err := h.Service.LatestCheckin(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("LatestCheckin: %v", err)
	}
	if latest.Health != domain.HealthGreen || latest.Status != "Missed by a little; the trend is right." {
		t.Errorf("latest Check-in = %s %q, want the confirmed Green", latest.Health, latest.Status)
	}
}

// A No change can't repeat a Green while a Metric is off target: it lands on
// the Check-in page with the Metric's miss as the form's error, and how to keep
// Green.
func TestNoChangeRefusedWhileMetricOffTargetLandsOnCheckinForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetIdleGoal(t, h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	assertNoChangeRefusalOnForm(t, page, "Idle capacity is 40.88 percent; its target was 20 by Jan 30. To keep Green, submit a Check-in that confirms it, or update the Health.")
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("a refused No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// An overdue Milestone's refusal wins: with a Metric off target too, the form
// shows the overdue error and offers no Confirm Green.
func TestCheckinOverdueMilestoneRefusalWinsOverOffTargetWarning(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetIdleGoal(t, h, sam)
	h.Clock.Set(onlyMilestone(t, h, goal).TargetDate.AddDate(0, 0, 1))
	client := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"On track."},
	})

	doc := parsePage(t, body)
	if !strings.Contains(textOf(doc), "is overdue") {
		t.Errorf("the overdue Milestone's refusal isn't shown; body:\n%s", body)
	}
	if buttonNamed(doc, "Confirm Green") != nil || len(findAll(doc, func(n *html.Node) bool { return strings.Contains(attrOf(n, "name"), "off_target") })) > 0 {
		t.Errorf("an overdue Milestone's refusal offers Confirm Green; body:\n%s", body)
	}
}

// The off-target warning sits by the submit buttons, so a one-line notice near
// the top of the form, before the Health, flags it and links down to it: an
// Owner who pressed Enter near the top still sees the question (#342). The
// notice isn't announced as an alert too; the warning is.
func TestCheckinOffTargetWarningIsFlaggedAtTopOfForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetIdleGoal(t, h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Old pool nearly drained."},
	})

	doc := parsePage(t, body)
	notice := findByTestID(t, doc, "checkin-off-target-notice")
	if !precedes(notice, findByTestID(t, doc, "checkin-health")) {
		t.Errorf("the notice comes after the Health")
	}
	if hasAttr(notice, "role") {
		t.Errorf("the notice is announced as role %q; only the warning should be", attrOf(notice, "role"))
	}
	if !strings.Contains(textOf(notice), "A Metric is off target, so a Green needs confirming.") {
		t.Errorf("notice reads %q, want it to say a Green needs confirming", textOf(notice))
	}
	if link := linkTo(notice, "#checkin-off-target"); link == nil || textOf(link) != "See below" {
		t.Errorf("notice does not link down to the warning: %s", renderNode(notice))
	}
	warning := findByTestID(t, doc, "checkin-off-target")
	if attrOf(warning, "id") != "checkin-off-target" {
		t.Errorf("warning has no id the notice can link to: %s", renderNode(warning))
	}
	if attrOf(warning, "role") != "alert" {
		t.Errorf("warning is announced as role %q, want alert", attrOf(warning, "role"))
	}
	if warned := formField(t, warning, "off_target_warned"); warned.Type != "hidden" || warned.Value != "1" {
		t.Errorf("warning carries off_target_warned as a %s holding %q, want a hidden 1", warned.Type, warned.Value)
	}
}

// Add another on a warned form keeps the off-target warning while Green is
// still picked, judged by the readings typed, and adds the Highlight row; once
// another Health is picked it shows no warning (#342).
func TestCheckinAddAnotherKeepsOffTargetWarning(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, idle := offTargetIdleGoal(t, h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")
	addAnother := func(health string) *html.Node {
		body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
			"health":                           {health},
			"status":                           {"Old pool nearly drained."},
			"path_to_green":                    {"Retire the old pool."},
			fmt.Sprintf("reading_%d", idle.ID): {"35.5"},
			"highlight_kind":                   {domain.HighlightMiss},
			"highlight_note":                   {"The pool drained slowly."},
			"off_target_warned":                {"1"},
			"add_highlight":                    {"1"},
		})
		return parsePage(t, body)
	}

	doc := addAnother(domain.HealthGreen)
	if warning := textOf(findByTestID(t, doc, "checkin-off-target")); !strings.Contains(warning, "Idle capacity is 35.5 percent; its target was 20 by Jan 30.") {
		t.Errorf("after Add another, the warning doesn't name Idle capacity at the reading typed: %q", warning)
	}
	if rows := highlightRows(t, doc); len(rows) != 2 {
		t.Errorf("form shows %d Highlight rows, want 2 after Add another", len(rows))
	}

	if doc := addAnother(domain.HealthYellow); hasTestID(doc, "checkin-off-target") {
		t.Errorf("Add another with Yellow picked still warns: %q", textOf(findByTestID(t, doc, "checkin-off-target")))
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("Add another recorded a Check-in: history has %d, want 1", len(history))
	}
}

// Confirm Green records Green, even when another Health was picked
// after the warning: the button does what it says (#342).
func TestCheckinSaveAsGreenAnywayRecordsGreenWhateverHealthIsPicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetIdleGoal(t, h, sam)
	client := noRedirects(signInClient(t, ts.URL, "sam@example.com"))

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":             {domain.HealthYellow},
		"status":             {"Missed by a little."},
		"path_to_green":      {"Retire the old pool."},
		"path_target_date":   {"2026-03-01"},
		"confirm_off_target": {"1"},
	})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 back to the Goal", resp.StatusCode)
	}
	latest, _, err := h.Service.LatestCheckin(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("LatestCheckin: %v", err)
	}
	if latest.Health != domain.HealthGreen || latest.Status != "Missed by a little." {
		t.Errorf("latest Check-in = %s %q, want the confirmed Green", latest.Health, latest.Status)
	}
}

// A confirmed Green refused for another field comes back without the warning,
// carrying the confirmation, so fixing the field and submitting records the
// Green without asking again (#342).
func TestCheckinOffTargetConfirmationSurvivesALaterError(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetIdleGoal(t, h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")
	form := url.Values{
		"health":             {domain.HealthGreen},
		"status":             {"Missed by a little; the trend is right."},
		"highlight_kind":     {""},
		"highlight_note":     {"The pool drained slowly."},
		"confirm_off_target": {"1"},
	}

	body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), form)
	doc := parsePage(t, body)
	if rows := highlightRows(t, doc); len(rows) != 1 || !strings.Contains(textOf(rows[0]), "Highlight 1 needs a kind") {
		t.Fatalf("want the Highlight's error; body:\n%s", body)
	}
	if hasTestID(doc, "checkin-off-target") {
		t.Errorf("a confirmed Green is warned again; body:\n%s", body)
	}
	if confirmed := formField(t, doc, "off_target_confirmed"); confirmed.Type != "hidden" || confirmed.Value != "1" {
		t.Errorf("the form carries the confirmation as a %s holding %q, want a hidden 1", confirmed.Type, confirmed.Value)
	}

	form.Del("confirm_off_target")
	form.Set("off_target_confirmed", "1")
	form.Set("highlight_kind", domain.HighlightMiss)
	if body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), form); hasTestID(parsePage(t, body), "checkin-form") {
		t.Fatalf("the fixed, confirmed Green was refused; body:\n%s", body)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 2 {
		t.Errorf("history has %d Check-ins, want 2 once the confirmed Green is recorded", len(history))
	}
}

// askFixture is sam, an Owner whose Manager is Mia Chen, with an Active Goal,
// and a client signed in as sam.
type askFixture struct {
	h      *testsupport.Harness
	ts     string
	sam    domain.Account
	mia    domain.Account
	goal   domain.Goal
	client *http.Client
}

func newAskFixture(t *testing.T) askFixture {
	t.Helper()
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	mia := h.SignInNamed("mia@example.com", "Mia Chen")
	h.SetManager(sam, mia)
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	return askFixture{h: h, ts: ts.URL, sam: sam, mia: mia, goal: goal, client: signInClient(t, ts.URL, "sam@example.com")}
}

// askBox is the Check-in form's "Ask for help" box, failing the test unless
// it is the asks_for_help checkbox.
func askBox(t *testing.T, doc *html.Node) pageField {
	t.Helper()
	box := formField(t, doc, "asks_for_help")
	if box.Type != "checkbox" || attrOf(box.Node, "data-testid") != "checkin-asks-for-help" {
		t.Fatalf("asks-for-help control isn't the checkin-asks-for-help checkbox: %s", renderNode(box.Node))
	}
	return box
}

// shownWithEmailOnHover reports whether root shows the person named label
// with their email on hover.
func shownWithEmailOnHover(root *html.Node, label, emailAddr string) bool {
	return len(findAll(root, func(n *html.Node) bool { return attrOf(n, "title") == emailAddr && textOf(n) == label })) > 0
}

// While an ask is open the Check-in form starts with its box ticked, so
// carrying the ask forward takes no effort, and names the Manager it goes to.
// With no open ask the box starts unticked.
func TestCheckinFormAsksForHelpTickedWhileOpen(t *testing.T) {
	t.Parallel()

	f := newAskFixture(t)
	formURL := fmt.Sprintf("%s/goals/%d/checkin", f.ts, f.goal.ID)

	page := getPage(t, f.client, formURL)
	box := askBox(t, page)
	if box.Checked {
		t.Errorf("box is ticked with no open ask")
	}
	// The box's label reads Ask for help, then whom it goes to; a person's
	// own button can't sit inside a label, so it shows by name alone.
	if want := "Ask for help · goes to Mia Chen"; box.Label != want {
		t.Errorf("box's label reads %q, want %q", box.Label, want)
	}
	addressee := findByTestID(t, page, "checkin-ask-addressee")
	if !shownWithEmailOnHover(addressee, "Mia Chen", f.mia.Email) {
		t.Errorf("hint doesn't name Mia Chen, her email on hover: %s", renderNode(addressee))
	}
	if len(findAll(addressee, func(n *html.Node) bool { return n.Data == "button" })) > 0 {
		t.Errorf("whom the ask goes to is a button inside the box's label: %s", renderNode(addressee))
	}

	f.h.AskForHelp(f.sam, f.goal.ID, domain.HealthYellow, "Short a reviewer.", "Borrow one.", pathDate)
	if !askBox(t, getPage(t, f.client, formURL)).Checked {
		t.Errorf("box is unticked while the ask is open")
	}
}

// With no one above the Owner, the form says the ask has no one to go to.
func TestCheckinFormAskHintWithNoOneAbove(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")

	hint := textOf(findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)), "checkin-ask-addressee"))
	if !strings.Contains(hint, "No one is above the Owner to ask.") {
		t.Errorf("hint = %q, want it to say no one is above the Owner", hint)
	}
}

// Posting a Yellow Check-in with the box ticked opens an ask; posting the next
// without it closes the ask.
func TestCheckinPostOpensAndClosesAnAsk(t *testing.T) {
	t.Parallel()

	f := newAskFixture(t)
	post := func(form url.Values) {
		t.Helper()
		resp := postForm(t, noRedirects(f.client), fmt.Sprintf("%s/goals/%d/checkins", f.ts, f.goal.ID), form)
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("POST check-in = %d, want 303", resp.StatusCode)
		}
	}
	yellow := url.Values{
		"health":           {domain.HealthYellow},
		"status":           {"Short a reviewer."},
		"path_to_green":    {"Borrow one."},
		"path_target_date": {"2026-06-15"},
	}

	asking := url.Values{"asks_for_help": {"1"}}
	for k, v := range yellow {
		asking[k] = v
	}
	post(asking)
	ask, ok, err := f.h.Service.OpenAskForHelp(context.Background(), f.goal.ID)
	if err != nil || !ok {
		t.Fatalf("OpenAskForHelp after posting the ask = ok %v, err %v; want an open ask", ok, err)
	}
	if ask.Addressee == nil || ask.Addressee.ID != f.mia.ID {
		t.Errorf("ask's addressee = %v, want Mia", ask.Addressee)
	}

	post(yellow)
	if _, ok, err := f.h.Service.OpenAskForHelp(context.Background(), f.goal.ID); err != nil || ok {
		t.Errorf("OpenAskForHelp after unticking = ok %v, err %v; want the ask closed", ok, err)
	}
}

// A refused submit comes back with the box as posted, still naming whom the
// ask goes to.
func TestCheckinRefusedSubmitKeepsAskAsPosted(t *testing.T) {
	t.Parallel()

	f := newAskFixture(t)
	f.h.AskForHelp(f.sam, f.goal.ID, domain.HealthYellow, "Short a reviewer.", "Borrow one.", pathDate)
	submitURL := fmt.Sprintf("%s/goals/%d/checkins", f.ts, f.goal.ID)

	body, _ := postFormHX(t, f.client, submitURL, url.Values{
		"health":        {domain.HealthYellow},
		"status":        {"Still short."},
		"asks_for_help": {"1"},
	})
	if !askBox(t, parsePage(t, body)).Checked {
		t.Errorf("re-rendered form lost the ticked box")
	}

	resp := postForm(t, f.client, submitURL, url.Values{
		"health": {domain.HealthYellow},
		"status": {"Still short."},
	})
	defer func() { _ = resp.Body.Close() }()
	doc := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("refused plain submit = %d, want 422", resp.StatusCode)
	}
	if askBox(t, doc).Checked {
		t.Errorf("re-rendered form ticked a box posted unticked")
	}
	if addressee := findByTestID(t, doc, "checkin-ask-addressee"); !shownWithEmailOnHover(addressee, "Mia Chen", f.mia.Email) {
		t.Errorf("422 re-render's hint doesn't name Mia Chen: %q", textOf(addressee))
	}
}

// The Latest status card shows an open ask under the Path to Green, naming
// whom it asks, with a link to answer it; a Green Check-in closes it.
func TestLatestStatusShowsAnOpenAsk(t *testing.T) {
	t.Parallel()

	f := newAskFixture(t)
	f.h.AskForHelp(f.sam, f.goal.ID, domain.HealthYellow, "Short a reviewer.", "Borrow one.", pathDate)
	goalURL := fmt.Sprintf("%s/goals/%d", f.ts, f.goal.ID)

	card := findByTestID(t, getPage(t, f.client, goalURL), "goal-checkins")
	ask := findByTestID(t, card, "goal-ask-for-help")
	if !precedes(findByTestID(t, card, "goal-path-to-green"), ask) {
		t.Errorf("the ask isn't under the Path to Green")
	}
	if got := textOf(ask); !strings.HasPrefix(got, "Asks for help of Mia Chen") {
		t.Errorf("ask reads %q, want \"Asks for help of Mia Chen\"", got)
	}
	if !shownWithEmailOnHover(ask, "Mia Chen", f.mia.Email) {
		t.Errorf("ask doesn't show Mia Chen with her email on hover: %s", renderNode(ask))
	}
	if answer := linkTo(ask, "#comments"); answer == nil || textOf(answer) != "Answer" {
		t.Errorf("ask has no Answer link to the Comments: %s", renderNode(ask))
	}
	if got := runningText(ask); !strings.Contains(got, "Mia Chen Answer") {
		t.Errorf("ask runs the Addressee into Answer with no space between: %q", got)
	}

	f.h.Checkin(f.sam, f.goal.ID, domain.HealthGreen, "Fixed.", "", time.Time{})
	if page := getPage(t, f.client, goalURL); hasTestID(page, "goal-ask-for-help") {
		t.Errorf("Goal page still shows the ask after a Green Check-in")
	}
}

// With no one above the Owner, the open ask says so.
func TestLatestStatusShowsAnAskWithNoOneAbove(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AskForHelp(sam, goal.ID, domain.HealthRed, "Blocked.", "Need budget.", pathDate)
	client := signInClient(t, ts.URL, "sam@example.com")

	got := textsOf(findAllByTestID(getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "goal-ask-for-help"))
	if len(got) != 1 || !strings.HasPrefix(got[0], "Asks for help · no one above the Owner") {
		t.Errorf("ask = %q, want it to read \"Asks for help · no one above the Owner\"", got)
	}
}

// The Check-in form asks for a reason for moving the back-to-Green date only
// once it is later than the latest Check-in's: the field starts hidden, and the
// date input carries that date for the page's script to compare against
// (CONTEXT.md: Date Slip).
func TestCheckinFormHidesTheBackToGreenReasonUntilTheDateMovesLater(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	form := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)), "checkin-form")
	reason := formField(t, form, "path_target_date_reason")
	if !isHidden(reason.Node) {
		t.Errorf("the reason field shows before the date moves")
	}
	if reason.Label != "Reason for moving it later" {
		t.Errorf("the reason field is labelled %q, want \"Reason for moving it later\"", reason.Label)
	}
	date := formField(t, form, "path_target_date")
	if got := attrOf(date.Node, "data-last"); got != "2026-06-15" {
		t.Errorf("Back to Green by data-last = %q, want 2026-06-15", got)
	}
	if !hasAttr(date.Node, "hx-on:input") {
		t.Errorf("Back to Green by has no input handler to show the reason: %s", renderNode(date.Node))
	}

	// A Goal with no back-to-Green date yet has nothing to slip from.
	fresh := h.ActiveGoal(sam, "Cut churn", "Keep customers.")
	form = findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, fresh.ID)), "checkin-form")
	if !isHidden(formField(t, form, "path_target_date_reason").Node) {
		t.Errorf("the reason field shows on a Goal with no back-to-Green date")
	}
	if date := formField(t, form, "path_target_date"); hasAttr(date.Node, "data-last") {
		t.Errorf("Back to Green by carries a data-last with no previous date: %s", renderNode(date.Node))
	}
}

// Moving the back-to-Green date later without a reason is refused, with the
// error beside the reason field, now shown; with a reason the Check-in is
// recorded.
func TestCheckinMovingTheBackToGreenDateLaterNeedsAReasonOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)
	client := signInClient(t, ts.URL, "sam@example.com")
	form := url.Values{
		"health":           {domain.HealthRed},
		"status":           {"Still blocked."},
		"path_to_green":    {"Escalate."},
		"path_target_date": {"2026-06-29"},
	}

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), form)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	doc := findByTestID(t, parsePage(t, readBody(t, resp)), "checkin-form")
	const want = "Moving the back-to-Green date later needs a reason."
	if msg := fieldError(t, doc, "path_target_date_reason"); !strings.HasPrefix(msg, want) {
		t.Errorf("reason field's error = %q, want it to read %q", msg, want)
	}
	if isHidden(formField(t, doc, "path_target_date_reason").Node) {
		t.Errorf("the refused form hides the reason field")
	}

	form.Set("path_target_date_reason", "The vendor is out until July.")
	if _, code := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), form); code != http.StatusOK {
		t.Fatalf("status with a reason = %d, want 200", code)
	}
	checkins, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(checkins) != 2 || checkins[0].Status != "Still blocked." {
		t.Errorf("Check-ins %+v, want the new one recorded", checkins)
	}
	slips, _ := h.Service.ListDateSlips(context.Background(), goal.ID)
	if len(slips) != 1 || !slips[0].BackToGreen || slips[0].Reason != "The vendor is out until July." {
		t.Errorf("Date Slips %+v, want the back-to-Green slip with its reason", slips)
	}
}

// backToGreenCell is the Latest status's "Back to Green by" cell on goal's
// page.
func backToGreenCell(t *testing.T, client *http.Client, base string, goal domain.Goal) *html.Node {
	t.Helper()
	cells := findByTestID(t, getPage(t, client, goalPageURL(base, goal)), "goal-status-cells")
	return findByTestID(t, cells, "goal-back-to-green-cell")
}

// The Latest status's Back to Green by shows a slipped back-to-Green date as a
// slipped Milestone's shows: the current date, the date it last slipped from
// struck through, and how many times it slipped once that is more than one,
// counted within the current Run off Green (CONTEXT.md: Date Slip, Run off
// Green).
func TestLatestStatusShowsASlippedBackToGreenDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")
	jun := func(d int) time.Time { return time.Date(2026, 6, d, 0, 0, 0, 0, time.UTC) }

	for _, tc := range []struct {
		name    string
		arrange func(domain.Goal)
		reads   string
		struck  []string
		slips   []string
	}{
		{"Slipped twice", func(g domain.Goal) {
			h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(15))
			h.CheckinMovingPath(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(22), "The vendor is out.")
			h.CheckinMovingPath(sam, g.ID, domain.HealthYellow, "Better.", "Escalate.", jun(29), "The vendor is still out.")
		}, "Jun 29 Jun 22 (2)", []string{"Jun 22"}, []string{"(2)"}},
		{"Slipped once", func(g domain.Goal) {
			h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(15))
			h.CheckinMovingPath(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(22), "The vendor is out.")
		}, "Jun 22 Jun 15", []string{"Jun 15"}, nil},
		// A Green Check-in ends the run, and its slips with it.
		{"Back and off again", func(g domain.Goal) {
			h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(15))
			h.CheckinMovingPath(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(22), "The vendor is out.")
			h.Clock.Advance(time.Hour)
			h.Checkin(sam, g.ID, domain.HealthGreen, "Unblocked.", "", time.Time{})
			h.Clock.Advance(time.Hour)
			h.Checkin(sam, g.ID, domain.HealthYellow, "Wobbling.", "Watch it.", jun(29))
		}, "Jun 29", nil, nil},
	} {
		goal := h.ActiveGoal(sam, tc.name, "It matters.")
		tc.arrange(goal)
		cell := backToGreenCell(t, client, ts.URL, goal)
		if _, value := statusCell(t, cell, "goal-back-to-green-cell"); value != tc.reads {
			t.Errorf("%s: the cell reads %q, want %q", tc.name, value, tc.reads)
		}
		if got := struckThrough(cell); !slices.Equal(got, tc.struck) {
			t.Errorf("%s: the cell strikes through %q, want %q", tc.name, got, tc.struck)
		}
		if got := textsOf(findAllByTestID(cell, "back-to-green-slips")); !slices.Equal(got, tc.slips) {
			t.Errorf("%s: the cell counts slips %q, want %q", tc.name, got, tc.slips)
		}
	}
}

// A No change is refused once the back-to-Green date has passed: the Check-in
// page says why in place of the button, and a No change posted anyway lands on
// the form with the reason (CONTEXT.md: Check-in).
func TestCheckinPageRefusesNoChangeOnceTheBackToGreenDateHasPassed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Behind.", "Escalate.", h.Clock.Now().AddDate(0, 0, 3))
	h.Clock.Advance(5 * day)
	client := signInClient(t, ts.URL, "sam@example.com")

	c := findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)), "no-change-card")
	if hasTestID(c, "no-change-checkin") {
		t.Errorf("the card offers a No change that would be refused: %q", textOf(c))
	}
	const want = "The back-to-Green date has passed: submit a Check-in with a new date and reason, or update the Health."
	if got := textsOf(findAllByTestID(c, "no-change-unavailable")); !slices.Equal(got, []string{want}) {
		t.Errorf("the card's reason = %q, want %q", got, want)
	}

	resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)
	defer func() { _ = resp.Body.Close() }()
	assertNoChangeRefusalOnForm(t, page, want)
}

// checkinChildrenFixture is a Goal Sam owns with two Active children Sam also
// owns, one checked in Yellow and one Green, so its Rolled-up Health is Yellow.
type checkinChildrenFixture struct {
	parent, yellow, green domain.Goal
}

func newCheckinChildrenFixture(h *testsupport.Harness, sam domain.Account) checkinChildrenFixture {
	parent := h.ActiveGoal(sam, "Delight customers in their first week", "First impressions stick.")
	yellow := h.ActiveChildOf(sam, parent, "Lift trial-to-paid conversion", "Revenue.")
	green := h.ActiveChildOf(sam, parent, "Raise the app store rating", "Reputation.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Fix the paywall.", pathDate)
	h.Checkin(sam, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	return checkinChildrenFixture{parent: parent, yellow: yellow, green: green}
}

// The Check-in page shows the Goal's children, the Goal page's "Contributed to
// by" list, so the Owner sees which children set the Rolled-up Health without
// leaving the form. It is read-only: no Remove button, even for the Owner of
// the Goal and of each child.
func TestCheckinPageListsTheGoalsChildren(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, f.parent.ID))
	list := findByTestID(t, page, "checkin-children")
	if headingNamed(list, "Contributed to by") == nil {
		t.Errorf("children list is not headed Contributed to by: %q", textOf(list))
	}
	for _, child := range []struct {
		goal   domain.Goal
		health string
	}{
		{f.yellow, domain.HealthYellow},
		{f.green, domain.HealthGreen},
	} {
		link := linkTo(list, fmt.Sprintf("/goals/%d", child.goal.ID))
		if link == nil || textOf(link) != child.goal.Title {
			t.Errorf("children list has no link to %s: %q", child.goal.Title, textOf(list))
			continue
		}
		item := link
		for item.Parent != nil && item.Data != "li" {
			item = item.Parent
		}
		if !slices.Contains(strings.Fields(textOf(item)), child.health) {
			t.Errorf("%s shows no %s: %q", child.goal.Title, child.health, textOf(item))
		}
	}
	if removes := findAll(list, func(n *html.Node) bool {
		return strings.Contains(attrOf(n, "action")+attrOf(n, "hx-post")+attrOf(n, "formaction"), "/links/")
	}); len(removes) > 0 || buttonNamed(list, "Remove") != nil {
		t.Errorf("children list offers a Remove form: %s", renderNode(list))
	}
}

// A Goal with no children has no children list: the page's column shows for
// every Goal (ticket #366).
func TestCheckinPageWithoutChildrenHasNoChildrenList(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship it", "It matters.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, g.ID))
	if hasTestID(page, "checkin-children") {
		t.Errorf("a Goal with no children shows a children list: %q", textOf(findByTestID(t, page, "checkin-children")))
	}
	if !hasTestID(page, "checkin-context") {
		t.Errorf("a Goal with no children loses the page's column")
	}
}

// The form's Rolled-up Health line counts the Goal's children that set it, as
// the badge's count line (ticket #364), and no longer reads as the Owner's
// children.
func TestCheckinFormRollupLineCountsTheGoalsChildren(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, f.parent.ID))
	if got, want := textOf(findByTestID(t, page, "checkin-form-rollup")), "Rolled-up Health: Yellow 1 of 2 Yellow"; got != want {
		t.Errorf("rollup line = %q, want %q", got, want)
	}
	if strings.Contains(textOf(page), "your Active children") {
		t.Errorf("rollup line still reads as the Owner's children")
	}
}

// A refused submit re-renders only the form, with its error, for htmx to swap
// in place; the children list sits outside the form, so it stays as it was.
func TestCheckinRefusedSubmitLeavesTheChildrenListAlone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)

	body, status := postFormHX(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkins", ts.URL, f.parent.ID), url.Values{
		"health":        {domain.HealthYellow},
		"status":        {""},
		"path_to_green": {"Fix the paywall."},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200", status)
	}
	doc := parsePage(t, body)
	fieldError(t, doc, "status")
	if !hasTestID(doc, "checkin-form") {
		t.Errorf("refused submit doesn't re-render the form; body:\n%s", body)
	}
	if hasTestID(doc, "checkin-children") {
		t.Errorf("refused submit re-renders the children list along with the form; body:\n%s", body)
	}
}

// The Check-in page reads top to bottom under headings: the page title, then
// "How is it going?" over the Status and the Rolled-up Health, then "Path to
// Green", then "Metrics", none numbered (ticket #364).
func TestCheckinPageHeadingHierarchy(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Grow the product", "Growth funds the roadmap.")
	if _, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, domain.AddMetricInput{
		GoalID: goal.ID, Name: "Signups", Unit: "per week", Direction: domain.MetricUp,
		Baseline: 100, Target: 500, TargetDate: h.Clock.Now().AddDate(0, 6, 0),
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	redChild := h.ActiveChildOf(sam, goal, "Red work", "Red why it matters.")
	// The Goal turns Yellow before its child turns Red, so its Check-in
	// needs no explanation.
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Slipping.", "Add a second on-call.", pathDate)
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	heading := func(level, text string) func() *html.Node {
		return func() *html.Node {
			if n := headingNamed(page, text); n != nil && n.Data == level {
				return n
			}
			return nil
		}
	}
	var last *html.Node
	for _, part := range []struct {
		what string
		find func() *html.Node
	}{
		{"<h1> Check in", heading("h1", "Check in")},
		{"<h2> How is it going?", heading("h2", "How is it going?")},
		{"the status", func() *html.Node { return formField(t, page, "status").Node }},
		{"the Rolled-up Health", func() *html.Node { return findByTestID(t, page, "checkin-form-rollup") }},
		{"<h2> Path to Green", heading("h2", "Path to Green")},
		{"<h2> Metrics", heading("h2", "Metrics")},
	} {
		n := part.find()
		if n == nil || last != nil && !precedes(last, n) {
			t.Fatalf("page has no %s after what came before it; it reads:\n%s", part.what, textOf(page))
		}
		last = n
	}
	for _, gone := range []string{"Metric readings", "Path to Green (required"} {
		if strings.Contains(textOf(page), gone) {
			t.Errorf("page still reads %q", gone)
		}
	}
	for _, h2 := range findAll(page, func(n *html.Node) bool { return n.Data == "h2" }) {
		if text := textOf(h2); text != "" && unicode.IsDigit([]rune(text)[0]) {
			t.Errorf("a heading is numbered: %q", text)
		}
	}
}

// The Check-in page's header reads Check in under the Goal's title, which
// links back to the Goal, whether it has a No change to offer and Contributed
// to by to show or not; it is the page's only title (DESIGN.md § Check-in
// page). That it sits in the flat frame, with no card, is tested in
// styles_test.go.
func TestCheckinPageIsAFlatForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)
	h.Checkin(sam, f.parent.ID, domain.HealthYellow, "Slipping.", "Fix the paywall.", pathDate)
	lone := h.ActiveGoal(sam, "Ship it", "It matters.")
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, g := range []domain.Goal{f.parent, lone} {
		page := getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, g.ID))
		h1s := findAll(page, func(n *html.Node) bool { return n.Data == "h1" })
		if got := textsOf(h1s); !slices.Equal(got, []string{"Check in"}) {
			t.Fatalf("%s: the page's titles read %q, want only Check in", g.Title, got)
		}
		crumb := linkTo(page, fmt.Sprintf("/goals/%d", g.ID))
		if crumb == nil || textOf(crumb) != g.Title || !precedes(crumb, h1s[0]) {
			t.Errorf("%s: the header has no breadcrumb above Check in reading the Goal's title and linking to it", g.Title)
		}
		if breadcrumbs := findAll(page, func(n *html.Node) bool { return attrOf(n, "aria-label") == "Breadcrumb" }); len(breadcrumbs) > 0 {
			t.Errorf("%s: the page keeps its own breadcrumb under the header", g.Title)
		}
	}
}

// Under its title the Check-in page says which week it is, how often the Goal
// checks in, and when it was last checked in on, if ever (ticket #364). The
// harness clock reads Fri Jan 2, 2026 in UTC, the company's timezone.
func TestCheckinPageMetaLine(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")
	checkinURL := fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)
	meta := func() string {
		t.Helper()
		return textOf(findByTestID(t, getPage(t, client, checkinURL), "checkin-page-meta"))
	}

	if got, want := meta(), "Week of Dec 29, 2025 · checks in every 7 days"; got != want {
		t.Errorf("with no Check-in, meta line = %q, want %q", got, want)
	}
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	if got, want := meta(), "Week of Dec 29, 2025 · checks in every 7 days · last check-in Jan 2"; got != want {
		t.Errorf("after a Check-in, meta line = %q, want %q", got, want)
	}
}

// The Rolled-up Health sits in a panel of its own under the Status: its badge
// with the count of children that set it, then the explanation asked for when
// the Owner's Health differs (ticket #364).
func TestCheckinFormRolledUpPanel(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red why it matters.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, parent.ID))
	if got := checkedRadio(page, "health"); got != domain.HealthGreen {
		t.Fatalf("the form starts on %q, want Green", got)
	}
	line := findByTestID(t, page, "checkin-form-rollup")
	if got, want := textOf(line), "Rolled-up Health: Red 1 of 1 Red"; got != want {
		t.Errorf("rollup line = %q, want %q", got, want)
	}
	explanation := formField(t, page, "explanation")
	if !precedes(line, explanation.Node) {
		t.Errorf("the explanation comes before the Rolled-up Health")
	}
	if want := "Explain the difference from Rolled-up Health Required while it differs from the Rolled-up Health"; explanation.Label != want {
		t.Errorf("explanation label = %q, want %q", explanation.Label, want)
	}
}

// The Path to Green's heading notes the Goal's Run off Green beside "Required
// while Yellow or Red", also on a form re-rendered after a refused submit;
// a Goal that is Green notes none (ticket #364).
func TestCheckinFormPathToGreenNotesRunOffGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	yellow := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Add a second on-call.", pathDate)
	green := h.ActiveGoal(sam, "Ship it", "It matters.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")
	legend := func(doc *html.Node) string {
		t.Helper()
		return textOf(firstTag(t, findByTestID(t, doc, "path-to-green-field"), "legend"))
	}

	page := getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, yellow.ID))
	got := legend(page)
	for _, want := range []string{"Path to Green", "Required while Yellow or Red", "1 wk off Green"} {
		if !strings.Contains(got, want) {
			t.Errorf("Yellow Goal's Path to Green legend = %q, want it to contain %q", got, want)
		}
	}
	// Beside the Back to Green by date, its last value and that a later one
	// needs a reason.
	if got, want := textOf(findByTestID(t, page, "path-to-green-field")), "Last time: Jun 15 A later date needs a reason."; !strings.Contains(got, want) {
		t.Errorf("the Path to Green reads %q, want Back to Green by's hints %q", got, want)
	}

	body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, yellow.ID), url.Values{
		"health": {domain.HealthYellow},
		"status": {"Still slipping."},
	})
	doc := parsePage(t, body)
	fieldError(t, doc, "path_target_date")
	if got := legend(doc); !strings.Contains(got, "1 wk off Green") {
		t.Errorf("re-rendered Path to Green legend = %q, want it to keep 1 wk off Green", got)
	}

	page = getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, green.ID))
	if got := legend(page); strings.Contains(got, "off Green") {
		t.Errorf("Green Goal's Path to Green legend = %q, want no Run off Green", got)
	}
	if strings.Contains(textOf(page), "A later date needs a reason.") {
		t.Errorf("a Goal with no previous Back to Green date says a later one needs a reason")
	}
}

// checkinSubmitBar returns the Check-in form's submit bar, failing unless
// nothing in the form comes after it.
func checkinSubmitBar(t *testing.T, form *html.Node) *html.Node {
	t.Helper()
	bar := findByTestID(t, form, "checkin-submit-bar")
	var last *html.Node
	for n := range form.Descendants() {
		if n.Type == html.ElementNode {
			last = n
		}
	}
	if !within(last, bar) {
		t.Fatalf("the submit bar isn't the form's last element; %s comes after it", renderNode(last))
	}
	return bar
}

// The Check-in form ends with its submit bar: a summary the script fills,
// empty from the server, then Cancel and Submit check-in, and no field. The
// hidden default submit is still the form's first, so Enter submits the
// Check-in (ticket #367).
func TestCheckinFormEndsWithSubmitBar(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	form := findByTestID(t, page, "checkin-form")
	bar := checkinSubmitBar(t, form)
	summary := findByTestID(t, bar, "checkin-summary")
	if attrOf(summary, "aria-live") != "polite" {
		t.Errorf("the summary isn't announced politely: %s", renderNode(summary))
	}
	if summary.FirstChild != nil {
		t.Errorf("the summary isn't empty from the server: %s", renderNode(summary))
	}
	if cancel := linkTo(bar, fmt.Sprintf("/goals/%d", goal.ID)); cancel == nil || textOf(cancel) != "Cancel" {
		t.Errorf("the submit bar has no Cancel back to the Goal: %q", textOf(bar))
	}
	if submit := buttonNamed(bar, "Submit check-in"); submit == nil || attrOf(submit, "type") != "submit" || hasAttr(submit, "name") {
		t.Errorf("the submit bar has no plain Submit check-in: %s", renderNode(bar))
	}
	if fields := findAll(bar, func(n *html.Node) bool { return n.Data == "input" || n.Data == "select" || n.Data == "textarea" }); len(fields) > 0 {
		t.Errorf("the submit bar holds a field: %s", renderNode(fields[0]))
	}
	if first := submitButtons(form)[0]; attrOf(first, "data-testid") != "checkin-default-submit" {
		t.Errorf("the form's first submit isn't the default submit: %s", renderNode(first))
	}
}

// The delivery date and each Milestone's date carry the Goal's current one as
// data-current, so the summary script can tell a changed date from one left
// alone, however the form was re-rendered (ticket #367). ActiveGoal delivers
// six months after the harness's Jan 2, 2026, and its Beta is due in three.
func TestCheckinDateFieldsCarryTheCurrentDates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal)
	client := signInClient(t, ts.URL, "sam@example.com")
	betaDate := fmt.Sprintf("milestone_date_%d", beta.ID)

	page := getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	if got := attrOf(formField(t, page, "delivery_date").Node, "data-current"); got != "2026-07-02" {
		t.Errorf("delivery_date's data-current = %q, want 2026-07-02", got)
	}
	if got := attrOf(formField(t, page, betaDate).Node, "data-current"); got != "2026-04-02" {
		t.Errorf("Beta's date's data-current = %q, want 2026-04-02", got)
	}

	// Refused for its blank status, the form comes back with the dates as
	// typed and the current ones still marked.
	body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":        {domain.HealthGreen},
		"status":        {" "},
		"delivery_date": {"2026-08-01"},
		betaDate:        {"2026-05-01"},
	})
	doc := parsePage(t, body)
	if delivery := formField(t, doc, "delivery_date"); delivery.Value != "2026-08-01" || attrOf(delivery.Node, "data-current") != "2026-07-02" {
		t.Errorf("re-rendered delivery_date = %s, want value 2026-08-01 and data-current 2026-07-02", renderNode(delivery.Node))
	}
	if milestone := formField(t, doc, betaDate); milestone.Value != "2026-05-01" || attrOf(milestone.Node, "data-current") != "2026-04-02" {
		t.Errorf("re-rendered Beta date = %s, want value 2026-05-01 and data-current 2026-04-02", renderNode(milestone.Node))
	}
}

// A refused htmx submit swaps the form back with its submit bar and the
// script that fills the bar's summary, so the summary runs again on the new
// form (ticket #367).
func TestCheckinRefusedSubmitBringsBackTheSubmitBar(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	body, status := postFormHX(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {" "},
	})
	if status != http.StatusOK {
		t.Fatalf("refused htmx submit: status %d, want 200 so htmx swaps the form", status)
	}
	form := findByTestID(t, parsePage(t, body), "checkin-form")
	checkinSubmitBar(t, form)
	found := false
	for _, script := range findAll(form, func(n *html.Node) bool { return n.Data == "script" }) {
		code := ""
		for c := script.FirstChild; c != nil; c = c.NextSibling {
			code += c.Data
		}
		found = found || strings.Contains(code, "checkin-form") && strings.Contains(code, "checkin-summary")
	}
	if !found {
		t.Errorf("the re-rendered form has no script filling the summary:\n%s", renderNode(form))
	}
}

// Beside the form, the Check-in page shows what the last Check-in said, its
// Health, back-to-Green date and Status, and the Goal's dates, so the Owner
// checks in without going back to the Goal page (ticket #366).
func TestCheckinPageShowsLastTimeAndDates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	const status = "The paywall fix slipped a week."
	h.Checkin(sam, goal.ID, domain.HealthYellow, status, "Ship the fix behind a flag.", testsupport.Epoch.AddDate(0, 2, 0))

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	aside := findByTestID(t, page, "checkin-context")
	last := findByTestID(t, aside, "checkin-last-time")
	if headingNamed(last, "Last time · Jan 2") == nil {
		t.Errorf("Last time isn't headed Last time · Jan 2: %q", textOf(last))
	}
	if got, want := textOf(last), "Last time · Jan 2 Yellow back to Green by Mar 2 "+status; got != want {
		t.Errorf("Last time reads %q, want %q", got, want)
	}
	if got := textsOf(findAllByTestID(last, "checkin-last-status")); !slices.Equal(got, []string{status}) {
		t.Errorf("Last time's Status = %q, want %q", got, status)
	}
	dates := findByTestID(t, aside, "checkin-context-dates")
	if headingNamed(dates, "Dates") == nil {
		t.Errorf("the Dates section isn't headed Dates: %q", textOf(dates))
	}
	if got, want := textOf(dates), "Dates Delivers Jul 2 · 1 planned Milestone, next Beta Apr 2"; got != want {
		t.Errorf("the Dates section reads %q, want %q", got, want)
	}
}

// A Goal with no Check-in yet still shows the column with its Dates, and leaves
// out Last time, which would have nothing to say (ticket #366).
func TestCheckinPageContextWithoutACheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	aside := findByTestID(t, page, "checkin-context")
	findByTestID(t, aside, "checkin-context-dates")
	if hasTestID(page, "checkin-last-time") {
		t.Errorf("a Goal with no Check-in shows Last time: %q", textOf(aside))
	}
}

// The column holds Last time, then a Goal's children, then Dates, as the
// design has them; a Goal with no children has the column but no children list
// (ticket #366).
func TestCheckinPageContextSectionsInOrder(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)
	h.Checkin(sam, f.parent.ID, domain.HealthYellow, "Slipping.", "Fix the paywall.", pathDate)
	lone := h.ActiveGoal(sam, "Ship it", "It matters.")
	client := signInClient(t, ts.URL, "sam@example.com")

	aside := findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, f.parent.ID)), "checkin-context")
	last := findByTestID(t, aside, "checkin-last-time")
	children := findByTestID(t, aside, "checkin-children")
	dates := findByTestID(t, aside, "checkin-context-dates")
	if !precedes(last, children) || !precedes(children, dates) {
		t.Errorf("the column reads %q, want Last time, children, then Dates", textOf(aside))
	}

	page := getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, lone.ID))
	findByTestID(t, page, "checkin-context")
	if hasTestID(page, "checkin-children") {
		t.Errorf("a Goal with no children shows a children list")
	}
}

// A refused htmx submit swaps back only the form; the column sits outside it,
// so it isn't sent again (ticket #366).
func TestCheckinRefusedSubmitLeavesTheContextAlone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	body, status := postFormHX(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {""},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200", status)
	}
	doc := parsePage(t, body)
	fieldError(t, doc, "status")
	if hasTestID(doc, "checkin-context") {
		t.Errorf("refused submit re-renders the column along with the form; body:\n%s", body)
	}
}

// checkinAddBar returns the Check-in page's "Add to this check-in" bar,
// failing unless it comes before the form.
func checkinAddBar(t *testing.T, page *html.Node) *html.Node {
	t.Helper()
	bar := findByTestID(t, page, "checkin-add-bar")
	if !precedes(bar, findByTestID(t, page, "checkin-form")) {
		t.Fatalf("the add bar isn't before the form")
	}
	return bar
}

// Above the form, a group of buttons adds to the Check-in: a Highlight of each
// kind, standing for the Highlights section's own add buttons, then Move a
// date or Milestone and Pause, finish or cancel, each controlling its section.
// None submits. That it shows only once script has run is tested in
// styles_test.go (ticket #365).
func TestCheckinPageHasAddToThisCheckinBar(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	bar := checkinAddBar(t, page)
	if attrOf(bar, "role") != "group" || attrOf(bar, "aria-label") != "Add to this check-in" {
		t.Errorf("the bar is role %q labelled %q, want a group labelled Add to this check-in", attrOf(bar, "role"), attrOf(bar, "aria-label"))
	}
	var kinds []string
	for _, button := range findAllByTestID(bar, "checkin-add-highlight") {
		if attrOf(button, "type") != "button" {
			t.Errorf("a Highlight button would submit the form: %s", renderNode(button))
		}
		kinds = append(kinds, attrOf(button, "data-kind"))
	}
	if want := []string{"Insight", "Accomplishment", "Miss"}; !slices.Equal(kinds, want) {
		t.Errorf("the bar's Highlight kinds = %q, want %q", kinds, want)
	}
	for testID, want := range map[string]struct{ text, controls string }{
		"checkin-add-dates":     {"Move a date or Milestone", "checkin-dates-section"},
		"checkin-add-lifecycle": {"Pause, finish or cancel", "checkin-lifecycle-section"},
	} {
		button := findByTestID(t, bar, testID)
		if button.Data != "button" || attrOf(button, "type") != "button" || attrOf(button, "aria-controls") != want.controls {
			t.Errorf("%s = %s, want a button controlling %s", testID, renderNode(button), want.controls)
		}
		if got := textOf(button); got != want.text {
			t.Errorf("%s reads %q, want %q", testID, got, want.text)
		}
	}
	if n := len(findAllByTestID(page, "highlight-add")); n != 3 {
		t.Errorf("the page has %d highlight-add buttons, want the Highlights section's 3", n)
	}
}

// An On Hold Goal's add bar offers to resume or cancel it, as its form does;
// an ended Goal, which takes no Check-in, has no bar (ticket #365).
func TestCheckinAddBarFollowsTheGoalsLifecycle(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	onHold := h.OnHoldGoal(sam, "Grow revenue", "Revenue funds the rest.", "Waiting on budget.")
	done := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)
	client := signInClient(t, ts.URL, "sam@example.com")

	bar := checkinAddBar(t, getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, onHold.ID)))
	if got := textOf(findByTestID(t, bar, "checkin-add-lifecycle")); got != "Resume or cancel" {
		t.Errorf("an On Hold Goal's Lifecycle button reads %q, want Resume or cancel", got)
	}

	page := getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, done.ID))
	if !hasTestID(page, "goal-no-health") {
		t.Fatalf("the Done Goal's Check-in page doesn't say it takes no Check-ins: %q", textOf(page))
	}
	if hasTestID(page, "checkin-add-bar") {
		t.Errorf("a Done Goal's Check-in page has an add bar")
	}
}

// The dates and Lifecycle sections carry the ids the add bar's buttons control
// and, for script to show in place of their summaries, a heading it can move
// focus to. A refused submit's re-render keeps them, and doesn't send the bar,
// which sits outside the form (ticket #365).
func TestCheckinSectionsCarryHeadingsForTheAddBar(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {""},
	})
	refused := parsePage(t, body)
	if !hasTestID(refused, "checkin-error") {
		t.Fatalf("a Check-in with no status wasn't refused; body:\n%s", body)
	}
	if hasTestID(refused, "checkin-add-bar") {
		t.Errorf("the refused submit re-renders the add bar")
	}
	for name, doc := range map[string]*html.Node{"page": page, "re-render": refused} {
		highlights := findByTestID(t, doc, "checkin-highlight-section")
		if attrOf(highlights, "id") != "checkin-highlight-section" {
			t.Errorf("%s: the Highlights section has id %q, want checkin-highlight-section", name, attrOf(highlights, "id"))
		}
		if heading := findByTestID(t, highlights, "checkin-highlights-heading"); attrOf(heading, "tabindex") != "-1" {
			t.Errorf("%s: the Highlights heading = %s, want tabindex -1", name, renderNode(heading))
		}
		if !strings.Contains(textOf(highlights), "A Report author can pull each one in") {
			t.Errorf("%s: the Highlights heading has no muted line: %q", name, textOf(highlights))
		}
		for section, heading := range map[string]struct{ testID, text string }{
			"checkin-dates-section":     {"checkin-dates-heading", "Dates and Milestones"},
			"checkin-lifecycle-section": {"checkin-lifecycle-heading", "Lifecycle"},
		} {
			details := findByTestID(t, doc, section)
			if attrOf(details, "id") != section {
				t.Errorf("%s: %s has id %q, want %s", name, section, attrOf(details, "id"), section)
			}
			h2 := findByTestID(t, details, heading.testID)
			if h2.Data != "h2" || attrOf(h2, "tabindex") != "-1" {
				t.Errorf("%s: %s = %s, want an <h2> with tabindex -1", name, heading.testID, renderNode(h2))
			}
			if got := textOf(h2); got != heading.text {
				t.Errorf("%s: %s reads %q, want %q", name, heading.testID, got, heading.text)
			}
		}
	}
}
