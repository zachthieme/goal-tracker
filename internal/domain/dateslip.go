package domain

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// DateSlip is a recorded change to a Goal's delivery date, a Milestone's date,
// or a Path to Green's back-to-Green date, always with a reason (CONTEXT.md:
// Date Slip). The full history is kept, each slip keeping the old and new
// dates and the Check-in that recorded it.
type DateSlip struct {
	ID        int64
	GoalID    int64
	CheckinID int64
	// MilestoneID is the Milestone whose date moved, or 0 when the slip moved the
	// Goal's delivery date or its back-to-Green date.
	MilestoneID int64
	// BackToGreen marks a slip of the Path to Green's back-to-Green date: one
	// Check-in moved it later than the one before had it.
	BackToGreen bool
	OldDate     time.Time
	NewDate     time.Time
	Reason      string
	CreatedAt   time.Time
}

// OnDeliveryDate reports whether the slip moved the Goal's delivery date,
// rather than a Milestone's date or the back-to-Green date.
func (d DateSlip) OnDeliveryDate() bool { return d.MilestoneID == 0 && !d.BackToGreen }

// priorDatesOf returns the dates the delivery date (milestoneID 0) or a
// Milestone held before each of slips, a Goal's Date Slips earliest first, in
// that order: what its date shows struck through (CONTEXT.md: Date Slip). A
// slip of the back-to-Green date is neither; see BackToGreenPriorDates.
func priorDatesOf(slips []DateSlip, milestoneID int64) []time.Time {
	var out []time.Time
	for _, d := range slips {
		if d.MilestoneID == milestoneID && !d.BackToGreen {
			out = append(out, d.OldDate)
		}
	}
	return out
}

// BackToGreenPriorDates returns the dates the back-to-Green date slipped from
// within run, earliest first: the OldDate of each back-to-Green slip recorded
// since the run began. A new Run off Green starts the count afresh, and there
// is none when the Goal isn't in a run (CONTEXT.md: Date Slip, Run off Green).
func BackToGreenPriorDates(slips []DateSlip, run RunOffGreen) []time.Time {
	if !run.Running() {
		return nil
	}
	var inRun []DateSlip
	for _, d := range slips {
		if d.BackToGreen && !d.CreatedAt.Before(run.Since) {
			inRun = append(inRun, d)
		}
	}
	slices.SortStableFunc(inRun, func(a, b DateSlip) int { return a.CreatedAt.Compare(b.CreatedAt) })
	out := make([]time.Time, 0, len(inRun))
	for _, d := range inRun {
		out = append(out, d.OldDate)
	}
	return out
}

// StruckDates returns the prior dates a date shows struck through, earliest
// first: prior with every date on current's calendar day left out, so a date
// is never shown struck through beside itself (CONTEXT.md: Date Slip).
func StruckDates(current time.Time, prior []time.Time) []time.Time {
	var out []time.Time
	for _, d := range prior {
		if d.Format(dateFormat) != current.Format(dateFormat) {
			out = append(out, d)
		}
	}
	return out
}

// SlipsWords is how many times a date slipped, in the words every slip count
// shows in: "1 slip", "3 slips" (CONTEXT.md: Date Slip).
func SlipsWords(n int) string {
	if n == 1 {
		return "1 slip"
	}
	return fmt.Sprintf("%d slips", n)
}

// slipPlan is a validated date change waiting to be written with its Check-in.
type slipPlan struct {
	milestoneID int64 // 0 for the delivery date or the back-to-Green date
	backToGreen bool  // the back-to-Green date, which no stored date follows
	oldDate     time.Time
	newDate     time.Time
	reason      string
}

// later reports whether the slip moves the date later.
func (p slipPlan) later() bool { return p.newDate.After(p.oldDate) }

// planDeliverySlip validates a Check-in's delivery date against the Goal's
// current one. A zero or unchanged date is no slip (nil). A change needs a
// reason, and only a Dated Goal has a delivery date to move.
func planDeliverySlip(goal Goal, newDate time.Time, reason string) (*slipPlan, error) {
	var current string
	if !goal.DeliveryDate.IsZero() {
		current = goal.DeliveryDate.Format(dateFormat)
	}
	if !newDate.IsZero() && newDate.Format(dateFormat) != current && goal.Kind != GoalDated {
		return nil, Refuse(ErrValidation, "only a Dated Goal has a delivery date to change")
	}
	return planSlip(0, "the delivery date", CheckinInputDeliveryDateReason, current, newDate, reason)
}

// planMilestoneSlip validates a Check-in's new date for a Milestone. A zero or
// unchanged date is no slip (nil); a change needs a reason.
func planMilestoneSlip(m Milestone, newDate time.Time, reason string) (*slipPlan, error) {
	return planSlip(m.ID, fmt.Sprintf("the date of Milestone %q", m.Name), CheckinMilestoneInput(m.ID, "date_reason"), m.TargetDate.Format(dateFormat), newDate, reason)
}

// planBackToGreenSlip validates a Check-in's back-to-Green date against the
// latest Check-in's, last. Only a later date is a slip, and it needs a reason;
// an earlier or unchanged one, or a first one (last is zero), is none (nil).
func planBackToGreenSlip(last, newDate time.Time, reason string) (*slipPlan, error) {
	if last.IsZero() || newDate.IsZero() {
		return nil, nil
	}
	// Dates are compared as the calendar dates stored, whatever the time of
	// day newDate carries.
	newDate, err := time.Parse(dateFormat, newDate.Format(dateFormat))
	if err != nil {
		return nil, fmt.Errorf("parse the back-to-Green date: %w", err)
	}
	if !newDate.After(last) {
		return nil, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, inputError(CheckinInputPathTargetDateReason, "moving the back-to-Green date later needs a reason")
	}
	return &slipPlan{backToGreen: true, oldDate: last, newDate: newDate, reason: reason}, nil
}

// planSlip validates moving a stored date (oldDate, in dateFormat) to newDate.
// A zero or unchanged newDate is no slip (nil); a change needs a reason. what
// names the date for the error message, and reasonInput the Check-in input its
// reason is given in.
func planSlip(milestoneID int64, what, reasonInput, oldDate string, newDate time.Time, reason string) (*slipPlan, error) {
	if newDate.IsZero() || newDate.Format(dateFormat) == oldDate {
		return nil, nil
	}
	old, err := time.Parse(dateFormat, oldDate)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", what, err)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, inputError(reasonInput, "changing %s needs a reason", what)
	}
	return &slipPlan{milestoneID: milestoneID, oldDate: old, newDate: newDate, reason: reason}, nil
}

// recordSlip writes a Date Slip for checkinID and moves the date it changes. A
// back-to-Green slip moves no stored date: the Check-in carries the new one.
func (s *Service) recordSlip(ctx context.Context, goalID, checkinID int64, p slipPlan) error {
	var milestoneID *int64
	if p.milestoneID != 0 {
		id := p.milestoneID
		milestoneID = &id
	}
	backToGreen := int64(0)
	if p.backToGreen {
		backToGreen = 1
	}
	if _, err := s.queries.CreateDateSlip(ctx, db.CreateDateSlipParams{
		GoalID:      goalID,
		CheckinID:   checkinID,
		MilestoneID: milestoneID,
		OldDate:     p.oldDate.Format(dateFormat),
		NewDate:     p.newDate.Format(dateFormat),
		Reason:      p.reason,
		CreatedAt:   s.clock.Now().Format(timeFormat),
		BackToGreen: backToGreen,
	}); err != nil {
		return fmt.Errorf("create date slip: %w", err)
	}
	if p.backToGreen {
		return nil
	}
	if p.milestoneID != 0 {
		if err := s.queries.SetMilestoneTargetDate(ctx, db.SetMilestoneTargetDateParams{
			TargetDate: p.newDate.Format(dateFormat),
			ID:         p.milestoneID,
		}); err != nil {
			return fmt.Errorf("set milestone date: %w", err)
		}
		return nil
	}
	if err := s.queries.SetGoalDeliveryDate(ctx, db.SetGoalDeliveryDateParams{
		DeliveryDate: p.newDate.Format(dateFormat),
		ID:           goalID,
	}); err != nil {
		return fmt.Errorf("set delivery date: %w", err)
	}
	return nil
}

// ListDateSlips returns a Goal's Date Slips, earliest first, covering its
// delivery date, its Milestones' dates and its back-to-Green date. Their count is the Goal's slip count.
func (s *Service) ListDateSlips(ctx context.Context, goalID int64) ([]DateSlip, error) {
	rows, err := s.queries.ListDateSlips(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list date slips: %w", err)
	}
	out := make([]DateSlip, 0, len(rows))
	for _, r := range rows {
		out = append(out, dateSlipFromRow(r))
	}
	return out, nil
}

func dateSlipFromRow(r db.DateSlip) DateSlip {
	oldDate, _ := time.Parse(dateFormat, r.OldDate)
	newDate, _ := time.Parse(dateFormat, r.NewDate)
	createdAt, _ := time.Parse(timeFormat, r.CreatedAt)
	var milestoneID int64
	if r.MilestoneID != nil {
		milestoneID = *r.MilestoneID
	}
	return DateSlip{
		ID:          r.ID,
		GoalID:      r.GoalID,
		CheckinID:   r.CheckinID,
		MilestoneID: milestoneID,
		BackToGreen: r.BackToGreen != 0,
		OldDate:     oldDate,
		NewDate:     newDate,
		Reason:      r.Reason,
		CreatedAt:   createdAt,
	}
}
