package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// day is a calendar date at midnight UTC, the form the domain stores dates in.
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Moving the delivery date in a Check-in records a Date Slip keeping the old and
// new dates and the reason, and the Goal takes the new date (CONTEXT.md: Date
// Slip).
func TestCheckinDeliveryDateChangeRecordsDateSlip(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	oldDate := goal.DeliveryDate
	newDate := oldDate.AddDate(0, 0, 14)

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthYellow,
		Status:             "Vendor is late.",
		PathToGreen:        "Swap vendors.",
		PathTargetDate:     pathDate,
		DeliveryDate:       newDate,
		DeliveryDateReason: "Vendor API delayed two weeks.",
	})
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
	if s.MilestoneID != 0 {
		t.Errorf("MilestoneID = %d, want 0 for a delivery-date slip", s.MilestoneID)
	}
	if !s.OldDate.Equal(oldDate) || !s.NewDate.Equal(newDate) {
		t.Errorf("slip dates = %s → %s, want %s → %s", s.OldDate, s.NewDate, oldDate, newDate)
	}
	if s.Reason != "Vendor API delayed two weeks." {
		t.Errorf("Reason = %q", s.Reason)
	}
	if s.CheckinID != c.ID {
		t.Errorf("CheckinID = %d, want %d", s.CheckinID, c.ID)
	}

	g, err := h.Service.ViewGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if !g.DeliveryDate.Equal(newDate) {
		t.Errorf("DeliveryDate = %s, want %s", g.DeliveryDate, newDate)
	}
}

// A delivery-date change without a reason is rejected, and nothing is recorded.
func TestCheckinDeliveryDateChangeRequiresReason(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthGreen,
		Status:             "Pulling in.",
		DeliveryDate:       goal.DeliveryDate.AddDate(0, 0, -7),
		DeliveryDateReason: "  ",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if slips, _ := h.Service.ListDateSlips(context.Background(), goal.ID); len(slips) != 0 {
		t.Errorf("recorded %d Date Slips despite the rejection", len(slips))
	}
	if _, ok, _ := h.Service.LatestCheckin(context.Background(), goal.ID); ok {
		t.Errorf("a Check-in was recorded despite the rejection")
	}
}

// Submitting the delivery date unchanged is not a slip and needs no reason.
func TestCheckinUnchangedDeliveryDateIsNotASlip(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:       goal.ID,
		AuthorID:     sam.ID,
		Health:       domain.HealthGreen,
		Status:       "On track.",
		DeliveryDate: goal.DeliveryDate,
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if slips, _ := h.Service.ListDateSlips(context.Background(), goal.ID); len(slips) != 0 {
		t.Errorf("recorded %d Date Slips for an unchanged date", len(slips))
	}
}

// onlyMilestone returns the single Milestone the ActiveGoal builder gives a Goal.
func onlyMilestone(t *testing.T, h *testsupport.Harness, goalID int64) domain.Milestone {
	t.Helper()
	ms, err := h.Service.ListMilestones(context.Background(), goalID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("got %d Milestones, want 1", len(ms))
	}
	return ms[0]
}

// Moving a Milestone's date in a Check-in records a Date Slip for that Milestone
// with the old and new dates and the reason, and the Milestone takes the new
// date. A Milestone slip that doesn't move the delivery date doesn't affect
// Health, so the Check-in may still be Green (CONTEXT.md: Milestone).
func TestCheckinMilestoneDateChangeRecordsSlipAndStaysGreen(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)
	newDate := beta.TargetDate.AddDate(0, 0, 10)

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Beta moves, launch holds.",
		Milestones: []domain.MilestoneChangeInput{
			{MilestoneID: beta.ID, TargetDate: newDate, DateReason: "Waiting on design review."},
		},
	}); err != nil {
		t.Fatalf("Green Check-in with a Milestone slip: %v", err)
	}

	slips, err := h.Service.ListDateSlips(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListDateSlips: %v", err)
	}
	if len(slips) != 1 {
		t.Fatalf("got %d Date Slips, want 1", len(slips))
	}
	s := slips[0]
	if s.MilestoneID != beta.ID || !s.OldDate.Equal(beta.TargetDate) || !s.NewDate.Equal(newDate) || s.Reason != "Waiting on design review." {
		t.Errorf("slip = %+v, want Milestone %d %s → %s with the reason", s, beta.ID, beta.TargetDate, newDate)
	}
	if got := onlyMilestone(t, h, goal.ID); !got.TargetDate.Equal(newDate) {
		t.Errorf("Milestone date = %s, want %s", got.TargetDate, newDate)
	}
}

// A Milestone date change without a reason is rejected.
func TestCheckinMilestoneDateChangeRequiresReason(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Beta moves.",
		Milestones: []domain.MilestoneChangeInput{
			{MilestoneID: beta.ID, TargetDate: beta.TargetDate.AddDate(0, 0, 3)},
		},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if got := onlyMilestone(t, h, goal.ID); !got.TargetDate.Equal(beta.TargetDate) {
		t.Errorf("Milestone date moved to %s despite the rejection", got.TargetDate)
	}
}

// A Check-in may only change Milestones on its own Goal.
func TestCheckinRejectsMilestoneOnAnotherGoal(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	other := h.ActiveGoal(sam, "Ship v3", "Next.")
	foreign := onlyMilestone(t, h, other.ID)

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Fine.",
		Milestones: []domain.MilestoneChangeInput{
			{MilestoneID: foreign.ID, TargetDate: foreign.TargetDate.AddDate(0, 0, 3), DateReason: "Sneaky."},
		},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}
