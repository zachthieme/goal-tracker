package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Milestone is a dated checkpoint within a Goal (CONTEXT.md: Milestone).
type Milestone struct {
	ID         int64
	GoalID     int64
	Name       string
	TargetDate time.Time
	// Status is MilestonePlanned until a Check-in marks it MilestoneDone or
	// MilestoneRemoved; RemovedReason explains a removal.
	Status        string
	RemovedReason string
	CreatedAt     time.Time
}

// Milestone statuses. A Planned Milestone past its date is overdue, and Green is
// rejected while one is.
const (
	MilestonePlanned = "Planned"
	MilestoneDone    = "Done"
	MilestoneRemoved = "Removed"
)

// overdueMilestone returns the first of milestones that is overdue — Planned and
// past its date — as of today (a dateFormat date), or ok false when none is.
func overdueMilestone(milestones []db.Milestone, today string) (db.Milestone, bool) {
	for _, m := range milestones {
		if m.Status == MilestonePlanned && m.TargetDate < today {
			return m, true
		}
	}
	return db.Milestone{}, false
}

// rejectGreenWhileOverdue refuses a Green Health while any of the Goal's
// Milestones, as they will stand after the Check-in, is overdue: a slip is
// never hidden behind a Green (CONTEXT.md: Date Slip).
func (s *Service) rejectGreenWhileOverdue(health string, milestones []db.Milestone) error {
	if health != HealthGreen {
		return nil
	}
	if m, ok := overdueMilestone(milestones, s.clock.Now().Format(dateFormat)); ok {
		return fmt.Errorf("%w: Milestone %q is overdue (due %s), so the Goal can't be Green", ErrValidation, m.Name, m.TargetDate)
	}
	return nil
}

// AddMilestoneInput is the add-Milestone command's input.
type AddMilestoneInput struct {
	GoalID     int64
	Name       string
	TargetDate time.Time
}

// AddMilestone adds a Milestone to a Goal. Name and target date are required.
func (s *Service) AddMilestone(ctx context.Context, in AddMilestoneInput) (Milestone, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Milestone{}, fmt.Errorf("%w: a Milestone needs a name", ErrValidation)
	}
	if in.TargetDate.IsZero() {
		return Milestone{}, fmt.Errorf("%w: a Milestone needs a date", ErrValidation)
	}
	if _, err := s.queries.GetGoal(ctx, in.GoalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Milestone{}, fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return Milestone{}, fmt.Errorf("look up goal: %w", err)
	}

	row, err := s.queries.CreateMilestone(ctx, db.CreateMilestoneParams{
		GoalID:     in.GoalID,
		Name:       name,
		TargetDate: in.TargetDate.Format(dateFormat),
		CreatedAt:  s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return Milestone{}, fmt.Errorf("create milestone: %w", err)
	}
	return milestoneFromRow(row), nil
}

// EditMilestoneInput is the edit-Milestone command's input.
type EditMilestoneInput struct {
	MilestoneID int64
	Name        string
	TargetDate  time.Time
}

// EditMilestone changes a Milestone's name and date. Both are required.
func (s *Service) EditMilestone(ctx context.Context, in EditMilestoneInput) (Milestone, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Milestone{}, fmt.Errorf("%w: a Milestone needs a name", ErrValidation)
	}
	if in.TargetDate.IsZero() {
		return Milestone{}, fmt.Errorf("%w: a Milestone needs a date", ErrValidation)
	}
	if _, err := s.queries.GetMilestone(ctx, in.MilestoneID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Milestone{}, fmt.Errorf("%w: milestone %d", ErrNotFound, in.MilestoneID)
		}
		return Milestone{}, fmt.Errorf("look up milestone: %w", err)
	}

	row, err := s.queries.UpdateMilestone(ctx, db.UpdateMilestoneParams{
		Name:       name,
		TargetDate: in.TargetDate.Format(dateFormat),
		ID:         in.MilestoneID,
	})
	if err != nil {
		return Milestone{}, fmt.Errorf("update milestone: %w", err)
	}
	return milestoneFromRow(row), nil
}

// MilestoneChangeInput is one Milestone's change in a Check-in (CONTEXT.md:
// Check-in carries Date Slips and Milestone changes). TargetDate moves the
// Milestone's date, recording a Date Slip that needs DateReason; the zero time,
// or the Milestone's current date, leaves it unchanged.
type MilestoneChangeInput struct {
	MilestoneID int64
	TargetDate  time.Time
	DateReason  string
}

// milestonePlan is a Check-in's validated Milestone changes: the Date Slips
// they make, and the Goal's Milestones as they will stand once applied.
type milestonePlan struct {
	slips []slipPlan
	after []db.Milestone
}

// planMilestoneChanges validates a Check-in's Milestone changes against the
// Goal's Milestones: each must name a Milestone on this Goal, at most once.
func (s *Service) planMilestoneChanges(ctx context.Context, goalID int64, changes []MilestoneChangeInput) (milestonePlan, error) {
	rows, err := s.queries.ListMilestones(ctx, goalID)
	if err != nil {
		return milestonePlan{}, fmt.Errorf("list milestones: %w", err)
	}
	index := make(map[int64]int, len(rows))
	for i, r := range rows {
		index[r.ID] = i
	}
	seen := make(map[int64]bool, len(changes))
	plan := milestonePlan{after: rows}
	for _, ch := range changes {
		i, ok := index[ch.MilestoneID]
		if !ok {
			return milestonePlan{}, fmt.Errorf("%w: milestone %d is not on this Goal", ErrValidation, ch.MilestoneID)
		}
		m := &plan.after[i]
		if seen[m.ID] {
			return milestonePlan{}, fmt.Errorf("%w: Milestone %q is changed twice", ErrValidation, m.Name)
		}
		seen[m.ID] = true
		slip, err := planMilestoneSlip(*m, ch.TargetDate, ch.DateReason)
		if err != nil {
			return milestonePlan{}, err
		}
		if slip != nil {
			plan.slips = append(plan.slips, *slip)
			m.TargetDate = slip.newDate.Format(dateFormat)
		}
	}
	return plan, nil
}

// ListMilestones returns a Goal's Milestones, earliest target date first.
func (s *Service) ListMilestones(ctx context.Context, goalID int64) ([]Milestone, error) {
	rows, err := s.queries.ListMilestones(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	out := make([]Milestone, 0, len(rows))
	for _, r := range rows {
		out = append(out, milestoneFromRow(r))
	}
	return out, nil
}

func milestoneFromRow(m db.Milestone) Milestone {
	targetDate, _ := time.Parse(dateFormat, m.TargetDate)
	createdAt, _ := time.Parse(timeFormat, m.CreatedAt)
	return Milestone{
		ID:            m.ID,
		GoalID:        m.GoalID,
		Name:          m.Name,
		TargetDate:    targetDate,
		Status:        m.Status,
		RemovedReason: m.RemovedReason,
		CreatedAt:     createdAt,
	}
}
