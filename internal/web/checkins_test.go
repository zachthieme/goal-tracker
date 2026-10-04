package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"slices"
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
	oldDate := goal.DeliveryDate.Format("2006-01-02")
	newDate := goal.DeliveryDate.AddDate(0, 0, 14).Format("2006-01-02")

	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":               {domain.HealthYellow},
		"status":               {"Vendor is late."},
		"path_to_green":        {"Swap vendors."},
		"path_target_date":     {"2026-06-15"},
		"delivery_date":        {newDate},
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
		{"goal-back-to-green-cell", "Back to Green by", "2026-06-15"},
	} {
		cell := between(t, cells, `data-testid="`+tc.testID+`"`, "")
		cell, _, _ = strings.Cut(cell, "<div ")
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
	oldBeta := beta.TargetDate.Format("2006-01-02")
	newBeta := beta.TargetDate.AddDate(0, 0, 10).Format("2006-01-02")
	resp = postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Beta moves; launch holds."},
		fmt.Sprintf("milestone_date_%d", beta.ID):           {newBeta},
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
	if msg := pageElement(t, body, "p", "checkin-error"); !strings.Contains(msg, "cancelling a Goal needs a reason") {
		t.Errorf("error = %q, want it to say cancelling a Goal needs a reason", msg)
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

// The Check-in form pre-fills each Metric's reading with its latest value, like
// the rest of the form, so a "same as last week" Check-in stays one click; a
// Metric with no readings yet stays blank (ticket #29).
func TestCheckinFormPrefillsLatestReadings(t *testing.T) {
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
	if want := fmt.Sprintf(`name="reading_%d" value="135.5"`, signups.ID); !strings.Contains(form, want) {
		t.Errorf("Signups reading not pre-filled with its latest value (want %s); form:\n%s", want, form)
	}
	if want := fmt.Sprintf(`name="reading_%d" value=""`, referrals.ID); !strings.Contains(form, want) {
		t.Errorf("Referrals has no readings, so its input should be blank (want %s); form:\n%s", want, form)
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
	msg := fieldError(t, body, `name="explanation"`)
	if !strings.Contains(msg, "differs from the Rolled-up Health") {
		t.Errorf("explanation field's error = %q, want the Rolled-up Health one", msg)
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
	if msg := fieldError(t, body, `name="path_target_date"`); !strings.Contains(msg, "needs a Path to Green") {
		t.Errorf("Path to Green field's error = %q, want the Path to Green one", msg)
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

// onlyMilestone returns the one Milestone an ActiveGoal starts with.
func onlyMilestone(t *testing.T, h *testsupport.Harness, goal domain.Goal) domain.Milestone {
	t.Helper()
	ms, err := h.Service.ListMilestones(context.Background(), goal.ID)
	if err != nil || len(ms) != 1 {
		t.Fatalf("ListMilestones = %v, %v", ms, err)
	}
	return ms[0]
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
	if msg := fieldError(t, body, marker); !strings.Contains(msg, "removing Milestone") {
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
	if msg := fieldError(t, body, marker); !strings.Contains(msg, "is overdue") {
		t.Errorf("Milestone date field's error = %q, want the overdue one", msg)
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
	if msg := fieldError(t, body, `data-testid="checkin-form"`); !strings.Contains(msg, "only an Active Goal") {
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

// Health is picked from a radio group, not a select, prefilled from the latest
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
		"Records the same Health and status with today's date.",
		`data-testid="no-change-checkin"`,
	} {
		if !strings.Contains(page[card:], want) {
			t.Errorf("no-change card missing %q; body:\n%s", want, page)
		}
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
	goal := h.ActiveGoal(owner, "Blue-green deploys for the monolith", "Deploys take the site down.")
	h.Checkin(owner, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(100 * day) // past the Beta Milestone's date
	return goal
}

// The Goal page's No change posts with htmx; a refusal comes back as the whole
// Check-in page with 200, so htmx swaps it in, and the Check-in page's URL is
// pushed so the address bar matches what the person sees (#103).
func TestNoChangeRefusedOverHtmxSwapsInCheckinPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := overdueGreenGoal(h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	goalPage := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	button := tagAround(t, goalPage, `data-testid="no-change-checkin"`)
	action := fmt.Sprintf("/goals/%d/checkins/no-change", goal.ID)
	if !strings.Contains(button, `hx-post="`+action+`"`) {
		t.Fatalf("the Goal page's No change does not post with htmx to %s: %s", action, button)
	}

	resp, page := postNoChange(t, client, ts.URL+action, true)

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
		assertNoChangeRefusalOnForm(t, page, "there is no previous Check-in to repeat")
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
	const reason = "only an Active Goal can be checked in on"

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
