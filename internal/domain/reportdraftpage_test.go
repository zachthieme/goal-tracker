package domain_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Report draft page's loader returns what the separate Service methods
// return for its viewer (ADR 0010): the draft as DraftReport drafts it, the
// scope SummarizeReport says, the publications, the Report log, what the
// viewer may do to the Report Definition (ReportPermissions) and to each of
// its open Action Items (ActionItemPermissions) — for its creator, a Report
// Delegate, an Admin, the creator's Manager, an Action Item's owner, a
// reader and a Departed Report Delegate.
func TestReportDraftPageReturnsWhatEachMethodDoes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	boss := h.SignIn("boss@example.com")
	manager := h.SignIn("morgan@example.com")
	delegate := h.SignIn("dee@example.com")
	gone := h.SignIn("gone@example.com")
	owner := h.SignIn("priya@example.com")
	reader := h.SignIn("rita@example.com")
	h.SetManager(boss, manager)
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.AddReportDelegate(boss, delegate, def)
	h.AddReportDelegate(boss, gone, def)
	h.MarkDeparted(admin, gone)
	pub := h.PublishReport(boss, def)
	for _, in := range []domain.RaiseActionItemInput{
		{PublicationID: pub.ID, Text: "Hire in Berlin.", OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7)},
		{PublicationID: pub.ID, GoalID: g.ID, Text: "Price in euros.", OwnerID: boss.ID, DueDate: h.Clock.Now().AddDate(0, 0, 14)},
	} {
		if _, err := h.Service.RaiseActionItem(ctx, boss.ID, in); err != nil {
			t.Fatalf("RaiseActionItem: %v", err)
		}
	}
	h.Clock.Advance(24 * time.Hour)
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightMiss, "Lost the Paris deal.")

	for _, baseline := range []time.Time{{}, h.Clock.Now().AddDate(0, 0, -10)} {
		want, err := h.Service.DraftReport(ctx, def, baseline)
		if err != nil {
			t.Fatalf("DraftReport: %v", err)
		}
		if len(want.ActionItems) != 2 {
			t.Fatalf("the draft carries %d open Action Items, want 2", len(want.ActionItems))
		}
		sum, err := h.Service.SummarizeReport(ctx, want)
		if err != nil {
			t.Fatalf("SummarizeReport: %v", err)
		}
		pubs, err := h.Service.ListPublications(ctx, def.ID)
		if err != nil {
			t.Fatalf("ListPublications: %v", err)
		}
		log, err := h.Service.ReportLog(ctx, def.ID)
		if err != nil {
			t.Fatalf("ReportLog: %v", err)
		}
		for _, v := range []struct {
			name   string
			viewer domain.Account
			// manage and delegates are whether they may manage the Report and
			// its Report Delegates.
			manage, delegates bool
		}{
			{"its creator", boss, true, true},
			{"a Report Delegate", delegate, true, false},
			{"an Admin", admin, true, true},
			{"the creator's Manager", manager, false, false},
			{"an Action Item's owner", owner, false, false},
			{"a reader", reader, false, false},
			{"a Departed Report Delegate", gone, false, false},
		} {
			got, err := h.Service.ReportDraftPage(ctx, v.viewer.ID, def, baseline)
			if err != nil {
				t.Fatalf("ReportDraftPage for %s: %v", v.name, err)
			}
			if got.Permissions.Manage != v.manage || got.Permissions.ManageDelegates != v.delegates {
				t.Errorf("ReportDraftPage for %s: Manage %v, ManageDelegates %v; want %v, %v",
					v.name, got.Permissions.Manage, got.Permissions.ManageDelegates, v.manage, v.delegates)
			}
			permissions, err := h.Service.ReportPermissions(ctx, v.viewer.ID, def)
			if err != nil {
				t.Fatalf("ReportPermissions: %v", err)
			}
			items, err := h.Service.ActionItemPermissions(ctx, v.viewer.ID, want.ActionItems)
			if err != nil {
				t.Fatalf("ActionItemPermissions: %v", err)
			}
			for _, f := range []struct {
				field     string
				got, want any
			}{
				{"Report", got.Report, want},
				{"Scope", got.Scope, sum.Scope},
				{"Publications", got.Publications, pubs},
				{"Log", got.Log, log},
				{"Permissions", got.Permissions, permissions},
				{"ActionItemPermissions", got.ActionItemPermissions, items},
			} {
				if !reflect.DeepEqual(f.got, f.want) {
					t.Errorf("ReportDraftPage for %s against %v: %s = %+v, want %+v", v.name, baseline, f.field, f.got, f.want)
				}
			}
		}
	}
}
