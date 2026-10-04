package domain

import (
	"context"
	"fmt"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// LinkEvent is one change to a "contributes to" link (CONTEXT.md: Contributes
// to), as the History of either of its Goals shows it: what changed, who
// changed it and when. Read for one Goal, it names the Goal on the other side.
type LinkEvent struct {
	ID        int64
	ChildID   int64
	ParentID  int64
	Kind      string
	Actor     Account
	CreatedAt time.Time
	// GoalID is the Goal the event was read for, its child or its parent;
	// OtherID and OtherTitle are the Goal on the other side.
	GoalID     int64
	OtherID    int64
	OtherTitle string
}

// LinkEvent kinds. A link made Accepted without a separate accept (a request
// to a Goal the requester owns, or an import) is linked; one requested and then
// accepted is requested, then accepted.
const (
	LinkEventRequested       = "requested"
	LinkEventLinked          = "linked"
	LinkEventAccepted        = "accepted"
	LinkEventRejected        = "rejected"
	LinkEventRejectionUndone = "rejection-undone"
	LinkEventRemoved         = "removed"
	LinkEventRemovalUndone   = "removal-undone"
)

// OnChild reports whether the event was read for the link's child Goal.
func (e LinkEvent) OnChild() bool {
	return e.GoalID == e.ChildID
}

// LinkEvents returns every change to goalID's links, as its child or its
// parent, oldest first, for its History.
func (s *Service) LinkEvents(ctx context.Context, goalID int64) ([]LinkEvent, error) {
	rows, err := s.queries.ListLinkEventsForGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list link events: %w", err)
	}
	out := make([]LinkEvent, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.LinkEvent.CreatedAt)
		e := LinkEvent{
			ID:         r.LinkEvent.ID,
			ChildID:    r.LinkEvent.ChildID,
			ParentID:   r.LinkEvent.ParentID,
			Kind:       r.LinkEvent.Kind,
			Actor:      accountFromRow(r.Account),
			CreatedAt:  createdAt,
			GoalID:     goalID,
			OtherID:    r.LinkEvent.ParentID,
			OtherTitle: r.ParentTitle,
		}
		if !e.OnChild() {
			e.OtherID, e.OtherTitle = r.LinkEvent.ChildID, r.ChildTitle
		}
		out = append(out, e)
	}
	return out, nil
}

// recordLinkEvent logs a change of kind to the link from childID to parentID,
// made by actorID at at. Each link command calls it in the transaction that
// makes the change, so a refused or rolled-back change logs nothing.
func (s *Service) recordLinkEvent(ctx context.Context, childID, parentID int64, kind string, actorID int64, at time.Time) error {
	if _, err := s.queries.CreateLinkEvent(ctx, db.CreateLinkEventParams{
		ChildID:   childID,
		ParentID:  parentID,
		Kind:      kind,
		ActorID:   actorID,
		CreatedAt: at.Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("record link event: %w", err)
	}
	return nil
}
