package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
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
		id:       actorID,
		account:  s.actorAccount(ctx, actorID),
		delegate: func() (bool, error) { return s.isDelegate(ctx, goalID, actorID) },
		importer: s.importedBy(actorID, goalID),
	}
}

// accountActor is actorID as an Account command's rule judges them, by their
// Account alone: they are no Goal's Delegate or importer.
func (s *Service) accountActor(ctx context.Context, actorID int64) goalActor {
	return goalActor{
		id:       actorID,
		account:  s.actorAccount(ctx, actorID),
		delegate: func() (bool, error) { return false, nil },
	}
}

// actorAccount reads actorID's Account through s, ok false when none exists.
func (s *Service) actorAccount(ctx context.Context, actorID int64) func() (Account, bool, error) {
	return func() (Account, bool, error) {
		row, err := s.queries.GetAccount(ctx, actorID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return Account{}, false, nil
		case err != nil:
			return Account{}, false, fmt.Errorf("look up actor: %w", err)
		}
		return accountFromRow(row), true, nil
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

// checkinAuthorRuleRefusal is the rule for who writes g's Check-ins, in any
// Lifecycle: its Owner or a Delegate the Owner has authorized; anyone else is
// refused (CONTEXT.md: Delegate; acceptance: Non-Delegates can't submit
// Check-ins). A Departed person can't act, so is refused whichever they are
// (CONTEXT.md: Departed).
func checkinAuthorRuleRefusal(a goalActor, g Goal) error {
	acc, ok, err := a.account()
	if err != nil {
		return err
	}
	if !ok {
		return Refuse(ErrNotAuthorized, "the author does not exist")
	}
	if acc.Departed {
		return Refuse(ErrNotAuthorized, "a Departed person can't submit a Check-in")
	}
	if acc.ID == g.Owner.ID {
		return nil
	}
	if ok, err := a.delegate(); err != nil {
		return err
	} else if !ok {
		return Refuse(ErrNotAuthorized, "only the Owner or a Delegate may submit a Check-in")
	}
	return nil
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

// goalOwnerRefusal is the one Owner-only rule: it refuses a anyone but g's
// Owner, or the Admin whose import created it, saying only the Owner may act;
// a Delegate and an Admin are refused too. It holds in any Lifecycle. An
// Owner who has Departed can't act, and their Goal is Ownerless by definition
// (CONTEXT.md: Departed, Ownerless), so g.Ownerless refuses them. The
// importer acts for the Owner, so a Goal an import row gives a Departed Owner
// still takes the import's writes.
func goalOwnerRefusal(a goalActor, g Goal, act string) error {
	if a.importer {
		return nil
	}
	if a.id != g.Owner.ID {
		return Refuse(ErrNotAuthorized, "only the Goal's Owner may %s", act)
	}
	if g.Ownerless {
		return departedRefusal(act)
	}
	return nil
}

// departedRefusal refuses a Departed person act, since they can't act at all
// (CONTEXT.md: Departed).
func departedRefusal(act string) error {
	return Refuse(ErrNotAuthorized, "a Departed person can't %s", act)
}

// departedActorRefusal refuses a when their Account is Departed, saying they
// can't act; one with no Account is left to the rule asking.
func departedActorRefusal(a goalActor, act string) error {
	acc, ok, err := a.account()
	if err != nil {
		return err
	}
	if ok && acc.Departed {
		return departedRefusal(act)
	}
	return nil
}

// linkRemoveRefusal is RemoveLink's rule for the link from child to parent:
// the Owner of either linked Goal, who hasn't Departed. A Departed Owner's
// Goal is Ownerless, so its Ownerless refuses them, as goalOwnerRefusal does.
func linkRemoveRefusal(a goalActor, parent, child Goal) error {
	for _, g := range []Goal{child, parent} {
		if a.id == g.Owner.ID {
			if g.Ownerless {
				return departedRefusal("remove the link")
			}
			return nil
		}
	}
	return Refuse(ErrNotAuthorized, "only an Owner of the linked Goals may remove the link")
}

// goalAdminRefusal refuses a unless they are an Admin, by adminRefusal; one
// with no Account is no Admin.
func goalAdminRefusal(a goalActor) error {
	acc, ok, err := a.account()
	if err != nil {
		return err
	}
	if !ok {
		return Refuse(ErrNotAuthorized, "not an Admin")
	}
	return adminRefusal(acc)
}

// goalOwnerOrAdminRefusal refuses a anyone but g's Owner (as goalOwnerRefusal
// allows them) or an Admin, saying only they may act; a Delegate is refused.
// A Departed Owner is refused as goalOwnerRefusal refuses them, and a Departed
// Admin as adminRefusal does.
func goalOwnerOrAdminRefusal(a goalActor, g Goal, act string) error {
	err := goalOwnerRefusal(a, g, act)
	if err == nil || a.id == g.Owner.ID {
		return err
	}
	if err := departedActorRefusal(a, act); err != nil {
		return err
	}
	if err := goalAdminRefusal(a); errors.Is(err, ErrNotAuthorized) {
		return Refuse(ErrNotAuthorized, "only the Goal's Owner or an Admin may %s", act)
	} else if err != nil {
		return err
	}
	return nil
}

// checkInRefusal is SubmitCheckin's rule: those checkinAuthorRuleRefusal
// allows, while g takes Check-ins (LifecycleTakesCheckins). The Lifecycles a
// Check-in may then move g to are LifecycleChangesFrom's.
func checkInRefusal(a goalActor, g Goal) error {
	if err := checkinAuthorRuleRefusal(a, g); err != nil {
		return err
	}
	if !LifecycleTakesCheckins(g.Lifecycle) {
		return lifecycleTakesNoCheckinsRefusal(g.Lifecycle)
	}
	return nil
}

// lifecycleTakesNoCheckinsRefusal refuses a Check-in on a Goal in lifecycle,
// one LifecycleTakesCheckins says takes none, naming the Lifecycles that do.
func lifecycleTakesNoCheckinsRefusal(lifecycle string) error {
	return Refuse(ErrValidation, "a %s Goal can't be checked in on: only an Active or On Hold Goal takes Check-ins", lifecycle)
}

// noChangeCheckinRefusal is SubmitNoChangeCheckin's rule: those
// checkinAuthorRuleRefusal allows, while g is Active. Whether one would then
// be recorded is the No change verdict's (judgeNoChange).
func noChangeCheckinRefusal(a goalActor, g Goal) error {
	if err := checkinAuthorRuleRefusal(a, g); err != nil {
		return err
	}
	if g.Lifecycle != LifecycleActive {
		return errNotActiveForNoChange
	}
	return nil
}

// milestoneAdditionRefusal is AddMilestoneAsAuthor's rule: those
// milestoneEditorRefusal allows, until g is Done or Cancelled
// (milestonesAddable).
func milestoneAdditionRefusal(a goalActor, g Goal) error {
	if err := milestoneEditorRefusal(a, g); err != nil {
		return err
	}
	if !milestonesAddable(g.Lifecycle) {
		return Refuse(ErrValidation, "a %s Goal's Milestones can't change", g.Lifecycle)
	}
	return nil
}

// draftHighlighterRefusal refuses a anyone but those who may write g's
// Check-ins, by checkinAuthorRuleRefusal: who may log, see, edit or delete its
// pending Draft Highlights.
func draftHighlighterRefusal(a goalActor, g Goal) error {
	if err := checkinAuthorRuleRefusal(a, g); errors.Is(err, ErrNotAuthorized) {
		return Refuse(ErrNotAuthorized, "only the Goal's Owner or a Delegate may log, see, edit or delete its Draft Highlights")
	} else if err != nil {
		return err
	}
	return nil
}

// logDraftHighlightRefusal is LogDraftHighlight's rule: those
// draftHighlighterRefusal allows, while g takes Draft Highlights
// (draftHighlightsTaken).
func logDraftHighlightRefusal(a goalActor, g Goal) error {
	if err := draftHighlighterRefusal(a, g); err != nil {
		return err
	}
	return draftHighlightsTaken(g)
}

// goalValueSetterRefusal is the rule for who sets g's Dimension values and
// Fields: its Owner, one of its Delegates or an Admin, in any Lifecycle
// (CONTEXT.md: Delegate). Whether they are a Delegate, then an Admin, is
// asked only when it decides. A Departed person, whichever they are, can't
// act (CONTEXT.md: Departed).
func goalValueSetterRefusal(a goalActor, g Goal) error {
	const act = "set a Goal's Dimension values"
	if a.id == g.Owner.ID {
		if g.Ownerless {
			return departedRefusal(act)
		}
		return nil
	}
	if err := departedActorRefusal(a, act); err != nil {
		return err
	}
	if ok, err := a.delegate(); err != nil {
		return err
	} else if ok {
		return nil
	}
	if err := goalAdminRefusal(a); errors.Is(err, ErrNotAuthorized) {
		return Refuse(ErrNotAuthorized, "only the Owner, a Delegate or an Admin may set a Goal's Dimension values")
	} else if err != nil {
		return err
	}
	return nil
}

// metricEditRefusal is the rule for adding and editing g's Metrics
// (AddMetric, EditMetric): its Owner alone, in any Lifecycle.
func metricEditRefusal(a goalActor, g Goal) error {
	return goalOwnerRefusal(a, g, "change its Metrics")
}

// defineGoalRefusal is DefineGoal's rule: its Owner, while g is Proposed.
func defineGoalRefusal(a goalActor, g Goal) error {
	if err := goalOwnerRefusal(a, g, "define it"); err != nil {
		return err
	}
	if g.Lifecycle != LifecycleProposed {
		return Refuse(ErrValidation, "%s is %s, so it is no longer defined here", g.Title, g.Lifecycle)
	}
	return nil
}

// activateGoalRefusal is ActivateGoal's rule: its Owner, while g is Proposed.
// Whether g then meets the minimum standard is the activation gate's.
func activateGoalRefusal(a goalActor, g Goal) error {
	if err := goalOwnerRefusal(a, g, "activate it"); err != nil {
		return err
	}
	if g.Lifecycle != LifecycleProposed {
		return Refuse(ErrValidation, "%s is %s, so it can't be activated", g.Title, g.Lifecycle)
	}
	return nil
}

// suggestParentRefusal is SuggestParent's rule: anyone with an Account but
// g's Owner, who requests the link instead, while g is Active or Proposed
// (suggestable). A Departed person can't act (CONTEXT.md: Departed).
func suggestParentRefusal(a goalActor, g Goal) error {
	if a.id == g.Owner.ID {
		return Refuse(ErrNotAuthorized, "the Goal's Owner links it to a parent rather than suggesting one")
	}
	if acc, ok, err := a.account(); err != nil {
		return err
	} else if !ok {
		return Refuse(ErrNotAuthorized, "the suggester does not exist")
	} else if acc.Departed {
		return departedRefusal("suggest a parent")
	}
	if !suggestable(g.Lifecycle) {
		return Refuse(ErrValidation, "a parent can only be suggested for an Active or Proposed Goal")
	}
	return nil
}

// decideSuggestionRefusal is the rule for accepting or declining a Parent
// suggestion for g: its Owner alone.
func decideSuggestionRefusal(a goalActor, g Goal) error {
	return goalOwnerRefusal(a, g, "decide a parent suggestion")
}

// handoffRefusal is StartHandoff's rule: g's Owner or an Admin, while g has a
// present Owner. An Ownerless Goal is resolved by Reassign, or by accepting a
// Handoff its Owner started before leaving (CONTEXT.md: Ownerless).
func handoffRefusal(a goalActor, g Goal) error {
	if err := goalOwnerOrAdminRefusal(a, g, "start a Handoff"); err != nil {
		return err
	}
	if g.Ownerless {
		return Refuse(ErrValidation, "this Goal is Ownerless: reassign it instead")
	}
	return nil
}

// reassignRefusal is ReassignGoal's rule: an Admin, once g is Ownerless.
func reassignRefusal(a goalActor, g Goal) error {
	if err := goalAdminRefusal(a); err != nil {
		return err
	}
	if !g.Ownerless {
		return Refuse(ErrValidation, "only an Ownerless Goal is reassigned; hand off a Goal with a present Owner")
	}
	return nil
}

// markDepartedRefusal is MarkDeparted's rule for marking person: an Admin,
// while person hasn't Departed. The Goal page asks it of the Goal's Owner.
func markDepartedRefusal(a goalActor, person Account) error {
	if err := goalAdminRefusal(a); err != nil {
		return err
	}
	if person.Departed {
		return Refuse(ErrValidation, "%s has already left the company", person.Label())
	}
	return nil
}

// markReturnedRefusal is MarkReturned's rule for marking person: an Admin,
// once person has Departed.
func markReturnedRefusal(a goalActor, person Account) error {
	if err := goalAdminRefusal(a); err != nil {
		return err
	}
	if !person.Departed {
		return Refuse(ErrValidation, "%s hasn't left the company", person.Label())
	}
	return nil
}

// lifecycleKeptRefusal is SubmitCheckin's rule for a Check-in leaving g's
// Lifecycle as it is: those checkInRefusal allows, unless g's Lifecycle must
// change in one (lifecycleKeepRefusal).
func lifecycleKeptRefusal(a goalActor, g Goal) error {
	if err := checkInRefusal(a, g); err != nil {
		return err
	}
	return lifecycleKeepRefusal(g.Lifecycle)
}

// LifecycleChangesFrom are the Lifecycles a Check-in may move a Goal in
// lifecycle to (CONTEXT.md: Lifecycle): an Active Goal may go On Hold, be
// marked Done or be Cancelled, and an On Hold one resumed to Active or
// Cancelled. Every other Lifecycle is closed to Check-ins, so has none: a
// Proposed Goal becomes Active through the activation gate, and Done and
// Cancelled are end states. planLifecycleChange allows just these.
func LifecycleChangesFrom(lifecycle string) []string {
	switch lifecycle {
	case LifecycleActive:
		return []string{LifecycleOnHold, LifecycleDone, LifecycleCancelled}
	case LifecycleOnHold:
		return []string{LifecycleActive, LifecycleCancelled}
	}
	return nil
}

// lifecycleChangeRefusal is SubmitCheckin's rule for a Check-in moving g to
// lifecycle to: those checkInRefusal allows, when LifecycleChangesFrom
// offers it.
func lifecycleChangeRefusal(a goalActor, g Goal, to string) error {
	if err := checkInRefusal(a, g); err != nil {
		return err
	}
	if !slices.Contains(LifecycleChangesFrom(g.Lifecycle), to) {
		return inputError(CheckinInputLifecycle, "a Check-in can't move a %s Goal to %q", g.Lifecycle, to)
	}
	return nil
}
