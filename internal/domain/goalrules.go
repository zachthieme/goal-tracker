package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The Goal rules: who may make each Goal command, and in which Lifecycle, each
// a pure function of the actor and the Goal. The command evaluates its rule
// inside its transaction and the Goal page (permitGoalPage) evaluates the
// same one, so the page offers just what the command would accept. Each rule
// refuses for who the actor is first, with ErrNotAuthorized, then for the
// Goal's Lifecycle or state, with ErrValidation.

// goalActor is the person a Goal rule judges, as the command or the Goal page
// knows them: their Account ID, and the facts about them a rule may ask for,
// each read only when it is asked.
type goalActor struct {
	id int64
	// account reads their Account; ok is false when none exists.
	account func() (a Account, ok bool, err error)
	// delegate says whether they are one of the Goal's Delegates.
	delegate func() (bool, error)
	// importer is true when they are the Admin whose import created the Goal,
	// who makes its creation-time writes as its Owner would (ForImportBy).
	importer bool
}

// goalActorFor is actorID as a Goal rule judges them on goalID, reading their
// Account and whether they are a Delegate through s when a rule asks.
func (s *Service) goalActorFor(ctx context.Context, actorID, goalID int64) goalActor {
	return goalActor{
		id: actorID,
		account: func() (Account, bool, error) {
			row, err := s.queries.GetAccount(ctx, actorID)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				return Account{}, false, nil
			case err != nil:
				return Account{}, false, fmt.Errorf("look up actor: %w", err)
			}
			return accountFromRow(row), true, nil
		},
		delegate: func() (bool, error) { return s.isDelegate(ctx, goalID, actorID) },
		importer: s.importedBy(actorID, goalID),
	}
}

// goalRule loads goalID and evaluates rule for actorID on it, as a command
// does inside its transaction; a missing Goal is ErrNotFound.
func (s *Service) goalRule(ctx context.Context, actorID, goalID int64, rule func(goalActor, Goal) error) error {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return err
	}
	return rule(s.goalActorFor(ctx, actorID, goalID), g)
}

// checkinAuthorRuleRefusal refuses a in anyone but g's Owner or a Delegate,
// neither Departed, by checkinAuthorRefusal: who may write g's Check-ins.
func checkinAuthorRuleRefusal(a goalActor, g Goal) error {
	acc, ok, err := a.account()
	if err != nil {
		return err
	}
	if !ok {
		return Refuse(ErrNotAuthorized, "the author does not exist")
	}
	return checkinAuthorRefusal(acc, g.Owner.ID, a.delegate)
}

// milestoneEditorRefusal refuses a anyone but those who may change g's
// Milestones: its Owner or a Delegate, by the rule for who writes its
// Check-ins, or the Admin whose import created it.
func milestoneEditorRefusal(a goalActor, g Goal) error {
	if a.importer {
		return nil
	}
	err := checkinAuthorRuleRefusal(a, g)
	if errors.Is(err, ErrNotAuthorized) {
		return errMilestonesOwnerOrDelegateOnly
	}
	return err
}

// milestoneEditRefusal is EditMilestone's rule for a on one of g's
// Milestones, whose status is status: those milestoneEditorRefusal allows may
// rename or re-date it only while g is Proposed and it is Planned. After that
// its date moves in a Check-in, as a Date Slip with a reason (CONTEXT.md:
// Milestone, Date Slip). The Goal page asks it of a Planned Milestone.
func milestoneEditRefusal(a goalActor, g Goal, status string) error {
	if err := milestoneEditorRefusal(a, g); err != nil {
		return err
	}
	if g.Lifecycle != LifecycleProposed {
		return Refuse(ErrValidation, "a Milestone is edited only while its Goal is Proposed: change its date in a Check-in, with a reason")
	}
	if status != MilestonePlanned {
		return Refuse(ErrValidation, "a Milestone is edited only while it is Planned, and this one is %s", status)
	}
	return nil
}
