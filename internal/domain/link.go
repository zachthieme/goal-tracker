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

// Link is a "contributes to" relationship from a child Goal to a parent Goal
// (CONTEXT.md: Contributes to). It is Pending until the parent's Owner accepts
// it, at which point it is Accepted and part of the Goal graph.
type Link struct {
	ID        int64
	Child     Goal
	Parent    Goal
	Status    string
	Note      string
	CreatedAt time.Time
}

// Link status values. A rejected or removed link is deleted, so it has no
// status of its own; a rejection leaves a LinkRejection its rejecter can undo,
// and a removal a LinkRemoval its remover can undo.
const (
	LinkPending  = "pending"
	LinkAccepted = "accepted"
)

var (
	// ErrCycle is returned when a link would make the Goal graph cyclic
	// (ADR-0001: the graph has no cycles).
	ErrCycle = errors.New("link would create a cycle")
	// ErrNotAuthorized is returned when the acting Account may not perform the
	// requested link action (e.g. accepting a request to a Goal it does not own).
	ErrNotAuthorized = errors.New("not authorized")
)

// RequestLinkInput is the request-a-link command's input: the requester offers
// their child Goal up to contribute to parent, with an optional note.
type RequestLinkInput struct {
	ChildID     int64
	ParentID    int64
	Note        string
	RequesterID int64
}

// RequestLink requests that ChildID contribute to ParentID. The requester must
// own the child. The request is accepted immediately when the requester also
// owns the parent (CONTEXT.md: creating a child directly under a Goal you own);
// otherwise it waits Pending for the parent's Owner. A request that would close
// a cycle is rejected here and again at accept time (ADR-0001). Any open Parent
// suggestion of the same parent for the child closes as no longer applying, as
// it does when ImportLink makes the link or RestoreLinkRequest or RestoreLink
// brings it back.
func (s *Service) RequestLink(ctx context.Context, in RequestLinkInput) (Link, error) {
	if in.ChildID == in.ParentID {
		return Link{}, fmt.Errorf("%w: a Goal cannot contribute to itself", ErrValidation)
	}

	child, err := s.queries.GetGoal(ctx, in.ChildID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Link{}, fmt.Errorf("%w: child goal does not exist", ErrValidation)
		}
		return Link{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, in.ParentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Link{}, fmt.Errorf("%w: parent goal does not exist", ErrValidation)
		}
		return Link{}, fmt.Errorf("look up parent goal: %w", err)
	}

	if child.Goal.OwnerID != in.RequesterID {
		return Link{}, fmt.Errorf("%w: only the child's Owner may request a link", ErrNotAuthorized)
	}

	if _, err := s.queries.GetLinkByChildParent(ctx, db.GetLinkByChildParentParams{
		ChildID:  in.ChildID,
		ParentID: in.ParentID,
	}); err == nil {
		return Link{}, fmt.Errorf("%w: a link between these Goals already exists", ErrValidation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Link{}, fmt.Errorf("look up existing link: %w", err)
	}

	if err := s.ensureNoCycle(ctx, in.ChildID, in.ParentID); err != nil {
		return Link{}, err
	}

	status := LinkPending
	if parent.Goal.OwnerID == in.RequesterID {
		status = LinkAccepted
	}

	now := s.clock.Now()
	var row db.Link
	err = s.WithinTx(ctx, func(tx *Service) error {
		row, err = tx.queries.CreateLink(ctx, db.CreateLinkParams{
			ChildID:     in.ChildID,
			ParentID:    in.ParentID,
			Status:      status,
			Note:        strings.TrimSpace(in.Note),
			RequestedBy: in.RequesterID,
			CreatedAt:   now.Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create link: %w", err)
		}
		kind := LinkEventRequested
		if status == LinkAccepted {
			kind = LinkEventLinked
		}
		if err := tx.recordLinkEvent(ctx, in.ChildID, in.ParentID, kind, in.RequesterID, now); err != nil {
			return err
		}
		return tx.closeSuggestionsForLink(ctx, in.ChildID, in.ParentID)
	})
	if err != nil {
		return Link{}, err
	}

	return Link{
		ID:        row.ID,
		Child:     goalFromRow(child.Goal, child.Account),
		Parent:    goalFromRow(parent.Goal, parent.Account),
		Status:    row.Status,
		Note:      row.Note,
		CreatedAt: now,
	}, nil
}

// ImportLink creates an already-Accepted "contributes to" link from childID to
// parentID, without waiting for the parent's Owner to accept it. It is used by
// the spreadsheet import an Admin runs, where links between imported Goals are
// accepted automatically (ticket #22). It still refuses a self-link, a duplicate,
// and any link that would close a cycle (ADR-0001). The request is attributed to
// the child's Owner, who is the person a normal request would come from. Any
// open Parent suggestion of the same parent for the child closes as no longer
// applying (ADR-0006).
func (s *Service) ImportLink(ctx context.Context, childID, parentID int64) (Link, error) {
	if childID == parentID {
		return Link{}, fmt.Errorf("%w: a Goal cannot contribute to itself", ErrValidation)
	}
	child, err := s.queries.GetGoal(ctx, childID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Link{}, fmt.Errorf("%w: child goal does not exist", ErrValidation)
		}
		return Link{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, parentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Link{}, fmt.Errorf("%w: parent goal does not exist", ErrValidation)
		}
		return Link{}, fmt.Errorf("look up parent goal: %w", err)
	}

	if _, err := s.queries.GetLinkByChildParent(ctx, db.GetLinkByChildParentParams{
		ChildID:  childID,
		ParentID: parentID,
	}); err == nil {
		return Link{}, fmt.Errorf("%w: a link between these Goals already exists", ErrValidation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Link{}, fmt.Errorf("look up existing link: %w", err)
	}

	if err := s.ensureNoCycle(ctx, childID, parentID); err != nil {
		return Link{}, err
	}

	now := s.clock.Now()
	var row db.Link
	err = s.WithinTx(ctx, func(tx *Service) error {
		row, err = tx.queries.CreateLink(ctx, db.CreateLinkParams{
			ChildID:     childID,
			ParentID:    parentID,
			Status:      LinkAccepted,
			Note:        "",
			RequestedBy: child.Goal.OwnerID,
			CreatedAt:   now.Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create link: %w", err)
		}
		if err := tx.recordLinkEvent(ctx, childID, parentID, LinkEventLinked, child.Goal.OwnerID, now); err != nil {
			return err
		}
		return tx.closeSuggestionsForLink(ctx, childID, parentID)
	})
	if err != nil {
		return Link{}, err
	}
	return Link{
		ID:        row.ID,
		Child:     goalFromRow(child.Goal, child.Account),
		Parent:    goalFromRow(parent.Goal, parent.Account),
		Status:    row.Status,
		Note:      row.Note,
		CreatedAt: now,
	}, nil
}

// AcceptLink accepts a pending request. The actor must own the parent Goal. The
// cycle check is repeated because another accepted link may have appeared since
// the request was made (ADR-0001).
func (s *Service) AcceptLink(ctx context.Context, linkID, actorID int64) (Link, error) {
	row, err := s.getPendingLink(ctx, linkID)
	if err != nil {
		return Link{}, err
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return Link{}, fmt.Errorf("look up parent goal: %w", err)
	}
	if parent.Goal.OwnerID != actorID {
		return Link{}, fmt.Errorf("%w: only the parent's Owner may accept a link", ErrNotAuthorized)
	}
	if err := s.ensureNoCycle(ctx, row.ChildID, row.ParentID); err != nil {
		return Link{}, err
	}
	err = s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.queries.SetLinkStatus(ctx, db.SetLinkStatusParams{
			Status: LinkAccepted,
			ID:     linkID,
		}); err != nil {
			return fmt.Errorf("accept link: %w", err)
		}
		return tx.recordLinkEvent(ctx, row.ChildID, row.ParentID, LinkEventAccepted, actorID, tx.clock.Now())
	})
	if err != nil {
		return Link{}, err
	}
	return s.loadLink(ctx, linkID)
}

// LinkRejection is the record a rejected request leaves: the two Goals it
// would have linked, who rejected it and when, and whether that rejection has
// since been undone. It is what lets the rejecter Undo the rejection, putting
// the request back as pending.
type LinkRejection struct {
	ID         int64
	Child      Goal
	Parent     Goal
	RejectedBy int64
	RejectedAt time.Time
	Restored   bool
	// UndoToken is the one-time token RestoreLinkRequest takes from the
	// rejecter. Only RejectLink sets it.
	UndoToken string
}

// RejectLink rejects a pending request, deleting it and recording the
// rejection so the rejecter can Undo it (RestoreLinkRequest) with the token it
// returns, for UndoWindow. The actor must own the parent Goal.
func (s *Service) RejectLink(ctx context.Context, linkID, actorID int64) (LinkRejection, error) {
	row, err := s.getPendingLink(ctx, linkID)
	if err != nil {
		return LinkRejection{}, err
	}
	child, err := s.queries.GetGoal(ctx, row.ChildID)
	if err != nil {
		return LinkRejection{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return LinkRejection{}, fmt.Errorf("look up parent goal: %w", err)
	}
	if parent.Goal.OwnerID != actorID {
		return LinkRejection{}, fmt.Errorf("%w: only the parent's Owner may reject a link", ErrNotAuthorized)
	}
	now := s.clock.Now()
	var rejection db.RejectedLinkRequest
	var token string
	err = s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.queries.DeleteLink(ctx, linkID); err != nil {
			return fmt.Errorf("reject link: %w", err)
		}
		rejection, err = tx.queries.CreateRejectedLinkRequest(ctx, db.CreateRejectedLinkRequestParams{
			ChildID:          row.ChildID,
			ParentID:         row.ParentID,
			Note:             row.Note,
			RequestedBy:      row.RequestedBy,
			RequestCreatedAt: row.CreatedAt,
			RejectedBy:       actorID,
			RejectedAt:       now.Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("record link rejection: %w", err)
		}
		if err := tx.recordLinkEvent(ctx, row.ChildID, row.ParentID, LinkEventRejected, actorID, now); err != nil {
			return err
		}
		token, err = tx.issueUndo(ctx, undoLinkRejection, rejection.ID, actorID, "")
		return err
	})
	if err != nil {
		return LinkRejection{}, err
	}
	return LinkRejection{
		ID:         rejection.ID,
		Child:      goalFromRow(child.Goal, child.Account),
		Parent:     goalFromRow(parent.Goal, parent.Account),
		RejectedBy: actorID,
		RejectedAt: now,
		UndoToken:  token,
	}, nil
}

// RestoreLinkRequest undoes a rejection, putting the request back as pending
// between the same two Goals with its original note, requester and request
// time, as if it had never been rejected. Only the person who rejected it may,
// with the token RejectLink gave them, within UndoWindow, and only once.
// Presenting the token spends it, even when the Undo is then refused. It is
// refused, changing nothing, when the same link has been requested, accepted,
// removed or rejected again since, or when it would now close a cycle
// (ADR-0001). Rejecting tells no one, so neither does the Undo.
func (s *Service) RestoreLinkRequest(ctx context.Context, rejectionID, actorID int64, token string) (Link, error) {
	rejection, err := s.queries.GetRejectedLinkRequest(ctx, rejectionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Link{}, fmt.Errorf("%w: link rejection %d", ErrNotFound, rejectionID)
		}
		return Link{}, fmt.Errorf("look up link rejection: %w", err)
	}
	if rejection.RejectedBy != actorID {
		return Link{}, fmt.Errorf("%w: only the person who rejected a request may undo it", ErrNotAuthorized)
	}
	if rejection.RestoredAt != nil {
		return Link{}, fmt.Errorf("%w: this rejection has already been undone", ErrValidation)
	}
	if _, err := s.spendUndo(ctx, token, undoLinkRejection, rejectionID, actorID); err != nil {
		return Link{}, err
	}
	var linkID int64
	err = s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.ensureNotRequestedSince(ctx, rejection); err != nil {
			return err
		}
		if err := tx.ensureNoCycle(ctx, rejection.ChildID, rejection.ParentID); err != nil {
			return err
		}
		now := tx.clock.Now()
		restoredAt := now.Format(timeFormat)
		n, err := tx.queries.MarkRejectedLinkRequestRestored(ctx, db.MarkRejectedLinkRequestRestoredParams{
			RestoredAt: &restoredAt,
			ID:         rejectionID,
		})
		if err != nil {
			return fmt.Errorf("mark link rejection restored: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: this rejection has already been undone", ErrValidation)
		}
		row, err := tx.queries.CreateLink(ctx, db.CreateLinkParams{
			ChildID:     rejection.ChildID,
			ParentID:    rejection.ParentID,
			Status:      LinkPending,
			Note:        rejection.Note,
			RequestedBy: rejection.RequestedBy,
			CreatedAt:   rejection.RequestCreatedAt,
		})
		if err != nil {
			return fmt.Errorf("restore link request: %w", err)
		}
		linkID = row.ID
		if err := tx.recordLinkEvent(ctx, row.ChildID, row.ParentID, LinkEventRejectionUndone, actorID, now); err != nil {
			return err
		}
		return tx.closeSuggestionsForLink(ctx, row.ChildID, row.ParentID)
	})
	if err != nil {
		return Link{}, err
	}
	return s.loadLink(ctx, linkID)
}

// ensureNotRequestedSince refuses to restore rejection when the same link has
// been requested or accepted again since: it exists now, a later request for
// it was rejected too, or it was accepted and has since been removed.
func (s *Service) ensureNotRequestedSince(ctx context.Context, rejection db.RejectedLinkRequest) error {
	again := fmt.Errorf("%w: this link has been requested again since it was rejected", ErrValidation)
	if _, err := s.queries.GetLinkByChildParent(ctx, db.GetLinkByChildParentParams{
		ChildID:  rejection.ChildID,
		ParentID: rejection.ParentID,
	}); err == nil {
		return again
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("look up existing link: %w", err)
	}
	later, err := s.queries.CountLaterRejectedLinkRequests(ctx, db.CountLaterRejectedLinkRequestsParams{
		ChildID:  rejection.ChildID,
		ParentID: rejection.ParentID,
		ID:       rejection.ID,
	})
	if err != nil {
		return fmt.Errorf("look up later rejections: %w", err)
	}
	if later > 0 {
		return again
	}
	removals, err := s.queries.ListLinkRemovalTimes(ctx, db.ListLinkRemovalTimesParams{
		ChildID:  rejection.ChildID,
		ParentID: rejection.ParentID,
	})
	if err != nil {
		return fmt.Errorf("look up link removals: %w", err)
	}
	rejectedAt, _ := time.Parse(timeFormat, rejection.RejectedAt)
	for _, raw := range removals {
		// A removal no earlier than the rejection is of a link made since.
		if removedAt, _ := time.Parse(timeFormat, raw); !removedAt.Before(rejectedAt) {
			return again
		}
	}
	return nil
}

// LinkRejection returns the record of a rejected request, with both Goals
// resolved, so a page can say what was rejected and offer its rejecter the
// Undo.
func (s *Service) LinkRejection(ctx context.Context, rejectionID int64) (LinkRejection, error) {
	row, err := s.queries.GetRejectedLinkRequest(ctx, rejectionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LinkRejection{}, fmt.Errorf("%w: link rejection %d", ErrNotFound, rejectionID)
		}
		return LinkRejection{}, fmt.Errorf("look up link rejection: %w", err)
	}
	child, err := s.queries.GetGoal(ctx, row.ChildID)
	if err != nil {
		return LinkRejection{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return LinkRejection{}, fmt.Errorf("look up parent goal: %w", err)
	}
	rejectedAt, _ := time.Parse(timeFormat, row.RejectedAt)
	return LinkRejection{
		ID:         row.ID,
		Child:      goalFromRow(child.Goal, child.Account),
		Parent:     goalFromRow(parent.Goal, parent.Account),
		RejectedBy: row.RejectedBy,
		RejectedAt: rejectedAt,
		Restored:   row.RestoredAt != nil,
	}, nil
}

// LinkRemoval is the record a removed link leaves: the two Goals it connected,
// who removed it and when, and whether that removal has since been undone. It
// is what lets the remover Undo the removal without asking the parent's Owner
// to accept the link again.
type LinkRemoval struct {
	ID        int64
	Child     Goal
	Parent    Goal
	RemovedBy int64
	RemovedAt time.Time
	Restored  bool
	// UndoToken is the one-time token RestoreLink takes from the remover. Only
	// RemoveLink sets it.
	UndoToken string
}

// RemoveLink removes an accepted link, deleting it and recording the removal so
// the remover can Undo it (RestoreLink) with the token it returns, for
// UndoWindow. Either the child's Owner or the parent's Owner may remove it.
func (s *Service) RemoveLink(ctx context.Context, linkID, actorID int64) (LinkRemoval, error) {
	row, err := s.queries.GetLink(ctx, linkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LinkRemoval{}, fmt.Errorf("%w: link %d", ErrNotFound, linkID)
		}
		return LinkRemoval{}, fmt.Errorf("look up link: %w", err)
	}
	if row.Status != LinkAccepted {
		return LinkRemoval{}, fmt.Errorf("%w: link %d is not accepted", ErrNotFound, linkID)
	}
	child, err := s.queries.GetGoal(ctx, row.ChildID)
	if err != nil {
		return LinkRemoval{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return LinkRemoval{}, fmt.Errorf("look up parent goal: %w", err)
	}
	if child.Goal.OwnerID != actorID && parent.Goal.OwnerID != actorID {
		return LinkRemoval{}, fmt.Errorf("%w: only an Owner of the linked Goals may remove the link", ErrNotAuthorized)
	}
	now := s.clock.Now()
	var removal db.LinkRemoval
	var token string
	err = s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.queries.DeleteLink(ctx, linkID); err != nil {
			return fmt.Errorf("remove link: %w", err)
		}
		removal, err = tx.queries.CreateLinkRemoval(ctx, db.CreateLinkRemovalParams{
			ChildID:       row.ChildID,
			ParentID:      row.ParentID,
			Note:          row.Note,
			RequestedBy:   row.RequestedBy,
			LinkCreatedAt: row.CreatedAt,
			RemovedBy:     actorID,
			RemovedAt:     now.Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("record link removal: %w", err)
		}
		if err := tx.recordLinkEvent(ctx, row.ChildID, row.ParentID, LinkEventRemoved, actorID, now); err != nil {
			return err
		}
		token, err = tx.issueUndo(ctx, undoLinkRemoval, removal.ID, actorID, "")
		return err
	})
	if err != nil {
		return LinkRemoval{}, err
	}
	return LinkRemoval{
		ID:        removal.ID,
		Child:     goalFromRow(child.Goal, child.Account),
		Parent:    goalFromRow(parent.Goal, parent.Account),
		RemovedBy: actorID,
		RemovedAt: now,
		UndoToken: token,
	}, nil
}

// LinkRemoval returns the record of a removed link, with both Goals resolved,
// so a page can say what was removed and offer its remover the Undo.
func (s *Service) LinkRemoval(ctx context.Context, removalID int64) (LinkRemoval, error) {
	row, err := s.queries.GetLinkRemoval(ctx, removalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LinkRemoval{}, fmt.Errorf("%w: link removal %d", ErrNotFound, removalID)
		}
		return LinkRemoval{}, fmt.Errorf("look up link removal: %w", err)
	}
	child, err := s.queries.GetGoal(ctx, row.ChildID)
	if err != nil {
		return LinkRemoval{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return LinkRemoval{}, fmt.Errorf("look up parent goal: %w", err)
	}
	removedAt, _ := time.Parse(timeFormat, row.RemovedAt)
	return LinkRemoval{
		ID:        row.ID,
		Child:     goalFromRow(child.Goal, child.Account),
		Parent:    goalFromRow(parent.Goal, parent.Account),
		RemovedBy: row.RemovedBy,
		RemovedAt: removedAt,
		Restored:  row.RestoredAt != nil,
	}, nil
}

// RestoreLink undoes a removal, putting the link back as accepted between the
// same two Goals with its original note, without asking the parent's Owner to
// accept it again: they accepted it once. Because that skips acceptance, only
// the person who removed the link may restore it, with the token RemoveLink
// gave them, within UndoWindow, only while they still own one of its Goals,
// and only once. Presenting the token spends it, even when the Undo is then
// refused. It is refused, changing nothing, when the same link has been
// requested, accepted, removed or rejected again since, or restoring it would
// now close a cycle (ADR-0001).
func (s *Service) RestoreLink(ctx context.Context, removalID, actorID int64, token string) (Link, error) {
	removal, err := s.queries.GetLinkRemoval(ctx, removalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Link{}, fmt.Errorf("%w: link removal %d", ErrNotFound, removalID)
		}
		return Link{}, fmt.Errorf("look up link removal: %w", err)
	}
	if removal.RemovedBy != actorID {
		return Link{}, fmt.Errorf("%w: only the person who removed a link may undo it", ErrNotAuthorized)
	}
	if removal.RestoredAt != nil {
		return Link{}, fmt.Errorf("%w: this removal has already been undone", ErrValidation)
	}
	child, err := s.queries.GetGoal(ctx, removal.ChildID)
	if err != nil {
		return Link{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, removal.ParentID)
	if err != nil {
		return Link{}, fmt.Errorf("look up parent goal: %w", err)
	}
	if child.Goal.OwnerID != actorID && parent.Goal.OwnerID != actorID {
		return Link{}, fmt.Errorf("%w: only an Owner of the linked Goals may undo removing the link", ErrNotAuthorized)
	}
	if _, err := s.spendUndo(ctx, token, undoLinkRemoval, removalID, actorID); err != nil {
		return Link{}, err
	}
	var linkID int64
	err = s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.queries.GetLinkByChildParent(ctx, db.GetLinkByChildParentParams{
			ChildID:  removal.ChildID,
			ParentID: removal.ParentID,
		}); err == nil {
			return fmt.Errorf("%w: a link between these Goals already exists again", ErrValidation)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up existing link: %w", err)
		}
		if err := tx.ensureNotLinkedSince(ctx, removal); err != nil {
			return err
		}
		if err := tx.ensureNoCycle(ctx, removal.ChildID, removal.ParentID); err != nil {
			return err
		}
		now := tx.clock.Now()
		restoredAt := now.Format(timeFormat)
		n, err := tx.queries.MarkLinkRemovalRestored(ctx, db.MarkLinkRemovalRestoredParams{
			RestoredAt: &restoredAt,
			ID:         removalID,
		})
		if err != nil {
			return fmt.Errorf("mark link removal restored: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: this removal has already been undone", ErrValidation)
		}
		row, err := tx.queries.CreateLink(ctx, db.CreateLinkParams{
			ChildID:     removal.ChildID,
			ParentID:    removal.ParentID,
			Status:      LinkAccepted,
			Note:        removal.Note,
			RequestedBy: removal.RequestedBy,
			CreatedAt:   removal.LinkCreatedAt,
		})
		if err != nil {
			return fmt.Errorf("restore link: %w", err)
		}
		linkID = row.ID
		if err := tx.recordLinkEvent(ctx, row.ChildID, row.ParentID, LinkEventRemovalUndone, actorID, now); err != nil {
			return err
		}
		return tx.closeSuggestionsForLink(ctx, row.ChildID, row.ParentID)
	})
	if err != nil {
		return Link{}, err
	}
	return s.loadLink(ctx, linkID)
}

// ensureNotLinkedSince refuses to restore removal when the same link has been
// made or asked for again since: a later removal of it, or a request for it
// rejected no earlier than the removal. (A link that exists again is refused
// on its own.)
func (s *Service) ensureNotLinkedSince(ctx context.Context, removal db.LinkRemoval) error {
	again := fmt.Errorf("%w: this link has been requested again since it was removed", ErrValidation)
	later, err := s.queries.CountLaterLinkRemovals(ctx, db.CountLaterLinkRemovalsParams{
		ChildID:  removal.ChildID,
		ParentID: removal.ParentID,
		ID:       removal.ID,
	})
	if err != nil {
		return fmt.Errorf("look up later removals: %w", err)
	}
	if later > 0 {
		return again
	}
	rejections, err := s.queries.ListLinkRejectionTimes(ctx, db.ListLinkRejectionTimesParams{
		ChildID:  removal.ChildID,
		ParentID: removal.ParentID,
	})
	if err != nil {
		return fmt.Errorf("look up link rejections: %w", err)
	}
	removedAt, _ := time.Parse(timeFormat, removal.RemovedAt)
	for _, raw := range rejections {
		// A rejection no earlier than the removal is of a request made since.
		if rejectedAt, _ := time.Parse(timeFormat, raw); !rejectedAt.Before(removedAt) {
			return again
		}
	}
	return nil
}

// PendingLinkRequests returns the requests awaiting a decision from ownerID, the
// Owner of each request's parent Goal.
func (s *Service) PendingLinkRequests(ctx context.Context, ownerID int64) ([]Link, error) {
	rows, err := s.queries.ListPendingLinksForOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list pending links: %w", err)
	}
	links := make([]Link, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.Link.CreatedAt)
		links = append(links, Link{
			ID:        r.Link.ID,
			Child:     goalFromRow(r.Goal, r.Account),     // child goal + its Owner
			Parent:    goalFromRow(r.Goal_2, r.Account_2), // parent goal + its Owner
			Status:    r.Link.Status,
			Note:      r.Link.Note,
			CreatedAt: createdAt,
		})
	}
	return links, nil
}

// GoalLink pairs a linked Goal with the accepted link that connects it, so a
// caller can both navigate to the Goal and remove the link.
type GoalLink struct {
	LinkID int64
	Goal   Goal
}

// ParentLinks returns goalID's accepted parents paired with their link ids, for
// navigation and removal.
func (s *Service) ParentLinks(ctx context.Context, goalID int64) ([]GoalLink, error) {
	rows, err := s.queries.ListParentGoals(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list parent goals: %w", err)
	}
	links := make([]GoalLink, 0, len(rows))
	for _, r := range rows {
		links = append(links, GoalLink{LinkID: r.LinkID, Goal: goalFromRow(r.Goal, r.Account)})
	}
	return links, nil
}

// PendingParentLinks returns the parents goalID has requested and is waiting
// on, oldest request first, each paired with its request's link id.
func (s *Service) PendingParentLinks(ctx context.Context, goalID int64) ([]GoalLink, error) {
	rows, err := s.queries.ListPendingParentLinks(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list pending parent links: %w", err)
	}
	links := make([]GoalLink, 0, len(rows))
	for _, r := range rows {
		links = append(links, GoalLink{LinkID: r.LinkID, Goal: goalFromRow(r.Goal, r.Account)})
	}
	return links, nil
}

// ChildLinks returns goalID's accepted children paired with their link ids, for
// navigation and removal.
func (s *Service) ChildLinks(ctx context.Context, goalID int64) ([]GoalLink, error) {
	rows, err := s.queries.ListChildGoals(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list child goals: %w", err)
	}
	links := make([]GoalLink, 0, len(rows))
	for _, r := range rows {
		links = append(links, GoalLink{LinkID: r.LinkID, Goal: goalFromRow(r.Goal, r.Account)})
	}
	return links, nil
}

// ParentsOf returns the Goals goalID contributes to through accepted links.
func (s *Service) ParentsOf(ctx context.Context, goalID int64) ([]Goal, error) {
	rows, err := s.queries.ListParentGoals(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list parent goals: %w", err)
	}
	goals := make([]Goal, 0, len(rows))
	for _, r := range rows {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
	}
	return goals, nil
}

// ChildrenOf returns the Goals that contribute to goalID through accepted links.
func (s *Service) ChildrenOf(ctx context.Context, goalID int64) ([]Goal, error) {
	rows, err := s.queries.ListChildGoals(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list child goals: %w", err)
	}
	goals := make([]Goal, 0, len(rows))
	for _, r := range rows {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
	}
	return goals, nil
}

// getPendingLink loads a link that must exist and still be pending.
func (s *Service) getPendingLink(ctx context.Context, linkID int64) (db.Link, error) {
	row, err := s.queries.GetLink(ctx, linkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Link{}, fmt.Errorf("%w: link %d", ErrNotFound, linkID)
		}
		return db.Link{}, fmt.Errorf("look up link: %w", err)
	}
	if row.Status != LinkPending {
		return db.Link{}, fmt.Errorf("%w: link %d is not pending", ErrNotFound, linkID)
	}
	return row, nil
}

// loadLink loads a link with both endpoint Goals resolved.
func (s *Service) loadLink(ctx context.Context, linkID int64) (Link, error) {
	row, err := s.queries.GetLink(ctx, linkID)
	if err != nil {
		return Link{}, fmt.Errorf("look up link: %w", err)
	}
	child, err := s.queries.GetGoal(ctx, row.ChildID)
	if err != nil {
		return Link{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return Link{}, fmt.Errorf("look up parent goal: %w", err)
	}
	createdAt, _ := time.Parse(timeFormat, row.CreatedAt)
	return Link{
		ID:        row.ID,
		Child:     goalFromRow(child.Goal, child.Account),
		Parent:    goalFromRow(parent.Goal, parent.Account),
		Status:    row.Status,
		Note:      row.Note,
		CreatedAt: createdAt,
	}, nil
}
