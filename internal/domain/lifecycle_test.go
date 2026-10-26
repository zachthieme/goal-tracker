package domain_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Owner puts an Active Goal On Hold in a Check-in, with a reason: the Goal
// leaves Active and the Check-in records the change and its reason (CONTEXT.md:
// Lifecycle — leaving Active for any state other than Done requires a reason).
func TestCheckinPutsGoalOnHoldWithReason(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	m, err := h.Service.AddMetric(context.Background(), h.GoalOwnerID(goalID), in)
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	return m
}

// Cancelling a Goal needs a reason; with one, the Goal is Cancelled and the
// reason is kept on the Check-in.
func TestCheckinCancelRequiresReason(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

// A Goal whose Lifecycle takes no Check-ins, Proposed, Done or Cancelled,
// refuses one with a reason naming its Lifecycle and saying which take them.
func TestCheckinRefusedOnLifecycleTakingNoCheckins(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	proposed := h.CreateGoal(sam, "Ship search", "People can't find things.")
	done := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)
	cancelled := h.ActiveGoal(sam, "Cut churn", "Customers leave.")
	h.EndGoalInCheckin(sam, cancelled.ID, domain.LifecycleCancelled)

	for _, c := range []struct {
		lifecycle string
		goalID    int64
	}{
		{domain.LifecycleProposed, proposed.ID},
		{domain.LifecycleDone, done.ID},
		{domain.LifecycleCancelled, cancelled.ID},
	} {
		_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
			GoalID: c.goalID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track.",
		})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", c.lifecycle, err)
			continue
		}
		want := "a " + c.lifecycle + " Goal can't be checked in on: only an Active or On Hold Goal takes Check-ins"
		if got := domain.Reason(err); got != want {
			t.Errorf("%s: reason = %q, want %q", c.lifecycle, got, want)
		}
	}
}

// A Check-in's Lifecycle change planned from one Lifecycle, made on a Goal
// that has since moved to another still taking Check-ins, is refused saying
// the Lifecycle changed meanwhile, and leaves the Goal where it is.
func TestLifecycleChangeFromALifecycleTheGoalLeftIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	err := domain.SetGoalLifecycleFrom(ctx, h.Service, goal.ID, domain.LifecycleChange{
		From: domain.LifecycleOnHold, To: domain.LifecycleCancelled, Reason: "Priorities moved.",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if got, want := domain.Reason(err), "the Goal's Lifecycle changed meanwhile: reload it and try again"; got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
	assertLifecycle(t, h, goal.ID, domain.LifecycleActive)
}

// Lifecycle changes appear in the Goal's Check-in history, newest first, each
// with where the Goal moved from and to and its reason.
func TestLifecycleChangesAppearInCheckinHistory(t *testing.T) {
	t.Parallel()

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

// The Lifecycle says what a Goal in it takes, in Lifecycle order: Check-ins
// only while it is Active or On Hold, Draft Highlights and Milestones until it
// has ended, Done or Cancelled, and a parent suggestion only while it is
// Proposed or Active; and where a Check-in may move it (CONTEXT.md: Lifecycle,
// Draft Highlight, Milestone). The refusals that list Lifecycles name them in
// that order.
func TestLifecycleSaysWhatAGoalTakes(t *testing.T) {
	t.Parallel()
	want := []string{domain.LifecycleProposed, domain.LifecycleActive, domain.LifecycleOnHold, domain.LifecycleDone, domain.LifecycleCancelled}
	if got := domain.Lifecycles(); !slices.Equal(got, want) {
		t.Errorf("Lifecycles() = %q, want %q", got, want)
	}
	rows := map[string]struct {
		checkins, ended, draftHighlights, milestones, suggestable bool
		changesTo                                                 []string
	}{
		domain.LifecycleProposed:  {false, false, true, true, true, nil},
		domain.LifecycleActive:    {true, false, true, true, true, []string{domain.LifecycleOnHold, domain.LifecycleDone, domain.LifecycleCancelled}},
		domain.LifecycleOnHold:    {true, false, true, true, false, []string{domain.LifecycleActive, domain.LifecycleCancelled}},
		domain.LifecycleDone:      {false, true, false, false, false, nil},
		domain.LifecycleCancelled: {false, true, false, false, false, nil},
	}
	for _, l := range domain.Lifecycles() {
		c, ok := rows[l]
		if !ok {
			t.Errorf("Lifecycle %q has no row in the test", l)
			continue
		}
		if got := domain.LifecycleTakesCheckins(l); got != c.checkins {
			t.Errorf("LifecycleTakesCheckins(%s) = %t, want %t", l, got, c.checkins)
		}
		if got := domain.LifecycleEnded(l); got != c.ended {
			t.Errorf("LifecycleEnded(%s) = %t, want %t", l, got, c.ended)
		}
		if got := domain.LifecycleTakesDraftHighlights(l); got != c.draftHighlights {
			t.Errorf("LifecycleTakesDraftHighlights(%s) = %t, want %t", l, got, c.draftHighlights)
		}
		if got := domain.LifecycleTakesMilestones(l); got != c.milestones {
			t.Errorf("LifecycleTakesMilestones(%s) = %t, want %t", l, got, c.milestones)
		}
		if got := domain.LifecycleSuggestable(l); got != c.suggestable {
			t.Errorf("LifecycleSuggestable(%s) = %t, want %t", l, got, c.suggestable)
		}
		if got := domain.LifecycleChangesFrom(l); !slices.Equal(got, c.changesTo) {
			t.Errorf("LifecycleChangesFrom(%s) = %q, want %q", l, got, c.changesTo)
		}
	}

	if got, want := domain.Reason(domain.LifecycleTakesNoCheckinsRefusal(domain.LifecycleProposed)),
		"a Proposed Goal can't be checked in on: only an Active or On Hold Goal takes Check-ins"; got != want {
		t.Errorf("takes-no-Check-ins refusal = %q, want %q", got, want)
	}
	if got, want := domain.SuggestParentLifecycleSentence(), "a parent can only be suggested for a Proposed or Active Goal"; got != want {
		t.Errorf("suggest-parent sentence = %q, want %q", got, want)
	}
}
