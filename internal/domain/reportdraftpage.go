package domain

import (
	"context"
	"fmt"
	"time"
)

// ReportDraftPage is everything a Report's draft page shows one viewer, read
// by Service.ReportDraftPage (ADR 0010). Each field is what the Service method
// it is named for returns.
type ReportDraftPage struct {
	// Report is the draft, as DraftReport drafts it. Its ActionItems are the
	// Report Definition's open Action Items, which the page's discussion
	// offers too.
	Report Report
	// Scope is what its Definition selects in plain words, as
	// SummarizeReport's Scope says.
	Scope string
	// Permissions are what the viewer may do to the Definition
	// (ReportPermissions), and ActionItemPermissions what they may do to each
	// of Report.ActionItems, by id (ActionItemPermissions).
	Permissions           ReportPermissions
	ActionItemPermissions map[int64]ActionItemPermissions
	// Publications are the Definition's publications (ListPublications) and
	// Log its Report log (ReportLog), both newest first.
	Publications []Publication
	Log          []ReportLogEntry
}

// ReportDraftPage reads everything def's draft page shows actorID, against
// baseline as DraftReport reads it, running each query of its own once (ADR
// 0010). It judges the viewer, looked up once, against the Report Delegates
// and publications it read, as ReportPermissions and ActionItemPermissions
// do. DraftReport's own repeats stay, as it drafts for publishing and the
// Report list too.
func (s *Service) ReportDraftPage(ctx context.Context, actorID int64, def ReportDefinition, baseline time.Time) (ReportDraftPage, error) {
	report, err := s.DraftReport(ctx, def, baseline)
	if err != nil {
		return ReportDraftPage{}, err
	}
	p := ReportDraftPage{Report: report}
	if p.Scope, err = s.reportScope(ctx, report); err != nil {
		return ReportDraftPage{}, err
	}
	rule, err := s.reportRuleDefinitionOf(ctx, def)
	if err != nil {
		return ReportDraftPage{}, err
	}
	viewer, found, err := s.actorAccount(ctx, actorID)()
	if err != nil {
		return ReportDraftPage{}, err
	}
	actor := goalActor{
		id:       actorID,
		account:  func() (Account, bool, error) { return viewer, found, nil },
		delegate: func() (bool, error) { return false, nil },
	}
	p.Permissions = ReportPermissions{Delegates: rule.delegates}
	if p.Permissions.Manage, err = goalPagePermits(editReportRefusal(actor, rule)); err != nil {
		return ReportDraftPage{}, err
	}
	if p.Permissions.ManageDelegates, err = goalPagePermits(reportDelegatesRefusal(actor, rule)); err != nil {
		return ReportDraftPage{}, err
	}
	if p.Publications, err = s.ListPublications(ctx, def.ID); err != nil {
		return ReportDraftPage{}, err
	}
	if p.Log, err = s.ReportLog(ctx, def.ID); err != nil {
		return ReportDraftPage{}, err
	}
	creatorManager := s.reportDraftCreatorManager(ctx, def, actorID, viewer)
	publishers := make(map[int64]int64, len(p.Publications))
	for _, pub := range p.Publications {
		publishers[pub.ID] = pub.PublishedBy.ID
	}
	p.ActionItemPermissions, err = permitActionItems(actor, report.ActionItems, func(item ActionItem) (actionItemRuleReport, error) {
		publisherID, ok := publishers[item.PublicationID]
		if !ok {
			return actionItemRuleReport{}, fmt.Errorf("%w: publication %d of report definition %d", ErrNotFound, item.PublicationID, def.ID)
		}
		managerID, err := creatorManager()
		if err != nil {
			return actionItemRuleReport{}, err
		}
		return actionItemRuleReport{reportRuleDefinition: rule, publisherID: publisherID, creatorManagerID: managerID}, nil
	})
	if err != nil {
		return ReportDraftPage{}, err
	}
	return p, nil
}

// reportDraftCreatorManager finds def's creator's Manager, 0 when they have
// none, as an Action Item rule judges them: the viewer's, when they are its
// creator, and otherwise read once, when first asked.
func (s *Service) reportDraftCreatorManager(ctx context.Context, def ReportDefinition, viewerID int64, viewer Account) func() (int64, error) {
	var managerID *int64
	return func() (int64, error) {
		if managerID != nil {
			return *managerID, nil
		}
		creator := viewer
		if def.CreatedBy != viewerID {
			row, err := s.queries.GetAccount(ctx, def.CreatedBy)
			if err != nil {
				return 0, fmt.Errorf("look up report creator: %w", err)
			}
			creator = accountFromRow(row)
		}
		var id int64
		if creator.ManagerID != nil {
			id = *creator.ManagerID
		}
		managerID = &id
		return id, nil
	}
}
