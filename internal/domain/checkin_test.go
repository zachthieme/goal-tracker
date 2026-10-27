package domain_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Owner submits a Green Check-in on an Active Goal: it records the Health, the
// status, its author and the Owner it was written for, and becomes the Goal's
// latest Check-in (CONTEXT.md: Check-in).
func TestSubmitCheckinRecordsHealthAuthorAndOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "On track for the beta.",
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if c.Health != domain.HealthGreen {
		t.Errorf("Health = %q, want %q", c.Health, domain.HealthGreen)
	}
	if c.Status != "On track for the beta." {
		t.Errorf("Status = %q, want the submitted status", c.Status)
	}
	if c.Author.ID != sam.ID {
		t.Errorf("Author = %d, want %d", c.Author.ID, sam.ID)
	}
	if c.Owner.ID != sam.ID {
		t.Errorf("Owner = %d, want %d", c.Owner.ID, sam.ID)
	}

	latest, ok, err := h.Service.LatestCheckin(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("LatestCheckin: %v", err)
	}
	if !ok {
		t.Fatalf("LatestCheckin ok = false, want the Check-in just submitted")
	}
	if latest.ID != c.ID {
		t.Errorf("LatestCheckin ID = %d, want %d", latest.ID, c.ID)
	}
}

// An author ID that names no Account is neither the Owner nor a Delegate, so
// may not submit a Check-in, full or No change, and nothing is recorded.
func TestSubmitCheckinRefusesAnAuthorWithNoAccount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", testsupport.Epoch)
	nobody := sam.ID + 1000

	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: nobody,
		Health:   domain.HealthGreen,
		Status:   "Written by nobody.",
	}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("SubmitCheckin by an author with no Account: err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.SubmitNoChangeCheckin(ctx, goal.ID, nobody); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("SubmitNoChangeCheckin by an author with no Account: err = %v, want ErrNotAuthorized", err)
	}
	history, err := h.Service.ListCheckins(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("history = %+v, want only Sam's Check-in", history)
	}
}

var pathDate = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

// A Proposed Goal has no Health and can't be checked in on (CONTEXT.md: Health
// is how an Active Goal is tracking).
func TestSubmitCheckinRejectedOnProposedGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.") // stays Proposed

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Looks fine.",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if _, ok, _ := h.Service.LatestCheckin(context.Background(), goal.ID); ok {
		t.Errorf("a Check-in was recorded on a Proposed Goal")
	}
}

// A Yellow or Red Check-in requires a Path to Green with both text and a target
// date (CONTEXT.md: Path to Green).
func TestSubmitCheckinYellowOrRedRequiresPathToGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	for _, health := range []string{domain.HealthYellow, domain.HealthRed} {
		// Missing the Path text.
		_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
			GoalID:         goal.ID,
			AuthorID:       sam.ID,
			Health:         health,
			Status:         "Slipping.",
			PathTargetDate: pathDate,
		})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s without a Path text: err = %v, want ErrValidation", health, err)
		}
		// Missing the target date.
		_, err = h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
			GoalID:      goal.ID,
			AuthorID:    sam.ID,
			Health:      health,
			Status:      "Slipping.",
			PathToGreen: "Add two engineers.",
		})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s without a target date: err = %v, want ErrValidation", health, err)
		}
	}

	// With both, it is accepted and keeps the Path.
	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthRed,
		Status:         "Slipping.",
		PathToGreen:    "Add two engineers.",
		PathTargetDate: pathDate,
	})
	if err != nil {
		t.Fatalf("SubmitCheckin with a Path to Green: %v", err)
	}
	if c.PathToGreen != "Add two engineers." || !c.PathTargetDate.Equal(pathDate) {
		t.Errorf("Path to Green not recorded: %q / %v", c.PathToGreen, c.PathTargetDate)
	}
}

// A Green Check-in needs no Path to Green, and any Path text passed with it is
// dropped since Green carries none.
func TestSubmitCheckinGreenDropsPathToGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthGreen,
		Status:         "All good.",
		PathToGreen:    "leftover text",
		PathTargetDate: pathDate,
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if c.PathToGreen != "" || !c.PathTargetDate.IsZero() {
		t.Errorf("Green Check-in kept a Path to Green: %q / %v", c.PathToGreen, c.PathTargetDate)
	}
}

// A Check-in needs a status.
func TestSubmitCheckinRequiresStatus(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "   ",
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// Only the Goal's Owner may submit a Check-in.
func TestSubmitCheckinOnlyByOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: pat.ID,
		Health:   domain.HealthGreen,
		Status:   "Meddling.",
	}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Fatalf("err = %v, want ErrNotAuthorized", err)
	}
}

// Check-ins are immutable and kept as history: the latest carries the Goal's
// current Health, status, and Path to Green, and ListCheckins returns the whole
// history newest first (CONTEXT.md: current values come from the latest
// Check-in).
func TestCheckinsAreImmutableHistoryWithLatestCurrent(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	first := h.Checkin(sam, goal.ID, domain.HealthGreen, "Kicking off.", "", time.Time{})
	h.Clock.Advance(24 * time.Hour)
	second := h.Checkin(sam, goal.ID, domain.HealthRed, "Blocked on vendor.", "Escalate to vendor.", pathDate)

	history, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2 (Check-ins are immutable, never replaced)", len(history))
	}
	if history[0].ID != second.ID || history[1].ID != first.ID {
		t.Errorf("history order = [%d, %d], want newest first [%d, %d]", history[0].ID, history[1].ID, second.ID, first.ID)
	}

	latest, ok, err := h.Service.LatestCheckin(context.Background(), goal.ID)
	if err != nil || !ok {
		t.Fatalf("LatestCheckin: %v, ok=%v", err, ok)
	}
	if latest.Health != domain.HealthRed || latest.Status != "Blocked on vendor." || latest.PathToGreen != "Escalate to vendor." {
		t.Errorf("current values not from the latest Check-in: %+v", latest)
	}
}

// A Check-in records the current value of each Metric on the Goal, and a
// Metric's readings come back over time earliest-first for its trend against
// target (CONTEXT.md: Metric - its current value is recorded at each Check-in).
func TestSubmitCheckinRecordsMetricReadings(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")

	latency, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, validMetric(goal.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Started measuring.",
		Readings: []domain.MetricReadingInput{{MetricID: latency.ID, Value: 900}},
	}); err != nil {
		t.Fatalf("SubmitCheckin with reading: %v", err)
	}
	h.Clock.Advance(7 * 24 * time.Hour)
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Improving.",
		Readings: []domain.MetricReadingInput{{MetricID: latency.ID, Value: 700}},
	}); err != nil {
		t.Fatalf("second SubmitCheckin with reading: %v", err)
	}

	readings, err := h.Service.ListMetricReadings(context.Background(), latency.ID)
	if err != nil {
		t.Fatalf("ListMetricReadings: %v", err)
	}
	if len(readings) != 2 {
		t.Fatalf("readings = %d, want 2 recorded over two Check-ins", len(readings))
	}
	if readings[0].Value != 900 || readings[1].Value != 700 {
		t.Errorf("trend = [%v, %v], want earliest-first [900, 700]", readings[0].Value, readings[1].Value)
	}
}

// A reading for a Metric that is not on the Goal being checked in on is rejected,
// and the whole Check-in is refused (nothing is recorded).
func TestSubmitCheckinRejectsReadingForForeignMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")
	other := h.ActiveGoal(sam, "Grow signups", "More signups, more revenue.")
	foreign, err := h.Service.AddMetric(context.Background(), other.Owner.ID, validMetric(other.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Mislabelled reading.",
		Readings: []domain.MetricReadingInput{{MetricID: foreign.ID, Value: 1}},
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded despite the foreign-metric reading: %d", len(history))
	}
}

// A Check-in records one value per Metric, so one that names a Metric twice is
// refused by name before anything is recorded.
func TestSubmitCheckinRefusesAMetricReadTwice(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")
	latency, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, validMetric(goal.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}

	_, err = h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Read it twice.",
		Readings: []domain.MetricReadingInput{
			{MetricID: latency.ID, Value: 900},
			{MetricID: latency.ID, Value: 800},
		},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if reason := domain.Reason(err); !strings.Contains(reason, "p95 checkout latency") {
		t.Errorf("reason = %q, want it to name the Metric read twice", reason)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded despite the Metric read twice: %d", len(history))
	}
}

// Each reading SubmitCheckin refuses says why in its own words: a Metric that
// doesn't exist, one on another Goal, and one read twice (#591).
func TestSubmitCheckinReadingRefusalsSayWhy(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")
	other := h.ActiveGoal(sam, "Grow signups", "More signups, more revenue.")
	latency, err := h.Service.AddMetric(ctx, sam.ID, validMetric(goal.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	foreign, err := h.Service.AddMetric(ctx, sam.ID, validMetric(other.ID))
	if err != nil {
		t.Fatalf("AddMetric on the other Goal: %v", err)
	}
	const missing = int64(9999)

	for _, tc := range []struct {
		name     string
		readings []domain.MetricReadingInput
		want     string
	}{
		{"no such Metric", []domain.MetricReadingInput{{MetricID: latency.ID, Value: 900}, {MetricID: missing, Value: 1}}, "metric 9999 does not exist"},
		{"a Metric on another Goal", []domain.MetricReadingInput{{MetricID: latency.ID, Value: 900}, {MetricID: foreign.ID, Value: 1}}, fmt.Sprintf("metric %d is not on this Goal", foreign.ID)},
		{"a Metric read twice", []domain.MetricReadingInput{{MetricID: latency.ID, Value: 900}, {MetricID: latency.ID, Value: 800}}, `a Check-in records one value per Metric; "p95 checkout latency" has more than one`},
	} {
		_, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
			GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Read.", Readings: tc.readings,
		})
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("%s: err = %v, want ErrValidation", tc.name, err)
		}
		if got := domain.Reason(err); got != tc.want {
			t.Errorf("%s: reason = %q, want %q", tc.name, got, tc.want)
		}
	}
	if history, _ := h.Service.ListCheckins(ctx, goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded despite a refused reading: %d", len(history))
	}
}

// One-click "no change" records a Check-in that repeats the previous values,
// author and Owner it is written for aside.
func TestNoChangeCheckinRepeatsPreviousValues(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	prev := h.Checkin(sam, goal.ID, domain.HealthYellow, "Recovering.", "Ship the hotfix.", pathDate)
	h.Clock.Advance(24 * time.Hour)

	repeat, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID)
	if err != nil {
		t.Fatalf("SubmitNoChangeCheckin: %v", err)
	}
	if repeat.ID == prev.ID {
		t.Errorf("no-change reused the previous Check-in instead of recording a new one")
	}
	if repeat.Health != prev.Health || repeat.Status != prev.Status ||
		repeat.PathToGreen != prev.PathToGreen || !repeat.PathTargetDate.Equal(prev.PathTargetDate) {
		t.Errorf("no-change did not repeat previous values: got %+v, prev %+v", repeat, prev)
	}
}

// "No change" needs a previous Check-in to repeat; on a Goal with none it is
// rejected.
func TestNoChangeCheckinRejectedWithoutPrevious(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// A slip is never hidden behind a Green: a Check-in that moves the delivery date
// later can't be Green, while one that pulls it in can (CONTEXT.md: Date Slip).
func TestGreenRejectedWhenDeliveryDateMovesLater(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthGreen,
		Status:             "Still fine.",
		DeliveryDate:       goal.DeliveryDate.AddDate(0, 0, 7),
		DeliveryDateReason: "Vendor slipped.",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Green with a later delivery date: err = %v, want ErrValidation", err)
	}
	if slips, _ := h.Service.ListDateSlips(context.Background(), goal.ID); len(slips) != 0 {
		t.Errorf("recorded %d Date Slips despite the rejection", len(slips))
	}

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthGreen,
		Status:             "Ahead of plan.",
		DeliveryDate:       goal.DeliveryDate.AddDate(0, 0, -7),
		DeliveryDateReason: "Beta went faster than planned.",
	}); err != nil {
		t.Errorf("Green with an earlier delivery date: %v", err)
	}
}

// Green is rejected while any Milestone is overdue — Planned and past its date —
// judged by the Service's clock. On the Milestone's own date it is not yet
// overdue.
func TestGreenRejectedWhileMilestoneOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)

	green := domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "On track.",
	}

	h.Clock.Set(beta.TargetDate.Add(23 * time.Hour))
	if _, err := h.Service.SubmitCheckin(context.Background(), green); err != nil {
		t.Fatalf("Green on the Milestone's own date: %v", err)
	}

	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
	_, err := h.Service.SubmitCheckin(context.Background(), green)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Green with an overdue Milestone: err = %v, want ErrValidation", err)
	}
	assertMilestoneOverdueError(t, err, beta)
	assertOverdueRulesOut(t, err, domain.OverdueRulesOutGreen)
	if want := `validation failed: milestone "Beta" overdue (due ` + beta.TargetDate.Format("2006-01-02") + `), rules out green`; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}

	// Yellow is still accepted, with its Path to Green.
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Beta is late.",
		PathToGreen:    "Cut scope.",
		PathTargetDate: pathDate,
	}); err != nil {
		t.Fatalf("Yellow with an overdue Milestone: %v", err)
	}
}

// Slipping the overdue Milestone to a future date in the same Check-in means it
// is no longer overdue, and a Milestone slip doesn't affect Health, so Green is
// accepted.
func TestGreenAcceptedWhenOverdueMilestoneSlipsInSameCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)
	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 2))

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Beta re-planned; launch holds.",
		Milestones: []domain.MilestoneChangeInput{
			{MilestoneID: beta.ID, TargetDate: beta.TargetDate.AddDate(0, 0, 14), DateReason: "Design review moved."},
		},
	}); err != nil {
		t.Fatalf("Green after slipping the overdue Milestone: %v", err)
	}
}

// The one-click no-change Check-in can't repeat a Green while a Milestone is
// overdue, and says how to respond to the missed date.
func TestNoChangeGreenRejectedWhileMilestoneOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})

	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
	_, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("no-change Green with an overdue Milestone: err = %v, want ErrValidation", err)
	}
	assertOverdueNoChangeRefusal(t, err)
	assertMilestoneOverdueError(t, err, beta)
	assertOverdueRulesOut(t, err, domain.OverdueRulesOutNoChange)
	if want := `validation failed: milestone "Beta" overdue (due ` + beta.TargetDate.Format("2006-01-02") + `), rules out no change`; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

// assertOverdueRulesOut checks err is a refusal for an overdue Milestone that
// says what the missed date rules out, so a page can compose its message.
func assertOverdueRulesOut(t *testing.T, err error, want domain.OverdueRulesOut) {
	t.Helper()
	var overdue *domain.MilestoneOverdueError
	if !errors.As(err, &overdue) {
		t.Fatalf("err = %v, want a *MilestoneOverdueError", err)
	}
	if overdue.RulesOut != want {
		t.Errorf("overdue RulesOut = %v, want %v", overdue.RulesOut, want)
	}
}

// assertMilestoneOverdueError checks err is a refusal for the overdue
// Milestone m that a caller can find with errors.As, naming m and its date.
func assertMilestoneOverdueError(t *testing.T, err error, m domain.Milestone) {
	t.Helper()
	var overdue *domain.MilestoneOverdueError
	if !errors.As(err, &overdue) {
		t.Fatalf("err = %v, want a *MilestoneOverdueError", err)
	}
	if overdue.Milestone.ID != m.ID || overdue.Milestone.Name != m.Name || !overdue.Milestone.TargetDate.Equal(m.TargetDate) {
		t.Errorf("overdue Milestone = %+v, want %+v", overdue.Milestone, m)
	}
}

// A No change can't repeat a Yellow or Red either while a Milestone is overdue:
// the missed date needs a response, so the refusal says what to do with it.
func TestNoChangeRefusedWhileMilestoneOverdueWhateverTheHealth(t *testing.T) {
	t.Parallel()

	for _, health := range []string{domain.HealthYellow, domain.HealthRed} {
		t.Run(health, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
			beta := onlyMilestone(t, h, goal.ID)
			h.Checkin(sam, goal.ID, health, "Behind.", "Add a second reviewer.", beta.TargetDate.AddDate(0, 1, 0))

			h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
			_, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("no-change %s with an overdue Milestone: err = %v, want ErrValidation", health, err)
			}
			assertOverdueNoChangeRefusal(t, err)
			if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
				t.Errorf("got %d Check-ins, want only the original one", len(history))
			}
		})
	}
}

// A full Yellow Check-in may still leave a Milestone overdue: only a Green or a
// No change is refused for it.
func TestYellowCheckinMayLeaveMilestoneOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)

	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Beta is late.",
		PathToGreen:    "Add a second reviewer.",
		PathTargetDate: beta.TargetDate.AddDate(0, 1, 0),
	}); err != nil {
		t.Fatalf("Yellow leaving an overdue Milestone: %v", err)
	}
}

// NoChangeRefusal answers, without recording anything, whether a No change
// would be refused and why: nil when it would be recorded, otherwise the
// refusal SubmitNoChangeCheckin gives.
func TestNoChangeRefusal(t *testing.T) {
	t.Parallel()

	t.Run("allowed", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
		h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})

		if err := h.Service.NoChangeRefusal(t.Context(), goal.ID); err != nil {
			t.Fatalf("NoChangeRefusal = %v, want nil", err)
		}
	})
	t.Run("no previous Check-in", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

		if err := h.Service.NoChangeRefusal(t.Context(), goal.ID); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("NoChangeRefusal = %v, want ErrValidation", err)
		}
	})
	t.Run("not Active", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.OnHoldGoal(sam, "Ship v2", "Customers wait too long.", "Waiting on the vendor.")

		if err := h.Service.NoChangeRefusal(t.Context(), goal.ID); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("NoChangeRefusal = %v, want ErrValidation", err)
		}
	})
	t.Run("overdue Milestone", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
		beta := onlyMilestone(t, h, goal.ID)
		h.Checkin(sam, goal.ID, domain.HealthRed, "Behind.", "Add a second reviewer.", beta.TargetDate.AddDate(0, 1, 0))

		h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
		err := h.Service.NoChangeRefusal(t.Context(), goal.ID)
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("NoChangeRefusal = %v, want ErrValidation", err)
		}
		assertOverdueNoChangeRefusal(t, err)
	})
	t.Run("off-target Green", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal, _ := offTargetMetricGoal(t, h, sam)

		err := h.Service.NoChangeRefusal(t.Context(), goal.ID)
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("NoChangeRefusal = %v, want ErrValidation", err)
		}
		var offTarget *domain.GreenOffTargetError
		if !errors.As(err, &offTarget) {
			t.Fatalf("NoChangeRefusal = %v, want a *GreenOffTargetError", err)
		}
		if want := "validation failed: green off target: Idle capacity 40.88 percent, target 20 by 2026-01-30"; err.Error() != want {
			t.Errorf("NoChangeRefusal = %q, want %q", err, want)
		}
	})
}

// assertOverdueNoChangeRefusal checks a No change refused for the overdue Beta
// Milestone names it and that its missed date rules out a No change, so a page
// can say how to respond to it.
func assertOverdueNoChangeRefusal(t *testing.T, err error) {
	t.Helper()
	var overdue *domain.MilestoneOverdueError
	if !errors.As(err, &overdue) {
		t.Fatalf("err = %v, want a *MilestoneOverdueError", err)
	}
	if overdue.Milestone.Name != "Beta" || overdue.RulesOut != domain.OverdueRulesOutNoChange {
		t.Errorf("overdue = %+v, want Beta ruling out a No change", overdue)
	}
}

// NoChangeRefusalsByGoal judges a No change on each of many Goals at once,
// with the refusal for each and none for a Goal it would be recorded on. Its
// refusals come in order: one whose Health the roll-up now needs explaining is
// refused for that even while a Milestone is overdue too (#449, #558).
func TestNoChangeRefusalsByGoal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	early := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	addPilot := func(g domain.Goal) {
		t.Helper()
		if _, err := h.Service.AddMilestoneAsAuthor(ctx, sam.ID, domain.AddMilestoneInput{
			GoalID: g.ID, Name: "Pilot", TargetDate: early,
		}); err != nil {
			t.Fatalf("AddMilestoneAsAuthor: %v", err)
		}
	}

	allowed := h.ActiveGoal(sam, "Allowed", "It matters.")
	h.Checkin(sam, allowed.ID, domain.HealthGreen, "On track.", "", time.Time{})
	noCheckin := h.ActiveGoal(sam, "No Check-in", "It matters.")
	onHold := h.OnHoldGoal(sam, "On Hold", "It matters.", "Waiting on the vendor.")
	milestoneOverdue := h.ActiveGoal(sam, "Milestone overdue", "It matters.")
	addPilot(milestoneOverdue)
	h.Checkin(sam, milestoneOverdue.ID, domain.HealthGreen, "Pilot lined up.", "", time.Time{})
	pathOverdue := h.ActiveGoal(sam, "Path overdue", "It matters.")
	h.Checkin(sam, pathOverdue.ID, domain.HealthYellow, "Behind.", "Add a reviewer.", early)
	// A parent checked in Green before its child turned Red now differs from
	// its Rolled-up Health with no explanation to repeat.
	unexplained := h.ActiveGoal(sam, "Unexplained", "It matters.")
	h.Checkin(sam, unexplained.ID, domain.HealthGreen, "On track.", "", time.Time{})
	redChild := h.ActiveChildOf(sam, unexplained, "Red child", "It helps.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)
	// So does one whose Pilot is overdue too: the roll-up is judged first.
	unexplainedOverdue := h.ActiveGoal(sam, "Unexplained and overdue", "It matters.")
	addPilot(unexplainedOverdue)
	h.Checkin(sam, unexplainedOverdue.ID, domain.HealthGreen, "On track.", "", time.Time{})
	thirdRedChild := h.ActiveChildOf(sam, unexplainedOverdue, "Third red child", "It helps.")
	h.Checkin(sam, thirdRedChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)
	// One checked in Green over its Red child with an explanation repeats it.
	explained := h.ActiveGoal(sam, "Explained", "It matters.")
	otherRedChild := h.ActiveChildOf(sam, explained, "Other red child", "It helps.")
	h.Checkin(sam, otherRedChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:      explained.ID,
		AuthorID:    sam.ID,
		Health:      domain.HealthGreen,
		Status:      "Fine overall.",
		Explanation: "The Red child is a stretch item.",
	}); err != nil {
		t.Fatalf("SubmitCheckin with an explanation: %v", err)
	}
	offTarget, _ := offTargetMetricGoal(t, h, sam) // moves the clock to Feb 2

	// Each refused Goal's refusal reads as this; "" is a Goal it allows.
	wantReason := map[int64]string{
		allowed.ID:            "",
		noCheckin.ID:          "there is no previous Check-in to repeat",
		onHold.ID:             "only an Active Goal takes a No change",
		milestoneOverdue.ID:   `milestone "Pilot" overdue (due 2026-01-20), rules out no change`,
		pathOverdue.ID:        "the back-to-Green date has passed: submit a Check-in with a new date and reason, or update the Health",
		unexplained.ID:        "this Health differs from the Rolled-up Health (Red); explain why — submit a Check-in to explain",
		redChild.ID:           "",
		unexplainedOverdue.ID: "this Health differs from the Rolled-up Health (Red); explain why — submit a Check-in to explain",
		thirdRedChild.ID:      "",
		explained.ID:          "",
		otherRedChild.ID:      "",
		offTarget.ID:          "green off target: Idle capacity 40.88 percent, target 20 by 2026-01-30",
	}
	all, err := h.Service.ListGoals(ctx)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	var goals []domain.Goal
	for _, g := range all {
		if _, ok := wantReason[g.ID]; ok {
			goals = append(goals, g)
		}
	}
	got, err := h.Service.NoChangeRefusalsByGoal(ctx, goals)
	if err != nil {
		t.Fatalf("NoChangeRefusalsByGoal: %v", err)
	}
	for _, g := range goals {
		refusal, refused := got[g.ID]
		switch want := wantReason[g.ID]; {
		case want == "" && refused:
			t.Errorf("%s: refused (%v), want it allowed", g.Title, refusal)
		case want != "" && !refused:
			t.Errorf("%s: allowed, want it refused: %s", g.Title, want)
		case want != "" && !errors.Is(refusal, domain.ErrValidation):
			t.Errorf("%s: refusal %v isn't ErrValidation", g.Title, refusal)
		case want != "" && !strings.HasSuffix(domain.Reason(refusal), want):
			t.Errorf("%s: refused with %q, want it to end %q", g.Title, domain.Reason(refusal), want)
		}
	}
	var green *domain.GreenOffTargetError
	if !errors.As(got[offTarget.ID], &green) {
		t.Errorf("off-target refusal = %v, want a *GreenOffTargetError", got[offTarget.ID])
	}
}

// A Milestone added in the Check-in with a date already past is overdue at once,
// so the same Check-in can't be Green.
func TestGreenRejectedWhenAddedMilestoneIsAlreadyPast(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:        goal.ID,
		AuthorID:      sam.ID,
		Health:        domain.HealthGreen,
		Status:        "Backfilling a gate.",
		NewMilestones: []domain.NewMilestoneInput{{Name: "Kickoff", TargetDate: h.Clock.Now().AddDate(0, 0, -3)}},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// offTargetMetricGoal arranges an Active Goal whose Idle capacity Metric (down,
// target 20 by Jan 30) last read 40.88 in a Green Check-in, then moves the
// clock to Feb 2: past the Metric's target date, so it is off target, but
// before the Goal's Milestone is due.
func offTargetMetricGoal(t *testing.T, h *testsupport.Harness, owner domain.Account) (domain.Goal, domain.Metric) {
	t.Helper()
	ctx := context.Background()
	goal := h.ActiveGoal(owner, "Reclaim idle capacity", "Idle machines cost money.")
	m, err := h.Service.AddMetric(ctx, goal.Owner.ID, domain.AddMetricInput{
		GoalID:     goal.ID,
		Name:       "Idle capacity",
		Unit:       "percent",
		Direction:  domain.MetricDown,
		Baseline:   55,
		Target:     20,
		TargetDate: time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: owner.ID,
		Health:   domain.HealthGreen,
		Status:   "Draining the old pool.",
		Readings: []domain.MetricReadingInput{{MetricID: m.ID, Value: 40.88}},
	}); err != nil {
		t.Fatalf("Green before the Metric's target date: %v", err)
	}
	h.Clock.Set(time.Date(2026, 2, 2, 15, 0, 0, 0, time.UTC))
	return goal, m
}

// A Green Check-in while a Metric is off target is refused until the Owner
// confirms it: unconfirmed it records nothing and names the Metric and its
// reading; confirmed, it records Green.
func TestGreenWithOffTargetMetricNeedsConfirming(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, idle := offTargetMetricGoal(t, h, sam)
	before, _, _ := h.Service.LatestCheckin(context.Background(), goal.ID)

	green := domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Still on track.",
	}
	_, err := h.Service.SubmitCheckin(context.Background(), green)
	if !errors.Is(err, domain.ErrGreenOffTarget) || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("unconfirmed Green: err = %v, want ErrGreenOffTarget and ErrValidation", err)
	}
	var offTarget *domain.GreenOffTargetError
	if !errors.As(err, &offTarget) {
		t.Fatalf("unconfirmed Green: err = %v, want a *GreenOffTargetError", err)
	}
	if len(offTarget.Metrics) != 1 || offTarget.Metrics[0].Metric.ID != idle.ID || offTarget.Metrics[0].Reading != 40.88 {
		t.Errorf("off-target Metrics = %+v, want Idle capacity at 40.88", offTarget.Metrics)
	}
	if want := "validation failed: green off target: Idle capacity 40.88 percent, target 20 by 2026-01-30"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	if latest, _, _ := h.Service.LatestCheckin(context.Background(), goal.ID); latest.ID != before.ID {
		t.Errorf("an unconfirmed Green was recorded")
	}

	green.ConfirmOffTarget = true
	c, err := h.Service.SubmitCheckin(context.Background(), green)
	if err != nil {
		t.Fatalf("confirmed Green: %v", err)
	}
	if c.Health != domain.HealthGreen {
		t.Errorf("Health = %q, want %q", c.Health, domain.HealthGreen)
	}
}

// Only Green asks for confirming: a Yellow Check-in while a Metric is off
// target records as any other.
func TestYellowWithOffTargetMetricRecordsWithoutConfirming(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetMetricGoal(t, h, sam)

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Idle capacity missed.",
		PathToGreen:    "Retire the old pool.",
		PathTargetDate: pathDate,
	}); err != nil {
		t.Fatalf("Yellow with an off-target Metric: %v", err)
	}
}

// The reading entered in the Check-in is the one judged: one that reaches the
// target means the Metric isn't off target, whatever it last read.
func TestGreenWithReadingReachingTargetNeedsNoConfirming(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, idle := offTargetMetricGoal(t, h, sam)

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Hit it.",
		Readings: []domain.MetricReadingInput{{MetricID: idle.ID, Value: 20}},
	}); err != nil {
		t.Fatalf("Green with a reading at the target: %v", err)
	}
}

// A Metric that has never been read isn't off target, even past its target
// date.
func TestGreenWithUnreadMetricPastItsDateNeedsNoConfirming(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reclaim idle capacity", "Idle machines cost money.")
	if _, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, domain.AddMetricInput{
		GoalID: goal.ID, Name: "Idle capacity", Unit: "percent", Direction: domain.MetricDown,
		Baseline: 55, Target: 20, TargetDate: time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	h.Clock.Set(time.Date(2026, 2, 2, 15, 0, 0, 0, time.UTC))

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "On track.",
	}); err != nil {
		t.Fatalf("Green with a never-read Metric: %v", err)
	}
}

// When a Milestone is overdue too, Green is refused for the Milestone, which
// no confirming gets past, and not with the off-target refusal.
func TestGreenRefusedForOverdueMilestoneBeforeOffTargetMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetMetricGoal(t, h, sam)
	beta := onlyMilestone(t, h, goal.ID)
	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "On track.",
	})
	if !errors.Is(err, domain.ErrValidation) || errors.Is(err, domain.ErrGreenOffTarget) {
		t.Fatalf("Green with an overdue Milestone and an off-target Metric: err = %v, want the overdue refusal", err)
	}

	if _, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID); !errors.Is(err, domain.ErrValidation) || errors.Is(err, domain.ErrGreenOffTarget) {
		t.Fatalf("no-change Green with an overdue Milestone and an off-target Metric: err = %v, want the overdue refusal", err)
	}
}

// A Metric's target date is judged in the org's timezone: late on its target
// date in Los Angeles, already the next day in UTC, it isn't off target yet.
func TestOffTargetJudgedInOrgTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	h.Service = domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetMetricGoal(t, h, sam)

	green := domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track."}
	h.Clock.Set(time.Date(2026, 1, 31, 5, 0, 0, 0, time.UTC)) // Jan 30, 9pm in Los Angeles
	if _, err := h.Service.SubmitCheckin(context.Background(), green); err != nil {
		t.Fatalf("Green on the Metric's target date in the org's timezone: %v", err)
	}
	h.Clock.Set(time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)) // Jan 31, 1am in Los Angeles
	if _, err := h.Service.SubmitCheckin(context.Background(), green); !errors.Is(err, domain.ErrGreenOffTarget) {
		t.Fatalf("Green the day after the Metric's target date: err = %v, want ErrGreenOffTarget", err)
	}
}

// A Metric's target date is judged in the Goal's Owner's Timezone, not the
// company's (CONTEXT.md: Timezone): with the company on UTC, late on the target
// date in the Owner's Los Angeles it isn't off target yet, for a Check-in or a
// No change.
func TestOffTargetJudgedInTheOwnersTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	h.SetTimezone(sam, "America/Los_Angeles")
	goal, _ := offTargetMetricGoal(t, h, sam)

	green := domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track."}
	h.Clock.Set(time.Date(2026, 1, 31, 5, 0, 0, 0, time.UTC)) // Jan 30, 9pm in Los Angeles
	if _, err := h.Service.SubmitCheckin(ctx, green); err != nil {
		t.Fatalf("Green on the Metric's target date in the Owner's timezone: %v", err)
	}
	if err := h.Service.NoChangeRefusal(ctx, goal.ID); err != nil {
		t.Fatalf("No change on the Metric's target date in the Owner's timezone refused: %v", err)
	}
	h.Clock.Set(time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)) // Jan 31, 1am in Los Angeles
	if _, err := h.Service.SubmitCheckin(ctx, green); !errors.Is(err, domain.ErrGreenOffTarget) {
		t.Fatalf("Green the day after the Metric's target date: err = %v, want ErrGreenOffTarget", err)
	}
}

// Whether a Milestone is overdue is judged by today's date in the Goal's
// Owner's Timezone, the company's for an Owner without one (CONTEXT.md:
// Timezone): late on Beta's date in Los Angeles a Green or No change is still
// taken, and early the next day in Tokyo, while still Beta's date in UTC, a
// Green is refused.
func TestOverdueMilestoneJudgedInTheOwnersTimezone(t *testing.T) {
	t.Parallel()

	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	// arrange gives a Green Check-in on a Goal whose only Milestone, Beta, is
	// due 2026-04-02, with its Owner in tz ("" for none), and the clock at
	// Beta's date plus after.
	arrange := func(t *testing.T, h *testsupport.Harness, tz string, after time.Duration) (domain.Account, domain.Goal, domain.Milestone) {
		t.Helper()
		sam := h.SignIn("sam@example.com")
		h.SetTimezone(sam, tz)
		goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
		beta := onlyMilestone(t, h, goal.ID)
		h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
		h.Clock.Set(beta.TargetDate.Add(after))
		return sam, goal, beta
	}
	green := func(sam domain.Account, goal domain.Goal) domain.SubmitCheckinInput {
		return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track."}
	}

	t.Run("late on its date in the Owner's Los Angeles", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		ctx := context.Background()
		sam, goal, _ := arrange(t, h, "America/Los_Angeles", 26*time.Hour) // Apr 2, 7pm in Los Angeles
		if err := h.Service.NoChangeRefusal(ctx, goal.ID); err != nil {
			t.Fatalf("No change on Beta's date in the Owner's timezone refused: %v", err)
		}
		if _, err := h.Service.SubmitCheckin(ctx, green(sam, goal)); err != nil {
			t.Fatalf("Green on Beta's date in the Owner's timezone: %v", err)
		}
		if _, err := h.Service.SubmitNoChangeCheckin(ctx, goal.ID, sam.ID); err != nil {
			t.Fatalf("No change on Beta's date in the Owner's timezone: %v", err)
		}
	})

	t.Run("the next day in the Owner's Los Angeles", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		ctx := context.Background()
		sam, goal, beta := arrange(t, h, "America/Los_Angeles", 32*time.Hour) // Apr 3, 1am in Los Angeles
		_, err := h.Service.SubmitCheckin(ctx, green(sam, goal))
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("Green the day after Beta's date: err = %v, want ErrValidation", err)
		}
		assertMilestoneOverdueError(t, err, beta)
		_, err = h.Service.SubmitNoChangeCheckin(ctx, goal.ID, sam.ID)
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("No change the day after Beta's date: err = %v, want ErrValidation", err)
		}
		assertMilestoneOverdueError(t, err, beta)
	})

	t.Run("the next day in the Owner's Tokyo", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam, goal, beta := arrange(t, h, "Asia/Tokyo", 20*time.Hour) // Apr 3, 5am in Tokyo
		_, err := h.Service.SubmitCheckin(context.Background(), green(sam, goal))
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("Green the day after Beta's date in the Owner's timezone: err = %v, want ErrValidation", err)
		}
		assertMilestoneOverdueError(t, err, beta)
	})

	t.Run("an Owner with no Timezone uses the company's", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		h.Service = domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))
		sam, goal, _ := arrange(t, h, "", 26*time.Hour) // Apr 2, 7pm in Los Angeles
		if _, err := h.Service.SubmitCheckin(context.Background(), green(sam, goal)); err != nil {
			t.Fatalf("Green on Beta's date in the company's timezone: %v", err)
		}
	})
}

// A No change can't repeat a Green while a Metric is off target; the Owner
// writes a full Check-in instead.
func TestNoChangeGreenRefusedWhileMetricOffTarget(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetMetricGoal(t, h, sam)

	if _, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID); !errors.Is(err, domain.ErrGreenOffTarget) {
		t.Fatalf("no-change Green with an off-target Metric: err = %v, want ErrGreenOffTarget", err)
	}
}

// OffTargetMetrics judges each of the Goal's Metrics by the reading entered,
// over the one last recorded: an entered reading still short of the target is
// off target, with that reading; one that meets the target leaves none.
func TestOffTargetMetricsJudgesTheReadingEntered(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, idle := offTargetMetricGoal(t, h, sam)
	ctx := context.Background()

	missed, err := h.Service.OffTargetMetrics(ctx, goal.ID, []domain.MetricReadingInput{{MetricID: idle.ID, Value: 35.5}})
	if err != nil {
		t.Fatalf("OffTargetMetrics: %v", err)
	}
	if len(missed) != 1 || missed[0].Metric.ID != idle.ID || missed[0].Reading != 35.5 {
		t.Errorf("off-target Metrics = %+v, want Idle capacity at the 35.5 entered", missed)
	}

	missed, err = h.Service.OffTargetMetrics(ctx, goal.ID, []domain.MetricReadingInput{{MetricID: idle.ID, Value: 20}})
	if err != nil {
		t.Fatalf("OffTargetMetrics: %v", err)
	}
	if len(missed) != 0 {
		t.Errorf("off-target Metrics = %+v, want none once the reading entered meets the target", missed)
	}
}

// A Yellow Check-in can mark its Path to Green as asking for help, and keeps
// the mark (CONTEXT.md: Path to Green, Ask for help).
func TestSubmitCheckinStoresAsksForHelpWhenYellow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Short a reviewer.",
		PathToGreen:    "Borrow a reviewer from another team.",
		PathTargetDate: pathDate,
		AsksForHelp:    true,
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if !c.AsksForHelp {
		t.Errorf("AsksForHelp = false, want the ask kept")
	}
	latest, _, err := h.Service.LatestCheckin(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("LatestCheckin: %v", err)
	}
	if !latest.AsksForHelp {
		t.Errorf("latest AsksForHelp = false, want the ask stored")
	}
}

// A Check-in with no Path to Green stores no ask either: a Green one, and one
// that puts the Goal On Hold, which records no Health.
func TestSubmitCheckinDropsAsksForHelpWithoutPathToGreen(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   domain.SubmitCheckinInput
	}{
		{"Green", domain.SubmitCheckinInput{Health: domain.HealthGreen, Status: "Fine.", AsksForHelp: true}},
		{"On Hold", domain.SubmitCheckinInput{
			Health: domain.HealthYellow, Status: "Pausing.", PathToGreen: "Wait.", PathTargetDate: pathDate,
			Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Waiting on budget.", AsksForHelp: true,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := testsupport.New(t)
			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
			in := tc.in
			in.GoalID, in.AuthorID = goal.ID, sam.ID
			c, err := h.Service.SubmitCheckin(context.Background(), in)
			if err != nil {
				t.Fatalf("SubmitCheckin: %v", err)
			}
			if c.AsksForHelp {
				t.Errorf("AsksForHelp = true, want false for a %s Check-in", tc.name)
			}
		})
	}
}

// A No change after a Check-in that asks for help asks too, as it repeats the
// Path to Green.
func TestNoChangeCheckinRepeatsAsksForHelp(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AskForHelp(sam, goal.ID, domain.HealthYellow, "Short a reviewer.", "Borrow a reviewer.", pathDate)

	c, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID)
	if err != nil {
		t.Fatalf("SubmitNoChangeCheckin: %v", err)
	}
	if !c.AsksForHelp {
		t.Errorf("AsksForHelp = false, want the No change to repeat the ask")
	}
}

// redMovingPath submits a Red Check-in on goalID whose back-to-Green date is
// date, with reason for moving it.
func redMovingPath(h *testsupport.Harness, author domain.Account, goalID int64, date time.Time, reason string) (domain.Checkin, error) {
	return h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:               goalID,
		AuthorID:             author.ID,
		Health:               domain.HealthRed,
		Status:               "Still behind.",
		PathToGreen:          "Add a second reviewer.",
		PathTargetDate:       date,
		PathTargetDateReason: reason,
	})
}

// Moving the back-to-Green date later is a Date Slip, so it needs a reason;
// without one nothing is recorded (CONTEXT.md: Date Slip).
func TestMovingBackToGreenDateLaterNeedsAReason(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Checkin(sam, goal.ID, domain.HealthRed, "Behind.", "Add a second reviewer.", pathDate)

	_, err := redMovingPath(h, sam, goal.ID, pathDate.AddDate(0, 0, 7), "  ")
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "moving the back-to-Green date later needs a reason") {
		t.Errorf("err = %q, want it to ask for a reason", err)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
		t.Errorf("got %d Check-ins, want only the original one", len(history))
	}
}

// With a reason, moving the back-to-Green date later records a Date Slip of
// that date on the new Check-in, and leaves the delivery date alone.
func TestMovingBackToGreenDateLaterRecordsADateSlip(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Checkin(sam, goal.ID, domain.HealthRed, "Behind.", "Add a second reviewer.", pathDate)
	later := pathDate.AddDate(0, 0, 7)

	c, err := redMovingPath(h, sam, goal.ID, later, "  The reviewer starts a week late.  ")
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}

	slips, err := h.Service.ListDateSlips(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListDateSlips: %v", err)
	}
	if len(slips) != 1 {
		t.Fatalf("got %d Date Slips, want 1", len(slips))
	}
	s := slips[0]
	if !s.BackToGreen || s.MilestoneID != 0 {
		t.Errorf("BackToGreen = %v, MilestoneID = %d; want a back-to-Green slip", s.BackToGreen, s.MilestoneID)
	}
	if !s.OldDate.Equal(pathDate) || !s.NewDate.Equal(later) {
		t.Errorf("slip dates = %s → %s, want %s → %s", s.OldDate, s.NewDate, pathDate, later)
	}
	if s.Reason != "The reviewer starts a week late." {
		t.Errorf("Reason = %q", s.Reason)
	}
	if s.CheckinID != c.ID {
		t.Errorf("CheckinID = %d, want %d", s.CheckinID, c.ID)
	}
	g, err := h.Service.ViewGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if !g.DeliveryDate.Equal(goal.DeliveryDate) {
		t.Errorf("DeliveryDate = %s, want it unchanged at %s", g.DeliveryDate, goal.DeliveryDate)
	}
}

// Only a later back-to-Green date than the latest Check-in's is a slip: an
// earlier or unchanged one, or a first one after a Green, records none, even
// with a reason typed.
func TestBackToGreenDateRecordsNoSlipUnlessLater(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		before string
		date   time.Time
	}{
		{"earlier", domain.HealthRed, pathDate.AddDate(0, 0, -3)},
		{"unchanged", domain.HealthRed, pathDate},
		{"after a Green", domain.HealthGreen, pathDate.AddDate(0, 0, 7)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
			if tc.before == domain.HealthGreen {
				h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
			} else {
				h.Checkin(sam, goal.ID, tc.before, "Behind.", "Add a second reviewer.", pathDate)
			}

			if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
				GoalID:               goal.ID,
				AuthorID:             sam.ID,
				Health:               domain.HealthYellow,
				Status:               "Behind.",
				PathToGreen:          "Add a second reviewer.",
				PathTargetDate:       tc.date,
				PathTargetDateReason: "A reason nobody needed.",
			}); err != nil {
				t.Fatalf("SubmitCheckin: %v", err)
			}
			if slips, _ := h.Service.ListDateSlips(context.Background(), goal.ID); len(slips) != 0 {
				t.Errorf("got %d Date Slips, want none: %+v", len(slips), slips)
			}
		})
	}
}

// A No change can't repeat a back-to-Green date that has passed, whatever the
// Health: the Owner must submit a Check-in with a new date and reason, or
// update the Health (CONTEXT.md: Check-in).
func TestNoChangeRefusedOnceTheBackToGreenDateHasPassed(t *testing.T) {
	t.Parallel()

	for _, health := range []string{domain.HealthYellow, domain.HealthRed} {
		t.Run(health, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
			h.Checkin(sam, goal.ID, health, "Behind.", "Add a second reviewer.", h.Clock.Now().AddDate(0, 0, 3))

			h.Clock.Advance(5 * 24 * time.Hour)
			const want = "the back-to-Green date has passed: submit a Check-in with a new date and reason, or update the Health"
			refusal := h.Service.NoChangeRefusal(context.Background(), goal.ID)
			_, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("no-change %s past its back-to-Green date: err = %v, want ErrValidation", health, err)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("err = %q, want it to contain %q", err, want)
			}
			if refusal == nil || refusal.Error() != err.Error() {
				t.Errorf("NoChangeRefusal = %v, want %v", refusal, err)
			}
			if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
				t.Errorf("got %d Check-ins, want only the original one", len(history))
			}
		})
	}
}

// Each refusal of a Check-in names the input it is about, as the Check-in form
// names it, so the form can show it there whatever the message says; the
// refusal is still an ErrValidation, and reads as it always has.
func TestCheckinRefusalNamesItsInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// refused builds a Check-in on goal that is refused, and the input the
		// refusal should name.
		refused func(h *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string)
		// message is the refusal's reason, as it reads without the
		// ErrValidation prefix.
		message string
	}{
		{
			name: "an unknown Health",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: "Blue", Status: "Fine."}, domain.CheckinInputHealth
			},
			message: `Health must be "Green", "Yellow", or "Red"`,
		},
		{
			name: "no status",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: " "}, domain.CheckinInputStatus
			},
			message: "a Check-in needs a status",
		},
		{
			name: "no status when putting the Goal On Hold",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Reorg."}, domain.CheckinInputStatus
			},
			message: "a Check-in needs a status",
		},
		{
			name: "a Yellow without a Path to Green",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthYellow, Status: "Slipping.", PathTargetDate: pathDate}, domain.CheckinInputPathToGreen
			},
			message: "a Yellow Goal needs a Path to Green",
		},
		{
			name: "a Path to Green without a target date",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthRed, Status: "Slipping.", PathToGreen: "Add two engineers."}, domain.CheckinInputPathToGreen
			},
			message: "a Path to Green needs a target date",
		},
		{
			name: "a later back-to-Green date without a reason",
			refused: func(h *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				h.Checkin(sam, goal.ID, domain.HealthYellow, "Behind.", "Add a reviewer.", pathDate)
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthYellow, Status: "Still behind.", PathToGreen: "Add a reviewer.", PathTargetDate: pathDate.AddDate(0, 0, 7)}, domain.CheckinInputPathTargetDateReason
			},
			message: "moving the back-to-Green date later needs a reason",
		},
		{
			name: "a Health unlike the Rolled-up Health, unexplained",
			refused: func(h *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				child := h.ActiveChildOf(sam, goal, "Fix the pager", "Pages wake people.")
				h.Checkin(sam, child.ID, domain.HealthRed, "Paging storm.", "Tune alerts.", pathDate)
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Fine."}, domain.CheckinInputExplanation
			},
			message: "this Health differs from the Rolled-up Health (Red); explain why",
		},
		{
			name: "putting the Goal On Hold without a reason",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Status: "Paused.", Lifecycle: domain.LifecycleOnHold}, domain.CheckinInputLifecycleReason
			},
			message: "putting a Goal On Hold needs a reason",
		},
		{
			name: "cancelling the Goal without a reason",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Status: "Stopping.", Lifecycle: domain.LifecycleCancelled}, domain.CheckinInputLifecycleReason
			},
			message: "cancelling a Goal needs a reason",
		},
		{
			name: "marking the Goal Done without an outcome",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Status: "Shipped.", Lifecycle: domain.LifecycleDone}, domain.CheckinInputOutcome
			},
			message: "marking a Goal Done needs an outcome",
		},
		{
			name: "an outcome of two lines",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Status: "Shipped.", Lifecycle: domain.LifecycleDone, Outcome: "Shipped.\nMostly."}, domain.CheckinInputOutcome
			},
			message: "a Goal's outcome is one line",
		},
		{
			name: "a Lifecycle a Check-in can't move to",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Status: "Back to the drawing board.", Lifecycle: domain.LifecycleProposed}, domain.CheckinInputLifecycle
			},
			message: `a Check-in can't move an Active Goal to "Proposed"`,
		},
		{
			name: "an On Hold Goal left On Hold",
			refused: func(h *testsupport.Harness, sam domain.Account, _ domain.Goal) (domain.SubmitCheckinInput, string) {
				held := h.OnHoldGoal(sam, "Migrate billing", "Old billing is costly.", "Waiting on finance.")
				return domain.SubmitCheckinInput{GoalID: held.ID, AuthorID: sam.ID, Status: "Still waiting."}, domain.CheckinInputLifecycle
			},
			message: "an On Hold Goal can only be resumed or Cancelled in a Check-in",
		},
		{
			name: "marking the Goal Done without a Metric's final value",
			refused: func(h *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				latency := addNamedMetric(h.T, h, goal.ID, "p99 latency")
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Status: "Shipped.", Lifecycle: domain.LifecycleDone, Outcome: "Shipped."}, domain.CheckinReadingInput(latency.ID)
			},
			message: `marking a Goal Done needs a final value for every Metric; "p99 latency" has none`,
		},
		{
			name: "a Green moving the delivery date later",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Fine.", DeliveryDate: goal.DeliveryDate.AddDate(0, 0, 7), DeliveryDateReason: "Vendor slipped."}, domain.CheckinInputDeliveryDate
			},
			message: "a Check-in that moves the delivery date later can't be Green",
		},
		{
			name: "a new delivery date without a reason",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Ahead.", DeliveryDate: goal.DeliveryDate.AddDate(0, 0, -7)}, domain.CheckinInputDeliveryDateReason
			},
			message: "changing the delivery date needs a reason",
		},
		{
			name: "a Milestone's new date without a reason",
			refused: func(h *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				beta := onlyMilestone(h.T, h, goal.ID)
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Fine.", Milestones: []domain.MilestoneChangeInput{
					{MilestoneID: beta.ID, TargetDate: beta.TargetDate.AddDate(0, 0, 7)},
				}}, domain.CheckinMilestoneInput(beta.ID, "date_reason")
			},
			message: `changing the date of Milestone "Beta" needs a reason`,
		},
		{
			name: "removing a Milestone without a reason",
			refused: func(h *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				beta := onlyMilestone(h.T, h, goal.ID)
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Fine.", Milestones: []domain.MilestoneChangeInput{
					{MilestoneID: beta.ID, Status: domain.MilestoneRemoved},
				}}, domain.CheckinMilestoneInput(beta.ID, "removed_reason")
			},
			message: `removing Milestone "Beta" needs a reason`,
		},
		{
			name: "a new Milestone without a name",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Fine.", NewMilestones: []domain.NewMilestoneInput{
					{Name: "Launch", TargetDate: pathDate},
					{Name: " ", TargetDate: pathDate},
				}}, domain.CheckinNewMilestoneInput(1)
			},
			message: "a Milestone needs a name",
		},
		{
			name: "a new Milestone without a date",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Fine.", NewMilestones: []domain.NewMilestoneInput{
					{Name: "Launch"},
				}}, domain.CheckinNewMilestoneInput(0)
			},
			message: "a Milestone needs a date",
		},
		{
			name: "a Highlight without a kind",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Fine.", Highlights: []domain.HighlightInput{
					{Kind: domain.HighlightInsight, Note: "Caching helped."},
					{Note: "Paging is noisy."},
				}}, domain.CheckinHighlightInput(2)
			},
			message: "Highlight 2 needs a kind: Insight, Accomplishment, or Miss",
		},
		{
			name: "a Highlight of an unknown kind",
			refused: func(_ *testsupport.Harness, sam domain.Account, goal domain.Goal) (domain.SubmitCheckinInput, string) {
				return domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Fine.", Highlights: []domain.HighlightInput{
					{Kind: "Rumour", Note: "Paging is noisy."},
				}}, domain.CheckinHighlightInput(1)
			},
			message: `Highlight 1 must be an "Insight", "Accomplishment", or "Miss"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
			in, want := tc.refused(h, sam, goal)

			_, err := h.Service.SubmitCheckin(context.Background(), in)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			problems := domain.InputErrors(err)
			if len(problems) != 1 || problems[0].Input != want {
				t.Fatalf("InputErrors(%v) = %+v, want one naming %q", err, problems, want)
			}
			if got := problems[0].Message; got != tc.message {
				t.Errorf("Message = %q, want %q", got, tc.message)
			}
			if got, want := err.Error(), "validation failed: "+tc.message; got != want {
				t.Errorf("err = %q, want %q", got, want)
			}
		})
	}
}

// A Green refused for an overdue Milestone names that Milestone's date: a
// Planned one's, or a new one's added already past. It is still a
// *MilestoneOverdueError, so a page can show the due date as it shows dates.
func TestGreenWhileMilestoneOverdueNamesItsDate(t *testing.T) {
	t.Parallel()

	t.Run("a Planned Milestone", func(t *testing.T) {
		t.Parallel()

		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
		beta := onlyMilestone(t, h, goal.ID)
		h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))

		_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
			GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track.",
		})
		assertMilestoneOverdueError(t, err, beta)
		assertNamesInput(t, err, domain.CheckinMilestoneInput(beta.ID, "date"))
		if want := `validation failed: milestone "Beta" overdue (due ` + beta.TargetDate.Format("2006-01-02") + `), rules out green`; err.Error() != want {
			t.Errorf("err = %q, want %q", err, want)
		}
	})

	t.Run("a new Milestone", func(t *testing.T) {
		t.Parallel()

		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
		past := h.Clock.Now().AddDate(0, 0, -3)

		_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
			GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Backfilling a gate.",
			NewMilestones: []domain.NewMilestoneInput{{Name: "Launch", TargetDate: pathDate}, {Name: "Kickoff", TargetDate: past}},
		})
		var overdue *domain.MilestoneOverdueError
		if !errors.As(err, &overdue) || overdue.Milestone.Name != "Kickoff" {
			t.Fatalf("err = %v, want a *MilestoneOverdueError for Kickoff", err)
		}
		assertNamesInput(t, err, domain.CheckinNewMilestoneInput(1))
	})
}

// assertNamesInput fails unless err refuses exactly the one input named.
func assertNamesInput(t *testing.T, err error, input string) {
	t.Helper()
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if problems := domain.InputErrors(err); len(problems) != 1 || problems[0].Input != input {
		t.Errorf("InputErrors(%v) = %+v, want one naming %q", err, problems, input)
	}
}

// raceCheckinPair runs a and b at the same moment and returns their errors.
func raceCheckinPair(a, b func() error) (errA, errB error) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() { defer wg.Done(); <-start; errA = a() }()
	go func() { defer wg.Done(); <-start; errB = b() }()
	close(start)
	wg.Wait()
	return errA, errB
}

// closingCheckin returns a Check-in by author that ends goal in lifecycle, Done
// or Cancelled.
func closingCheckin(h *testsupport.Harness, author domain.Account, goal domain.Goal, lifecycle string) func() error {
	return func() error {
		_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
			GoalID:          goal.ID,
			AuthorID:        author.ID,
			Status:          "Wrapping this up.",
			Lifecycle:       lifecycle,
			LifecycleReason: "No longer needed.",
			Outcome:         "It shipped.",
		})
		return err
	}
}

// A Done and a Cancelled Check-in sent at the same moment on one Active Goal,
// by its Owner and a Delegate: only one is accepted, the other is refused as it
// would be sent after, and the Goal ends in the accepted one's Lifecycle
// (CONTEXT.md: Lifecycle — Done and Cancelled are end states).
func TestDoneAndCancelledCheckinsSentTogetherCloseTheGoalOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	ctx := context.Background()
	for i := range 30 {
		goal := h.ActiveGoal(sam, fmt.Sprintf("Ship v%d", i), "Customers wait too long.")
		h.AddDelegate(sam, dee, goal.ID)
		errDone, errCancelled := raceCheckinPair(
			closingCheckin(h, sam, goal, domain.LifecycleDone),
			closingCheckin(h, dee, goal, domain.LifecycleCancelled),
		)
		if (errDone == nil) == (errCancelled == nil) {
			t.Fatalf("round %d: Done err = %v, Cancelled err = %v; want exactly one accepted", i, errDone, errCancelled)
		}
		for _, err := range []error{errDone, errCancelled} {
			if err != nil && !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("round %d: err = %v, want ErrValidation for the Check-in refused", i, err)
			}
		}
		history, err := h.Service.ListCheckins(ctx, goal.ID)
		if err != nil {
			t.Fatalf("ListCheckins: %v", err)
		}
		var changes []domain.LifecycleChange
		for _, c := range history {
			if c.LifecycleChange.Changed() {
				changes = append(changes, c.LifecycleChange)
			}
		}
		if len(changes) != 1 {
			t.Fatalf("round %d: lifecycle changes = %+v, want exactly one", i, changes)
		}
		want := domain.LifecycleDone
		if errDone != nil {
			want = domain.LifecycleCancelled
		}
		if changes[0].To != want {
			t.Errorf("round %d: recorded change to %q, want %q", i, changes[0].To, want)
		}
		got, err := h.Service.ViewGoal(ctx, goal.ID)
		if err != nil {
			t.Fatalf("ViewGoal: %v", err)
		}
		if got.Lifecycle != want {
			t.Fatalf("round %d: Lifecycle = %q, want %q", i, got.Lifecycle, want)
		}
	}
}

// Two Check-ins that leave the Goal Active, sent at the same moment by its
// Owner and a Delegate, are both recorded: only a Lifecycle change is decided
// by which lands first.
func TestOrdinaryCheckinsSentTogetherAreBothAccepted(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	ctx := context.Background()
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.AddDelegate(sam, dee, goal.ID)
	green := func(author domain.Account, status string) func() error {
		return func() error {
			_, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
				GoalID: goal.ID, AuthorID: author.ID, Health: domain.HealthGreen, Status: status,
			})
			return err
		}
	}
	errSam, errDee := raceCheckinPair(green(sam, "On track."), green(dee, "Still on track."))
	if errSam != nil || errDee != nil {
		t.Fatalf("Owner's err = %v, Delegate's err = %v; want both accepted", errSam, errDee)
	}
	history, err := h.Service.ListCheckins(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(history) != 2 {
		t.Errorf("Check-ins = %d, want 2", len(history))
	}
}

// A No change and a Cancelled Check-in sent at the same moment on one Active
// Goal: a No change is never recorded on the Goal after it is Cancelled.
func TestNoChangeSentWithACancelNeverLandsOnTheCancelledGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	ctx := context.Background()
	for i := range 30 {
		goal := h.ActiveGoal(sam, fmt.Sprintf("Ship v%d", i), "Customers wait too long.")
		h.AddDelegate(sam, dee, goal.ID)
		h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
		errNoChange, errCancel := raceCheckinPair(
			func() error {
				_, err := h.Service.SubmitNoChangeCheckin(ctx, goal.ID, dee.ID)
				return err
			},
			closingCheckin(h, sam, goal, domain.LifecycleCancelled),
		)
		if errCancel != nil {
			t.Fatalf("round %d: Cancel err = %v, want accepted", i, errCancel)
		}
		if errNoChange != nil && !errors.Is(errNoChange, domain.ErrValidation) {
			t.Fatalf("round %d: No change err = %v, want ErrValidation when refused", i, errNoChange)
		}
		history, err := h.Service.ListCheckins(ctx, goal.ID)
		if err != nil {
			t.Fatalf("ListCheckins: %v", err)
		}
		// Newest first: the Cancel must be the latest Check-in, with nothing
		// recorded after it.
		if len(history) == 0 || history[0].LifecycleChange.To != domain.LifecycleCancelled {
			t.Fatalf("round %d: history = %+v, want the Cancel latest", i, history)
		}
	}
}
