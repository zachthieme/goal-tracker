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
	goal, err := s.queries.GetGoal(ctx, in.GoalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Milestone{}, fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return Milestone{}, fmt.Errorf("look up goal: %w", err)
	}
	return s.createMilestone(ctx, in.GoalID, name, in.TargetDate, goal.Goal.Lifecycle == LifecycleActive)
}

// createMilestone inserts a Planned Milestone. addedWhileActive marks one added
// once its Goal was Active, which counts toward Milestone Churn.
func (s *Service) createMilestone(ctx context.Context, goalID int64, name string, targetDate time.Time, addedWhileActive bool) (Milestone, error) {
	var active int64
	if addedWhileActive {
		active = 1
	}
	row, err := s.queries.CreateMilestone(ctx, db.CreateMilestoneParams{
		GoalID:           goalID,
		Name:             name,
		TargetDate:       targetDate.Format(dateFormat),
		CreatedAt:        s.clock.Now().Format(timeFormat),
		AddedWhileActive: active,
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

// EditMilestone changes a Milestone's name and date. Both are required. The
// date can only change here while the Goal is Proposed; after that it moves in a
// Check-in as a Date Slip.
func (s *Service) EditMilestone(ctx context.Context, in EditMilestoneInput) (Milestone, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Milestone{}, fmt.Errorf("%w: a Milestone needs a name", ErrValidation)
	}
	if in.TargetDate.IsZero() {
		return Milestone{}, fmt.Errorf("%w: a Milestone needs a date", ErrValidation)
	}
	current, err := s.queries.GetMilestone(ctx, in.MilestoneID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Milestone{}, fmt.Errorf("%w: milestone %d", ErrNotFound, in.MilestoneID)
		}
		return Milestone{}, fmt.Errorf("look up milestone: %w", err)
	}
	// Once the Goal is past Proposed its Milestone dates move only in a
	// Check-in, which records the Date Slip and its reason (CONTEXT.md: Date
	// Slip).
	if in.TargetDate.Format(dateFormat) != current.TargetDate {
		goal, err := s.queries.GetGoal(ctx, current.GoalID)
		if err != nil {
			return Milestone{}, fmt.Errorf("look up goal: %w", err)
		}
		if goal.Goal.Lifecycle != LifecycleProposed {
			return Milestone{}, fmt.Errorf("%w: change a Milestone's date in a Check-in, with a reason", ErrValidation)
		}
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
// Check-in carries Date Slips and Milestone changes). Only a Planned Milestone
// can change. TargetDate moves its date, recording a Date Slip that needs
// DateReason; the zero time, or its current date, leaves it unchanged. Status
// marks it MilestoneDone or MilestoneRemoved (which needs RemovedReason); empty
// or MilestonePlanned leaves it Planned.
type MilestoneChangeInput struct {
	MilestoneID   int64
	TargetDate    time.Time
	DateReason    string
	Status        string
	RemovedReason string
}

// NewMilestoneInput is a Milestone added in a Check-in. Name and date are
// required.
type NewMilestoneInput struct {
	Name       string
	TargetDate time.Time
}

// milestoneStatusChange is a validated Done or Removed marking waiting to be
// written with its Check-in.
type milestoneStatusChange struct {
	milestoneID   int64
	status        string
	removedReason string
}

// planMilestoneStatus validates a Check-in's new status for a Planned Milestone.
// Empty or Planned is no change (nil); Removed needs a reason.
func planMilestoneStatus(m db.Milestone, status, removedReason string) (*milestoneStatusChange, error) {
	switch status {
	case "", MilestonePlanned:
		return nil, nil
	case MilestoneDone:
		return &milestoneStatusChange{milestoneID: m.ID, status: MilestoneDone}, nil
	case MilestoneRemoved:
		reason := strings.TrimSpace(removedReason)
		if reason == "" {
			return nil, fmt.Errorf("%w: removing Milestone %q needs a reason", ErrValidation, m.Name)
		}
		return &milestoneStatusChange{milestoneID: m.ID, status: MilestoneRemoved, removedReason: reason}, nil
	default:
		return nil, fmt.Errorf("%w: a Milestone must be %q, %q, or %q", ErrValidation, MilestonePlanned, MilestoneDone, MilestoneRemoved)
	}
}

// milestonePlan is a Check-in's validated Milestone changes: the Date Slips
// they make, the Milestones they mark Done or Removed, the Milestones they add,
// and the Goal's Milestones as they will stand once applied.
type milestonePlan struct {
	slips     []slipPlan
	statuses  []milestoneStatusChange
	added     []NewMilestoneInput
	resulting []db.Milestone
}

// planMilestoneChanges validates a Check-in's Milestone changes against the
// Goal's Milestones — each must name a Milestone on this Goal, at most once —
// and the Milestones it adds, each of which needs a name and a date.
func (s *Service) planMilestoneChanges(ctx context.Context, goalID int64, changes []MilestoneChangeInput, added []NewMilestoneInput) (milestonePlan, error) {
	rows, err := s.queries.ListMilestones(ctx, goalID)
	if err != nil {
		return milestonePlan{}, fmt.Errorf("list milestones: %w", err)
	}
	index := make(map[int64]int, len(rows))
	for i, r := range rows {
		index[r.ID] = i
	}
	seen := make(map[int64]bool, len(changes))
	plan := milestonePlan{resulting: rows}
	for _, ch := range changes {
		i, ok := index[ch.MilestoneID]
		if !ok {
			return milestonePlan{}, fmt.Errorf("%w: milestone %d is not on this Goal", ErrValidation, ch.MilestoneID)
		}
		m := &plan.resulting[i]
		if seen[m.ID] {
			return milestonePlan{}, fmt.Errorf("%w: Milestone %q is changed twice", ErrValidation, m.Name)
		}
		seen[m.ID] = true
		if m.Status != MilestonePlanned {
			return milestonePlan{}, fmt.Errorf("%w: Milestone %q is already %s", ErrValidation, m.Name, m.Status)
		}
		slip, err := planMilestoneSlip(*m, ch.TargetDate, ch.DateReason)
		if err != nil {
			return milestonePlan{}, err
		}
		if slip != nil {
			plan.slips = append(plan.slips, *slip)
			m.TargetDate = slip.newDate.Format(dateFormat)
		}
		change, err := planMilestoneStatus(*m, ch.Status, ch.RemovedReason)
		if err != nil {
			return milestonePlan{}, err
		}
		if change != nil {
			plan.statuses = append(plan.statuses, *change)
			m.Status = change.status
		}
	}
	for _, a := range added {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return milestonePlan{}, fmt.Errorf("%w: a new Milestone needs a name", ErrValidation)
		}
		if a.TargetDate.IsZero() {
			return milestonePlan{}, fmt.Errorf("%w: new Milestone %q needs a date", ErrValidation, name)
		}
		plan.added = append(plan.added, NewMilestoneInput{Name: name, TargetDate: a.TargetDate})
		plan.resulting = append(plan.resulting, db.Milestone{Name: name, TargetDate: a.TargetDate.Format(dateFormat), Status: MilestonePlanned})
	}
	return plan, nil
}

// apply writes the plan's Date Slips, Done and Removed markings, and added
// Milestones against checkinID. Milestones added in a Check-in are added while
// the Goal is Active, so they count toward its Milestone Churn.
func (p milestonePlan) apply(ctx context.Context, tx *Service, goalID, checkinID int64) error {
	for _, slip := range p.slips {
		if err := tx.recordSlip(ctx, goalID, checkinID, slip); err != nil {
			return err
		}
	}
	for _, change := range p.statuses {
		if err := tx.recordMilestoneStatus(ctx, change); err != nil {
			return err
		}
	}
	for _, a := range p.added {
		if _, err := tx.createMilestone(ctx, goalID, a.Name, a.TargetDate, true); err != nil {
			return err
		}
	}
	return nil
}

// recordMilestoneStatus writes a Check-in's Done or Removed marking.
func (s *Service) recordMilestoneStatus(ctx context.Context, c milestoneStatusChange) error {
	if err := s.queries.SetMilestoneStatus(ctx, db.SetMilestoneStatusParams{
		Status:        c.status,
		RemovedReason: c.removedReason,
		ID:            c.milestoneID,
	}); err != nil {
		return fmt.Errorf("set milestone status: %w", err)
	}
	return nil
}

// MilestoneChurn returns the number of Milestones added or removed on a Goal
// since it became Active — a scope-creep and capacity signal read alongside its
// Date Slips (CONTEXT.md: Milestone Churn). Marking a Milestone Done is not
// churn.
func (s *Service) MilestoneChurn(ctx context.Context, goalID int64) (int, error) {
	n, err := s.queries.CountMilestoneChurn(ctx, goalID)
	if err != nil {
		return 0, fmt.Errorf("count milestone churn: %w", err)
	}
	return int(n), nil
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
