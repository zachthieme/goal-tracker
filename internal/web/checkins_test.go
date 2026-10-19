package web_test

import (
	"context"
	"fmt"
	"html"
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
	if !strings.Contains(body, `data-testid="checkin-error"`) {
		t.Errorf("response missing the inline error; body:\n%s", body)
	}
	if !strings.Contains(body, "Path to Green") {
		t.Errorf("error does not mention the Path to Green; body:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="checkin-form"`) {
		t.Errorf("error response is not the re-rendered form; body:\n%s", body)
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit first check-in: status %d", resp.StatusCode)
	}

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
	page := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if !strings.Contains(page, `data-testid="goal-health">Green`) {
		t.Errorf("Goal page missing current Health; body:\n%s", page)
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
	metric, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
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

	page := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	for _, want := range []string{"850", "Shipped the caching layer.", domain.HighlightAccomplishment} {
		if !strings.Contains(page, want) {
			t.Errorf("Goal page missing %q; body:\n%s", want, page)
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit check-in: status %d", resp.StatusCode)
	}
	if slips, _ := h.Service.ListDateSlips(context.Background(), goal.ID); len(slips) != 1 {
		t.Fatalf("recorded %d Date Slips, want 1", len(slips))
	}

	page := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	meta := pageElement(t, page, "p", "goal-meta")
	if delivery := pageElement(t, meta, "span", "goal-delivery-date"); !strings.Contains(delivery, fmt.Sprintf("<del>%s</del> <strong>%s</strong>", oldDate, newDate)) {
		t.Errorf("metadata line does not show the delivery date struck through; element:\n%s", delivery)
	}
	if slips := between(t, historyBlock(t, page), `data-testid="entry-slips"`, "</ul>"); !strings.Contains(slips, "Vendor API delayed two weeks.") {
		t.Errorf("the Check-in on the History timeline is missing the Date Slip's reason:\n%s", slips)
	}
	if !strings.Contains(page, `data-testid="goal-slip-count">1<`) {
		t.Errorf("Goal page missing the slip count")
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

	cell := between(t, getBody(t, client, goalPageURL(ts.URL, red)), `data-testid="goal-health-cell"`, "</dd>")
	if got := elementTexts(cell, "span", "goal-run-off-green"); !slices.Equal(got, []string{"· 2 wks"}) {
		t.Errorf("Owner's Health cell reads its Run off Green %q, want \"· 2 wks\"; cell:\n%s", got, cell)
	}
	if page := getBody(t, client, goalPageURL(ts.URL, green)); strings.Contains(page, `data-testid="goal-run-off-green"`) {
		t.Errorf("a Green Goal's page shows a Run off Green:\n%s", between(t, page, `data-testid="goal-health-cell"`, "</dd>"))
	}
}

// The Latest status card opens with a row of cells, each a small label over a
// value: the Owner's Health, the Rolled-up Health with its Stale-children count
// when the Goal has Active children, and the Back to Green date when there is a
// Path to Green. The status, the Path to Green and the explanation follow.
func TestLatestStatusOpensWithCells(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	h.ActiveChildOf(sam, parent, "Silent child", "It matters.")
	h.Clock.Advance(10 * 24 * time.Hour)
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red so what.")
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

	card := pageElement(t, getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID)), "section", "goal-checkins")
	cells := pageElement(t, card, "dl", "goal-status-cells")
	for _, tc := range []struct{ testID, label, value string }{
		{"goal-health-cell", "Owner's Health", `data-testid="goal-health">Yellow<`},
		{"goal-rollup-cell", "Rolled-up Health", `data-testid="goal-rollup-health">Red<`},
		{"goal-rollup-cell", "Rolled-up Health", `data-testid="goal-rollup-stale">1 of 2 Stale<`},
		{"goal-back-to-green-cell", "Back to Green by", "<dd><span>Jun 15</span></dd>"},
	} {
		cell := between(t, cells, `data-testid="`+tc.testID+`"`, "")
		cell, _, _ = strings.Cut(cell, "<div ")
		// Whitespace before a closing tag shows as nothing.
		cell = regexp.MustCompile(`\s+</`).ReplaceAllString(cell, "</")
		if !strings.Contains(cell, `<dt class="label">`+tc.label+`</dt>`) || !strings.Contains(cell, tc.value) {
			t.Errorf("%s cell lacks label %q over %s: %s", tc.testID, tc.label, tc.value, cell)
		}
	}
	_, rest, _ := strings.Cut(card, `data-testid="goal-status-cells"`)
	for _, want := range []string{`data-testid="goal-status"`, `data-testid="goal-path-to-green"`, `data-testid="goal-rollup-explanation"`} {
		if !strings.Contains(rest, want) {
			t.Errorf("Latest status has no %s below its cells: %s", want, card)
		}
	}

	// A childless Goal with no Path to Green shows only the Owner's Health cell.
	lone := h.ActiveGoal(sam, "Lone", "It matters.")
	h.Checkin(sam, lone.ID, domain.HealthGreen, "On track.", "", time.Time{})
	card = pageElement(t, getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, lone.ID)), "section", "goal-checkins")
	cells = pageElement(t, card, "dl", "goal-status-cells")
	if !strings.Contains(cells, `data-testid="goal-health">Green<`) {
		t.Errorf("Latest status has no Owner's Health cell: %s", cells)
	}
	for _, absent := range []string{"goal-rollup-cell", "Rolled-up Health", "goal-back-to-green-cell", "Back to Green"} {
		if strings.Contains(card, absent) {
			t.Errorf("a childless Goal with no Path to Green shows %s: %s", absent, card)
		}
	}

	// A Goal with no Health keeps its message in place of the cells.
	proposed := h.CreateGoal(sam, "Proposed", "It matters.")
	card = pageElement(t, getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, proposed.ID)), "section", "goal-checkins")
	if strings.Contains(card, `data-testid="goal-status-cells"`) || !strings.Contains(card, "A Proposed Goal has no Health until it is Active.") {
		t.Errorf("a Proposed Goal's Latest status should say it has no Health, with no cells: %s", card)
	}
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
	if msg := fieldError(t, body, `name="delivery_date"`); !strings.Contains(msg, "moves the delivery date later") {
		t.Errorf("delivery date field's error = %q, want the Green one", msg)
	}
	if !strings.Contains(body, "Vendor slipped.") {
		t.Errorf("re-rendered form lost the typed reason; body:\n%s", body)
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

	page := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	milestones := pageElement(t, page, "section", "goal-milestones")
	for _, want := range []string{fmt.Sprintf("<span>%s</span> <del>%s</del>", newBeta, oldBeta), "Docs folded into GA."} {
		if !strings.Contains(milestones, want) {
			t.Errorf("Milestone list missing %q; section:\n%s", want, milestones)
		}
	}
	if slips := between(t, historyBlock(t, page), `data-testid="entry-slips"`, "</ul>"); !strings.Contains(slips, "Design review moved.") {
		t.Errorf("the Check-in on the History timeline is missing the Date Slip's reason:\n%s", slips)
	}
	for _, want := range []string{`data-testid="goal-slip-count">1<`, `data-testid="goal-milestone-churn">2<`} {
		if !strings.Contains(page, want) {
			t.Errorf("Goal page missing %q", want)
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put On Hold: status %d", resp.StatusCode)
	}

	page := getBody(t, samClient, goalURL)
	if !strings.Contains(page, `data-testid="goal-lifecycle">On Hold<`) {
		t.Errorf("Goal page does not show the Goal On Hold")
	}
	if note := pageElement(t, page, "p", "goal-lifecycle-note"); !strings.Contains(note, "Team moved to the payments incident.") {
		t.Errorf("Goal page missing why it is On Hold; element:\n%s", note)
	}
	if entry := pageElement(t, page, "span", "checkin-lifecycle"); !strings.Contains(entry, "Active → On Hold") || !strings.Contains(entry, "Team moved to the payments incident.") {
		t.Errorf("History timeline missing the Lifecycle change; element:\n%s", entry)
	}
	form := pageElement(t, getBody(t, samClient, goalURL+"/checkin"), "fieldset", "checkin-lifecycle-fields")
	if !strings.Contains(form, `value="Active"`) || strings.Contains(form, `value="Done"`) {
		t.Errorf("On Hold form should offer resume and Cancel only; form:\n%s", form)
	}
	if strings.Contains(page, `data-testid="no-change-checkin"`) {
		t.Errorf("On Hold Goal offers the no-change Check-in")
	}

	resp = postForm(t, samClient, goalURL+"/checkins", url.Values{
		"health":    {domain.HealthGreen},
		"status":    {"Back on it."},
		"lifecycle": {domain.LifecycleActive},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resume: status %d", resp.StatusCode)
	}
	page = getBody(t, samClient, goalURL)
	if !strings.Contains(page, `data-testid="goal-lifecycle">Active<`) {
		t.Errorf("Goal page does not show the resumed Goal Active")
	}
	if !strings.Contains(page, "On Hold → Active") {
		t.Errorf("Check-in history missing the resume")
	}
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
	metric, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
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
	if msg := fieldError(t, body, fmt.Sprintf(`name="reading_%d"`, metric.ID)); !strings.Contains(msg, "final value") {
		t.Errorf("reading field's error = %q, want the final-value one", msg)
	}
	if !strings.Contains(body, "p95 latency down to 380ms.") {
		t.Errorf("re-rendered form lost the typed outcome; body:\n%s", body)
	}

	done.Set(fmt.Sprintf("reading_%d", metric.ID), "380")
	resp := postForm(t, samClient, goalURL+"/checkins", done)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark Done: status %d", resp.StatusCode)
	}

	page := getBody(t, samClient, goalURL)
	if !strings.Contains(page, `data-testid="goal-lifecycle">Done<`) {
		t.Errorf("Goal page does not show the Goal Done")
	}
	if note := pageElement(t, page, "p", "goal-lifecycle-note"); !strings.Contains(note, "Outcome: p95 latency down to 380ms.") {
		t.Errorf("Goal page missing the outcome; element:\n%s", note)
	}
	if strings.Contains(page, `data-testid="checkin-form"`) {
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
	if msg := pageElement(t, body, "p", "checkin-error"); !strings.Contains(msg, "Cancelling a Goal needs a reason") {
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
	metric, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
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

	body, status := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":                             {domain.HealthGreen},
		"status":                             {"On track."},
		fmt.Sprintf("reading_%d", metric.ID): {"abc"},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200", status)
	}
	msg := fieldError(t, body, fmt.Sprintf(`name="reading_%d"`, metric.ID))
	if !strings.Contains(msg, `&#34;Signups&#34; needs a number`) {
		t.Errorf("error = %q, want it to say \"Signups\" needs a number", msg)
	}
	if strings.Contains(msg, "metric") {
		t.Errorf("error = %q, still names the Metric by its id", msg)
	}
	if !strings.Contains(body, `value="abc"`) {
		t.Errorf("re-rendered form lost the typed reading; body:\n%s", body)
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
		m, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
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
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("submit check-in: status %d", resp.StatusCode)
		}
	}

	form := pageElement(t, getBody(t, samClient, goalURL+"/checkin"), "fieldset", "checkin-readings")
	if want := fmt.Sprintf(`name="reading_%d" value="" placeholder="135.5"`, signups.ID); !strings.Contains(form, want) {
		t.Errorf("Signups reading not empty with its latest value as ghost text (want %s); form:\n%s", want, form)
	}
	if want := fmt.Sprintf(`name="reading_%d" value="">`, referrals.ID); !strings.Contains(form, want) {
		t.Errorf("Referrals has no readings, so its input should be blank with no ghost text (want %s); form:\n%s", want, form)
	}
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
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red so what.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	samClient := signInClient(t, ts.URL, "sam@example.com")
	body, _ := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, parent.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Looks fine."},
	})
	const want = "This Health differs from the Rolled-up Health (Red); explain why."
	if msg := fieldError(t, body, `name="explanation"`); !strings.Contains(msg, `">`+want) {
		t.Errorf("explanation field's error = %q, want it to read %q", msg, want)
	}
	if strings.Contains(body, "validation failed") {
		t.Errorf("page shows the internal \"validation failed\" prefix; body:\n%s", body)
	}
}

// fieldError returns the form's one validation error, failing unless it sits
// right after the field matched by fieldMarker — after that field and before
// the next field's label — so it is shown next to the field it is about.
func fieldError(t *testing.T, body, fieldMarker string) string {
	t.Helper()
	if n := strings.Count(body, `data-testid="checkin-error"`); n != 1 {
		t.Fatalf("form shows %d validation errors, want 1; body:\n%s", n, body)
	}
	field := strings.Index(body, fieldMarker)
	if field < 0 {
		t.Fatalf("form has no field %s; body:\n%s", fieldMarker, body)
	}
	after := body[field:]
	if next := strings.Index(after[1:], "<label"); next >= 0 {
		after = after[:next+1]
	}
	if !strings.Contains(after, `data-testid="checkin-error"`) {
		t.Fatalf("the error is not next to %s; body:\n%s", fieldMarker, body)
	}
	return pageElement(t, after, "p", "checkin-error")
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
	const want = "A Yellow Goal needs a Path to Green"
	if msg := fieldError(t, body, `name="path_target_date"`); !strings.Contains(msg, `">`+want) {
		t.Errorf("Path to Green field's error = %q, want it to start %q", msg, want)
	}
	if strings.Contains(body, "validation failed") {
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
	if msg := fieldError(t, body, `name="lifecycle_reason"`); !strings.Contains(msg, "On Hold needs a reason") {
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
	if msg := fieldError(t, body, `name="outcome"`); !strings.Contains(msg, "needs an outcome") {
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
	if msg := fieldError(t, body, `name="lifecycle" value="Cancelled"`); !strings.Contains(msg, "Active Goal to") {
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
	if msg := fieldError(t, body, `name="delivery_date_reason"`); !strings.Contains(msg, "delivery date needs a reason") {
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
	marker := fmt.Sprintf(`name="milestone_date_reason_%d"`, beta.ID)
	if msg := fieldError(t, body, marker); !strings.Contains(msg, "needs a reason") {
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
	marker := fmt.Sprintf(`name="milestone_removed_reason_%d"`, beta.ID)
	if msg := fieldError(t, body, marker); !strings.Contains(msg, "Removing Milestone") {
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
	marker := fmt.Sprintf(`name="milestone_date_%d"`, beta.ID)
	msg := html.UnescapeString(fieldError(t, body, marker))
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
	_, docs, ok := strings.Cut(body, `value="Docs"`)
	if !ok {
		t.Fatalf("form lost the Docs row; body:\n%s", body)
	}
	if msg := fieldError(t, docs, `name="new_milestone_date"`); !strings.Contains(msg, "needs a date") {
		t.Errorf("new Milestone's error = %q, want the missing date one", msg)
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
	if msg := fieldError(t, body, `name="new_milestone_date"`); !strings.Contains(msg, "is overdue") {
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
	marker := fmt.Sprintf(`name="milestone_date_%d"`, beta.ID)
	if msg := fieldError(t, body, marker); !strings.Contains(msg, "invalid date") {
		t.Errorf("Milestone date field's error = %q, want the invalid date one", msg)
	}
}

// highlightRows splits a rendered Check-in form into its Highlight rows, in
// order.
func highlightRows(t *testing.T, body string) []string {
	t.Helper()
	section := pageElement(t, body, "details", "checkin-highlight-section")
	parts := strings.Split(section, `data-testid="checkin-highlight-row"`)
	return parts[1:]
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
	rows := highlightRows(t, body)
	if len(rows) != 2 {
		t.Fatalf("form shows %d Highlight rows, want the 2 typed", len(rows))
	}
	if strings.Contains(rows[0], `data-testid="checkin-error"`) {
		t.Errorf("Highlight 1 shows an error; row:\n%s", rows[0])
	}
	if !strings.Contains(rows[1], `data-testid="checkin-error"`) || !strings.Contains(rows[1], "Highlight 2 needs a kind") {
		t.Errorf("Highlight 2 lacks its error naming it; row:\n%s", rows[1])
	}
	if !strings.Contains(rows[1], "Found a cheaper vendor.") {
		t.Errorf("Highlight 2 lost its note; row:\n%s", rows[1])
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	rows := highlightRows(t, page)
	if len(rows) != 1 {
		t.Fatalf("form shows %d Highlight rows, want 1", len(rows))
	}
	if !strings.Contains(rows[0], `<textarea name="highlight_note"></textarea>`) {
		t.Errorf("the Highlight row is not empty; row:\n%s", rows[0])
	}
	section := pageElement(t, page, "details", "checkin-highlight-section")
	if !strings.Contains(section, `name="add_highlight"`) {
		t.Errorf("no Add another button; section:\n%s", section)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	form := pageElement(t, page, "form", "checkin-form")
	first := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`).FindString(form)
	if attr(first, "data-testid") != "checkin-default-submit" {
		t.Fatalf("the form's first submit, which Enter presses, isn't the default submit: %s", first)
	}
	if strings.Contains(first, `name=`) {
		t.Errorf("the default submit posts a name, so it isn't a plain Submit check-in: %s", first)
	}
	if strings.Index(form, first) > strings.Index(form, `name="add_highlight"`) {
		t.Errorf("the default submit comes after Add another:\n%s", form)
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
			if strings.Contains(body, `data-testid="checkin-error"`) {
				t.Errorf("add another shows a validation error; body:\n%s", body)
			}
			rows := highlightRows(t, body)
			if len(rows) != 2 {
				t.Fatalf("form shows %d Highlight rows, want 2", len(rows))
			}
			if !strings.Contains(rows[0], "The runbook was a week late.") || !strings.Contains(rows[0], `value="Miss" selected`) {
				t.Errorf("Highlight 1 lost what was typed; row:\n%s", rows[0])
			}
			if !strings.Contains(rows[1], `<textarea name="highlight_note"></textarea>`) {
				t.Errorf("the added row is not empty; row:\n%s", rows[1])
			}
			section := pageElement(t, body, "details", "checkin-highlight-section")
			if !strings.Contains(openTag(section), " open") {
				t.Errorf("the Highlight section is collapsed after Add another")
			}
			for _, typed := range []string{"Failover slipped a week.", "Drill again on Friday.", `value="Yellow" checked`} {
				if !strings.Contains(body, typed) {
					t.Errorf("form lost %q", typed)
				}
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
	if msg := fieldError(t, body, `name="status"`); !strings.Contains(msg, "needs a status") {
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
	if msg := fieldError(t, body, `name="health" value="Red"`); !strings.Contains(msg, "Health must be") {
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
	if msg := fieldError(t, body, `data-testid="checkin-form"`); !strings.Contains(msg, "Only an Active Goal") {
		t.Errorf("the form's top error = %q, want the Active Goal one", msg)
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
		page := getBody(t, signInClient(t, ts.URL, who), checkinURL)
		if !strings.Contains(page, ">Check in</h1>") {
			t.Errorf("%s: page is not titled Check in; body:\n%s", who, page)
		}
		if !strings.Contains(page, fmt.Sprintf(`href="/goals/%d"`, goal.ID)) || !strings.Contains(page, "Reduce outages") {
			t.Errorf("%s: page has no breadcrumb back to the Goal; body:\n%s", who, page)
		}
		if !strings.Contains(page, `data-testid="checkin-form"`) {
			t.Errorf("%s: page has no Check-in form; body:\n%s", who, page)
		}
	}

	resp, err := signInClient(t, ts.URL, "eve@example.com").Get(checkinURL)
	if err != nil {
		t.Fatalf("GET %s: %v", checkinURL, err)
	}
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	section := between(t, page, `data-testid="goal-actions"`, `data-testid="goal-more"`)
	if !strings.Contains(section, fmt.Sprintf(`href="/goals/%d/checkin"`, goal.ID)) {
		t.Errorf("Goal page does not link to the Check-in page; section:\n%s", section)
	}
	if strings.Contains(page, `data-testid="checkin-form"`) {
		t.Errorf("Goal page still embeds the Check-in form")
	}
	if !strings.Contains(section, `data-testid="no-change-checkin"`) {
		t.Errorf("Goal page lost the no-change button; section:\n%s", section)
	}
}

// Health is picked from a radio group, not a select, picked from the latest
// Check-in. The Path to Green fieldset stays in the form, hidden by CSS alone
// while Green is picked.
func TestCheckinPageHealthIsRadioGroup(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Slipping.", "Add a second on-call.", pathDate)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	if strings.Contains(page, `<select name="health"`) {
		t.Errorf("Health is still a select")
	}
	for _, want := range []string{
		`type="radio" name="health" value="Green"`,
		`type="radio" name="health" value="Yellow" checked`,
		`type="radio" name="health" value="Red"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("Health radio group missing %s; body:\n%s", want, page)
		}
	}
	if !strings.Contains(page, `data-testid="path-to-green-field"`) {
		t.Errorf("Path to Green fieldset left the form")
	}
	if !strings.Contains(page, `:has(input[name=health][value=Green]:checked) .path-to-green:not(.has-error)`) {
		t.Errorf("no CSS rule hides the Path to Green while Green is picked; body:\n%s", page)
	}
}

// A Path to Green error keeps the fieldset shown whatever Health is picked.
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
}

// The Highlight, the dates and Milestones, and the Lifecycle each sit in a
// section collapsed by default, its summary line showing the current value.
func TestCheckinPageOptionalSectionsCollapsedByDefault(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	for testID, summary := range map[string]string{
		"checkin-highlight-section": "Flag a highlight",
		"checkin-dates-section":     "Change dates or milestones · delivers Jul 2 · 1 planned milestone",
		"checkin-lifecycle-section": "Pause, finish, or cancel this goal · stays Active",
	} {
		section := pageElement(t, page, "details", testID)
		if strings.Contains(openTag(section), " open") {
			t.Errorf("%s is open on a fresh Check-in", testID)
		}
		if !strings.Contains(section, summary) {
			t.Errorf("%s summary missing %q; section:\n%s", testID, summary, section)
		}
	}
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

	page := getBody(t, client, checkin+"?open=dates")
	for testID, open := range map[string]bool{
		"checkin-dates-section":     true,
		"checkin-highlight-section": false,
		"checkin-lifecycle-section": false,
	} {
		if got := strings.Contains(openTag(pageElement(t, page, "details", testID)), " open"); got != open {
			t.Errorf("with ?open=dates, %s open = %v, want %v", testID, got, open)
		}
	}
	for _, query := range []string{"?open=other", ""} {
		page := getBody(t, client, checkin+query)
		if strings.Contains(openTag(pageElement(t, page, "details", "checkin-dates-section")), " open") {
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
			if !strings.Contains(body, `data-testid="checkin-error"`) {
				t.Fatalf("expected a validation error; body:\n%s", body)
			}
			section := pageElement(t, body, "details", tc.section)
			if !strings.Contains(openTag(section), " open") {
				t.Errorf("%s stays collapsed; section:\n%s", tc.section, section)
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
		{"On%20Hold", domain.LifecycleOnHold, "Pause, finish, or cancel this goal · On Hold"},
		{"Done", domain.LifecycleDone, "Pause, finish, or cancel this goal · Done"},
		{"Cancelled", domain.LifecycleCancelled, "Pause, finish, or cancel this goal · Cancelled"},
	} {
		page := getBody(t, samClient, fmt.Sprintf("%s/goals/%d/checkin?lifecycle=%s", ts.URL, goal.ID, tc.query))
		section := pageElement(t, page, "details", "checkin-lifecycle-section")
		if !strings.Contains(openTag(section), " open") {
			t.Errorf("?lifecycle=%s: the Lifecycle section stays collapsed; section:\n%s", tc.query, section)
		}
		if !strings.Contains(section, tc.summary) {
			t.Errorf("?lifecycle=%s: summary missing %q; section:\n%s", tc.query, tc.summary, section)
		}
		if got := checkedLifecycle(section); got != tc.picked {
			t.Errorf("?lifecycle=%s: %q is picked, want %q; section:\n%s", tc.query, got, tc.picked, section)
		}
		if !strings.Contains(section, `<textarea name="lifecycle_reason"></textarea>`) {
			t.Errorf("?lifecycle=%s: the reason isn't empty; section:\n%s", tc.query, section)
		}
		if !strings.Contains(section, `<input type="text" name="outcome" value="">`) {
			t.Errorf("?lifecycle=%s: the outcome isn't empty; section:\n%s", tc.query, section)
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

	page := getBody(t, samClient, ts.URL+menuItems(t, getBody(t, samClient, goalURL))["Put on hold…"])
	picked := checkedLifecycle(pageElement(t, page, "details", "checkin-lifecycle-section"))
	if picked != domain.LifecycleOnHold {
		t.Fatalf("Put on hold… opened the Check-in with %q picked", picked)
	}
	form := url.Values{"health": {domain.HealthGreen}, "status": {"Pausing for the reorg."}, "lifecycle": {picked}}

	body, _ := postFormHX(t, samClient, goalURL+"/checkins", form)
	if msg := fieldError(t, body, `name="lifecycle_reason"`); !strings.Contains(msg, "On Hold needs a reason") {
		t.Errorf("with no reason the error is %q, want the On Hold one", msg)
	}
	if got, err := h.Service.ViewGoal(t.Context(), goal.ID); err != nil || got.Lifecycle != domain.LifecycleActive {
		t.Fatalf("after a refused Check-in the Goal is %q (%v), want Active", got.Lifecycle, err)
	}

	form.Set("lifecycle_reason", "Team moved to the payments incident.")
	if resp := postForm(t, samClient, goalURL+"/checkins", form); resp.StatusCode != http.StatusOK {
		t.Fatalf("put On Hold: status %d", resp.StatusCode)
	}
	page = getBody(t, samClient, goalURL)
	if !strings.Contains(page, `data-testid="goal-lifecycle">On Hold<`) {
		t.Errorf("Goal page does not show the Goal On Hold")
	}
	if entry := pageElement(t, page, "span", "checkin-lifecycle"); !strings.Contains(entry, "Active → On Hold") || !strings.Contains(entry, "Team moved to the payments incident.") {
		t.Errorf("History missing the Lifecycle change; element:\n%s", entry)
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
		{onHold, "Done", domain.LifecycleActive},
		{onHold, "On%20Hold", domain.LifecycleActive},
		{onHold, "Active", domain.LifecycleActive},
		{active, "bogus", ""},
		{active, "Active", ""},
		{active, "", ""},
	} {
		checkinURL := fmt.Sprintf("%s/goals/%d/checkin", ts.URL, tc.goal.ID)
		plain := between(t, getBody(t, samClient, checkinURL), `data-testid="checkin-form"`, "</form>")
		form := between(t, getBody(t, samClient, checkinURL+"?lifecycle="+tc.query), `data-testid="checkin-form"`, "</form>")
		if form != plain {
			t.Errorf("%s ?lifecycle=%s: the form differs from the one without it:\n%s\nwant:\n%s", tc.goal.Lifecycle, tc.query, form, plain)
		}
		section := pageElement(t, form, "details", "checkin-lifecycle-section")
		if strings.Contains(openTag(section), " open") {
			t.Errorf("%s ?lifecycle=%s: the Lifecycle section is open", tc.goal.Lifecycle, tc.query)
		}
		if got := checkedLifecycle(section); got != tc.picked {
			t.Errorf("%s ?lifecycle=%s: %q is picked, want the default %q", tc.goal.Lifecycle, tc.query, got, tc.picked)
		}
	}
}

// checkedLifecycle is the value of the Lifecycle radio checked in the section.
func checkedLifecycle(section string) string {
	for _, tag := range strings.Split(section, "<input")[1:] {
		tag, _, _ = strings.Cut(tag, ">")
		if strings.Contains(tag, `name="lifecycle"`) && strings.Contains(tag, " checked") {
			return html.UnescapeString(attr(tag, "value"))
		}
	}
	return "none"
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

	if page := getBody(t, samClient, checkinURL); strings.Contains(page, `data-testid="no-change-checkin"`) {
		t.Errorf("a Goal with no Check-in offers the no-change card")
	}

	latest := h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	page := getBody(t, samClient, checkinURL)
	card := strings.Index(page, `data-testid="no-change-card"`)
	if card < 0 {
		t.Fatalf("Check-in page has no no-change card; body:\n%s", page)
	}
	if form := strings.Index(page, `data-testid="checkin-form"`); form < card {
		t.Errorf("the no-change card does not come before the form")
	}
	for _, want := range []string{
		"Nothing changed since " + latest.CreatedAt.Format("Jan 2") + "?",
		`data-testid="no-change-checkin"`,
	} {
		if !strings.Contains(page[card:], want) {
			t.Errorf("no-change card missing %q; body:\n%s", want, page)
		}
	}
	// No change sits in the page's header, not in a card of its own.
	if tag := tagAround(t, page, `data-testid="no-change-card"`); strings.Contains(tag, `class="card"`) {
		t.Errorf("No change is still a card: %s", tag)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	_, form, ok := strings.Cut(page, `data-testid="checkin-form"`)
	if !ok {
		t.Fatalf("Check-in page has no form; body:\n%s", page)
	}
	form = openTag(form)
	if !strings.Contains(form, "hx-on:htmx:confirm=") || !strings.Contains(form, "value=Cancelled]:checked") || !strings.Contains(form, "confirm(") {
		t.Errorf("the form does not confirm a Cancel before sending; form tag:\n%s", form)
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
	page := readBody(t, resp)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	assertNoChangeRefusalOnForm(t, page, `Milestone &#34;Beta&#34; is overdue`)
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("a refused No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// assertNoChangeRefusalOnForm checks a refused No change came back as the
// Check-in page, inside the site's chrome, with the reason as the form's top
// error and without the internal "validation failed:" prefix.
func assertNoChangeRefusalOnForm(t *testing.T, page, reason string) {
	t.Helper()
	if !strings.Contains(page, `data-testid="nav-home"`) {
		t.Errorf("refusal is not inside the normal page; body:\n%s", page)
	}
	start := strings.Index(page, `data-testid="checkin-form"`)
	if start < 0 {
		t.Fatalf("refusal does not show the Check-in form; body:\n%s", page)
	}
	form := page[start : start+strings.Index(page[start:], "</form>")]
	errAt := strings.Index(form, `data-testid="checkin-error"`)
	if errAt < 0 || errAt > strings.Index(form, `data-testid="checkin-health"`) {
		t.Fatalf("the form has no error at its top; form:\n%s", form)
	}
	if !strings.Contains(form[errAt:], reason) {
		t.Errorf("the form's error does not give the reason %q; form:\n%s", reason, form)
	}
	if strings.Contains(page, "validation failed") {
		t.Errorf("the reason carries the internal prefix; body:\n%s", page)
	}
}

// postNoChange clicks a No change button: it posts to the button's action, as
// htmx does when hx is set, and returns the response with its body read.
func postNoChange(t *testing.T, client *http.Client, rawURL string, hx bool) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(""))
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

// checkedHealth is the value of the Health radio checked on the page, or
// "none".
func checkedHealth(page string) string {
	for _, tag := range strings.Split(page, "<input")[1:] {
		tag, _, _ = strings.Cut(tag, ">")
		if strings.Contains(tag, `name="health"`) && strings.Contains(tag, " checked") {
			return attr(tag, "value")
		}
	}
	return "none"
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

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	card := between(t, page, `data-testid="no-change-card"`, `data-testid="checkin-form"`)
	unavailable := tagAround(t, card, `data-testid="no-change-unavailable"`)
	if got := html.UnescapeString(between(t, card[strings.Index(card, unavailable)+len(unavailable):], "", "</p>")); got != want {
		t.Errorf("the card's reason = %q, want %q", got, want)
	}

	_, refused := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)
	topError := pageElement(t, refused, "p", "checkin-error")
	if got := html.UnescapeString(topError[strings.Index(topError, ">")+1:]); got != want {
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
		{domain.HealthGreen, "none"},
		{domain.HealthYellow, domain.HealthYellow},
	} {
		t.Run(tc.health, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			ts := newServer(t, h)
			sam := h.SignIn("sam@example.com")
			goal := overdueGoal(h, sam, tc.health)
			client := signInClient(t, ts.URL, "sam@example.com")

			_, refused := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)
			for name, page := range map[string]string{
				"Check-in page":     getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)),
				"refused No change": refused,
			} {
				if got := checkedHealth(page); got != tc.want {
					t.Errorf("%s: Health checked = %s, want %s", name, got, tc.want)
				}
				if section := pageElement(t, page, "details", "checkin-dates-section"); !strings.Contains(openTag(section), " open") {
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

	if got := checkedHealth(getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))); got != domain.HealthGreen {
		t.Errorf("Health checked = %s, want Green", got)
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
	card := func(page string) string {
		return between(t, page, `data-testid="no-change-card"`, `data-testid="checkin-form"`)
	}

	c := card(getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, overdue.ID)))
	if strings.Contains(c, `data-testid="no-change-checkin"`) {
		t.Errorf("the overdue Goal's card offers a No change that would be refused:\n%s", c)
	}
	unavailable := tagAround(t, c, `data-testid="no-change-unavailable"`)
	if !hasClass(unavailable, "muted") {
		t.Errorf("the card's reason is not muted: %s", unavailable)
	}
	reason := html.UnescapeString(between(t, c[strings.Index(c, unavailable)+len(unavailable):], "", "</p>"))
	if !strings.HasPrefix(reason, `Milestone "Beta" is overdue`) || !strings.HasSuffix(reason, ".") {
		t.Errorf("the card's reason = %q, want the overdue Milestone named, as a sentence", reason)
	}

	_, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, overdue.ID), false)
	if c := card(page); strings.Contains(c, `data-testid="no-change-unavailable"`) {
		t.Errorf("after a refused No change, the card repeats the form's error:\n%s", c)
	}
	if n := strings.Count(page, `Milestone &#34;Beta&#34; is overdue`); n != 1 {
		t.Errorf("after a refused No change, the reason shows %d times, want once", n)
	}
	assertNoChangeRefusalOnForm(t, page, `Milestone &#34;Beta&#34; is overdue`)

	c = card(getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, repeatable.ID)))
	if !strings.Contains(c, `data-testid="no-change-checkin"`) {
		t.Errorf("the repeatable Goal's card has no No change:\n%s", c)
	}
	if strings.Contains(c, `data-testid="no-change-unavailable"`) {
		t.Errorf("the repeatable Goal's card says No change is unavailable:\n%s", c)
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

	if resp.StatusCode != http.StatusOK {
		t.Errorf("htmx status = %d, want 200 so htmx swaps the page in", resp.StatusCode)
	}
	if got, want := resp.Header.Get("HX-Push-Url"), fmt.Sprintf("/goals/%d/checkin", goal.ID); got != want {
		t.Errorf("HX-Push-Url = %q, want %q", got, want)
	}
	assertNoChangeRefusalOnForm(t, page, `Milestone &#34;Beta&#34; is overdue`)
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

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	assertNoChangeRefusalOnForm(t, page, "differs from the Rolled-up Health (Red)")
	if tag, typed := checkinGhostTextarea(t, page, "status"); typed != "" || attr(tag, "placeholder") != "On track." {
		t.Errorf("status = %q with %s, want empty with the last Check-in's as ghost text", typed, tag)
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
		_, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), hx)
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
			if !strings.Contains(page, `data-testid="nav-home"`) || strings.Contains(page, `data-testid="checkin-form"`) {
				t.Errorf("Cancelled, hx=%v: want the Check-in page without a form; body:\n%s", hx, page)
			}
			if msg := pageElement(t, page, "p", "checkin-error"); !strings.Contains(msg, reason) {
				t.Errorf("Cancelled, hx=%v: error = %q, want %q", hx, msg, reason)
			}
			if !strings.Contains(page, `data-testid="goal-no-health">A Cancelled Goal takes no Check-ins.<`) {
				t.Errorf("Cancelled, hx=%v: page does not say the Goal takes no Check-ins; body:\n%s", hx, page)
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
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("hx=%v: status = %d, want 403", hx, resp.StatusCode)
		}
		assertCheckinUnavailable(t, page, "Only the Goal&#39;s Owner or a Delegate may check in on Ship search.")
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
		form := html.UnescapeString(tagAround(t, getBody(t, samClient, ts.URL+path), `data-testid="no-change-checkin"`))
		for _, want := range []string{"hx-on:htmx:response-error=", "htmx.swap('body', event.detail.xhr.responseText"} {
			if !strings.Contains(form, want) {
				t.Errorf("on %s, the No change form lacks %q; form tag:\n%s", path, want, form)
			}
		}
		if handler := between(t, form, "hx-on:htmx:response-error=", "htmx.swap"); strings.Contains(handler, "status") {
			t.Errorf("on %s, the No change form swaps only some error statuses; form tag:\n%s", path, form)
		}
	}

	resp, page := postNoChange(t, signInClient(t, ts.URL, "kim@example.com"), fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), true)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("forbidden: status = %d, want 403", resp.StatusCode)
	}
	assertCheckinUnavailable(t, page, "Only the Goal&#39;s Owner or a Delegate may check in on Ship search.")

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
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_checkin BEFORE INSERT ON checkins
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, hx := range []bool{false, true} {
		resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), hx)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("hx=%v: status = %d, want 500", hx, resp.StatusCode)
		}
		assertCheckinUnavailable(t, page, "The No change couldn&#39;t be recorded. Try again.")
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("a failed No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// assertCheckinUnavailable checks a No change failure came back inside the
// site's chrome with a sentence saying what happened.
func assertCheckinUnavailable(t *testing.T, page, sentence string) {
	t.Helper()
	if !strings.Contains(page, `data-testid="nav-home"`) {
		t.Errorf("failure is not inside the normal page; body:\n%s", page)
	}
	if !strings.Contains(pageElement(t, page, "p", "checkin-unavailable"), sentence) {
		t.Errorf("failure does not say %q; body:\n%s", sentence, page)
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

// checkinHighlightLegends are the Check-in form's Highlight row legends, in
// order.
func checkinHighlightLegends(body string) []string {
	return elementTexts(body, "legend", "checkin-highlight-legend")
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	rows := highlightRows(t, page)
	if len(rows) != 3 {
		t.Fatalf("form shows %d Highlight rows, want 2 offered and 1 empty", len(rows))
	}
	for i, tc := range []struct {
		draft domain.DraftHighlight
		kind  string
	}{{first, `value="Accomplishment" selected`}, {second, `value="" selected`}} {
		id := strconv.FormatInt(tc.draft.ID, 10)
		row := rows[i]
		hidden := tagAround(t, row, `name="draft_highlight_id"`)
		if attr(hidden, "type") != "hidden" || attr(hidden, "value") != id {
			t.Errorf("row %d's hidden id = %s, want Draft Highlight %s", i+1, hidden, id)
		}
		keep := tagAround(t, row, `name="draft_highlight_keep"`)
		if attr(keep, "type") != "checkbox" || attr(keep, "value") != id || !strings.Contains(keep, " checked") {
			t.Errorf("row %d's Keep box = %s, want ticked with value %s", i+1, keep, id)
		}
		if !strings.Contains(row, `name="draft_highlight_kind_`+id+`"`) || !strings.Contains(row, tc.kind) {
			t.Errorf("row %d lacks its kind select with %s; row:\n%s", i+1, tc.kind, row)
		}
		if !strings.Contains(row, `<textarea name="draft_highlight_note_`+id+`">`+tc.draft.Note+`</textarea>`) {
			t.Errorf("row %d lacks its editable note; row:\n%s", i+1, row)
		}
	}
	if !strings.Contains(rows[2], `<textarea name="highlight_note"></textarea>`) || strings.Contains(rows[2], "draft_highlight") {
		t.Errorf("the third row isn't the usual empty row; row:\n%s", rows[2])
	}
	if got := checkinHighlightLegends(page); !slices.Equal(got, []string{"Highlight 1 · Draft", "Highlight 2 · Draft", "Highlight 3"}) {
		t.Errorf("legends = %q, want every row numbered, the offered ones marked Draft", got)
	}
	if section := pageElement(t, page, "details", "checkin-highlight-section"); !strings.Contains(openTag(section), " open") {
		t.Errorf("the Highlight section is collapsed while it offers Draft Highlights")
	}
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
	rows := highlightRows(t, body)
	if len(rows) != 3 {
		t.Fatalf("form shows %d Highlight rows, want the 3 posted", len(rows))
	}
	if strings.Contains(rows[0], `data-testid="checkin-error"`) {
		t.Errorf("Highlight 1 shows an error; row:\n%s", rows[0])
	}
	if !strings.Contains(rows[1], `data-testid="checkin-error"`) || !strings.Contains(rows[1], "Highlight 2 needs a kind") {
		t.Errorf("Highlight 2 lacks its error naming it; row:\n%s", rows[1])
	}
	if keep := tagAround(t, rows[0], `name="draft_highlight_keep"`); strings.Contains(keep, " checked") {
		t.Errorf("Highlight 1's Keep box came back ticked: %s", keep)
	}
	if keep := tagAround(t, rows[1], `name="draft_highlight_keep"`); !strings.Contains(keep, " checked") {
		t.Errorf("Highlight 2's Keep box came back unticked: %s", keep)
	}
	if !strings.Contains(rows[0], "First, edited.") || !strings.Contains(rows[1], "No kind yet, edited.") {
		t.Errorf("the offered rows lost their notes as typed:\n%s\n%s", rows[0], rows[1])
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
	rows := highlightRows(t, body)
	if len(rows) != 4 {
		t.Fatalf("form shows %d Highlight rows, want 2 offered, 1 typed and 1 added", len(rows))
	}
	if keep := tagAround(t, rows[0], `name="draft_highlight_keep"`); strings.Contains(keep, " checked") {
		t.Errorf("Highlight 1's Keep box came back ticked: %s", keep)
	}
	if !strings.Contains(rows[0], `value="Miss" selected`) || !strings.Contains(rows[0], "First, edited.") {
		t.Errorf("Highlight 1 lost its kind or note as typed; row:\n%s", rows[0])
	}
	if keep := tagAround(t, rows[1], `name="draft_highlight_keep"`); !strings.Contains(keep, " checked") {
		t.Errorf("Highlight 2's Keep box came back unticked: %s", keep)
	}
	if !strings.Contains(rows[1], `value="Accomplishment" selected`) || !strings.Contains(rows[1], "Second, edited.") {
		t.Errorf("Highlight 2 lost its kind or note as typed; row:\n%s", rows[1])
	}
	if !strings.Contains(rows[2], "Typed.") || !strings.Contains(rows[3], `<textarea name="highlight_note"></textarea>`) {
		t.Errorf("the typed row or the added empty row is wrong:\n%s\n%s", rows[2], rows[3])
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
	form := getBody(t, samClient, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, parent.ID))
	form := between(t, page, `id="checkin-form"`, "</form>")
	for name, want := range map[string]string{
		"status":        "Vendor sign-off is slower than expected.",
		"path_to_green": "Escalate to the vendor.",
		"explanation":   "The load tests miss the vendor risk.",
	} {
		tag, typed := checkinGhostTextarea(t, form, name)
		if typed != "" {
			t.Errorf("%s starts as %q, want empty", name, typed)
		}
		if got := html.UnescapeString(attr(tag, "placeholder")); got != want {
			t.Errorf("%s placeholder = %q, want %q", name, got, want)
		}
	}
	reading := tagAround(t, form, fmt.Sprintf(`name="reading_%d"`, rps.ID))
	if got := attr(reading, "value"); got != "" {
		t.Errorf("reading starts as %q, want empty", got)
	}
	if got := attr(reading, "placeholder"); got != "1250.5" {
		t.Errorf("reading placeholder = %q, want 1250.5", got)
	}
	date := tagAround(t, form, `name="path_target_date"`)
	if got := attr(date, "value"); got != "" {
		t.Errorf("Back to Green by starts as %q, want empty", got)
	}
	if got := elementTexts(form, "span", "checkin-path-target-last"); !slices.Equal(got, []string{"Last time: Jun 15"}) {
		t.Errorf("Back to Green by hint = %q, want [Last time: Jun 15]", got)
	}
	hint := tagAround(t, form, `data-testid="checkin-path-target-last"`)
	if described := attr(date, "aria-describedby"); described == "" || described != attr(hint, "id") {
		t.Errorf("Back to Green by is not described by its hint: input %s, hint %s", date, hint)
	}
	if !strings.Contains(form, `type="radio" name="health" value="Yellow" checked`) {
		t.Errorf("Yellow is not picked; form:\n%s", form)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, parent.ID))
	form := between(t, page, `id="checkin-form"`, "</form>")
	if !strings.Contains(form, `name="explanation"`) {
		t.Fatalf("form has no explanation field to check; form:\n%s", form)
	}
	for _, field := range []string{"checkin-readings", "path-to-green-field"} {
		if fieldset := pageElement(t, form, "fieldset", field); strings.Contains(fieldset, "placeholder=") {
			t.Errorf("%s shows ghost text with no previous Check-in:\n%s", field, fieldset)
		}
	}
	for _, name := range []string{"status", "explanation"} {
		if tag, _ := checkinGhostTextarea(t, form, name); strings.Contains(tag, "placeholder=") {
			t.Errorf("%s shows ghost text with no previous Check-in: %s", name, tag)
		}
	}
	if strings.Contains(form, `data-testid="checkin-path-target-last"`) {
		t.Errorf("form shows a Last time hint with no previous Check-in; form:\n%s", form)
	}
	if !strings.Contains(form, `type="radio" name="health" value="Green" checked`) {
		t.Errorf("a first Check-in does not start on Green; form:\n%s", form)
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
	if plainResp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("plain refused post status = %d, want 422", plainResp.StatusCode)
	}
	htmxAgain, _ := postFormHX(t, samClient, checkinsURL, addAnother)
	plainAgain := postForm(t, samClient, checkinsURL, addAnother)
	for how, body := range map[string]string{
		"htmx refused":      htmxBody,
		"plain refused":     readBody(t, plainResp),
		"htmx add another":  htmxAgain,
		"plain add another": readBody(t, plainAgain),
	} {
		form := between(t, body, `id="checkin-form"`, "</form>")
		if refused := strings.Contains(form, `data-testid="checkin-error"`); refused != strings.HasSuffix(how, "refused") {
			t.Fatalf("%s: form shows an error = %v; form:\n%s", how, refused, form)
		}
		tag, status := checkinGhostTextarea(t, form, "status")
		if status != "The vendor went quiet." {
			t.Errorf("%s: status re-rendered as %q, want what was typed", how, status)
		}
		if got := html.UnescapeString(attr(tag, "placeholder")); got != "Vendor sign-off is slower than expected." {
			t.Errorf("%s: status placeholder = %q, want the latest Check-in's", how, got)
		}
		if tag, _ := checkinGhostTextarea(t, form, "path_to_green"); attr(tag, "placeholder") != "Escalate to the vendor." {
			t.Errorf("%s: Path to Green placeholder lost: %s", how, tag)
		}
		reading := tagAround(t, form, fmt.Sprintf(`name="reading_%d"`, rps.ID))
		if attr(reading, "value") != "900" || attr(reading, "placeholder") != "1250.5" {
			t.Errorf("%s: reading = %s, want value 900 and placeholder 1250.5", how, reading)
		}
		if got := elementTexts(form, "span", "checkin-path-target-last"); !slices.Equal(got, []string{"Last time: Jun 15"}) {
			t.Errorf("%s: Back to Green by hint = %q, want [Last time: Jun 15]", how, got)
		}
		if !strings.Contains(form, `type="radio" name="health" value="Red" checked`) {
			t.Errorf("%s: the Health picked was not kept; form:\n%s", how, form)
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
	if msg := fieldError(t, body, `name="status"`); !strings.Contains(msg, "needs a status") {
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
	m, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
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

// checkinGhostTextarea returns the named textarea's opening tag and what it
// holds as typed.
func checkinGhostTextarea(t *testing.T, form, name string) (tag, typed string) {
	t.Helper()
	start := strings.Index(form, `<textarea name="`+name+`"`)
	if start < 0 {
		t.Fatalf("form has no textarea %s", name)
	}
	rest := form[start:]
	open, end := strings.Index(rest, ">"), strings.Index(rest, "</textarea>")
	if end < 0 {
		t.Fatalf("textarea %s is not closed", name)
	}
	return rest[:open+1], html.UnescapeString(rest[open+1 : end])
}

// offTargetIdleGoal arranges an Active Goal whose Idle capacity Metric (down,
// target 20 by Jan 30) last read 40.88, with the clock moved to Feb 2: past the
// Metric's target date, so it is off target, and before the Beta Milestone is
// due.
func offTargetIdleGoal(t *testing.T, h *testsupport.Harness, owner domain.Account) (domain.Goal, domain.Metric) {
	t.Helper()
	ctx := context.Background()
	goal := h.ActiveGoal(owner, "Reclaim idle capacity", "Idle machines cost money.")
	idle, err := h.Service.AddMetric(ctx, domain.AddMetricInput{
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
// target and target date, and a Save as Green anyway button in the form.
func TestCheckinGreenWithOffTargetMetricWarnsBeforeSaving(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, idle := offTargetIdleGoal(t, h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	body, status := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":                           {domain.HealthGreen},
		"status":                           {"Old pool nearly drained."},
		fmt.Sprintf("reading_%d", idle.ID): {"35.5"},
	})

	if status != http.StatusOK {
		t.Errorf("htmx status = %d, want 200 so the form swaps in", status)
	}
	warning := pageElement(t, body, "section", "checkin-off-target")
	if !strings.Contains(warning, "Idle capacity is 35.5 percent; its target was 20 by Jan 30.") {
		t.Errorf("warning does not name the Metric, the reading entered, its target and date: %s", warning)
	}
	confirm := tagAround(t, warning, `name="confirm_off_target"`)
	if !strings.Contains(confirm, `type="submit"`) || attr(confirm, "value") != "1" || !strings.Contains(warning, "Save as Green anyway") {
		t.Errorf("warning offers no Save as Green anyway that submits the form confirmed: %s", warning)
	}
	if !strings.Contains(body, "Old pool nearly drained.</textarea>") {
		t.Errorf("the status typed was lost; body:\n%s", body)
	}
	if !strings.Contains(body, `value="35.5"`) {
		t.Errorf("the reading typed was lost; body:\n%s", body)
	}
	if !strings.Contains(tagAround(t, body, `value="Green"`), "checked") {
		t.Errorf("the Health picked was lost; body:\n%s", body)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("an unconfirmed Green was recorded: history has %d, want 1", len(history))
	}
}

// Save as Green anyway posts the form again confirmed, and the Check-in records
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
// the Check-in page with the Metric's miss as the form's error.
func TestNoChangeRefusedWhileMetricOffTargetLandsOnCheckinForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetIdleGoal(t, h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp, page := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	assertNoChangeRefusalOnForm(t, page, "Idle capacity is 40.88 percent; its target was 20 by Jan 30.")
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("a refused No change recorded a Check-in: history has %d, want 1", len(history))
	}
}

// An overdue Milestone's refusal wins: with a Metric off target too, the form
// shows the overdue error and offers no Save as Green anyway.
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

	if !strings.Contains(body, "is overdue") {
		t.Errorf("the overdue Milestone's refusal isn't shown; body:\n%s", body)
	}
	if strings.Contains(body, "confirm_off_target") {
		t.Errorf("an overdue Milestone's refusal offers Save as Green anyway; body:\n%s", body)
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

	notice := pageElement(t, body, "p", "checkin-off-target-notice")
	if at, health := strings.Index(body, `data-testid="checkin-off-target-notice"`), strings.Index(body, `data-testid="checkin-health"`); at > health {
		t.Errorf("the notice comes after the Health; body:\n%s", body)
	}
	tag := openTag(notice)
	if !hasClass(tag, "alert") || !hasClass(tag, "y") || !hasClass(tag, "checkin-off-target-notice") || strings.Contains(tag, "role=") {
		t.Errorf("notice tag = %s, want classes alert y and checkin-off-target-notice, and no role", tag)
	}
	if !strings.Contains(notice, "A Metric is off target, so a Green needs confirming.") {
		t.Errorf("notice = %s, want it to say a Green needs confirming", notice)
	}
	if link := tagAround(t, notice, `href=`); attr(link, "href") != "#checkin-off-target" || !strings.Contains(notice, "See below</a>") {
		t.Errorf("notice does not link down to the warning: %s", notice)
	}
	warning := pageElement(t, body, "section", "checkin-off-target")
	if attr(openTag(warning)+">", "id") != "checkin-off-target" {
		t.Errorf("warning has no id the notice can link to: %s", openTag(warning))
	}
	if warned := tagAround(t, warning, `name="off_target_warned"`); attr(warned, "type") != "hidden" || attr(warned, "value") != "1" {
		t.Errorf("warning carries no hidden off_target_warned: %s", warned)
	}
}

// The off-target warning and its notice hide, by CSS alone, while another
// Health than Green is picked: changing the Health answers the question.
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
	addAnother := func(health string) string {
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
		return body
	}

	body := addAnother(domain.HealthGreen)
	warning := pageElement(t, body, "section", "checkin-off-target")
	if !strings.Contains(warning, "Idle capacity is 35.5 percent; its target was 20 by Jan 30.") {
		t.Errorf("after Add another, the warning doesn't name Idle capacity at the reading typed: %s", warning)
	}
	if rows := highlightRows(t, body); len(rows) != 2 {
		t.Errorf("form shows %d Highlight rows, want 2 after Add another", len(rows))
	}

	if body := addAnother(domain.HealthYellow); strings.Contains(body, `data-testid="checkin-off-target"`) {
		t.Errorf("Add another with Yellow picked still warns; body:\n%s", body)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("Add another recorded a Check-in: history has %d, want 1", len(history))
	}
}

// Save as Green anyway records Green, even when another Health was picked
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
	if rows := highlightRows(t, body); len(rows) != 1 || !strings.Contains(rows[0], "Highlight 1 needs a kind") {
		t.Fatalf("want the Highlight's error; body:\n%s", body)
	}
	if strings.Contains(body, `data-testid="checkin-off-target"`) {
		t.Errorf("a confirmed Green is warned again; body:\n%s", body)
	}
	if confirmed := tagAround(t, body, `name="off_target_confirmed"`); attr(confirmed, "type") != "hidden" || attr(confirmed, "value") != "1" {
		t.Errorf("the form doesn't carry the confirmation: %s", confirmed)
	}

	form.Del("confirm_off_target")
	form.Set("off_target_confirmed", "1")
	form.Set("highlight_kind", domain.HighlightMiss)
	if body, _ := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), form); strings.Contains(body, "checkin-form") {
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

// askBoxChecked reports whether the Check-in form's "This asks for help" box
// is ticked.
func askBoxChecked(t *testing.T, page string) bool {
	t.Helper()
	tag := tagAround(t, page, `data-testid="checkin-asks-for-help"`)
	if attr(tag, "name") != "asks_for_help" || attr(tag, "type") != "checkbox" {
		t.Fatalf("asks-for-help control isn't the asks_for_help checkbox: %s", tag)
	}
	return strings.Contains(tag, " checked")
}

// askHint is the hint under the box naming whom the ask goes to.
func askHint(t *testing.T, page string) string {
	t.Helper()
	return between(t, page, `data-testid="checkin-ask-addressee"`, "</fieldset>")
}

// While an ask is open the Check-in form starts with its box ticked, so
// carrying the ask forward takes no effort, and names the Manager it goes to.
// With no open ask the box starts unticked.
func TestCheckinFormAsksForHelpTickedWhileOpen(t *testing.T) {
	t.Parallel()

	f := newAskFixture(t)
	formURL := fmt.Sprintf("%s/goals/%d/checkin", f.ts, f.goal.ID)

	page := getBody(t, f.client, formURL)
	if askBoxChecked(t, page) {
		t.Errorf("box is ticked with no open ask")
	}
	if hint := askHint(t, page); !strings.Contains(hint, "Goes to "+shownAs(f.mia.Email, "Mia Chen")+".") {
		t.Errorf("hint doesn't name Mia Chen as whom the ask goes to: %s", hint)
	}

	f.h.AskForHelp(f.sam, f.goal.ID, domain.HealthYellow, "Short a reviewer.", "Borrow one.", pathDate)
	page = getBody(t, f.client, formURL)
	if !askBoxChecked(t, page) {
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

	hint := askHint(t, getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID)))
	if !strings.Contains(hint, "No one is above the Owner to ask.") {
		t.Errorf("hint = %s, want it to say no one is above the Owner", hint)
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
	if !askBoxChecked(t, body) {
		t.Errorf("re-rendered form lost the ticked box")
	}

	resp := postForm(t, f.client, submitURL, url.Values{
		"health": {domain.HealthYellow},
		"status": {"Still short."},
	})
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("refused plain submit = %d, want 422", resp.StatusCode)
	}
	if askBoxChecked(t, body) {
		t.Errorf("re-rendered form ticked a box posted unticked")
	}
	if hint := askHint(t, body); !strings.Contains(hint, shownAs(f.mia.Email, "Mia Chen")) {
		t.Errorf("422 re-render's hint doesn't name Mia Chen: %s", hint)
	}
}

// The Latest status card shows an open ask under the Path to Green, naming
// whom it asks, with a link to answer it; a Green Check-in closes it.
func TestLatestStatusShowsAnOpenAsk(t *testing.T) {
	t.Parallel()

	f := newAskFixture(t)
	f.h.AskForHelp(f.sam, f.goal.ID, domain.HealthYellow, "Short a reviewer.", "Borrow one.", pathDate)
	goalURL := fmt.Sprintf("%s/goals/%d", f.ts, f.goal.ID)

	card := pageElement(t, getBody(t, f.client, goalURL), "section", "goal-checkins")
	_, afterPath, _ := strings.Cut(card, `data-testid="goal-path-to-green"`)
	ask := between(t, afterPath, `<p data-testid="goal-ask-for-help">`, "</p>")
	if !strings.Contains(ask, "<strong>Asks for help</strong> of "+shownAs(f.mia.Email, "Mia Chen")) {
		t.Errorf("ask doesn't read \"Asks for help of Mia Chen\": %s", ask)
	}
	if !strings.Contains(ask, `href="#comments"`) || !strings.Contains(ask, ">Answer</a>") {
		t.Errorf("ask has no Answer link to the Comments: %s", ask)
	}
	if !strings.Contains(ask, shownAs(f.mia.Email, "Mia Chen")+` <a href="#comments">Answer</a>`) {
		t.Errorf("ask runs the Addressee into Answer with no space between: %s", ask)
	}

	f.h.Checkin(f.sam, f.goal.ID, domain.HealthGreen, "Fixed.", "", time.Time{})
	if page := getBody(t, f.client, goalURL); strings.Contains(page, "goal-ask-for-help") {
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

	got := elementTexts(getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "p", "goal-ask-for-help")
	if len(got) != 1 || !strings.HasPrefix(got[0], "Asks for help · no one above the Owner") {
		t.Errorf("ask = %q, want it to read \"Asks for help · no one above the Owner\"", got)
	}
}

// pathReasonLabel is the open tag of the Check-in form's "Reason for moving it
// later" label.
func pathReasonLabel(t *testing.T, form string) string {
	t.Helper()
	return tagAround(t, form, `data-testid="checkin-path-target-reason"`)
}

// isHidden reports whether an open tag carries the hidden attribute.
func isHidden(tag string) bool {
	return regexp.MustCompile(`\shidden(\s|>|=|/)`).MatchString(tag)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	form := between(t, page, `id="checkin-form"`, "</form>")
	label := pathReasonLabel(t, form)
	if !isHidden(label) {
		t.Errorf("the reason field shows before the date moves: %s", label)
	}
	if !strings.Contains(between(t, form, `data-testid="checkin-path-target-reason"`, "</label>"), "Reason for moving it later") {
		t.Errorf("the reason field isn't labelled \"Reason for moving it later\"; form:\n%s", form)
	}
	if !strings.Contains(form, `name="path_target_date_reason"`) {
		t.Errorf("the form has no path_target_date_reason; form:\n%s", form)
	}
	date := tagAround(t, form, `name="path_target_date"`)
	if got := attr(date, "data-last"); got != "2026-06-15" {
		t.Errorf("Back to Green by data-last = %q, want 2026-06-15: %s", got, date)
	}
	if !strings.Contains(date, "hx-on:input=") {
		t.Errorf("Back to Green by has no input handler to show the reason: %s", date)
	}

	// A Goal with no back-to-Green date yet has nothing to slip from.
	fresh := h.ActiveGoal(sam, "Cut churn", "Keep customers.")
	page = getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, fresh.ID))
	form = between(t, page, `id="checkin-form"`, "</form>")
	if !isHidden(pathReasonLabel(t, form)) {
		t.Errorf("the reason field shows on a Goal with no back-to-Green date")
	}
	if date := tagAround(t, form, `name="path_target_date"`); strings.Contains(date, "data-last=") {
		t.Errorf("Back to Green by carries a data-last with no previous date: %s", date)
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
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	body := between(t, readBody(t, resp), `id="checkin-form"`, "</form>")
	const want = "Moving the back-to-Green date later needs a reason."
	if msg := fieldError(t, body, `name="path_target_date_reason"`); !strings.Contains(msg, `">`+want) {
		t.Errorf("reason field's error = %q, want it to read %q", msg, want)
	}
	if label := pathReasonLabel(t, body); isHidden(label) {
		t.Errorf("the refused form hides the reason field: %s", label)
	}
	if fieldset := tagAround(t, body, `data-testid="path-to-green-field"`); !hasClass(fieldset, "has-error") {
		t.Errorf("the Path to Green isn't marked as holding the error: %s", fieldset)
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
func backToGreenCell(t *testing.T, client *http.Client, base string, goal domain.Goal) string {
	t.Helper()
	cells := pageElement(t, getBody(t, client, goalPageURL(base, goal)), "dl", "goal-status-cells")
	return between(t, cells, `data-testid="goal-back-to-green-cell"`, "</dd>")
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

	twice := h.ActiveGoal(sam, "Slipped twice", "It matters.")
	h.Checkin(sam, twice.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(15))
	h.CheckinMovingPath(sam, twice.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(22), "The vendor is out.")
	h.CheckinMovingPath(sam, twice.ID, domain.HealthYellow, "Better.", "Escalate.", jun(29), "The vendor is still out.")
	cell := backToGreenCell(t, client, ts.URL, twice)
	if want := "<dd><span>Jun 29</span><del>Jun 22</del><span data-testid=\"back-to-green-slips\">(2)</span>"; !strings.Contains(strings.Join(strings.Fields(cell), ""), strings.Join(strings.Fields(want), "")) {
		t.Errorf("the cell doesn't read Jun 29 ~~Jun 22~~ (2): %s", cell)
	}

	once := h.ActiveGoal(sam, "Slipped once", "It matters.")
	h.Checkin(sam, once.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(15))
	h.CheckinMovingPath(sam, once.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(22), "The vendor is out.")
	cell = backToGreenCell(t, client, ts.URL, once)
	if !strings.Contains(cell, "<span>Jun 22</span>") || !strings.Contains(cell, "<del>Jun 15</del>") {
		t.Errorf("the cell doesn't read Jun 22 ~~Jun 15~~: %s", cell)
	}
	if strings.Contains(cell, "back-to-green-slips") {
		t.Errorf("a date that slipped once shows a count: %s", cell)
	}

	// A Green Check-in ends the run, and its slips with it.
	recovered := h.ActiveGoal(sam, "Back and off again", "It matters.")
	h.Checkin(sam, recovered.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(15))
	h.CheckinMovingPath(sam, recovered.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(22), "The vendor is out.")
	h.Clock.Advance(time.Hour)
	h.Checkin(sam, recovered.ID, domain.HealthGreen, "Unblocked.", "", time.Time{})
	h.Clock.Advance(time.Hour)
	h.Checkin(sam, recovered.ID, domain.HealthYellow, "Wobbling.", "Watch it.", jun(29))
	cell = backToGreenCell(t, client, ts.URL, recovered)
	if !strings.Contains(cell, "<span>Jun 29</span>") || strings.Contains(cell, "<del>") {
		t.Errorf("a new Run off Green strikes the last run's date: %s", cell)
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

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	c := between(t, page, `data-testid="no-change-card"`, `data-testid="checkin-form"`)
	if strings.Contains(c, `data-testid="no-change-checkin"`) {
		t.Errorf("the card offers a No change that would be refused:\n%s", c)
	}
	const want = "The back-to-Green date has passed: submit a Check-in to move it later, with a reason, or come back to Green."
	if got := elementTexts(c, "p", "no-change-unavailable"); !slices.Equal(got, []string{want}) {
		t.Errorf("the card's reason = %q, want %q", got, want)
	}

	_, page = postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), false)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, f.parent.ID))
	aside := pageElement(t, page, "aside", "checkin-children")
	if !strings.Contains(aside, "<h3>Contributed to by</h3>") {
		t.Errorf("children list is not headed Contributed to by; aside:\n%s", aside)
	}
	for _, child := range []struct {
		goal  domain.Goal
		badge string
	}{
		{f.yellow, `<span class="badge y"><span class="dot"></span>Yellow</span>`},
		{f.green, `<span class="badge g"><span class="dot"></span>Green</span>`},
	} {
		link := fmt.Sprintf(`<a href="/goals/%d">%s</a>`, child.goal.ID, child.goal.Title)
		at := strings.Index(aside, link)
		if at < 0 {
			t.Errorf("children list has no link %s; aside:\n%s", link, aside)
			continue
		}
		item, _, _ := strings.Cut(aside[at:], "</li>")
		if !strings.Contains(item, child.badge) {
			t.Errorf("%s shows no %s; item:\n%s", child.goal.Title, child.badge, item)
		}
	}
	if strings.Contains(aside, "/links/") {
		t.Errorf("children list offers a Remove form; aside:\n%s", aside)
	}
}

// A Goal with no children keeps the Check-in page as it was: no children list,
// and the page stays one column.
func TestCheckinPageWithoutChildrenHasNoChildrenList(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship it", "It matters.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, g.ID))
	if strings.Contains(page, `data-testid="checkin-children"`) {
		t.Errorf("a Goal with no children shows a children list; body:\n%s", page)
	}
	if !strings.Contains(page, `<div class="checkin-page">`) {
		t.Errorf("a Goal with no children doesn't keep the one-column Check-in page; body:\n%s", page)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, f.parent.ID))
	line := pageElement(t, page, "p", "checkin-form-rollup")
	if got, want := checkinPageText(line), "Rolled-up Health: Yellow 1 of 2 Yellow"; got != want {
		t.Errorf("rollup line = %q, want %q", got, want)
	}
	if !strings.Contains(line, `<span class="muted">1 of 2 Yellow</span>`) {
		t.Errorf("rollup line doesn't show the count as the badge's muted count line; line:\n%s", line)
	}
	if strings.Contains(page, "your Active children") {
		t.Errorf("rollup line still reads as the Owner's children; body:\n%s", page)
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
	fieldError(t, body, `name="status"`)
	if !strings.Contains(body, `data-testid="checkin-form"`) {
		t.Errorf("refused submit doesn't re-render the form; body:\n%s", body)
	}
	if strings.Contains(body, `data-testid="checkin-children"`) {
		t.Errorf("refused submit re-renders the children list along with the form; body:\n%s", body)
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

// checkinPageText is a page fragment's text, its markup dropped and its
// whitespace collapsed.
func checkinPageText(fragment string) string {
	return strings.Join(strings.Fields(html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(fragment, " "))), " ")
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
	if _, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
		GoalID: goal.ID, Name: "Signups", Unit: "per week", Direction: domain.MetricUp,
		Baseline: 100, Target: 500, TargetDate: h.Clock.Now().AddDate(0, 6, 0),
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	redChild := h.ActiveChildOf(sam, goal, "Red work", "Red so what.")
	// The Goal turns Yellow before its child turns Red, so its Check-in
	// needs no explanation.
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Slipping.", "Add a second on-call.", pathDate)
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	at := 0
	for _, want := range []string{
		"<h1>Check in</h1>",
		"<h2>How is it going?</h2>",
		`<textarea name="status"`,
		`data-testid="checkin-form-rollup"`,
		"<h2>Path to Green</h2>",
		"<h2>Metrics</h2>",
	} {
		i := strings.Index(page[at:], want)
		if i < 0 {
			t.Fatalf("page has no %s after what came before it; body:\n%s", want, page)
		}
		at += i + len(want)
	}
	for _, gone := range []string{"Metric readings", "Path to Green (required"} {
		if strings.Contains(page, gone) {
			t.Errorf("page still reads %q", gone)
		}
	}
	if numbered := regexp.MustCompile(`<h2[^>]*>\s*\d`).FindString(page); numbered != "" {
		t.Errorf("a heading is numbered: %s", numbered)
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
		return checkinPageText(pageElement(t, getBody(t, client, checkinURL), "p", "checkin-page-meta"))
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
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red so what.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/checkin", ts.URL, parent.ID))
	if !strings.Contains(page, `type="radio" name="health" value="Green" checked`) {
		t.Fatalf("the form doesn't start on Green; body:\n%s", page)
	}
	line := pageElement(t, page, "p", "checkin-form-rollup")
	if got, want := checkinPageText(line), "Rolled-up Health: Red 1 of 1 Red"; got != want {
		t.Errorf("rollup line = %q, want %q", got, want)
	}
	if !strings.Contains(line, `<span class="badge r"><span class="dot"></span>Red</span>`) {
		t.Errorf("rollup line doesn't show Red as a badge; line:\n%s", line)
	}
	explanation := between(t, page, `data-testid="checkin-form-rollup"`, `name="explanation"`)
	label := checkinPageText(explanation[strings.LastIndex(explanation, "<label"):])
	if want := "Why is your Health different? Required while it differs from the Rolled-up Health"; !strings.Contains(label, want) {
		t.Errorf("explanation label = %q, want it to read %q", label, want)
	}
}
