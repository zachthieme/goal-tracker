package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Highlight is an optional note an Owner flags in a Check-in, marked as an
// Insight, Accomplishment, or Miss, that a Report author may later pull into a
// Report's narrative; the Goal's Owner is credited (CONTEXT.md: Highlight). A
// Check-in carries any number, of any mix of kinds, and an author pulls each
// one separately.
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

// Highlight kinds a Check-in's Highlights can be marked as (CONTEXT.md:
// Highlight).
const (
	HighlightInsight        = "Insight"
	HighlightAccomplishment = "Accomplishment"
	HighlightMiss           = "Miss"
)

func validHighlightKind(k string) bool {
	return k == HighlightInsight || k == HighlightAccomplishment || k == HighlightMiss
}

// HighlightInput is one Highlight recorded with a Check-in. Kind must be an
// Insight, Accomplishment, or Miss; a blank note means the Owner flagged
// nothing in this row.
type HighlightInput struct {
	Kind string
	Note string
}

// planHighlights returns the Highlights a Check-in records, in the order
// entered, each note trimmed. A row with a blank note is ignored; a row with a
// note needs a valid kind, and the error names the row by its position among
// those entered, counting from 1.
func planHighlights(rows []HighlightInput) ([]HighlightInput, error) {
	var out []HighlightInput
	for i, row := range rows {
		note := strings.TrimSpace(row.Note)
		if note == "" {
			continue
		}
		if row.Kind == "" {
			return nil, fmt.Errorf("%w: Highlight %d needs a kind: %s, %s, or %s", ErrValidation, i+1, HighlightInsight, HighlightAccomplishment, HighlightMiss)
		}
		if !validHighlightKind(row.Kind) {
			return nil, fmt.Errorf("%w: Highlight %d must be an %q, %q, or %q", ErrValidation, i+1, HighlightInsight, HighlightAccomplishment, HighlightMiss)
		}
		out = append(out, HighlightInput{Kind: row.Kind, Note: note})
	}
	return out, nil
}

// recordHighlights inserts a Check-in's Highlights in the order given, each
// stamped with the Service's clock. The caller has already planned them.
func (s *Service) recordHighlights(ctx context.Context, checkinID int64, highlights []HighlightInput) error {
	now := s.clock.Now().Format(timeFormat)
	for _, hl := range highlights {
		if _, err := s.queries.CreateHighlight(ctx, db.CreateHighlightParams{
			CheckinID: checkinID,
			Kind:      hl.Kind,
			Note:      hl.Note,
			CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("create highlight: %w", err)
		}
	}
	return nil
}

// ListHighlightsByGoal returns a Goal's Highlights, newest Check-in first and
// each Check-in's in the order entered, each crediting its Owner. Report
// curation queries Highlights by Goal (CONTEXT.md: Report).
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
// within [from, to] inclusive, newest Check-in first and each Check-in's in
// the order entered. Report curation queries Highlights by Goal and by time
// range (CONTEXT.md: Report).
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
