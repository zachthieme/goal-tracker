package domain_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// reportOffers is everything the Report pages offer a viewer, read from the
// domain: what they may do to the Report Definition, whether they may raise
// Action Items on its publication, and what they may do to one Action Item.
type reportOffers struct {
	report domain.ReportPermissions
	author bool
	item   domain.ActionItemPermissions
}

// reportCommand is one Report or Action Item command the parity table runs:
// allowed reads whether the Report pages offer it, and run makes it, as
// viewer, through tx, on item where it takes one.
type reportCommand struct {
	name    string
	allowed func(reportOffers) bool
	run     func(tx *domain.Service, viewer domain.Account, item domain.ActionItem) error
}

// reportRulesWorld is a published Report and its people: the Definition's
// creator, a Report Delegate, an Admin, who published it (a Report Delegate
// no longer), the creator's Manager, an Action Item's owner, anyone else, a
// Departed Report Delegate, a Departed Admin and a Departed Action Item
// owner. Its Action Items are one open item each of the owners', and one the
// owner has closed. newcomer is no one's anything, to be added and handed
// items.
type reportRulesWorld struct {
	h                                                    *testsupport.Harness
	creator, delegate, admin, publisher, manager, owner  domain.Account
	rita, departedDelegate, departedAdmin, departedOwner domain.Account
	newcomer                                             domain.Account
	goal                                                 domain.Goal
	def                                                  domain.ReportDefinition
	pub                                                  domain.Publication
	open, departedOwners, closed                         domain.ActionItem
	// departedDelegatesPub is the Report as the Departed Report Delegate
	// published it, and thread a comment on it.
	departedDelegatesPub domain.Publication
	thread               domain.Comment
}

func newReportRulesWorld(t *testing.T) reportRulesWorld {
	t.Helper()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com", "ada@example.com")
	w := reportRulesWorld{h: h}
	w.creator = h.SignIn("creator@example.com")
	w.delegate = h.SignIn("morgan@example.com")
	w.admin = h.SignIn("admin@example.com")
	w.publisher = h.SignIn("pat@example.com")
	w.manager = h.SignIn("mia@example.com")
	w.owner = h.SignIn("olive@example.com")
	w.rita = h.SignIn("rita@example.com")
	w.departedDelegate = h.SignIn("dana@example.com")
	w.departedAdmin = h.SignIn("ada@example.com")
	w.departedOwner = h.SignIn("otto@example.com")
	w.newcomer = h.SignIn("newcomer@example.com")
	h.SetManager(w.creator, w.manager)
	w.goal = h.ActiveGoal(w.owner, "Launch in EU", "Expand the market.")
	w.def = h.SaveReportDefinition(w.creator, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{w.goal.ID}})
	h.AddReportDelegate(w.creator, w.delegate, w.def)
	h.AddReportDelegate(w.creator, w.departedDelegate, w.def)
	h.AddReportDelegate(w.creator, w.publisher, w.def)
	w.pub = h.PublishReport(w.publisher, w.def)
	if err := h.Service.RemoveReportDelegate(ctx, w.creator.ID, w.def.ID, w.publisher.ID); err != nil {
		t.Fatalf("RemoveReportDelegate: %v", err)
	}
	raise := func(owner domain.Account, text string) domain.ActionItem {
		t.Helper()
		item, err := h.Service.RaiseActionItem(ctx, w.publisher.ID, domain.RaiseActionItemInput{
			PublicationID: w.pub.ID, GoalID: w.goal.ID, Text: text, OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
		})
		if err != nil {
			t.Fatalf("RaiseActionItem %q: %v", text, err)
		}
		return item
	}
	w.open = raise(w.owner, "Book the EU legal review.")
	w.departedOwners = raise(w.departedOwner, "Chase the vendor.")
	w.closed = raise(w.owner, "Translate the site.")
	var err error
	if w.closed, err = h.Service.CloseActionItem(ctx, w.owner.ID, w.closed.ID, "Done in-house."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	// The Departed Report Delegate published the latest publication before
	// leaving, and anyone else started a thread on it.
	w.departedDelegatesPub = h.PublishReport(w.departedDelegate, w.def)
	if w.thread, err = h.Service.AddComment(ctx, w.rita.ID, w.departedDelegatesPub.ID, w.goal.ID, "Is legal on track?"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	h.MarkDeparted(w.admin, w.departedDelegate)
	h.MarkDeparted(w.admin, w.departedAdmin)
	h.MarkDeparted(w.admin, w.departedOwner)
	return w
}

// For each Report and Action Item command and each kind of viewer — the
// Definition's creator, a Report Delegate, an Admin, the publisher, the
// creator's Manager, an Action Item's owner, anyone else, a Departed Report
// Delegate, a Departed Admin and a Departed Action Item owner — and each
// Action Item, open, a Departed owner's or closed, the Report pages offer the
// command exactly when the command, made with input it would otherwise take,
// doesn't refuse for who the viewer is or for the item's state: what they
// offer is accepted, and what they don't is refused with ErrNotAuthorized or
// ErrValidation. A command no viewer was both offered and had accepted fails
// the test, since its refusals would then prove nothing of its rule.
func TestReportPagesOfferExactlyWhatEachCommandAccepts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newReportRulesWorld(t)
	h := w.h

	manage := func(o reportOffers) bool { return o.report.Manage }
	manageDelegates := func(o reportOffers) bool { return o.report.ManageDelegates }
	commands := []reportCommand{
		{"UpdateReportDefinition", manage, func(tx *domain.Service, v domain.Account, _ domain.ActionItem) error {
			_, err := tx.UpdateReportDefinition(ctx, v.ID, w.def.ID, domain.SaveReportDefinitionInput{Name: "MBR, renamed", Mode: domain.ReportModePicked, Picked: []int64{w.goal.ID}})
			return err
		}},
		{"CurateNarrative", manage, func(tx *domain.Service, v domain.Account, _ domain.ActionItem) error {
			return tx.CurateNarrative(ctx, v.ID, w.def.ID, domain.CurateNarrativeInput{Notes: map[string][]string{domain.HighlightMiss: {"Watch churn."}}})
		}},
		{"PublishReport", manage, func(tx *domain.Service, v domain.Account, _ domain.ActionItem) error {
			_, err := tx.PublishReport(ctx, v.ID, w.def.ID, time.Time{})
			return err
		}},
		{"AddReportDelegate", manageDelegates, func(tx *domain.Service, v domain.Account, _ domain.ActionItem) error {
			return tx.AddReportDelegate(ctx, v.ID, w.def.ID, w.newcomer.ID)
		}},
		{"RemoveReportDelegate", manageDelegates, func(tx *domain.Service, v domain.Account, _ domain.ActionItem) error {
			return tx.RemoveReportDelegate(ctx, v.ID, w.def.ID, w.delegate.ID)
		}},
		{"RaiseActionItem", func(o reportOffers) bool { return o.author }, func(tx *domain.Service, v domain.Account, _ domain.ActionItem) error {
			_, err := tx.RaiseActionItem(ctx, v.ID, domain.RaiseActionItemInput{
				PublicationID: w.pub.ID, GoalID: w.goal.ID, Text: "Price the EU tiers.", OwnerID: w.owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
			})
			return err
		}},
		{"CloseActionItem", func(o reportOffers) bool { return o.item.Close }, func(tx *domain.Service, v domain.Account, item domain.ActionItem) error {
			_, err := tx.CloseActionItem(ctx, v.ID, item.ID, "Booked it.")
			return err
		}},
		{"ReassignActionItem", func(o reportOffers) bool { return o.item.Reassign }, func(tx *domain.Service, v domain.Account, item domain.ActionItem) error {
			_, err := tx.ReassignActionItem(ctx, v.ID, item.ID, w.newcomer.ID)
			return err
		}},
	}

	viewers := []struct {
		name   string
		viewer domain.Account
	}{
		{"the Definition's creator", w.creator},
		{"a Report Delegate", w.delegate},
		{"an Admin", w.admin},
		{"the publisher", w.publisher},
		{"the creator's Manager", w.manager},
		{"the Action Item's owner", w.owner},
		{"anyone else", w.rita},
		{"a Departed Report Delegate", w.departedDelegate},
		{"a Departed Admin", w.departedAdmin},
		{"a Departed Action Item owner", w.departedOwner},
	}
	items := []struct {
		name string
		item domain.ActionItem
	}{
		{"the open Action Item", w.open},
		{"the Departed owner's Action Item", w.departedOwners},
		{"the closed Action Item", w.closed},
	}
	accepted := map[string]bool{}
	for _, who := range viewers {
		v := who.viewer
		report, err := h.Service.ReportPermissions(ctx, v.ID, w.def)
		if err != nil {
			t.Fatalf("ReportPermissions for %s: %v", who.name, err)
		}
		author, err := h.Service.IsReportAuthor(ctx, v.ID, w.pub.ID)
		if err != nil {
			t.Fatalf("IsReportAuthor for %s: %v", who.name, err)
		}
		all := []domain.ActionItem{w.open, w.departedOwners, w.closed}
		itemOffers, err := h.Service.ActionItemPermissions(ctx, v.ID, all)
		if err != nil {
			t.Fatalf("ActionItemPermissions for %s: %v", who.name, err)
		}
		if len(itemOffers) != len(all) {
			t.Errorf("ActionItemPermissions for %s = %+v, want one for each of the %d Action Items", who.name, itemOffers, len(all))
		}
		// A Departed person can't act (CONTEXT.md: Departed), so is offered
		// nothing.
		if v.ID == w.departedDelegate.ID || v.ID == w.departedAdmin.ID || v.ID == w.departedOwner.ID {
			nothing := domain.ReportPermissions{Delegates: report.Delegates}
			if !reflect.DeepEqual(report, nothing) || author {
				t.Errorf("%s, Departed, is offered %+v and author %v, want nothing", who.name, report, author)
			}
			for id, o := range itemOffers {
				if o != (domain.ActionItemPermissions{}) {
					t.Errorf("%s, Departed, is offered %+v on Action Item %d, want nothing", who.name, o, id)
				}
			}
		}
		for _, it := range items {
			offers := reportOffers{report: report, author: author, item: itemOffers[it.item.ID]}
			for _, c := range commands {
				var got error
				if err := h.Service.WithinTx(ctx, func(tx *domain.Service) error {
					got = c.run(tx, v, it.item)
					return errRolledBack
				}); !errors.Is(err, errRolledBack) {
					t.Fatalf("%s by %s on %s: the transaction ended %v", c.name, who.name, it.name, err)
				}
				refused := errors.Is(got, domain.ErrNotAuthorized) || errors.Is(got, domain.ErrValidation)
				switch offered := c.allowed(offers); {
				case offered && got == nil:
					accepted[c.name] = true
				case offered && got != nil:
					t.Errorf("%s by %s on %s: the page offers it, but the command refused: %v", c.name, who.name, it.name, got)
				case !offered && !refused:
					t.Errorf("%s by %s on %s: the page doesn't offer it, but the command answered %v", c.name, who.name, it.name, got)
				}
			}
		}
	}
	for _, c := range commands {
		if !accepted[c.name] {
			t.Errorf("%s is offered and accepted for no viewer, so its refusals prove nothing of its rule", c.name)
		}
	}
}

// state is everything the world's commands could change, as the Service reads
// it back.
func (w reportRulesWorld) state(t *testing.T) []any {
	t.Helper()
	ctx := context.Background()
	s := w.h.Service
	read := func(what string, v any, err error) any {
		t.Helper()
		if err != nil {
			t.Fatalf("read %s: %v", what, err)
		}
		return v
	}
	defs, err := s.ListReportDefinitions(ctx)
	out := []any{read("Report Definitions", defs, err)}
	def, err := s.GetReportDefinition(ctx, w.def.ID)
	out = append(out, read("the Report Definition", def, err))
	log, err := s.ReportLog(ctx, w.def.ID)
	out = append(out, read("the Report log", log, err))
	delegates, err := s.ListReportDelegates(ctx, w.def.ID)
	out = append(out, read("Report Delegates", delegates, err))
	open, err := s.OpenActionItems(ctx, w.def.ID)
	out = append(out, read("open Action Items", open, err))
	reassignments, err := s.ActionItemReassignmentsAbout(ctx, w.goal.ID)
	out = append(out, read("reassignments", reassignments, err))
	goalThreads, err := s.GoalThreads(ctx, w.goal.ID)
	out = append(out, read("the Goal's threads", goalThreads, err))
	pubs, err := s.ListPublications(ctx, w.def.ID)
	out = append(out, read("publications", pubs, err))
	for _, p := range pubs {
		items, err := s.PublicationActionItems(ctx, p.ID)
		out = append(out, read("a publication's Action Items", items, err))
		threads, err := s.ListThreads(ctx, p.ID)
		out = append(out, read("a publication's threads", threads, err))
	}
	return out
}

// Every Report, Action Item and comment command refuses a Departed person
// with ErrNotAuthorized, though they are whom it would otherwise accept, and
// changes nothing (CONTEXT.md: Departed).
func TestReportCommandsRefuseADepartedPerson(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newReportRulesWorld(t)
	s := w.h.Service
	due := w.h.Clock.Now().AddDate(0, 0, 7)
	picked := domain.SaveReportDefinitionInput{Name: "QBR", Mode: domain.ReportModePicked, Picked: []int64{w.goal.ID}}

	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"SaveReportDefinition by anyone Departed", func() error {
			_, err := s.SaveReportDefinition(ctx, w.departedOwner.ID, picked)
			return err
		}},
		{"UpdateReportDefinition by a Departed Report Delegate", func() error {
			_, err := s.UpdateReportDefinition(ctx, w.departedDelegate.ID, w.def.ID, picked)
			return err
		}},
		{"CurateNarrative by a Departed Report Delegate", func() error {
			return s.CurateNarrative(ctx, w.departedDelegate.ID, w.def.ID, domain.CurateNarrativeInput{Notes: map[string][]string{domain.HighlightMiss: {"Watch churn."}}})
		}},
		{"PublishReport by a Departed Report Delegate", func() error {
			_, err := s.PublishReport(ctx, w.departedDelegate.ID, w.def.ID, time.Time{})
			return err
		}},
		{"AddReportDelegate by a Departed Admin", func() error {
			return s.AddReportDelegate(ctx, w.departedAdmin.ID, w.def.ID, w.newcomer.ID)
		}},
		{"RemoveReportDelegate by a Departed Admin", func() error {
			return s.RemoveReportDelegate(ctx, w.departedAdmin.ID, w.def.ID, w.delegate.ID)
		}},
		{"RaiseActionItem by its Departed publisher", func() error {
			_, err := s.RaiseActionItem(ctx, w.departedDelegate.ID, domain.RaiseActionItemInput{
				PublicationID: w.departedDelegatesPub.ID, GoalID: w.goal.ID, Text: "Price the EU tiers.", OwnerID: w.owner.ID, DueDate: due,
			})
			return err
		}},
		{"CloseActionItem by its Departed owner", func() error {
			_, err := s.CloseActionItem(ctx, w.departedOwner.ID, w.departedOwners.ID, "Booked it.")
			return err
		}},
		{"ReassignActionItem by a Departed Report Delegate", func() error {
			_, err := s.ReassignActionItem(ctx, w.departedDelegate.ID, w.open.ID, w.newcomer.ID)
			return err
		}},
		{"AddComment by anyone Departed", func() error {
			_, err := s.AddComment(ctx, w.departedOwner.ID, w.departedDelegatesPub.ID, w.goal.ID, "Still here?")
			return err
		}},
		{"AddGoalComment by anyone Departed", func() error {
			_, err := s.AddGoalComment(ctx, w.departedOwner.ID, w.goal.ID, "Still here?")
			return err
		}},
		{"ReplyToComment by anyone Departed", func() error {
			_, err := s.ReplyToComment(ctx, w.departedOwner.ID, w.thread.ID, "It is.")
			return err
		}},
	} {
		before := w.state(t)
		if err := c.run(); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s: err %v, want ErrNotAuthorized", c.name, err)
		}
		if after := w.state(t); !reflect.DeepEqual(after, before) {
			t.Errorf("%s changed what it refused", c.name)
		}
	}
}
