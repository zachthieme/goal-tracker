package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Owner submits a Green Check-in on an Active Goal: it records the Health, the
// status, its author and the Owner it was written for, and becomes the Goal's
// latest Check-in (CONTEXT.md: Check-in).
func TestSubmitCheckinRecordsHealthAuthorAndOwner(t *testing.T) {
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

var pathDate = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

// A Proposed Goal has no Health and can't be checked in on (CONTEXT.md: Health
// is how an Active Goal is tracking).
func TestSubmitCheckinRejectedOnProposedGoal(t *testing.T) {
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
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")

	latency, err := h.Service.AddMetric(context.Background(), validMetric(goal.ID))
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
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")
	other := h.ActiveGoal(sam, "Grow signups", "More signups, more revenue.")
	foreign, err := h.Service.AddMetric(context.Background(), validMetric(other.ID))
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

// One-click "no change" records a Check-in that repeats the previous values,
// author and Owner it is written for aside.
func TestNoChangeCheckinRepeatsPreviousValues(t *testing.T) {
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
