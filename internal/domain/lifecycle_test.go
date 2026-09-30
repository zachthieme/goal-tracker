package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Owner puts an Active Goal On Hold in a Check-in, with a reason: the Goal
// leaves Active and the Check-in records the change and its reason (CONTEXT.md:
// Lifecycle — leaving Active for any state other than Done requires a reason).
func TestCheckinPutsGoalOnHoldWithReason(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          goal.ID,
		AuthorID:        sam.ID,
		Status:          "Pausing for the reorg.",
		Lifecycle:       domain.LifecycleOnHold,
		LifecycleReason: "Team moved to the payments incident.",
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	want := domain.LifecycleChange{
		From:   domain.LifecycleActive,
		To:     domain.LifecycleOnHold,
		Reason: "Team moved to the payments incident.",
	}
	if c.LifecycleChange != want {
		t.Errorf("LifecycleChange = %+v, want %+v", c.LifecycleChange, want)
	}

	g, err := h.Service.ViewGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if g.Lifecycle != domain.LifecycleOnHold {
		t.Errorf("Lifecycle = %q, want %q", g.Lifecycle, domain.LifecycleOnHold)
	}
}

// Putting a Goal On Hold without a reason is refused, and the Goal stays Active.
func TestCheckinOnHoldRequiresReason(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          goal.ID,
		AuthorID:        sam.ID,
		Status:          "Pausing.",
		Lifecycle:       domain.LifecycleOnHold,
		LifecycleReason: "   ",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleActive)
}

// assertLifecycle fails the test unless the Goal is in the want Lifecycle.
func assertLifecycle(t *testing.T, h *testsupport.Harness, goalID int64, want string) {
	t.Helper()
	g, err := h.Service.ViewGoal(context.Background(), goalID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if g.Lifecycle != want {
		t.Errorf("Lifecycle = %q, want %q", g.Lifecycle, want)
	}
}

// Marking a Goal Done records its one-line outcome and a final value for every
// Metric — the Done Check-in's readings (CONTEXT.md: Lifecycle — reaching Done
// requires an outcome).
func TestCheckinMarksGoalDoneWithOutcomeAndFinalValues(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut checkout latency", "Faster checkout lifts conversion.")
	latency := addNamedMetric(t, h, goal.ID, "p95 checkout latency")
	errors5xx := addNamedMetric(t, h, goal.ID, "checkout 5xx rate")

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:    goal.ID,
		AuthorID:  sam.ID,
		Status:    "Shipped the new cache.",
		Lifecycle: domain.LifecycleDone,
		Outcome:   "p95 latency down to 380ms, beating the 400ms target.",
		Readings: []domain.MetricReadingInput{
			{MetricID: latency.ID, Value: 380},
			{MetricID: errors5xx.ID, Value: 0.2},
		},
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if c.LifecycleChange.To != domain.LifecycleDone || c.LifecycleChange.Outcome != "p95 latency down to 380ms, beating the 400ms target." {
		t.Errorf("LifecycleChange = %+v, want Done with the outcome", c.LifecycleChange)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleDone)

	readings, err := h.Service.ListMetricReadings(context.Background(), latency.ID)
	if err != nil {
		t.Fatalf("ListMetricReadings: %v", err)
	}
	if len(readings) != 1 || readings[0].Value != 380 {
		t.Errorf("latency readings = %+v, want the final value 380", readings)
	}
}

// Done is refused while any Metric lacks a final value, and the Goal stays
// Active.
func TestCheckinDoneRequiresFinalValueForEveryMetric(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut checkout latency", "Faster checkout lifts conversion.")
	latency := addNamedMetric(t, h, goal.ID, "p95 checkout latency")
	addNamedMetric(t, h, goal.ID, "checkout 5xx rate")

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:    goal.ID,
		AuthorID:  sam.ID,
		Status:    "Shipped.",
		Lifecycle: domain.LifecycleDone,
		Outcome:   "Latency is down.",
		Readings:  []domain.MetricReadingInput{{MetricID: latency.ID, Value: 380}},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleActive)
}

// addNamedMetric adds a Metric with the given name to the Goal, failing the test on
// error.
func addNamedMetric(t *testing.T, h *testsupport.Harness, goalID int64, name string) domain.Metric {
	t.Helper()
	in := validMetric(goalID)
	in.Name = name
	m, err := h.Service.AddMetric(context.Background(), in)
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	return m
}

// Cancelling a Goal needs a reason; with one, the Goal is Cancelled and the
// reason is kept on the Check-in.
func TestCheckinCancelRequiresReason(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:    goal.ID,
		AuthorID:  sam.ID,
		Status:    "Stopping.",
		Lifecycle: domain.LifecycleCancelled,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleActive)

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          goal.ID,
		AuthorID:        sam.ID,
		Status:          "Stopping.",
		Lifecycle:       domain.LifecycleCancelled,
		LifecycleReason: "The vendor we were replacing renewed at half price.",
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if c.LifecycleChange.Reason != "The vendor we were replacing renewed at half price." {
		t.Errorf("Reason = %q, want the submitted reason", c.LifecycleChange.Reason)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleCancelled)
}

// Done needs an outcome, and only one line of it.
func TestCheckinDoneRequiresOneLineOutcome(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	for name, outcome := range map[string]string{
		"missing":   "  ",
		"multiline": "Shipped v2.\nAlso cleaned up the backlog.",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
				GoalID:    goal.ID,
				AuthorID:  sam.ID,
				Status:    "Shipped.",
				Lifecycle: domain.LifecycleDone,
				Outcome:   outcome,
			})
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			assertLifecycle(t, h, goal.ID, domain.LifecycleActive)
		})
	}
}

// A Check-in that takes the Goal out of Active records no Health — Health is how
// an Active Goal is tracking — so it needs no Path to Green either.
func TestCheckinLeavingActiveRecordsNoHealth(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          goal.ID,
		AuthorID:        sam.ID,
		Health:          domain.HealthRed,
		Status:          "Pausing.",
		Lifecycle:       domain.LifecycleOnHold,
		LifecycleReason: "Waiting on legal review.",
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if c.Health != "" || c.PathToGreen != "" {
		t.Errorf("Health = %q, PathToGreen = %q, want neither on a Goal leaving Active", c.Health, c.PathToGreen)
	}
}

// An On Hold Goal can be resumed to Active in a Check-in, which is a full
// Check-in with a Health; no reason is needed to come back.
func TestCheckinResumesOnHoldGoal(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.OnHoldGoal(sam, "Ship v2", "Customers wait too long.", "Waiting on legal review.")

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:    goal.ID,
		AuthorID:  sam.ID,
		Health:    domain.HealthGreen,
		Status:    "Legal signed off; back on it.",
		Lifecycle: domain.LifecycleActive,
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if c.Health != domain.HealthGreen {
		t.Errorf("Health = %q, want %q", c.Health, domain.HealthGreen)
	}
	want := domain.LifecycleChange{From: domain.LifecycleOnHold, To: domain.LifecycleActive}
	if c.LifecycleChange != want {
		t.Errorf("LifecycleChange = %+v, want %+v", c.LifecycleChange, want)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleActive)
}

// Resuming is an Active Check-in, so it still needs a valid Health.
func TestCheckinResumeNeedsHealth(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.OnHoldGoal(sam, "Ship v2", "Customers wait too long.", "Waiting on legal review.")

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:    goal.ID,
		AuthorID:  sam.ID,
		Status:    "Back on it.",
		Lifecycle: domain.LifecycleActive,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleOnHold)
}

// While On Hold a Goal is not being tracked: a Check-in on it must resume it or
// Cancel it (with a reason); it can't simply report a Health, go straight to
// Done, or skip the one-click no-change Check-in past the hold.
func TestCheckinOnHoldGoalOnlyResumesOrCancels(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.OnHoldGoal(sam, "Ship v2", "Customers wait too long.", "Waiting on legal review.")

	for name, in := range map[string]domain.SubmitCheckinInput{
		"plain check-in": {Health: domain.HealthGreen, Status: "Still waiting."},
		"done":           {Status: "Done.", Lifecycle: domain.LifecycleDone, Outcome: "Shipped."},
	} {
		t.Run(name, func(t *testing.T) {
			in.GoalID, in.AuthorID = goal.ID, sam.ID
			if _, err := h.Service.SubmitCheckin(context.Background(), in); !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			assertLifecycle(t, h, goal.ID, domain.LifecycleOnHold)
		})
	}
	if _, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("no-change err = %v, want ErrValidation", err)
	}

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          goal.ID,
		AuthorID:        sam.ID,
		Status:          "Not coming back.",
		Lifecycle:       domain.LifecycleCancelled,
		LifecycleReason: "Legal said no.",
	}); err != nil {
		t.Fatalf("cancel On Hold Goal: %v", err)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleCancelled)
}

// Done and Cancelled are end states: no further Check-in can reopen them.
func TestCheckinCantReopenEndedGoal(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: goal.ID, AuthorID: sam.ID, Status: "Shipped.", Lifecycle: domain.LifecycleDone, Outcome: "v2 is live.",
	}); err != nil {
		t.Fatalf("mark Done: %v", err)
	}

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Reopening.", Lifecycle: domain.LifecycleActive,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleDone)
}

// Lifecycle changes appear in the Goal's Check-in history, newest first, each
// with where the Goal moved from and to and its reason.
func TestLifecycleChangesAppearInCheckinHistory(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.OnHoldGoal(sam, "Ship v2", "Customers wait too long.", "Waiting on legal review.")
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Back on it.", Lifecycle: domain.LifecycleActive,
	}); err != nil {
		t.Fatalf("resume: %v", err)
	}

	history, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	var got []domain.LifecycleChange
	for _, c := range history {
		got = append(got, c.LifecycleChange)
	}
	want := []domain.LifecycleChange{
		{From: domain.LifecycleOnHold, To: domain.LifecycleActive},
		{From: domain.LifecycleActive, To: domain.LifecycleOnHold, Reason: "Waiting on legal review."},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("history lifecycle changes = %+v, want %+v", got, want)
	}
}
