package domain_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Moving the delivery date in a Check-in records a Date Slip keeping the old and
// new dates and the reason, and the Goal takes the new date (CONTEXT.md: Date
// Slip).
func TestCheckinDeliveryDateChangeRecordsDateSlip(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

// Every date change on an Active Goal is explained: outside a Check-in, a
// Milestone's date and the delivery date can't be moved (so no change skips its
// Date Slip), though a Milestone can still be renamed.
func TestActiveGoalDatesMoveOnlyInACheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)
	ctx := context.Background()

	if _, err := h.Service.EditMilestone(ctx, domain.EditMilestoneInput{
		MilestoneID: beta.ID, Name: "Beta", TargetDate: beta.TargetDate.AddDate(0, 0, 5),
	}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("EditMilestone moving the date on an Active Goal: err = %v, want ErrValidation", err)
	}
	if _, err := h.Service.EditMilestone(ctx, domain.EditMilestoneInput{
		MilestoneID: beta.ID, Name: "Public beta", TargetDate: beta.TargetDate,
	}); err != nil {
		t.Errorf("EditMilestone renaming on an Active Goal: %v", err)
	}
	if _, err := h.Service.MarkGoalDated(ctx, goal.Owner.ID, goal.ID, goal.DeliveryDate.AddDate(0, 1, 0)); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("MarkGoalDated on an Active Goal: err = %v, want ErrValidation", err)
	}
	if _, err := h.Service.MarkGoalOngoing(ctx, goal.Owner.ID, goal.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("MarkGoalOngoing on an Active Goal: err = %v, want ErrValidation", err)
	}
	g, _ := h.Service.ViewGoal(ctx, goal.ID)
	if !g.DeliveryDate.Equal(goal.DeliveryDate) {
		t.Errorf("delivery date moved to %s outside a Check-in", g.DeliveryDate)
	}
}

// Only a slip of neither a Milestone's date nor the back-to-Green date is on
// the delivery date.
func TestDateSlipOnDeliveryDate(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		slip domain.DateSlip
		want bool
	}{
		{"delivery date", domain.DateSlip{}, true},
		{"Milestone", domain.DateSlip{MilestoneID: 7}, false},
		{"back-to-Green date", domain.DateSlip{BackToGreen: true}, false},
	} {
		if got := tc.slip.OnDeliveryDate(); got != tc.want {
			t.Errorf("%s: OnDeliveryDate = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The back-to-Green date's prior dates are those it slipped from within the
// current Run off Green, earliest first: a slip before a Green Check-in ended
// the last run isn't counted, nor is any other date's slip, and there are none
// when the Goal isn't in a run (CONTEXT.md: Date Slip, Run off Green).
func TestBackToGreenPriorDatesCountWithinTheCurrentRun(t *testing.T) {
	t.Parallel()

	day := func(d int) time.Time { return time.Date(2026, 3, d, 0, 0, 0, 0, time.UTC) }
	at := func(d int) time.Time { return time.Date(2026, 2, d, 9, 0, 0, 0, time.UTC) }
	slips := []domain.DateSlip{
		{BackToGreen: true, OldDate: day(1), NewDate: day(5), CreatedAt: at(2)},    // the last run's
		{OldDate: day(10), NewDate: day(20), CreatedAt: at(12)},                    // the delivery date
		{BackToGreen: true, OldDate: day(12), NewDate: day(19), CreatedAt: at(16)}, // out of order
		{MilestoneID: 4, OldDate: day(8), NewDate: day(9), CreatedAt: at(13)},      // a Milestone
		{BackToGreen: true, OldDate: day(6), NewDate: day(12), CreatedAt: at(10)},  // the run's first slip, at its start
	}
	run := domain.RunOffGreen{Since: at(10), Weeks: 2}

	got := domain.BackToGreenPriorDates(slips, run)
	if want := []time.Time{day(6), day(12)}; !slices.EqualFunc(got, want, time.Time.Equal) {
		t.Errorf("BackToGreenPriorDates = %v, want %v", got, want)
	}
	if got := domain.BackToGreenPriorDates(slips, domain.RunOffGreen{}); got != nil {
		t.Errorf("BackToGreenPriorDates with no run = %v, want nil", got)
	}
}

// A date's struck-through prior dates leave out every one on the current date's
// calendar day, whatever its time of day, keeping the rest earliest first: a
// date is never shown struck through beside itself (CONTEXT.md: Date Slip).
func TestStruckDatesLeaveOutTheCurrentDate(t *testing.T) {
	t.Parallel()

	jun := func(d int) time.Time { return time.Date(2026, 6, d, 0, 0, 0, 0, time.UTC) }
	nov := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	for _, tc := range []struct {
		name    string
		current time.Time
		prior   []time.Time
		want    []time.Time
	}{
		{"the latest equals the current", jun(16), []time.Time{jun(9), jun(16)}, []time.Time{jun(9)}},
		{"the only one equals the current", jun(16), []time.Time{jun(16)}, nil},
		{"a middle one equals the current", nov(9), []time.Time{nov(1), nov(9), nov(23)}, []time.Time{nov(1), nov(23)}},
		{"another time of day", jun(16), []time.Time{jun(9), jun(16).Add(15 * time.Hour)}, []time.Time{jun(9)}},
		{"none", jun(16), nil, nil},
		{"all differ", nov(9), []time.Time{nov(23), nov(1), nov(5)}, []time.Time{nov(23), nov(1), nov(5)}},
	} {
		got := domain.StruckDates(tc.current, tc.prior)
		if len(got) != len(tc.want) || !slices.EqualFunc(got, tc.want, time.Time.Equal) {
			t.Errorf("%s: StruckDates = %v, want %v", tc.name, got, tc.want)
		}
	}
}
