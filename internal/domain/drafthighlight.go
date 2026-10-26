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
	var row db.DraftHighlight
	err := s.WithinTx(ctx, func(tx *Service) error {
		goal, err := tx.openGoalCommand(ctx, in.AuthorID, in.GoalID, logDraftHighlightRefusal, nil)
		if err != nil {
			return err
		}
		note, err := draftHighlightValues(in.Kind, in.Note)
		if err != nil {
			return err
		}
		row, err = tx.queries.CreateDraftHighlight(ctx, db.CreateDraftHighlightParams{
			GoalID:    goal.ID,
			Kind:      in.Kind,
			Note:      note,
			LoggedBy:  in.AuthorID,
			CreatedAt: tx.clock.Now().Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create draft highlight: %w", err)
		}
		return nil
	})
	if err != nil {
		return DraftHighlight{}, err
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
	if _, err := s.authorizeDraftHighlighter(ctx, viewerID, goalID); err != nil {
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

// DeleteDraftHighlight deletes a pending Draft Highlight outright, returning
// it as it was: it is no longer pending, and no Check-in keeps or discards it.
// Only its Goal's Owner or a Delegate, as actorID, may delete one. One that
// doesn't exist, or that a Check-in has already discarded, is ErrNotFound.
func (s *Service) DeleteDraftHighlight(ctx context.Context, actorID, id int64) (DraftHighlight, error) {
	var draft DraftHighlight
	err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		draft, _, err = tx.draftHighlightFor(ctx, actorID, id)
		if err != nil {
			return err
		}
		n, err := tx.queries.DeletePendingDraftHighlight(ctx, id)
		if err != nil {
			return fmt.Errorf("delete draft highlight: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: draft highlight %d is no longer pending", ErrNotFound, id)
		}
		return nil
	})
	if err != nil {
		return DraftHighlight{}, err
	}
	return draft, nil
}

// EditDraftHighlight changes a pending Draft Highlight's kind and note,
// returning it as edited. Only its Goal's Owner or a Delegate, as actorID, may
// edit one; who logged it and when stay as they were. The note is trimmed and
// required, and a kind, if given, must be an Insight, Accomplishment or Miss,
// as when it was logged. As with logging, a Done or Cancelled Goal's Draft
// Highlights can't be edited: it takes no more Check-ins to offer them at. One
// that doesn't exist, or that a Check-in has already kept or discarded, is
// ErrNotFound. The edit records nothing in the Goal's History: the Draft
// Highlight is still pending.
func (s *Service) EditDraftHighlight(ctx context.Context, actorID, id int64, kind, note string) (DraftHighlight, error) {
	var draft DraftHighlight
	err := s.WithinTx(ctx, func(tx *Service) error {
		var goal Goal
		var err error
		draft, goal, err = tx.draftHighlightFor(ctx, actorID, id)
		if err != nil {
			return err
		}
		if err := draftHighlightsTaken(goal); err != nil {
			return err
		}
		note, err = draftHighlightValues(kind, note)
		if err != nil {
			return err
		}
		n, err := tx.queries.UpdatePendingDraftHighlight(ctx, db.UpdatePendingDraftHighlightParams{Kind: kind, Note: note, ID: id})
		if err != nil {
			return fmt.Errorf("edit draft highlight: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: draft highlight %d is no longer pending", ErrNotFound, id)
		}
		return nil
	})
	if err != nil {
		return DraftHighlight{}, err
	}
	draft.Kind, draft.Note = kind, note
	return draft, nil
}

// DraftHighlight returns the Draft Highlight id, pending or not, with who
// logged it. Only its Goal's Owner or a Delegate, as viewerID, may see it.
func (s *Service) DraftHighlight(ctx context.Context, viewerID, id int64) (DraftHighlight, error) {
	draft, _, err := s.draftHighlightFor(ctx, viewerID, id)
	return draft, err
}

// draftHighlightFor loads the Draft Highlight id, pending or not, and its Goal
// for actorID to see or change: only the Goal's Owner or a Delegate may.
func (s *Service) draftHighlightFor(ctx context.Context, actorID, id int64) (DraftHighlight, Goal, error) {
	row, err := s.queries.GetDraftHighlight(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DraftHighlight{}, Goal{}, fmt.Errorf("%w: draft highlight %d", ErrNotFound, id)
		}
		return DraftHighlight{}, Goal{}, fmt.Errorf("get draft highlight: %w", err)
	}
	draft := draftHighlightFromRow(row.DraftHighlight, row.Account)
	goal, err := s.authorizeDraftHighlighter(ctx, actorID, draft.GoalID)
	if err != nil {
		return DraftHighlight{}, Goal{}, err
	}
	return draft, goal, nil
}

// draftHighlightsTaken refuses logging or editing a Draft Highlight on a Done
// or Cancelled Goal with ErrValidation: it takes no more Check-ins to offer
// one at.
func draftHighlightsTaken(goal Goal) error {
	if !LifecycleTakesDraftHighlights(goal.Lifecycle) {
		return Refuse(ErrValidation, "a %s Goal takes no more Check-ins, so it takes no Draft Highlights", goal.Lifecycle)
	}
	return nil
}

// draftHighlightValues checks a Draft Highlight's kind and note as logged or
// edited, returning the note trimmed: the note is required, and a kind, if
// given, must be an Insight, Accomplishment or Miss.
func draftHighlightValues(kind, note string) (string, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return "", Refuse(ErrValidation, "a Draft Highlight needs a note")
	}
	if kind != "" && !validHighlightKind(kind) {
		return "", Refuse(ErrValidation, "a Draft Highlight's kind must be an %q, %q, or %q, or none", HighlightInsight, HighlightAccomplishment, HighlightMiss)
	}
	return note, nil
}

// draftHighlightPicks is what a Check-in does with the Draft Highlights it
// offered: rows are its Highlight rows as planHighlights should read them,
// kept the IDs of the Draft Highlights that become Highlights, and discarded
// those it discards.
type draftHighlightPicks struct {
	rows      []HighlightInput
	kept      []int64
	discarded []int64
}

// planDraftHighlightPicks works out what a Check-in does with each row offered
// from a Draft Highlight, given pending, the Goal's pending Draft Highlights,
// asked for only when a row names one. The Check-in reads them within its
// transaction, so the ones it reads as pending are the ones it clears. A row
// naming one that is no longer pending on this Goal — deleted, or cleared by
// another Check-in — is ignored. A kept row with a note becomes a Highlight;
// one left out, or kept with its note blanked, records nothing and discards
// its Draft Highlight. Ignored and left-out rows stay in rows, blanked, so
// planHighlights still names each row by its position on the form. A Draft
// Highlight no row names, such as one logged after the form opened, stays
// pending.
func planDraftHighlightPicks(rows []HighlightInput, pending func() ([]DraftHighlight, error)) (draftHighlightPicks, error) {
	var out draftHighlightPicks
	var isPending map[int64]bool
	for _, row := range rows {
		if row.DraftHighlightID == 0 {
			out.rows = append(out.rows, row)
			continue
		}
		if isPending == nil {
			drafts, err := pending()
			if err != nil {
				return draftHighlightPicks{}, err
			}
			isPending = make(map[int64]bool, len(drafts))
			for _, d := range drafts {
				isPending[d.ID] = true
			}
		}
		id := row.DraftHighlightID
		switch {
		case !isPending[id]:
			out.rows = append(out.rows, HighlightInput{})
		case row.LeftOut || strings.TrimSpace(row.Note) == "":
			out.rows = append(out.rows, HighlightInput{})
			out.discarded = append(out.discarded, id)
		default:
			out.rows = append(out.rows, HighlightInput{Kind: row.Kind, Note: row.Note})
			out.kept = append(out.kept, id)
		}
		// A form naming the same Draft Highlight twice offers it once.
		delete(isPending, id)
	}
	return out, nil
}

// apply clears every Draft Highlight the Check-in checkinID offered: those kept
// are deleted, now its Highlights, and the rest are discarded with it.
func (p draftHighlightPicks) apply(ctx context.Context, tx *Service, checkinID int64) error {
	for _, id := range p.kept {
		if _, err := tx.queries.DeletePendingDraftHighlight(ctx, id); err != nil {
			return fmt.Errorf("clear kept draft highlight: %w", err)
		}
	}
	for _, id := range p.discarded {
		if _, err := tx.queries.DiscardDraftHighlight(ctx, db.DiscardDraftHighlightParams{DiscardedCheckinID: &checkinID, ID: id}); err != nil {
			return fmt.Errorf("discard draft highlight: %w", err)
		}
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
// Check-ins — its Owner or a Delegate, neither Departed — to log, see, edit or
// delete its pending Draft Highlights (draftHighlighterRefusal), returning the
// Goal goalID; a missing Goal is ErrNotFound.
func (s *Service) authorizeDraftHighlighter(ctx context.Context, actorID, goalID int64) (Goal, error) {
	return s.openGoalCommand(ctx, actorID, goalID, draftHighlighterRefusal, nil)
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
