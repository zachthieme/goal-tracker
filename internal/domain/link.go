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
//
// Each name starts Link, not LinkEvent, which names another group: the CHECK
// test on links.status finds its values by that prefix (enumchecks_test.go), so
// a value named otherwise goes unchecked.
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
// otherwise it waits Pending for the parent's Owner. A link to the Goal itself,
// or to a parent that doesn't exist, is refused (parentLinkProblems), and a
// request that would close a cycle is rejected here and again at accept time
// (ADR-0001). Any open Parent
// suggestion of the same parent for the child closes as no longer applying, as
// it does when ImportLink makes the link or RestoreLinkRequest or RestoreLink
// brings it back.
func (s *Service) RequestLink(ctx context.Context, in RequestLinkInput) (Link, error) {
	now := s.clock.Now()
	var child, parent Goal
	var row db.Link
	err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		if child, err = tx.openGoalCommand(ctx, in.RequesterID, in.ChildID, func(a goalActor, g Goal) error {
			return goalOwnerRefusal(a, g, "link it to a parent")
		}, nil); err != nil {
			return err
		}
		if parent, err = tx.linkParentGoal(ctx, in.ChildID, in.ParentID); err != nil {
			return err
		}
		status := LinkPending
		if parent.Owner.ID == in.RequesterID {
			status = LinkAccepted
		}
		// The duplicate and cycle checks read inside the transaction that
		// writes the link, so two requests sent together (A→B and B→A) can't
		// both pass them (ADR-0001).
		if err := tx.ensureNewAcyclicLink(ctx, in.ChildID, in.ParentID); err != nil {
			return err
		}
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
		Child:     child,
		Parent:    parent,
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
// applying (ADR-0006). Only the Admin running the import, actorID, may make one
// (ErrNotAuthorized; see ForImportBy).
func (s *Service) ImportLink(ctx context.Context, actorID, childID, parentID int64) (Link, error) {
	if err := s.requireImportingAdmin(actorID); err != nil {
		return Link{}, err
	}
	now := s.clock.Now()
	var child, parent Goal
	var row db.Link
	err := s.WithinTx(ctx, func(tx *Service) error {
		// The importing Admin may link any Goal, so the child opens for
		// anyone.
		var err error
		if child, err = tx.openGoalCommand(ctx, actorID, childID, nil, nil); err != nil {
			return err
		}
		if parent, err = tx.linkParentGoal(ctx, childID, parentID); err != nil {
			return err
		}
		if err := tx.ensureNewAcyclicLink(ctx, childID, parentID); err != nil {
			return err
		}
		row, err = tx.queries.CreateLink(ctx, db.CreateLinkParams{
			ChildID:     childID,
			ParentID:    parentID,
			Status:      LinkAccepted,
			Note:        "",
			RequestedBy: child.Owner.ID,
			CreatedAt:   now.Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create link: %w", err)
		}
		if err := tx.recordLinkEvent(ctx, childID, parentID, LinkEventLinked, child.Owner.ID, now); err != nil {
			return err
		}
		return tx.closeSuggestionsForLink(ctx, childID, parentID)
	})
	if err != nil {
		return Link{}, err
	}
	return Link{
		ID:        row.ID,
		Child:     child,
		Parent:    parent,
		Status:    row.Status,
		Note:      row.Note,
		CreatedAt: now,
	}, nil
}

// linkParentGoal loads parentID, the parent childID's link is asked for,
// refusing the link by parentLinkProblems: one to the child itself, or to a
// parent that doesn't exist, is refused as input, ErrValidation.
func (s *Service) linkParentGoal(ctx context.Context, childID, parentID int64) (Goal, error) {
	parent, err := s.loadGoal(ctx, parentID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Goal{}, err
	}
	if err := joinInputErrors(parentLinkProblems(childID, parentID, err == nil, "parent_id")); err != nil {
		return Goal{}, err
	}
	return parent, nil
}

// parentLinkProblems is the one check of a parent a Goal is linked to, shared
// by RequestLink, ImportLink and Goal definition, refused under input: a Goal
// can't contribute to itself, and the parent must exist (parentExists, as the
// caller read it). childID is 0 for a Goal not yet saved.
func parentLinkProblems(childID, parentID int64, parentExists bool, input string) []*InputError {
	if childID != 0 && childID == parentID {
		return []*InputError{inputError(input, "a Goal cannot contribute to itself")}
	}
	if !parentExists {
		return []*InputError{inputError(input, "that parent Goal does not exist")}
	}
	return nil
}

// AcceptLink accepts a pending request. The actor must own the parent Goal. The
// cycle check is repeated because another accepted link may have appeared since
// the request was made (ADR-0001). Every check reads inside the transaction
// that accepts the link, so two requests accepted together (A→B and B→A)
// can't both pass, and a request rejected meanwhile isn't accepted.
func (s *Service) AcceptLink(ctx context.Context, linkID, actorID int64) (Link, error) {
	err := s.WithinTx(ctx, func(tx *Service) error {
		row, _, err := tx.linkToDecide(ctx, linkID, actorID, "accept")
		if err != nil {
			return err
		}
		if err := tx.ensureNoCycle(ctx, row.ChildID, row.ParentID); err != nil {
			return err
		}
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
// returns, for UndoWindow. The actor must own the parent Goal. The checks read
// inside the transaction that deletes the request, so a request accepted
// meanwhile isn't deleted and recorded as rejected.
func (s *Service) RejectLink(ctx context.Context, linkID, actorID int64) (LinkRejection, error) {
	now := s.clock.Now()
	var child, parent db.GetGoalRow
	var rejection db.RejectedLinkRequest
	var token string
	err := s.WithinTx(ctx, func(tx *Service) error {
		var row db.Link
		var err error
		row, parent, err = tx.linkToDecide(ctx, linkID, actorID, "reject")
		if err != nil {
			return err
		}
		child, err = tx.queries.GetGoal(ctx, row.ChildID)
		if err != nil {
			return fmt.Errorf("look up child goal: %w", err)
		}
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
		return Link{}, Refuse(ErrNotAuthorized, "only the person who rejected a request may undo it")
	}
	if rejection.RestoredAt != nil {
		return Link{}, Refuse(ErrValidation, "this rejection has already been undone")
	}
	if _, err := s.spendUndo(ctx, token, undoLinkRejection, rejectionID, actorID); err != nil {
		return Link{}, err
	}
	rejectedAt, err := time.Parse(timeFormat, rejection.RejectedAt)
	if err != nil {
		return Link{}, fmt.Errorf("link rejection %d: rejected_at %q: %w", rejection.ID, rejection.RejectedAt, err)
	}
	var linkID int64
	err = s.WithinTx(ctx, func(tx *Service) error {
		history, err := tx.linkPairHistory(ctx, rejection.ChildID, rejection.ParentID)
		if err != nil {
			return err
		}
		if err := history.requestRestoreRefusal(rejection.ID, rejectedAt); err != nil {
			return err
		}
		linkID, err = tx.restoreUndoneLink(ctx, undoneLink{
			childID:     rejection.ChildID,
			parentID:    rejection.ParentID,
			status:      LinkPending,
			note:        rejection.Note,
			requestedBy: rejection.RequestedBy,
			createdAt:   rejection.RequestCreatedAt,
			event:       LinkEventRejectionUndone,
			actorID:     actorID,
			record:      "link rejection",
			markRestored: func(restoredAt *string) (int64, error) {
				return tx.queries.MarkRejectedLinkRequestRestored(ctx, db.MarkRejectedLinkRequestRestoredParams{
					RestoredAt: restoredAt,
					ID:         rejectionID,
				})
			},
			alreadyUndone: "this rejection has already been undone",
			what:          "link request",
		})
		return err
	})
	if err != nil {
		return Link{}, err
	}
	return s.loadLink(ctx, linkID)
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
// UndoWindow. Either the child's Owner or the parent's Owner may remove it. The
// checks read inside the transaction that deletes the link, so a link removed
// or handed off meanwhile isn't removed by someone no longer entitled to.
func (s *Service) RemoveLink(ctx context.Context, linkID, actorID int64) (LinkRemoval, error) {
	now := s.clock.Now()
	var child, parent db.GetGoalRow
	var removal db.LinkRemoval
	var token string
	err := s.WithinTx(ctx, func(tx *Service) error {
		row, err := tx.queries.GetLink(ctx, linkID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: link %d", ErrNotFound, linkID)
			}
			return fmt.Errorf("look up link: %w", err)
		}
		if row.Status != LinkAccepted {
			return fmt.Errorf("%w: link %d is not accepted", ErrNotFound, linkID)
		}
		if child, err = tx.queries.GetGoal(ctx, row.ChildID); err != nil {
			return fmt.Errorf("look up child goal: %w", err)
		}
		if parent, err = tx.queries.GetGoal(ctx, row.ParentID); err != nil {
			return fmt.Errorf("look up parent goal: %w", err)
		}
		if err := linkRemoveRefusal(tx.accountActor(ctx, actorID), goalFromRow(parent.Goal, parent.Account), goalFromRow(child.Goal, child.Account)); err != nil {
			return err
		}
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
		return Link{}, Refuse(ErrNotAuthorized, "only the person who removed a link may undo it")
	}
	if removal.RestoredAt != nil {
		return Link{}, Refuse(ErrValidation, "this removal has already been undone")
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
		return Link{}, Refuse(ErrNotAuthorized, "only an Owner of the linked Goals may undo removing the link")
	}
	if _, err := s.spendUndo(ctx, token, undoLinkRemoval, removalID, actorID); err != nil {
		return Link{}, err
	}
	removedAt, err := time.Parse(timeFormat, removal.RemovedAt)
	if err != nil {
		return Link{}, fmt.Errorf("link removal %d: removed_at %q: %w", removal.ID, removal.RemovedAt, err)
	}
	var linkID int64
	err = s.WithinTx(ctx, func(tx *Service) error {
		history, err := tx.linkPairHistory(ctx, removal.ChildID, removal.ParentID)
		if err != nil {
			return err
		}
		if history.linked {
			return Refuse(ErrValidation, "a link between these Goals already exists again")
		}
		if err := history.linkRestoreRefusal(removal.ID, removedAt); err != nil {
			return err
		}
		linkID, err = tx.restoreUndoneLink(ctx, undoneLink{
			childID:     removal.ChildID,
			parentID:    removal.ParentID,
			status:      LinkAccepted,
			note:        removal.Note,
			requestedBy: removal.RequestedBy,
			createdAt:   removal.LinkCreatedAt,
			event:       LinkEventRemovalUndone,
			actorID:     actorID,
			record:      "link removal",
			markRestored: func(restoredAt *string) (int64, error) {
				return tx.queries.MarkLinkRemovalRestored(ctx, db.MarkLinkRemovalRestoredParams{
					RestoredAt: restoredAt,
					ID:         removalID,
				})
			},
			alreadyUndone: "this removal has already been undone",
			what:          "link",
		})
		return err
	})
	if err != nil {
		return Link{}, err
	}
	return s.loadLink(ctx, linkID)
}

// undoneLink is a link an Undo brings back, as restoreUndoneLink makes it:
// the link's own fields, the link event that records the Undo, and how to
// mark the undone rejection or removal restored.
type undoneLink struct {
	childID, parentID int64
	status            string
	note              string
	requestedBy       int64
	// createdAt is when the link was first requested, as stored.
	createdAt string
	// event is the kind of the link event that records the Undo, by actorID.
	event   string
	actorID int64
	// record names the undone rejection or removal in an error.
	record string
	// markRestored marks the undone record restored at restoredAt, only if it
	// isn't already, and says how many rows it touched.
	markRestored func(restoredAt *string) (int64, error)
	// alreadyUndone is the refusal when markRestored touched no row.
	alreadyUndone string
	// what names the link being restored in an error.
	what string
}

// restoreUndoneLink brings back link l inside the caller's transaction, once
// the caller has judged the pair's history: it refuses a link that would now
// close a cycle (ADR-0001), marks the undone record restored, makes the link,
// records the Undo, and closes any open Parent suggestion of the same parent
// for the child. It returns the new link's id.
func (s *Service) restoreUndoneLink(ctx context.Context, l undoneLink) (int64, error) {
	if err := s.ensureNoCycle(ctx, l.childID, l.parentID); err != nil {
		return 0, err
	}
	now := s.clock.Now()
	restoredAt := now.Format(timeFormat)
	n, err := l.markRestored(&restoredAt)
	if err != nil {
		return 0, fmt.Errorf("mark %s restored: %w", l.record, err)
	}
	if n == 0 {
		return 0, Refuse(ErrValidation, "%s", l.alreadyUndone)
	}
	row, err := s.queries.CreateLink(ctx, db.CreateLinkParams{
		ChildID:     l.childID,
		ParentID:    l.parentID,
		Status:      l.status,
		Note:        l.note,
		RequestedBy: l.requestedBy,
		CreatedAt:   l.createdAt,
	})
	if err != nil {
		return 0, fmt.Errorf("restore %s: %w", l.what, err)
	}
	if err := s.recordLinkEvent(ctx, row.ChildID, row.ParentID, l.event, l.actorID, now); err != nil {
		return 0, err
	}
	if err := s.closeSuggestionsForLink(ctx, row.ChildID, row.ParentID); err != nil {
		return 0, err
	}
	return row.ID, nil
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

// ParentsByGoal returns the Goals each of goalIDs contributes to, keyed by Goal
// ID, as ParentsOf returns one Goal's, in one query, so a page or file listing
// many Goals reads them all at once. A Goal with no parents has no entry.
func (s *Service) ParentsByGoal(ctx context.Context, goalIDs []int64) (map[int64][]Goal, error) {
	return s.parentsByGoal(ctx, goalIDs)
}

// parentsByGoal returns the accepted parents of each of goalIDs, as ParentsOf
// reads one Goal's, keyed by Goal ID, in one query. No Goals reads nothing.
func (s *Service) parentsByGoal(ctx context.Context, goalIDs []int64) (map[int64][]Goal, error) {
	out := map[int64][]Goal{}
	if len(goalIDs) == 0 {
		return out, nil
	}
	rows, err := s.queries.ListParentGoalsForGoals(ctx, goalIDs)
	if err != nil {
		return nil, fmt.Errorf("list parent goals: %w", err)
	}
	for _, r := range rows {
		out[r.ChildID] = append(out[r.ChildID], goalFromRow(r.Goal, r.Account))
	}
	return out, nil
}

// childrenByGoal returns the accepted children of each of goalIDs, as
// ChildrenOf reads one Goal's, keyed by Goal ID, in one query. No Goals reads
// nothing.
func (s *Service) childrenByGoal(ctx context.Context, goalIDs []int64) (map[int64][]Goal, error) {
	out := map[int64][]Goal{}
	if len(goalIDs) == 0 {
		return out, nil
	}
	rows, err := s.queries.ListChildGoalsForGoals(ctx, goalIDs)
	if err != nil {
		return nil, fmt.Errorf("list child goals: %w", err)
	}
	for _, r := range rows {
		out[r.ParentID] = append(out[r.ParentID], goalFromRow(r.Goal, r.Account))
	}
	return out, nil
}

// linkToDecide loads the link request actorID would decide (decision:
// "accept" or "reject"), with its parent Goal. The link must exist (a rejected
// one is gone), and only the parent's Owner may decide it, which is checked
// before whether it is still pending, so nobody else learns that it was
// accepted.
func (s *Service) linkToDecide(ctx context.Context, linkID, actorID int64, decision string) (db.Link, db.GetGoalRow, error) {
	row, err := s.queries.GetLink(ctx, linkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Link{}, db.GetGoalRow{}, fmt.Errorf("%w: link %d", ErrNotFound, linkID)
		}
		return db.Link{}, db.GetGoalRow{}, fmt.Errorf("look up link: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return db.Link{}, db.GetGoalRow{}, fmt.Errorf("look up parent goal: %w", err)
	}
	if parent.Goal.OwnerID != actorID {
		return db.Link{}, db.GetGoalRow{}, Refuse(ErrNotAuthorized, "only the parent's Owner may %s a link", decision)
	}
	if row.Status != LinkPending {
		return db.Link{}, db.GetGoalRow{}, Refuse(ErrValidation, "this link request has already been accepted")
	}
	return row, parent, nil
}

// ensureNewAcyclicLink refuses a link from childID to parentID when one
// between them already exists (ErrValidation) or when it would close a cycle
// (ErrCycle, ADR-0001). Called inside the transaction that creates the link, so
// the checks see every link committed before it.
func (s *Service) ensureNewAcyclicLink(ctx context.Context, childID, parentID int64) error {
	if _, err := s.queries.GetLinkByChildParent(ctx, db.GetLinkByChildParentParams{
		ChildID:  childID,
		ParentID: parentID,
	}); err == nil {
		return Refuse(ErrValidation, "a link between these Goals already exists")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("look up existing link: %w", err)
	}
	return s.ensureNoCycle(ctx, childID, parentID)
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
