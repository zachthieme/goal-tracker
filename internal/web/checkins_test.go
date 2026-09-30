package web_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Yellow Check-in submitted without a Path to Green comes back with the
// validation error rendered in the form, next to the Path to Green field, so the
// Owner can fix it in place (htmx). This is the validation-error smoke test.
func TestSmokeCheckinValidationErrorReachesForm(t *testing.T) {
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

// postFormHX posts a form with the HX-Request header set, as htmx does, and
// returns the body and status without following redirects or asserting 200.
func postFormHX(t *testing.T, client *http.Client, rawURL string, form url.Values) (string, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b), resp.StatusCode
}

// An Owner moves the delivery date later in a Check-in through the web form,
// with a reason and a Yellow Health. The Goal page then shows the date history
// struck through (~~old~~ new), the reason, and the slip count. This is the Date
// Slip smoke test (ticket #5).
func TestSmokeCheckinDeliveryDateSlipShownStruckThrough(t *testing.T) {
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
	if delivery := pageElement(t, page, "span", "goal-delivery-date"); !strings.Contains(delivery, fmt.Sprintf("delivers <del>%s</del> %s", oldDate, newDate)) {
		t.Errorf("delivery date not shown struck through; element:\n%s", delivery)
	}
	if slips := pageElement(t, page, "section", "goal-date-slips"); !strings.Contains(slips, "Vendor API delayed two weeks.") {
		t.Errorf("Date Slip history missing the reason; section:\n%s", slips)
	}
	if !strings.Contains(page, `data-testid="goal-slip-count">1<`) {
		t.Errorf("Goal page missing the slip count")
	}
}

// A Green Check-in that moves the delivery date later comes back as the form
// with the error next to the delivery date, and nothing is recorded.
func TestSmokeGreenWithLaterDeliveryDateRejectedInForm(t *testing.T) {
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
	for _, want := range []string{fmt.Sprintf("<del>%s</del> %s", oldBeta, newBeta), "Docs folded into GA."} {
		if !strings.Contains(milestones, want) {
			t.Errorf("Milestone list missing %q; section:\n%s", want, milestones)
		}
	}
	if slips := pageElement(t, page, "section", "goal-date-slips"); !strings.Contains(slips, "Design review moved.") {
		t.Errorf("Date Slip history missing the reason; section:\n%s", slips)
	}
	for _, want := range []string{`data-testid="goal-slip-count">1<`, `data-testid="goal-milestone-churn">2<`} {
		if !strings.Contains(page, want) {
			t.Errorf("Goal page missing %q", want)
		}
	}
}

// pageElement returns the tag element carrying data-testid, up to its first
// closing tag, so an assertion can't be satisfied by the same text elsewhere on
// the page. It suits elements that don't nest their own tag.
func pageElement(t *testing.T, page, tag, testID string) string {
	t.Helper()
	start := strings.Index(page, "<"+tag+` data-testid="`+testID+`"`)
	if start < 0 {
		t.Fatalf("page has no <%s> %q", tag, testID)
	}
	end := strings.Index(page[start:], "</"+tag+">")
	if end < 0 {
		t.Fatalf("<%s> %q is not closed", tag, testID)
	}
	return page[start : start+end]
}

// Through the Check-in form an Owner puts a Goal On Hold with a reason, then
// resumes it. The Goal page shows the Lifecycle and why, the Check-in history
// shows each change, and while On Hold the form offers only to resume or Cancel.
func TestSmokeCheckinPutsGoalOnHoldAndResumes(t *testing.T) {
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
		t.Errorf("Check-in history missing the Lifecycle change; element:\n%s", entry)
	}
	form := pageElement(t, page, "fieldset", "checkin-lifecycle-fields")
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
	if !strings.Contains(body, `data-testid="checkin-error"`) || !strings.Contains(body, "final value") {
		t.Errorf("response missing the inline final-value error; body:\n%s", body)
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
// not its database id, and the typed text stays in the input (ticket #29).
func TestCheckinNonNumericReadingNamesTheMetric(t *testing.T) {
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
	msg := pageElement(t, body, "p", "checkin-error")
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

	form := pageElement(t, getBody(t, samClient, goalURL), "fieldset", "checkin-readings")
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
	if msg := fieldError(t, body, `name="lifecycle"`); !strings.Contains(msg, "Active Goal to") {
		t.Errorf("Lifecycle field's error = %q, want the Lifecycle change one", msg)
	}
}

// A Check-in that moves the delivery date without a reason gets its error next
// to the delivery date's reason field.
func TestCheckinDeliveryDateReasonErrorShownNextToReasonField(t *testing.T) {
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
	docs := body[strings.Index(body, `value="Docs"`):]
	if msg := fieldError(t, docs, `name="new_milestone_date"`); !strings.Contains(msg, "needs a date") {
		t.Errorf("new Milestone's error = %q, want the missing date one", msg)
	}
}

// A Green Check-in adding a Milestone that is already overdue gets its error
// next to that new Milestone.
func TestCheckinOverdueNewMilestoneErrorShownNextToNewMilestone(t *testing.T) {
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
