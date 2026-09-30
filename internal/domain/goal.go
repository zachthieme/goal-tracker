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

// Goal is the single unit of work being tracked (CONTEXT.md: Goal).
type Goal struct {
	ID        int64
	Title     string
	SoWhat    string
	Owner     Account
	Lifecycle string
	CreatedAt time.Time
}

// Lifecycle values a Goal can be in (CONTEXT.md: Lifecycle). Only Proposed is
// reachable in the walking skeleton.
const (
	LifecycleProposed = "Proposed"
)

// ErrValidation is returned when a command's input is not acceptable, e.g. a
// Goal created without a title or a So What.
var ErrValidation = errors.New("validation failed")

// CreateGoalInput is the create-Goal command's input.
type CreateGoalInput struct {
	Title   string
	SoWhat  string
	OwnerID int64
}

// CreateGoal creates a Proposed Goal owned by OwnerID. Title and So What are
// required (CONTEXT.md: So What is required when the Goal is created).
func (s *Service) CreateGoal(ctx context.Context, in CreateGoalInput) (Goal, error) {
	title := strings.TrimSpace(in.Title)
	soWhat := strings.TrimSpace(in.SoWhat)
	if title == "" {
		return Goal{}, fmt.Errorf("%w: a Goal needs a title", ErrValidation)
	}
	if soWhat == "" {
		return Goal{}, fmt.Errorf("%w: a Goal needs a So What", ErrValidation)
	}

	owner, err := s.queries.GetAccount(ctx, in.OwnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("%w: owner does not exist", ErrValidation)
		}
		return Goal{}, fmt.Errorf("look up owner: %w", err)
	}

	now := s.clock.Now()
	row, err := s.queries.CreateGoal(ctx, db.CreateGoalParams{
		Title:     title,
		SoWhat:    soWhat,
		OwnerID:   in.OwnerID,
		Lifecycle: LifecycleProposed,
		CreatedAt: now.Format(timeFormat),
	})
	if err != nil {
		return Goal{}, fmt.Errorf("create goal: %w", err)
	}

	return Goal{
		ID:        row.ID,
		Title:     row.Title,
		SoWhat:    row.SoWhat,
		Owner:     accountFromRow(owner),
		Lifecycle: row.Lifecycle,
		CreatedAt: now,
	}, nil
}

// ViewGoal returns the Goal with the given id, with its Owner resolved. This is
// the Goal-view query. It returns ErrNotFound if no such Goal exists.
func (s *Service) ViewGoal(ctx context.Context, id int64) (Goal, error) {
	row, err := s.queries.GetGoal(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("%w: goal %d", ErrNotFound, id)
		}
		return Goal{}, fmt.Errorf("get goal: %w", err)
	}
	return goalFromRow(row.Goal, row.Account), nil
}

// ListGoals returns every Goal, newest first, each with its Owner resolved.
func (s *Service) ListGoals(ctx context.Context) ([]Goal, error) {
	rows, err := s.queries.ListGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	goals := make([]Goal, 0, len(rows))
	for _, r := range rows {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
	}
	return goals, nil
}

func goalFromRow(g db.Goal, owner db.Account) Goal {
	createdAt, _ := time.Parse(timeFormat, g.CreatedAt)
	return Goal{
		ID:        g.ID,
		Title:     g.Title,
		SoWhat:    g.SoWhat,
		Owner:     accountFromRow(owner),
		Lifecycle: g.Lifecycle,
		CreatedAt: createdAt,
	}
}
