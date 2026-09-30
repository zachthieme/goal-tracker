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
	CreatedAt  time.Time
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
		ID:         m.ID,
		GoalID:     m.GoalID,
		Name:       m.Name,
		TargetDate: targetDate,
		CreatedAt:  createdAt,
	}
}
