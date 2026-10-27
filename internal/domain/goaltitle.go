package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// TitleRevision is one change to a Goal's title, kept with the old and new
// titles so it shows in the Goal's History (CONTEXT.md: Goal).
type TitleRevision struct {
	ID        int64
	GoalID    int64
	OldTitle  string
	NewTitle  string
	Author    Account
	CreatedAt time.Time
}

// EditTitle changes a Goal's title in any Lifecycle, recording the change with
// the old and new titles. The title is checked as at creation: trimmed, and
// required. A title the same as the current one changes nothing and records
// nothing. authorID records who made the change, and must be the Goal's Owner
// or an Admin (ErrNotAuthorized, by goalOwnerOrAdminRefusal).
func (s *Service) EditTitle(ctx context.Context, goalID int64, title string, authorID int64) (Goal, error) {
	var current Goal
	unchanged := false
	err := s.WithinTx(ctx, func(tx *Service) error {
		title = strings.TrimSpace(title)
		if title == "" {
			return Refuse(ErrValidation, "a Goal needs a title")
		}
		var err error
		current, err = tx.openGoalCommand(ctx, authorID, goalID, func(a goalActor, g Goal) error {
			return goalOwnerOrAdminRefusal(a, g, "change it")
		}, nil)
		if err != nil {
			return err
		}
		if title == current.Title {
			unchanged = true
			return nil
		}
		if _, err := tx.queries.SetGoalTitle(ctx, db.SetGoalTitleParams{Title: title, ID: goalID}); err != nil {
			return fmt.Errorf("set title: %w", err)
		}
		if _, err := tx.queries.CreateGoalTitleRevision(ctx, db.CreateGoalTitleRevisionParams{
			GoalID:    goalID,
			OldTitle:  current.Title,
			NewTitle:  title,
			AuthorID:  authorID,
			CreatedAt: tx.clock.Now().Format(timeFormat),
		}); err != nil {
			return fmt.Errorf("record title revision: %w", err)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	if unchanged {
		return current, nil
	}
	return s.loadGoal(ctx, goalID)
}

// EditTitleAndWhyItMatters saves the Edit title and Why it matters form, for
// the Goal's Owner or an Admin (ErrNotAuthorized). It changes the title, unless
// title is nil (the form sent none), and the Why it matters, each only when it
// differs from what the Goal has, all or nothing: an unchanged field records no
// revision, and a refused save changes neither. Each field is checked as
// EditTitle and EditWhyItMatters check it.
func (s *Service) EditTitleAndWhyItMatters(ctx context.Context, actorID, goalID int64, title *string, whyItMatters string) (Goal, error) {
	var g Goal
	err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		g, err = tx.openGoalCommand(ctx, actorID, goalID, func(a goalActor, goal Goal) error {
			return goalOwnerOrAdminRefusal(a, goal, "change it")
		}, nil)
		if err != nil {
			return err
		}
		if title != nil {
			if g, err = tx.EditTitle(ctx, goalID, *title, actorID); err != nil {
				return err
			}
		}
		if strings.TrimSpace(whyItMatters) != g.WhyItMatters {
			if g, err = tx.EditWhyItMatters(ctx, goalID, whyItMatters, actorID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	return g, nil
}

// ListTitleRevisions returns every change to a Goal's title, newest first, each
// with its author resolved.
func (s *Service) ListTitleRevisions(ctx context.Context, goalID int64) ([]TitleRevision, error) {
	rows, err := s.queries.ListGoalTitleRevisions(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list title revisions: %w", err)
	}
	out := make([]TitleRevision, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.GoalTitleRevision.CreatedAt)
		out = append(out, TitleRevision{
			ID:        r.GoalTitleRevision.ID,
			GoalID:    r.GoalTitleRevision.GoalID,
			OldTitle:  r.GoalTitleRevision.OldTitle,
			NewTitle:  r.GoalTitleRevision.NewTitle,
			Author:    accountFromRow(r.Account),
			CreatedAt: createdAt,
		})
	}
	return out, nil
}

// RequireGoalOwnerOrAdmin returns ErrNotAuthorized unless actorID is the Goal's
// Owner or an Admin (a Delegate is refused), by goalOwnerOrAdminRefusal, or
// ErrNotFound if no such Goal exists. EditTitle, EditTitleAndWhyItMatters and
// EditWhyItMatters apply the same rule for themselves, so their callers needn't
// check it first.
func (s *Service) RequireGoalOwnerOrAdmin(ctx context.Context, actorID, goalID int64) error {
	_, err := s.openGoalCommand(ctx, actorID, goalID, func(a goalActor, g Goal) error {
		return goalOwnerOrAdminRefusal(a, g, "change it")
	}, nil)
	return err
}
