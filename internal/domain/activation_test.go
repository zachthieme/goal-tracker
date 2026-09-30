package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The activation gate (CONTEXT.md: Lifecycle; AC on issue #3): Proposed → Active
// is rejected unless the Goal has a So What, an Owner, and either a delivery
// date plus at least one Milestone or Metric (Dated) or at least one Metric
// (Ongoing). Each missing item comes back as its own validation error.

var futureDate = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

func addMilestone(t *testing.T, h *testsupport.Harness, goalID int64) {
	t.Helper()
	if _, err := h.Service.AddMilestone(context.Background(), domain.AddMilestoneInput{
		GoalID:     goalID,
		Name:       "Beta cut",
		TargetDate: futureDate,
	}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
}

func addMetric(t *testing.T, h *testsupport.Harness, goalID int64) {
	t.Helper()
	if _, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
		GoalID:     goalID,
		Name:       "p95 latency",
		Unit:       "ms",
		Direction:  domain.MetricDown,
		Baseline:   1200,
		Target:     400,
		TargetDate: futureDate,
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
}

func TestActivateDatedGoalWithAMilestone(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")
	if _, err := h.Service.MarkGoalDated(context.Background(), g.ID, futureDate); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	addMilestone(t, h, g.ID)

	got, err := h.Service.ActivateGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleActive)
	}
}

func TestActivateDatedGoalWithAMetric(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")
	if _, err := h.Service.MarkGoalDated(context.Background(), g.ID, futureDate); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	addMetric(t, h, g.ID)

	got, err := h.Service.ActivateGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleActive)
	}
}

func TestActivateOngoingGoalWithAMetric(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Keep the lights on", "Uptime keeps customers.")
	if _, err := h.Service.MarkGoalOngoing(context.Background(), g.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	addMetric(t, h, g.ID)

	got, err := h.Service.ActivateGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleActive)
	}
}

func TestActivateRejectsGoalNotMarkedDatedOrOngoing(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	_, err := h.Service.ActivateGoal(context.Background(), g.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "Dated") || !strings.Contains(err.Error(), "Ongoing") {
		t.Errorf("error should name the missing choice; got %q", err.Error())
	}
}

func TestActivateRejectsDatedGoalWithNoMilestoneOrMetric(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")
	if _, err := h.Service.MarkGoalDated(context.Background(), g.ID, futureDate); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}

	_, err := h.Service.ActivateGoal(context.Background(), g.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "Milestone or Metric") {
		t.Errorf("error should name the missing Milestone or Metric; got %q", err.Error())
	}
}

func TestActivateRejectsOngoingGoalWithNoMetric(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Keep the lights on", "Uptime keeps customers.")
	if _, err := h.Service.MarkGoalOngoing(context.Background(), g.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}

	_, err := h.Service.ActivateGoal(context.Background(), g.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "Metric") {
		t.Errorf("error should name the missing Metric; got %q", err.Error())
	}
}

// A Dated Goal that is missing both a delivery date and a Milestone/Metric comes
// back with each missing item as its own validation error, so the Owner sees the
// full list at once rather than one at a time. (Reached by activating a Goal
// whose kind is Dated but which has neither a date nor any child — see the gate
// contract on issue #3.)
func TestActivateReportsEveryMissingItem(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	_, err := h.Service.ActivateGoal(context.Background(), g.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	reasons := domain.ActivationReasons(err)
	if len(reasons) == 0 {
		t.Fatalf("want the gate failures enumerated as separate reasons; got %v", err)
	}
	for _, r := range reasons {
		if !errors.Is(r, domain.ErrValidation) {
			t.Errorf("reason %q does not wrap ErrValidation", r)
		}
	}
}

func TestActivateLeavesGoalProposedWhenRejected(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	if _, err := h.Service.ActivateGoal(context.Background(), g.ID); err == nil {
		t.Fatal("want the activation to be rejected")
	}
	got, err := h.Service.ViewGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleProposed {
		t.Errorf("Lifecycle = %q, want it to stay %q", got.Lifecycle, domain.LifecycleProposed)
	}
}
