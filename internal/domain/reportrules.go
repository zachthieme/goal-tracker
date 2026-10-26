package domain

import (
	"context"
	"errors"
	"fmt"
)

// The Report rules: who may make each Report Definition and Action Item
// command, each a pure function of the actor (a goalActor built by
// accountActor, as an Account command's rule judges them) and what they act
// on. The command evaluates its rule inside its transaction and the Report
// pages (ReportPermissions, ActionItemPermissions) evaluate the same one, so
// they offer just what the command would accept. Each rule refuses for who
// the actor is first, with ErrNotAuthorized, then for state, with
// ErrValidation. A Departed person can't act (CONTEXT.md: Departed), so every
// rule refuses one; an actorID with no Account is left to the rest of the
// rule, as departedActorRefusal leaves it.

// reportRuleDefinition is a Report Definition as a Report rule judges an
// actor against it: the Definition and its Report Delegates.
type reportRuleDefinition struct {
	def       ReportDefinition
	delegates []Account
}

// reportRuleDefinitionOf is def with its Report Delegates, read through s.
func (s *Service) reportRuleDefinitionOf(ctx context.Context, def ReportDefinition) (reportRuleDefinition, error) {
	delegates, err := s.ListReportDelegates(ctx, def.ID)
	if err != nil {
		return reportRuleDefinition{}, err
	}
	return reportRuleDefinition{def: def, delegates: delegates}, nil
}

// reportRule evaluates rule for actorID on def, with its Report Delegates
// read through s, as a command does inside its transaction.
func (s *Service) reportRule(ctx context.Context, actorID int64, def ReportDefinition, rule func(goalActor, reportRuleDefinition) error) error {
	r, err := s.reportRuleDefinitionOf(ctx, def)
	if err != nil {
		return err
	}
	return rule(s.accountActor(ctx, actorID), r)
}

// saveReportDefinitionRefusal is SaveReportDefinition's rule: anyone signed
// in who hasn't Departed (CONTEXT.md: Report Definition).
func saveReportDefinitionRefusal(a goalActor) error {
	return departedActorRefusal(a, "save a Report Definition")
}

// commentRefusal is the rule for commenting (AddComment, AddGoalComment,
// ReplyToComment): anyone signed in who hasn't Departed.
func commentRefusal(a goalActor) error {
	return departedActorRefusal(a, "comment")
}

// reportManagerRefusal is the rule for who may act — edit, curate or publish
// — on r's Definition: its creator, one of its Report Delegates or an Admin
// (CONTEXT.md: Report Delegate).
func reportManagerRefusal(a goalActor, r reportRuleDefinition, act string) error {
	if err := departedActorRefusal(a, act+" "+r.def.Name); err != nil {
		return err
	}
	if a.id == r.def.CreatedBy || isReportDelegate(r.delegates, a.id) {
		return nil
	}
	if err := goalAdminRefusal(a); errors.Is(err, ErrNotAuthorized) {
		return Refuse(ErrNotAuthorized, "only its creator, a Report Delegate or an Admin may %s %s", act, r.def.Name)
	} else if err != nil {
		return err
	}
	return nil
}

// editReportRefusal is UpdateReportDefinition's rule, reportManagerRefusal's.
func editReportRefusal(a goalActor, r reportRuleDefinition) error {
	return reportManagerRefusal(a, r, "edit")
}

// curateNarrativeRefusal is CurateNarrative's rule, and PreviewNarrative's,
// reportManagerRefusal's.
func curateNarrativeRefusal(a goalActor, r reportRuleDefinition) error {
	return reportManagerRefusal(a, r, "curate")
}

// publishReportRefusal is PublishReport's rule, reportManagerRefusal's.
func publishReportRefusal(a goalActor, r reportRuleDefinition) error {
	return reportManagerRefusal(a, r, "publish")
}

// reportDelegatesRefusal is the rule for adding and removing r's Report
// Delegates (AddReportDelegate, RemoveReportDelegate): its creator or an
// Admin; a Report Delegate may not.
func reportDelegatesRefusal(a goalActor, r reportRuleDefinition) error {
	act := "change who are Report Delegates on " + r.def.Name
	if err := departedActorRefusal(a, act); err != nil {
		return err
	}
	if a.id == r.def.CreatedBy {
		return nil
	}
	if err := goalAdminRefusal(a); errors.Is(err, ErrNotAuthorized) {
		return Refuse(ErrNotAuthorized, "only its creator or an Admin may %s", act)
	} else if err != nil {
		return err
	}
	return nil
}

// ReportPermissions is what a Report's draft page offers its viewer on its
// Report Definition, each from the rule its command evaluates: Manage, to
// change, curate and publish it, and ManageDelegates, to add and remove its
// Report Delegates. Delegates are those Report Delegates, as the rules judged
// the viewer against them, for the page to list.
type ReportPermissions struct {
	Manage          bool
	ManageDelegates bool
	Delegates       []Account
}

// ReportPermissions is what actorID may do to def, as its draft page offers
// it.
func (s *Service) ReportPermissions(ctx context.Context, actorID int64, def ReportDefinition) (ReportPermissions, error) {
	r, err := s.reportRuleDefinitionOf(ctx, def)
	if err != nil {
		return ReportPermissions{}, err
	}
	a := s.accountActor(ctx, actorID)
	p := ReportPermissions{Delegates: r.delegates}
	if p.Manage, err = goalPagePermits(editReportRefusal(a, r)); err != nil {
		return ReportPermissions{}, err
	}
	if p.ManageDelegates, err = goalPagePermits(reportDelegatesRefusal(a, r)); err != nil {
		return ReportPermissions{}, err
	}
	return p, nil
}

// RequireReportEditor refuses actorID, as UpdateReportDefinition would, unless
// they may edit def: nil when they may.
func (s *Service) RequireReportEditor(ctx context.Context, actorID int64, def ReportDefinition) error {
	return s.reportRule(ctx, actorID, def, editReportRefusal)
}

// actionItemRuleReport is the published Report an Action Item rule judges an
// actor against: its Definition and Report Delegates, who published it, and
// the Definition's creator's Manager.
type actionItemRuleReport struct {
	reportRuleDefinition
	publisherID int64
	// creatorManagerID is the creator's Manager, 0 when they have none.
	creatorManagerID int64
}

// actionItemRuleReportOf is the publication pubID of the Report Definition
// defID as an Action Item rule judges an actor against it, read through s.
func (s *Service) actionItemRuleReportOf(ctx context.Context, defID, pubID int64) (actionItemRuleReport, error) {
	def, err := s.GetReportDefinition(ctx, defID)
	if err != nil {
		return actionItemRuleReport{}, err
	}
	r, err := s.reportRuleDefinitionOf(ctx, def)
	if err != nil {
		return actionItemRuleReport{}, err
	}
	pub, err := s.queries.GetReportPublication(ctx, pubID)
	if err != nil {
		return actionItemRuleReport{}, fmt.Errorf("get report publication: %w", err)
	}
	creator, err := s.queries.GetAccount(ctx, def.CreatedBy)
	if err != nil {
		return actionItemRuleReport{}, fmt.Errorf("look up report creator: %w", err)
	}
	out := actionItemRuleReport{reportRuleDefinition: r, publisherID: pub.PublishedBy}
	if creator.ManagerID != nil {
		out.creatorManagerID = *creator.ManagerID
	}
	return out, nil
}

// raiseActionItemRefusal is RaiseActionItem's rule on r: the Report's author,
// who created its Definition or published this publication.
func raiseActionItemRefusal(a goalActor, r actionItemRuleReport) error {
	if err := departedActorRefusal(a, "raise an Action Item"); err != nil {
		return err
	}
	if a.id == r.def.CreatedBy || a.id == r.publisherID {
		return nil
	}
	return Refuse(ErrNotAuthorized, "only the Report's author raises Action Items")
}

// raiseActionItemRule evaluates raiseActionItemRefusal for actorID on pub,
// reading what it judges through s.
func (s *Service) raiseActionItemRule(ctx context.Context, actorID int64, pub Publication) error {
	r, err := s.actionItemRuleReportOf(ctx, pub.DefinitionID, pub.ID)
	if err != nil {
		return err
	}
	return raiseActionItemRefusal(s.accountActor(ctx, actorID), r)
}

// closeActionItemRefusal is CloseActionItem's rule on item: its owner, while
// it is open.
func closeActionItemRefusal(a goalActor, item ActionItem) error {
	if err := departedActorRefusal(a, "close an Action Item"); err != nil {
		return err
	}
	if a.id != item.Owner.ID {
		return Refuse(ErrNotAuthorized, "only the Action Item's owner closes it")
	}
	if item.Closed {
		return Refuse(ErrValidation, "the Action Item is already closed")
	}
	return nil
}

// reassignActionItemRefusal is ReassignActionItem's rule on item, raised on
// r: its Report Definition's creator or one of its Report Delegates, who
// published r, an Admin, or the creator's Manager (CONTEXT.md: Action Item),
// while it is open.
func reassignActionItemRefusal(a goalActor, r actionItemRuleReport, item ActionItem) error {
	if err := departedActorRefusal(a, "reassign an Action Item"); err != nil {
		return err
	}
	switch {
	case a.id == r.def.CreatedBy, isReportDelegate(r.delegates, a.id), a.id == r.publisherID,
		r.creatorManagerID != 0 && a.id == r.creatorManagerID:
	default:
		if err := goalAdminRefusal(a); errors.Is(err, ErrNotAuthorized) {
			return Refuse(ErrNotAuthorized, "only the Report's creator, a Report Delegate or its publisher, an Admin, or the creator's Manager reassigns an Action Item")
		} else if err != nil {
			return err
		}
	}
	if item.Closed {
		return Refuse(ErrValidation, "the Action Item is already closed")
	}
	return nil
}

// ActionItemPermissions is what the Report pages offer their viewer on one
// open Action Item, each from the rule its command evaluates: Close, as its
// owner, and Reassign, to hand it to a new owner.
type ActionItemPermissions struct {
	Close    bool
	Reassign bool
}

// ActionItemPermissions is what actorID may do to each of items, by its id,
// as the Report pages offer it.
func (s *Service) ActionItemPermissions(ctx context.Context, actorID int64, items []ActionItem) (map[int64]ActionItemPermissions, error) {
	a := s.accountActor(ctx, actorID)
	reports := map[int64]actionItemRuleReport{}
	out := make(map[int64]ActionItemPermissions, len(items))
	for _, item := range items {
		r, ok := reports[item.PublicationID]
		if !ok {
			var err error
			if r, err = s.actionItemRuleReportOf(ctx, item.DefinitionID, item.PublicationID); err != nil {
				return nil, err
			}
			reports[item.PublicationID] = r
		}
		var p ActionItemPermissions
		var err error
		if p.Close, err = goalPagePermits(closeActionItemRefusal(a, item)); err != nil {
			return nil, err
		}
		if p.Reassign, err = goalPagePermits(reassignActionItemRefusal(a, r, item)); err != nil {
			return nil, err
		}
		out[item.ID] = p
	}
	return out, nil
}
