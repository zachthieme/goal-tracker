package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// linkPairHistory is what has happened to the link from one child Goal to one
// parent Goal: whether it exists now, and every rejection of a request for it
// and every removal of it. The Undos that would bring the link back judge it
// to tell whether the pair has moved on since the thing they undo.
type linkPairHistory struct {
	linked     bool
	rejections []linkPairRecord
	removals   []linkPairRecord
}

// linkPairRecord is one rejection or removal in a linkPairHistory: its id
// among its own kind, and when it happened.
type linkPairRecord struct {
	id int64
	at time.Time
}

// linkPairHistory reads the history of the link from childID to parentID.
func (s *Service) linkPairHistory(ctx context.Context, childID, parentID int64) (linkPairHistory, error) {
	linked := true
	if _, err := s.queries.GetLinkByChildParent(ctx, db.GetLinkByChildParentParams{
		ChildID:  childID,
		ParentID: parentID,
	}); errors.Is(err, sql.ErrNoRows) {
		linked = false
	} else if err != nil {
		return linkPairHistory{}, fmt.Errorf("look up existing link: %w", err)
	}
	rejections, err := s.queries.ListLinkPairRejections(ctx, db.ListLinkPairRejectionsParams{
		ChildID:  childID,
		ParentID: parentID,
	})
	if err != nil {
		return linkPairHistory{}, fmt.Errorf("look up link rejections: %w", err)
	}
	removals, err := s.queries.ListLinkPairRemovals(ctx, db.ListLinkPairRemovalsParams{
		ChildID:  childID,
		ParentID: parentID,
	})
	if err != nil {
		return linkPairHistory{}, fmt.Errorf("look up link removals: %w", err)
	}
	return linkPairHistoryFrom(linked, rejections, removals)
}

// linkPairHistoryFrom builds a linkPairHistory from the rows it is read from,
// refusing a rejection or removal whose time doesn't parse.
func linkPairHistoryFrom(linked bool, rejections []db.ListLinkPairRejectionsRow, removals []db.ListLinkPairRemovalsRow) (linkPairHistory, error) {
	h := linkPairHistory{linked: linked}
	for _, r := range rejections {
		at, err := time.Parse(timeFormat, r.RejectedAt)
		if err != nil {
			return linkPairHistory{}, fmt.Errorf("link rejection %d: rejected_at %q: %w", r.ID, r.RejectedAt, err)
		}
		h.rejections = append(h.rejections, linkPairRecord{id: r.ID, at: at})
	}
	for _, r := range removals {
		at, err := time.Parse(timeFormat, r.RemovedAt)
		if err != nil {
			return linkPairHistory{}, fmt.Errorf("link removal %d: removed_at %q: %w", r.ID, r.RemovedAt, err)
		}
		h.removals = append(h.removals, linkPairRecord{id: r.ID, at: at})
	}
	return h, nil
}

// requestRestoreRefusal refuses to restore the rejection rejectionID, made at
// rejectedAt, when the link has been requested or accepted again since: it
// exists now, a later request for it was rejected too (by id, whatever its
// time), or it was accepted and has since been removed (a removal no earlier
// than the rejection is of a link made since).
func (h linkPairHistory) requestRestoreRefusal(rejectionID int64, rejectedAt time.Time) error {
	if h.linked || laterLinkPairRecord(h.rejections, rejectionID) || linkPairRecordSince(h.removals, rejectedAt) {
		return Refuse(ErrValidation, "this link has been requested again since it was rejected")
	}
	return nil
}

// linkRestoreRefusal refuses to restore the removal removalID, made at
// removedAt, when the link has been made or asked for again since: a later
// removal of it (by id, whatever its time), or a request for it rejected no
// earlier than the removal. It leaves a link that exists again to the caller,
// which refuses that in its own words.
func (h linkPairHistory) linkRestoreRefusal(removalID int64, removedAt time.Time) error {
	if laterLinkPairRecord(h.removals, removalID) || linkPairRecordSince(h.rejections, removedAt) {
		return Refuse(ErrValidation, "this link has been requested again since it was removed")
	}
	return nil
}

// declineUndoRefusal refuses to reopen a Parent suggestion of the link,
// declined at closedAt, when the link has been requested or made since: it
// exists now, or a request for it was rejected or the link removed no earlier
// than the decline. (While a suggestion is open its link doesn't exist, so any
// such rejection or removal is of a request made since.)
func (h linkPairHistory) declineUndoRefusal(closedAt time.Time) error {
	if h.linked || linkPairRecordSince(h.rejections, closedAt) || linkPairRecordSince(h.removals, closedAt) {
		return Refuse(ErrValidation, "this link has been requested since the suggestion was declined")
	}
	return nil
}

// laterLinkPairRecord reports whether any of records has a higher id than id.
func laterLinkPairRecord(records []linkPairRecord, id int64) bool {
	for _, r := range records {
		if r.id > id {
			return true
		}
	}
	return false
}

// linkPairRecordSince reports whether any of records happened no earlier than t.
func linkPairRecordSince(records []linkPairRecord, t time.Time) bool {
	for _, r := range records {
		if !r.at.Before(t) {
			return true
		}
	}
	return false
}
