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

// DraftHighlight is a Highlight logged on a Goal between Check-ins, waiting for
// the next Check-in (CONTEXT.md: Draft Highlight). Only the Goal's Owner and
// Delegates log one or see a pending one. The next Check-in offers each one:
// those kept become its Highlights and are gone as drafts; the rest are
// discarded with it, which the Goal's History records.
type DraftHighlight struct {
	ID     int64
	GoalID int64
	// Kind is an Insight, Accomplishment or Miss, or "" for none yet.
	Kind     string
	Note     string
	LoggedBy Account
	// DiscardedCheckinID is the Check-in that discarded it; 0 while pending.
	DiscardedCheckinID int64
	CreatedAt          time.Time
}

// LogDraftHighlightInput is the log-a-Draft-Highlight command's input. Kind is
// optional; the note is required.
type LogDraftHighlightInput struct {
	GoalID   int64
	AuthorID int64
	Kind     string
	Note     string
}

// LogDraftHighlight logs a Draft Highlight on a Goal, recording who logged it
// and when. Only the Goal's Owner or a Delegate may log one — whoever may write
// its Check-ins — and only while the Goal is Proposed, Active or On Hold; a
// Done or Cancelled Goal takes no more Check-ins to offer it at. The note is
// trimmed and required; a kind, if given, must be an Insight, Accomplishment
// or Miss.
func (s *Service) LogDraftHighlight(ctx context.Context, in LogDraftHighlightInput) (DraftHighlight, error) {
	goal, err := s.loadGoal(ctx, in.GoalID)
	if err != nil {
		return DraftHighlight{}, err
	}
	if err := s.authorizeDraftHighlighter(ctx, goal, in.AuthorID); err != nil {
		return DraftHighlight{}, err
	}
	if goal.Lifecycle == LifecycleDone || goal.Lifecycle == LifecycleCancelled {
		return DraftHighlight{}, fmt.Errorf("%w: a %s Goal takes no more Check-ins, so it takes no Draft Highlights", ErrValidation, goal.Lifecycle)
	}
	note := strings.TrimSpace(in.Note)
	if note == "" {
		return DraftHighlight{}, fmt.Errorf("%w: a Draft Highlight needs a note", ErrValidation)
	}
	if in.Kind != "" && !validHighlightKind(in.Kind) {
		return DraftHighlight{}, fmt.Errorf("%w: a Draft Highlight's kind must be an %q, %q, or %q, or none", ErrValidation, HighlightInsight, HighlightAccomplishment, HighlightMiss)
	}
	row, err := s.queries.CreateDraftHighlight(ctx, db.CreateDraftHighlightParams{
		GoalID:    goal.ID,
		Kind:      in.Kind,
		Note:      note,
		LoggedBy:  in.AuthorID,
		CreatedAt: s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return DraftHighlight{}, fmt.Errorf("create draft highlight: %w", err)
	}
	author, err := s.queries.GetAccount(ctx, in.AuthorID)
	if err != nil {
		return DraftHighlight{}, fmt.Errorf("look up author: %w", err)
	}
	return draftHighlightFromRow(row, author), nil
}

// PendingDraftHighlights returns the Goal's pending Draft Highlights, oldest
// first, each with who logged it. Only the Goal's Owner or a Delegate, as
// viewerID, may see them.
func (s *Service) PendingDraftHighlights(ctx context.Context, viewerID, goalID int64) ([]DraftHighlight, error) {
	goal, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeDraftHighlighter(ctx, goal, viewerID); err != nil {
		return nil, err
	}
	return s.pendingDraftHighlights(ctx, goalID)
}

// pendingDraftHighlights is PendingDraftHighlights for a caller that has
// already authorized the viewer, or needs no viewer.
func (s *Service) pendingDraftHighlights(ctx context.Context, goalID int64) ([]DraftHighlight, error) {
	rows, err := s.queries.ListPendingDraftHighlights(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list pending draft highlights: %w", err)
	}
	out := make([]DraftHighlight, 0, len(rows))
	for _, r := range rows {
		out = append(out, draftHighlightFromRow(r.DraftHighlight, r.Account))
	}
	return out, nil
}

// DeleteDraftHighlight deletes a pending Draft Highlight outright: it is no
// longer pending, and no Check-in keeps or discards it. Only its Goal's Owner
// or a Delegate, as actorID, may delete one. One that doesn't exist, or that a
// Check-in has already discarded, is ErrNotFound.
func (s *Service) DeleteDraftHighlight(ctx context.Context, actorID, id int64) error {
	row, err := s.queries.GetDraftHighlight(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: draft highlight %d", ErrNotFound, id)
		}
		return fmt.Errorf("get draft highlight: %w", err)
	}
	goal, err := s.loadGoal(ctx, row.DraftHighlight.GoalID)
	if err != nil {
		return err
	}
	if err := s.authorizeDraftHighlighter(ctx, goal, actorID); err != nil {
		return err
	}
	n, err := s.queries.DeletePendingDraftHighlight(ctx, id)
	if err != nil {
		return fmt.Errorf("delete draft highlight: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: draft highlight %d is no longer pending", ErrNotFound, id)
	}
	return nil
}

// discardedDraftHighlightsByCheckin returns the Draft Highlights a Goal's
// Check-ins discarded, keyed by the Check-in, each Check-in's oldest first.
func (s *Service) discardedDraftHighlightsByCheckin(ctx context.Context, goalID int64) (map[int64][]DraftHighlight, error) {
	rows, err := s.queries.ListDiscardedDraftHighlightsByGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list discarded draft highlights: %w", err)
	}
	out := make(map[int64][]DraftHighlight)
	for _, r := range rows {
		d := draftHighlightFromRow(r.DraftHighlight, r.Account)
		out[d.DiscardedCheckinID] = append(out[d.DiscardedCheckinID], d)
	}
	return out, nil
}

// authorizeDraftHighlighter allows only those who may write the Goal's
// Check-ins — its Owner or a Delegate, neither Departed — to log, see or delete
// its pending Draft Highlights.
func (s *Service) authorizeDraftHighlighter(ctx context.Context, goal Goal, actorID int64) error {
	if err := s.authorizeCheckinAuthor(ctx, goal.ID, goal.Owner.ID, actorID); err != nil {
		if errors.Is(err, ErrNotAuthorized) {
			return fmt.Errorf("%w: only the Goal's Owner or a Delegate may log, see or delete its Draft Highlights", ErrNotAuthorized)
		}
		return err
	}
	return nil
}

func draftHighlightFromRow(d db.DraftHighlight, loggedBy db.Account) DraftHighlight {
	createdAt, _ := time.Parse(timeFormat, d.CreatedAt)
	var discarded int64
	if d.DiscardedCheckinID != nil {
		discarded = *d.DiscardedCheckinID
	}
	return DraftHighlight{
		ID:                 d.ID,
		GoalID:             d.GoalID,
		Kind:               d.Kind,
		Note:               d.Note,
		LoggedBy:           accountFromRow(loggedBy),
		DiscardedCheckinID: discarded,
		CreatedAt:          createdAt,
	}
}
