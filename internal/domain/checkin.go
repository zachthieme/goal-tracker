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

// Checkin is an Owner's routine update to one Goal: its Health, a short status,
// and — when the Health is Yellow or Red — a Path to Green (CONTEXT.md:
// Check-in). Check-ins are immutable; a Goal's current Health, status, and Path
// to Green are its latest Check-in's. Each records both its author and the Owner
// it was written for (CONTEXT.md: Delegate).
type Checkin struct {
	ID          int64
	GoalID      int64
	Author      Account
	Owner       Account
	Health      string
	Status      string
	PathToGreen string
	// PathTargetDate is the target date for being back to Green; the zero time
	// when there is no Path to Green (CONTEXT.md: Path to Green).
	PathTargetDate time.Time
	// Explanation is the Owner's reason their Health differs from the Rolled-up
	// Health (ADR-0003). Empty when the two match, or when there is nothing to
	// roll up.
	Explanation string
	CreatedAt   time.Time
}

// Health values a Check-in can set (CONTEXT.md: Health). Green needs no Path to
// Green; Yellow and Red require one.
const (
	HealthGreen  = "Green"
	HealthYellow = "Yellow"
	HealthRed    = "Red"
)

func validHealth(h string) bool {
	return h == HealthGreen || h == HealthYellow || h == HealthRed
}

// needsPathToGreen reports whether a Health requires a Path to Green: Yellow and
// Red do, Green does not (CONTEXT.md: Path to Green).
func needsPathToGreen(health string) bool {
	return health == HealthYellow || health == HealthRed
}

// SubmitCheckinInput is the submit-a-Check-in command's input. PathToGreen and
// PathTargetDate are required only when Health is Yellow or Red.
type SubmitCheckinInput struct {
	GoalID         int64
	AuthorID       int64
	Health         string
	Status         string
	PathToGreen    string
	PathTargetDate time.Time
	// Explanation is required only when the Health differs from the Goal's
	// Rolled-up Health (ADR-0003).
	Explanation string
}

// SubmitCheckin records a Check-in on a Goal. The Goal must be Active — a
// Proposed Goal has no Health and can't be checked in on (CONTEXT.md: Health is
// how an Active Goal is tracking). Health must be Green, Yellow, or Red and the
// status is required. Yellow or Red requires a Path to Green with text and a
// target date. Only the Goal's Owner may submit; the Check-in records its author
// and the Owner it was written for. Check-ins are immutable, so this always
// inserts a new one.
func (s *Service) SubmitCheckin(ctx context.Context, in SubmitCheckinInput) (Checkin, error) {
	goal, err := s.queries.GetGoal(ctx, in.GoalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Checkin{}, fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return Checkin{}, fmt.Errorf("look up goal: %w", err)
	}
	if goal.Goal.Lifecycle != LifecycleActive {
		return Checkin{}, fmt.Errorf("%w: only an Active Goal can be checked in on", ErrValidation)
	}
	// The Owner writes a Goal's Check-ins, as may any Delegate the Owner has
	// authorized; the Check-in records its author and still credits the Owner it
	// speaks for (CONTEXT.md: Delegate).
	if err := s.authorizeCheckinAuthor(ctx, goal.Goal.ID, goal.Goal.OwnerID, in.AuthorID); err != nil {
		return Checkin{}, err
	}

	status := strings.TrimSpace(in.Status)
	path := strings.TrimSpace(in.PathToGreen)
	explanation := strings.TrimSpace(in.Explanation)
	if err := validateCheckin(in.Health, status, path, in.PathTargetDate); err != nil {
		return Checkin{}, err
	}

	// The Owner has to explain why their Health differs from the Rolled-up
	// Health — the worst among the Goal's Active children (ADR-0003). When there
	// is nothing to roll up, or the two match, no explanation is required and any
	// stray one is dropped.
	explanation, err = s.resolveRollupExplanation(ctx, goal.Goal.ID, in.Health, explanation)
	if err != nil {
		return Checkin{}, err
	}

	pathTargetDate := ""
	if needsPathToGreen(in.Health) {
		pathTargetDate = in.PathTargetDate.Format(dateFormat)
	} else {
		// A Green Check-in carries no Path to Green.
		path = ""
	}

	return s.createCheckin(ctx, goal.Goal.ID, in.AuthorID, goal.Goal.OwnerID, in.Health, status, path, pathTargetDate, explanation)
}

// SubmitNoChangeCheckin records a Check-in that repeats the Goal's previous
// values in one click (CONTEXT.md: a Check-in should take minutes). It needs a
// previous Check-in to repeat, so it is rejected on a Goal with none. The new
// Check-in is still immutable and records its own author and Owner.
func (s *Service) SubmitNoChangeCheckin(ctx context.Context, goalID, authorID int64) (Checkin, error) {
	goal, err := s.queries.GetGoal(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Checkin{}, fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return Checkin{}, fmt.Errorf("look up goal: %w", err)
	}
	if goal.Goal.Lifecycle != LifecycleActive {
		return Checkin{}, fmt.Errorf("%w: only an Active Goal can be checked in on", ErrValidation)
	}
	if err := s.authorizeCheckinAuthor(ctx, goal.Goal.ID, goal.Goal.OwnerID, authorID); err != nil {
		return Checkin{}, err
	}

	prev, err := s.queries.GetLatestCheckin(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Checkin{}, fmt.Errorf("%w: there is no previous Check-in to repeat", ErrValidation)
		}
		return Checkin{}, fmt.Errorf("look up previous checkin: %w", err)
	}
	// A no-change repeat still cannot violate the Rolled-up Health rule: if the
	// roll-up has since changed so the repeated Health would differ from it and
	// the previous Check-in carried no explanation, refuse it so the Owner uses
	// the full form to explain (ADR-0003).
	explanation, err := s.resolveRollupExplanation(ctx, goalID, prev.Health, prev.Explanation)
	if err != nil {
		return Checkin{}, fmt.Errorf("%w — submit a Check-in to explain", err)
	}
	return s.createCheckin(ctx, goalID, authorID, goal.Goal.OwnerID, prev.Health, prev.Status, prev.PathToGreen, prev.PathTargetDate, explanation)
}

// authorizeCheckinAuthor allows a Check-in to be written only by the Goal's
// Owner or a Delegate the Owner has authorized; anyone else is refused
// (CONTEXT.md: Delegate; acceptance: Non-Delegates can't submit Check-ins).
func (s *Service) authorizeCheckinAuthor(ctx context.Context, goalID, ownerID, authorID int64) error {
	if authorID == ownerID {
		return nil
	}
	ok, err := s.isDelegate(ctx, goalID, authorID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: only the Owner or a Delegate may submit a Check-in", ErrNotAuthorized)
	}
	return nil
}

// resolveRollupExplanation enforces ADR-0003's explanation rule for a Check-in
// setting health on goalID: when the Goal's Rolled-up Health is present and
// differs from health, a non-empty explanation is required; otherwise none is,
// and any stray explanation is dropped. It returns the explanation to store.
func (s *Service) resolveRollupExplanation(ctx context.Context, goalID int64, health, explanation string) (string, error) {
	rollup, err := s.RolledUpHealth(ctx, goalID)
	if err != nil {
		return "", err
	}
	if rollup.Present && rollup.Health != health {
		if explanation == "" {
			return "", fmt.Errorf("%w: this Health differs from the Rolled-up Health (%s); explain why", ErrValidation, rollup.Health)
		}
		return explanation, nil
	}
	return "", nil
}

// validateCheckin enforces the rules shared by every Check-in: a valid Health, a
// status, and — when Yellow or Red — a Path to Green with text and a target date.
func validateCheckin(health, status, path string, pathTargetDate time.Time) error {
	if !validHealth(health) {
		return fmt.Errorf("%w: Health must be %q, %q, or %q", ErrValidation, HealthGreen, HealthYellow, HealthRed)
	}
	if status == "" {
		return fmt.Errorf("%w: a Check-in needs a status", ErrValidation)
	}
	if needsPathToGreen(health) {
		if path == "" {
			return fmt.Errorf("%w: a %s Goal needs a Path to Green", ErrValidation, health)
		}
		if pathTargetDate.IsZero() {
			return fmt.Errorf("%w: a Path to Green needs a target date", ErrValidation)
		}
	}
	return nil
}

// createCheckin inserts an immutable Check-in row and returns it resolved.
func (s *Service) createCheckin(ctx context.Context, goalID, authorID, ownerID int64, health, status, path, pathTargetDate, explanation string) (Checkin, error) {
	row, err := s.queries.CreateCheckin(ctx, db.CreateCheckinParams{
		GoalID:         goalID,
		AuthorID:       authorID,
		OwnerID:        ownerID,
		Health:         health,
		Status:         status,
		PathToGreen:    path,
		PathTargetDate: pathTargetDate,
		Explanation:    explanation,
		CreatedAt:      s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return Checkin{}, fmt.Errorf("create checkin: %w", err)
	}
	author, err := s.queries.GetAccount(ctx, authorID)
	if err != nil {
		return Checkin{}, fmt.Errorf("look up author: %w", err)
	}
	owner, err := s.queries.GetAccount(ctx, ownerID)
	if err != nil {
		return Checkin{}, fmt.Errorf("look up owner: %w", err)
	}
	return checkinFromRow(row, author, owner), nil
}

// LatestCheckin returns a Goal's most recent Check-in, which carries its current
// Health, status, and Path to Green. ok is false when the Goal has none yet.
func (s *Service) LatestCheckin(ctx context.Context, goalID int64) (Checkin, bool, error) {
	row, err := s.queries.GetLatestCheckin(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Checkin{}, false, nil
		}
		return Checkin{}, false, fmt.Errorf("get latest checkin: %w", err)
	}
	author, err := s.queries.GetAccount(ctx, row.AuthorID)
	if err != nil {
		return Checkin{}, false, fmt.Errorf("look up author: %w", err)
	}
	owner, err := s.queries.GetAccount(ctx, row.OwnerID)
	if err != nil {
		return Checkin{}, false, fmt.Errorf("look up owner: %w", err)
	}
	return checkinFromRow(row, author, owner), true, nil
}

// ListCheckins returns a Goal's Check-in history, newest first, each with its
// author and the Owner it was written for resolved.
func (s *Service) ListCheckins(ctx context.Context, goalID int64) ([]Checkin, error) {
	rows, err := s.queries.ListCheckins(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list checkins: %w", err)
	}
	out := make([]Checkin, 0, len(rows))
	for _, r := range rows {
		out = append(out, checkinFromRow(r.Checkin, r.Account, r.Account_2))
	}
	return out, nil
}

func checkinFromRow(c db.Checkin, author, owner db.Account) Checkin {
	createdAt, _ := time.Parse(timeFormat, c.CreatedAt)
	var pathTargetDate time.Time
	if c.PathTargetDate != "" {
		pathTargetDate, _ = time.Parse(dateFormat, c.PathTargetDate)
	}
	return Checkin{
		ID:             c.ID,
		GoalID:         c.GoalID,
		Author:         accountFromRow(author),
		Owner:          accountFromRow(owner),
		Health:         c.Health,
		Status:         c.Status,
		PathToGreen:    c.PathToGreen,
		PathTargetDate: pathTargetDate,
		Explanation:    c.Explanation,
		CreatedAt:      createdAt,
	}
}
