package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Highlight is the optional note an Owner flags in a Check-in, marked as an
// Insight, Accomplishment, or Miss, that a Report author may later pull into a
// Report's narrative; the Goal's Owner is credited (CONTEXT.md: Highlight). A
// Check-in carries at most one.
type Highlight struct {
	ID        int64
	CheckinID int64
	Kind      string
	Note      string
	// Owner is the Account the Highlight credits: the Owner the Check-in was
	// written for (CONTEXT.md: the Goal's Owner is credited).
	Owner     Account
	CreatedAt time.Time
}

// Highlight kinds a Check-in's Highlight can be marked as (CONTEXT.md:
// Highlight).
const (
	HighlightInsight        = "Insight"
	HighlightAccomplishment = "Accomplishment"
	HighlightMiss           = "Miss"
)

func validHighlightKind(k string) bool {
	return k == HighlightInsight || k == HighlightAccomplishment || k == HighlightMiss
}

// HighlightInput is the optional Highlight recorded with a Check-in. Kind must
// be an Insight, Accomplishment, or Miss, and the note is required.
type HighlightInput struct {
	Kind string
	Note string
}

// validateHighlight enforces the rules on a Check-in's Highlight: a valid kind
// and a non-empty note.
func validateHighlight(in HighlightInput) error {
	if !validHighlightKind(in.Kind) {
		return fmt.Errorf("%w: a Highlight must be an %q, %q, or %q", ErrValidation, HighlightInsight, HighlightAccomplishment, HighlightMiss)
	}
	if strings.TrimSpace(in.Note) == "" {
		return fmt.Errorf("%w: a Highlight needs a note", ErrValidation)
	}
	return nil
}

// recordHighlight inserts a Check-in's Highlight, stamped with the Service's
// clock. The note is trimmed; the caller has already validated it.
func (s *Service) recordHighlight(ctx context.Context, checkinID int64, in HighlightInput) error {
	if _, err := s.queries.CreateHighlight(ctx, db.CreateHighlightParams{
		CheckinID: checkinID,
		Kind:      in.Kind,
		Note:      strings.TrimSpace(in.Note),
		CreatedAt: s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("create highlight: %w", err)
	}
	return nil
}

// ListHighlightsByGoal returns a Goal's Highlights, newest first, each crediting
// its Owner. Report curation queries Highlights by Goal (CONTEXT.md: Report).
func (s *Service) ListHighlightsByGoal(ctx context.Context, goalID int64) ([]Highlight, error) {
	rows, err := s.queries.ListHighlightsByGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list highlights by goal: %w", err)
	}
	out := make([]Highlight, 0, len(rows))
	for _, r := range rows {
		out = append(out, highlightFromRow(r.Highlight, r.Account))
	}
	return out, nil
}

// ListHighlightsByGoalInRange returns a Goal's Highlights whose Check-in falls
// within [from, to] inclusive, newest first. Report curation queries Highlights
// by Goal and by time range (CONTEXT.md: Report).
func (s *Service) ListHighlightsByGoalInRange(ctx context.Context, goalID int64, from, to time.Time) ([]Highlight, error) {
	rows, err := s.queries.ListHighlightsByGoalInRange(ctx, db.ListHighlightsByGoalInRangeParams{
		GoalID: goalID,
		From:   from.Format(timeFormat),
		To:     to.Format(timeFormat),
	})
	if err != nil {
		return nil, fmt.Errorf("list highlights by goal in range: %w", err)
	}
	out := make([]Highlight, 0, len(rows))
	for _, r := range rows {
		out = append(out, highlightFromRow(r.Highlight, r.Account))
	}
	return out, nil
}

func highlightFromRow(h db.Highlight, owner db.Account) Highlight {
	createdAt, _ := time.Parse(timeFormat, h.CreatedAt)
	return Highlight{
		ID:        h.ID,
		CheckinID: h.CheckinID,
		Kind:      h.Kind,
		Note:      h.Note,
		Owner:     accountFromRow(owner),
		CreatedAt: createdAt,
	}
}
