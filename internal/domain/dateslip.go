package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// DateSlip is a recorded change to a Goal's delivery date or a Milestone's date,
// always with a reason (CONTEXT.md: Date Slip). The full history is kept, each
// slip keeping the old and new dates and the Check-in that recorded it.
type DateSlip struct {
	ID        int64
	GoalID    int64
	CheckinID int64
	// MilestoneID is the Milestone whose date moved, or 0 when the slip moved the
	// Goal's delivery date.
	MilestoneID int64
	OldDate     time.Time
	NewDate     time.Time
	Reason      string
	CreatedAt   time.Time
}

// slipPlan is a validated date change waiting to be written with its Check-in.
type slipPlan struct {
	milestoneID int64 // 0 for the delivery date
	oldDate     time.Time
	newDate     time.Time
	reason      string
}

// later reports whether the slip moves the date later.
func (p slipPlan) later() bool { return p.newDate.After(p.oldDate) }

// planDeliverySlip validates a Check-in's delivery date against the Goal's
// current one. A zero or unchanged date is no slip (nil). A change needs a
// reason, and only a Dated Goal has a delivery date to move.
func planDeliverySlip(goal db.Goal, newDate time.Time, reason string) (*slipPlan, error) {
	if !newDate.IsZero() && newDate.Format(dateFormat) != goal.DeliveryDate && goal.Kind != GoalDated {
		return nil, fmt.Errorf("%w: only a Dated Goal has a delivery date to change", ErrValidation)
	}
	return planSlip(0, "the delivery date", goal.DeliveryDate, newDate, reason)
}

// planMilestoneSlip validates a Check-in's new date for a Milestone. A zero or
// unchanged date is no slip (nil); a change needs a reason.
func planMilestoneSlip(m db.Milestone, newDate time.Time, reason string) (*slipPlan, error) {
	return planSlip(m.ID, fmt.Sprintf("the date of Milestone %q", m.Name), m.TargetDate, newDate, reason)
}

// planSlip validates moving a stored date (oldDate, in dateFormat) to newDate.
// A zero or unchanged newDate is no slip (nil); a change needs a reason. what
// names the date for the error message.
func planSlip(milestoneID int64, what, oldDate string, newDate time.Time, reason string) (*slipPlan, error) {
	if newDate.IsZero() || newDate.Format(dateFormat) == oldDate {
		return nil, nil
	}
	old, err := time.Parse(dateFormat, oldDate)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", what, err)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: changing %s needs a reason", ErrValidation, what)
	}
	return &slipPlan{milestoneID: milestoneID, oldDate: old, newDate: newDate, reason: reason}, nil
}

// recordSlip writes a Date Slip for checkinID and moves the date it changes.
func (s *Service) recordSlip(ctx context.Context, goalID, checkinID int64, p slipPlan) error {
	var milestoneID *int64
	if p.milestoneID != 0 {
		id := p.milestoneID
		milestoneID = &id
	}
	if _, err := s.queries.CreateDateSlip(ctx, db.CreateDateSlipParams{
		GoalID:      goalID,
		CheckinID:   checkinID,
		MilestoneID: milestoneID,
		OldDate:     p.oldDate.Format(dateFormat),
		NewDate:     p.newDate.Format(dateFormat),
		Reason:      p.reason,
		CreatedAt:   s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("create date slip: %w", err)
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

// ListDateSlips returns a Goal's Date Slips, earliest first, covering both its
// delivery date and its Milestones' dates. Their count is the Goal's slip count.
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
		OldDate:     oldDate,
		NewDate:     newDate,
		Reason:      r.Reason,
		CreatedAt:   createdAt,
	}
}
