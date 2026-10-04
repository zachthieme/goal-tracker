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

// ParentSuggestion is a proposal, from someone other than a Goal's Owner, that
// the Goal contribute to Parent (CONTEXT.md: Parent suggestion). It is its own
// record, not a link status (ADR 0006): only the Goal's Owner decides it, and
// accepting it requests the link as if they had. Every suggestion is kept with
// its outcome, in Status.
type ParentSuggestion struct {
	ID          int64
	Goal        Goal
	Parent      Goal
	SuggestedBy Account
	Note        string
	Status      string
	CreatedAt   time.Time
	// ClosedAt is when the suggestion got its outcome, the zero time while it
	// is open.
	ClosedAt time.Time
}

// Parent suggestion outcomes. A suggestion is open until its Goal's Owner
// accepts or declines it, its suggester withdraws it, or it no longer applies:
// the link it proposes came about another way, or its Goal or parent became
// Done or Cancelled.
const (
	SuggestionOpen            = "open"
	SuggestionAccepted        = "accepted"
	SuggestionDeclined        = "declined"
	SuggestionWithdrawn       = "withdrawn"
	SuggestionNoLongerApplies = "no longer applies"
)

// SuggestParent suggests, on behalf of actorID, that goalID contribute to
// parentID, with an optional note. Anyone signed in may but the Goal's Owner,
// who requests the link instead (RequestLink). The Goal and the parent must
// each be Active or Proposed. It is refused when the parent is already linked
// or Pending for the Goal, or already suggested and still open, and with
// ErrCycle when the link would make a cycle (ADR-0001).
func (s *Service) SuggestParent(ctx context.Context, actorID, goalID, parentID int64, note string) (ParentSuggestion, error) {
	goal, err := s.queries.GetGoal(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ParentSuggestion{}, fmt.Errorf("%w: goal %d", ErrNotFound, goalID)
		}
		return ParentSuggestion{}, fmt.Errorf("look up goal: %w", err)
	}
	if goal.Goal.OwnerID == actorID {
		return ParentSuggestion{}, fmt.Errorf("%w: the Goal's Owner links it to a parent rather than suggesting one", ErrNotAuthorized)
	}
	if !suggestable(goal.Goal.Lifecycle) {
		return ParentSuggestion{}, fmt.Errorf("%w: a parent can only be suggested for an Active or Proposed Goal", ErrValidation)
	}
	if parentID == goalID {
		return ParentSuggestion{}, fmt.Errorf("%w: a Goal cannot contribute to itself", ErrValidation)
	}
	parent, err := s.queries.GetGoal(ctx, parentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ParentSuggestion{}, fmt.Errorf("%w: parent goal does not exist", ErrValidation)
		}
		return ParentSuggestion{}, fmt.Errorf("look up parent goal: %w", err)
	}
	if !suggestable(parent.Goal.Lifecycle) {
		return ParentSuggestion{}, fmt.Errorf("%w: only an Active or Proposed Goal can be suggested as a parent", ErrValidation)
	}
	if link, err := s.queries.GetLinkByChildParent(ctx, db.GetLinkByChildParentParams{
		ChildID:  goalID,
		ParentID: parentID,
	}); err == nil {
		if link.Status == LinkPending {
			return ParentSuggestion{}, fmt.Errorf("%w: the Goal's Owner has already asked to link it to that parent", ErrValidation)
		}
		return ParentSuggestion{}, fmt.Errorf("%w: the Goal already contributes to that parent", ErrValidation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ParentSuggestion{}, fmt.Errorf("look up existing link: %w", err)
	}
	if open, err := s.queries.GetOpenParentSuggestionFor(ctx, db.GetOpenParentSuggestionForParams{
		GoalID:   goalID,
		ParentID: parentID,
	}); err == nil {
		return ParentSuggestion{}, fmt.Errorf("%w: %s has already suggested that parent", ErrValidation, accountFromRow(open.Account).Label())
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ParentSuggestion{}, fmt.Errorf("look up open suggestion: %w", err)
	}
	if err := s.ensureNoCycle(ctx, goalID, parentID); err != nil {
		return ParentSuggestion{}, err
	}
	row, err := s.queries.CreateParentSuggestion(ctx, db.CreateParentSuggestionParams{
		GoalID:      goalID,
		ParentID:    parentID,
		SuggestedBy: actorID,
		Note:        strings.TrimSpace(note),
		CreatedAt:   s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return ParentSuggestion{}, fmt.Errorf("record parent suggestion: %w", err)
	}
	return s.ParentSuggestion(ctx, row.ID)
}

// AcceptParentSuggestion accepts an open suggestion on behalf of actorID, who
// must own its Goal, and requests the link as if they had (RequestLink), all
// in one transaction. When the request waits Pending and the suggester owns
// the parent, they accept it too: both people who must agree have.
func (s *Service) AcceptParentSuggestion(ctx context.Context, actorID, suggestionID int64) (Link, error) {
	var link Link
	err := s.WithinTx(ctx, func(tx *Service) error {
		p, err := tx.openSuggestionForOwner(ctx, actorID, suggestionID)
		if err != nil {
			return err
		}
		if err := tx.closeSuggestion(ctx, p.ID, SuggestionAccepted); err != nil {
			return err
		}
		link, err = tx.RequestLink(ctx, RequestLinkInput{
			ChildID:     p.Goal.ID,
			ParentID:    p.Parent.ID,
			Note:        p.Note,
			RequesterID: actorID,
		})
		if err != nil {
			return err
		}
		if link.Status == LinkPending && link.Parent.Owner.ID == p.SuggestedBy.ID {
			link, err = tx.AcceptLink(ctx, link.ID, p.SuggestedBy.ID)
		}
		return err
	})
	if err != nil {
		return Link{}, err
	}
	return link, nil
}

// openSuggestionForOwner loads a suggestion actorID may decide: one that is
// still open, on a Goal they own.
func (s *Service) openSuggestionForOwner(ctx context.Context, actorID, suggestionID int64) (ParentSuggestion, error) {
	p, err := s.ParentSuggestion(ctx, suggestionID)
	if err != nil {
		return ParentSuggestion{}, err
	}
	if p.Goal.Owner.ID != actorID {
		return ParentSuggestion{}, fmt.Errorf("%w: only the Goal's Owner may decide a parent suggestion", ErrNotAuthorized)
	}
	if p.Status != SuggestionOpen {
		return ParentSuggestion{}, errSuggestionClosed
	}
	return p, nil
}

// errSuggestionClosed refuses to act on a suggestion that already has its
// outcome.
var errSuggestionClosed = fmt.Errorf("%w: this suggestion has already been decided", ErrValidation)

// closeSuggestion gives the open suggestion its outcome now, refusing one that
// already has one.
func (s *Service) closeSuggestion(ctx context.Context, suggestionID int64, outcome string) error {
	closedAt := s.clock.Now().Format(timeFormat)
	n, err := s.queries.CloseParentSuggestion(ctx, db.CloseParentSuggestionParams{
		Status:   outcome,
		ClosedAt: &closedAt,
		ID:       suggestionID,
	})
	if err != nil {
		return fmt.Errorf("close parent suggestion: %w", err)
	}
	if n == 0 {
		return errSuggestionClosed
	}
	return nil
}

// ParentSuggestion returns a suggestion with its Goal, parent and suggester.
func (s *Service) ParentSuggestion(ctx context.Context, suggestionID int64) (ParentSuggestion, error) {
	row, err := s.queries.GetParentSuggestion(ctx, suggestionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ParentSuggestion{}, fmt.Errorf("%w: parent suggestion %d", ErrNotFound, suggestionID)
		}
		return ParentSuggestion{}, fmt.Errorf("look up parent suggestion: %w", err)
	}
	return suggestionFromRow(row.ParentSuggestion, row.Goal, row.Account, row.Goal_2, row.Account_2, row.Account_3), nil
}

// OpenParentSuggestions returns goalID's open suggestions, oldest first.
func (s *Service) OpenParentSuggestions(ctx context.Context, goalID int64) ([]ParentSuggestion, error) {
	all, err := s.ParentSuggestions(ctx, goalID)
	if err != nil {
		return nil, err
	}
	var open []ParentSuggestion
	for _, p := range all {
		if p.Status == SuggestionOpen {
			open = append(open, p)
		}
	}
	return open, nil
}

// ParentSuggestions returns every suggestion made for goalID with its
// outcome, oldest first, for the Goal's History.
func (s *Service) ParentSuggestions(ctx context.Context, goalID int64) ([]ParentSuggestion, error) {
	rows, err := s.queries.ListParentSuggestionsForGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list parent suggestions: %w", err)
	}
	out := make([]ParentSuggestion, 0, len(rows))
	for _, r := range rows {
		out = append(out, suggestionFromRow(r.ParentSuggestion, r.Goal, r.Account, r.Goal_2, r.Account_2, r.Account_3))
	}
	return out, nil
}

// OpenParentSuggestionsFor returns the open suggestions on the Goals ownerID
// Owns, awaiting their decision, oldest first.
func (s *Service) OpenParentSuggestionsFor(ctx context.Context, ownerID int64) ([]ParentSuggestion, error) {
	rows, err := s.queries.ListOpenParentSuggestionsForOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list open parent suggestions: %w", err)
	}
	out := make([]ParentSuggestion, 0, len(rows))
	for _, r := range rows {
		out = append(out, suggestionFromRow(r.ParentSuggestion, r.Goal, r.Account, r.Goal_2, r.Account_2, r.Account_3))
	}
	return out, nil
}

func suggestionFromRow(p db.ParentSuggestion, goal db.Goal, goalOwner db.Account, parent db.Goal, parentOwner db.Account, suggester db.Account) ParentSuggestion {
	createdAt, _ := time.Parse(timeFormat, p.CreatedAt)
	var closedAt time.Time
	if p.ClosedAt != nil {
		closedAt, _ = time.Parse(timeFormat, *p.ClosedAt)
	}
	return ParentSuggestion{
		ID:          p.ID,
		Goal:        goalFromRow(goal, goalOwner),
		Parent:      goalFromRow(parent, parentOwner),
		SuggestedBy: accountFromRow(suggester),
		Note:        p.Note,
		Status:      p.Status,
		CreatedAt:   createdAt,
		ClosedAt:    closedAt,
	}
}

// suggestable reports whether a Goal in lifecycle may have a parent suggested
// for it, or be suggested as one: only while it is Active or Proposed.
func suggestable(lifecycle string) bool {
	return lifecycle == LifecycleActive || lifecycle == LifecycleProposed
}
