package domain_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The author turns a comment into an Action Item with an owner and a due date.
// It appears at the top of each new publication of the same Definition until
// its owner closes it with a note (CONTEXT.md: Action Item).
func TestActionItemCarriesIntoEachNewPublicationUntilClosed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	author := h.SignIn("author@example.com")
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	first := h.PublishReport(author, def)
	question, err := h.Service.AddComment(ctx, reader.ID, first.ID, g.ID, "Can we get a second vendor quote?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	due := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	item, err := h.Service.RaiseActionItem(ctx, author.ID, domain.RaiseActionItemInput{
		PublicationID: first.ID,
		CommentID:     question.ID,
		OwnerID:       owner.ID,
		DueDate:       due,
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	if item.Text != "Can we get a second vendor quote?" || item.Owner.ID != owner.ID || !item.DueDate.Equal(due) ||
		item.DefinitionID != def.ID || item.CommentID != question.ID || item.Closed {
		t.Errorf("action item %+v, want an open item from the comment, owned by the owner, due %v", item, due)
	}
	if got, err := h.Service.GetPublication(ctx, first.ID); err != nil || len(got.Report.ActionItems) != 0 {
		t.Errorf("the publication it was raised on now lists %+v (err %v), want it unchanged", got.Report.ActionItems, err)
	}

	h.Clock.Advance(7 * day)
	second := h.PublishReport(author, def)
	if len(second.Report.ActionItems) != 1 || second.Report.ActionItems[0].ID != item.ID {
		t.Fatalf("next publication's Action Items %+v, want the open item", second.Report.ActionItems)
	}
	h.Clock.Advance(7 * day)
	third := h.PublishReport(author, def)
	if len(third.Report.ActionItems) != 1 {
		t.Fatalf("third publication's Action Items %+v, want the still-open item", third.Report.ActionItems)
	}

	closed, err := h.Service.CloseActionItem(ctx, owner.ID, item.ID, "  Second quote is in: 20% cheaper.  ")
	if err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	if !closed.Closed || closed.ClosingNote != "Second quote is in: 20% cheaper." || !closed.ClosedAt.Equal(h.Clock.Now()) {
		t.Errorf("closed item %+v, want closed now with the trimmed note", closed)
	}

	h.Clock.Advance(7 * day)
	fourth := h.PublishReport(author, def)
	if len(fourth.Report.ActionItems) != 0 {
		t.Errorf("publication after closing lists %+v, want no Action Items", fourth.Report.ActionItems)
	}
	if got, err := h.Service.GetPublication(ctx, second.ID); err != nil || len(got.Report.ActionItems) != 1 || got.Report.ActionItems[0].Closed {
		t.Errorf("earlier publication's frozen Action Items %+v (err %v), want the item as it stood, open", got.Report.ActionItems, err)
	}
}

// The author creates an Action Item directly, without a comment; only the
// Report's author raises one, and it needs a description, an owner, and a due
// date (CONTEXT.md: Action Item).
func TestAuthorCreatesActionItemDirectly(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	author := h.SignIn("author@example.com")
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(reader, def)
	due := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	direct := domain.RaiseActionItemInput{PublicationID: pub.ID, Text: "Book the EU legal review.", OwnerID: owner.ID, DueDate: due}

	item, err := h.Service.RaiseActionItem(ctx, author.ID, direct)
	if err != nil {
		t.Fatalf("RaiseActionItem by the Definition's author: %v", err)
	}
	if item.Text != "Book the EU legal review." || item.CommentID != 0 || item.CreatedBy.ID != author.ID {
		t.Errorf("action item %+v, want the direct item raised by the author", item)
	}
	if _, err := h.Service.RaiseActionItemByEmail(ctx, reader.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Chase the vendor.", DueDate: due,
	}, "owner@example.com"); err != nil {
		t.Errorf("RaiseActionItemByEmail by the publication's publisher: %v", err)
	}
	items, err := h.Service.PublicationActionItems(ctx, pub.ID)
	if err != nil || len(items) != 2 {
		t.Errorf("publication's Action Items %+v (err %v), want both", items, err)
	}

	stranger := h.SignIn("stranger@example.com")
	if _, err := h.Service.RaiseActionItem(ctx, stranger.ID, direct); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("raised by someone who is not the author: err %v, want ErrNotAuthorized", err)
	}
	for name, in := range map[string]domain.RaiseActionItemInput{
		"no description": {PublicationID: pub.ID, OwnerID: owner.ID, DueDate: due},
		"no owner":       {PublicationID: pub.ID, Text: "x", DueDate: due},
		"no due date":    {PublicationID: pub.ID, Text: "x", OwnerID: owner.ID},
	} {
		if _, err := h.Service.RaiseActionItem(ctx, author.ID, in); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: err %v, want ErrValidation", name, err)
		}
	}
	if _, err := h.Service.RaiseActionItemByEmail(ctx, author.ID, direct, "nobody@example.com"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("owner with no account: err %v, want ErrValidation", err)
	}
}

// Only the Action Item's owner closes it, with a note, and only once.
func TestOnlyTheOwnerClosesAnActionItemWithANote(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	author := h.SignIn("author@example.com")
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	item, err := h.Service.RaiseActionItem(ctx, author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Book the EU legal review.", OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}

	if _, err := h.Service.CloseActionItem(ctx, author.ID, item.ID, "Done."); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("closed by the author, not its owner: err %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.CloseActionItem(ctx, owner.ID, item.ID, "  "); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("closed without a note: err %v, want ErrValidation", err)
	}
	if _, err := h.Service.CloseActionItem(ctx, owner.ID, item.ID, "Booked for 2/3."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	if _, err := h.Service.CloseActionItem(ctx, owner.ID, item.ID, "Again."); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("closed twice: err %v, want ErrValidation", err)
	}
	if open, err := h.Service.OpenActionItems(ctx, def.ID); err != nil || len(open) != 0 {
		t.Errorf("open Action Items %+v (err %v), want none", open, err)
	}
}

// An Action Item raised from a Comment is about that Comment's Goal; naming a
// different Goal is refused (CONTEXT.md: Action Item).
func TestActionItemFromACommentIsAboutItsGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	author := h.SignIn("author@example.com")
	owner := h.SignIn("owner@example.com")
	eu := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	v2 := h.ActiveGoal(owner, "Ship v2", "Win back churned accounts.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID, v2.ID}})
	pub := h.PublishReport(author, def)
	question, err := h.Service.AddComment(ctx, owner.ID, pub.ID, eu.ID, "Can we get a second vendor quote?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	fromComment := domain.RaiseActionItemInput{
		PublicationID: pub.ID, CommentID: question.ID, OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	}

	item, err := h.Service.RaiseActionItem(ctx, author.ID, fromComment)
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	if item.GoalID != eu.ID || item.GoalTitle != "Launch in EU" {
		t.Errorf("item about goal %d %q, want the Comment's Goal %d %q", item.GoalID, item.GoalTitle, eu.ID, eu.Title)
	}
	fromComment.GoalID = eu.ID
	if _, err := h.Service.RaiseActionItem(ctx, author.ID, fromComment); err != nil {
		t.Errorf("naming the Comment's own Goal: %v", err)
	}
	fromComment.GoalID = v2.ID
	if _, err := h.Service.RaiseActionItem(ctx, author.ID, fromComment); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("naming another Goal than the Comment's: err %v, want ErrValidation", err)
	}
}

// An Action Item raised directly is about the Report as a whole, or names a
// Goal the publication covers; a Goal it doesn't cover is refused.
func TestDirectActionItemNamesAGoalTheReportCovers(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	author := h.SignIn("author@example.com")
	owner := h.SignIn("owner@example.com")
	eu := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	elsewhere := h.ActiveGoal(owner, "Hire a CFO", "Get the books in order.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID}})
	pub := h.PublishReport(author, def)
	direct := domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Book the EU legal review.", OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	}

	whole, err := h.Service.RaiseActionItem(ctx, author.ID, direct)
	if err != nil {
		t.Fatalf("RaiseActionItem about the whole Report: %v", err)
	}
	if whole.GoalID != 0 || whole.GoalTitle != "" {
		t.Errorf("item about goal %d %q, want the whole Report", whole.GoalID, whole.GoalTitle)
	}
	direct.GoalID = eu.ID
	about, err := h.Service.RaiseActionItem(ctx, author.ID, direct)
	if err != nil {
		t.Fatalf("RaiseActionItem about a covered Goal: %v", err)
	}
	if about.GoalID != eu.ID || about.GoalTitle != "Launch in EU" {
		t.Errorf("item about goal %d %q, want %d %q", about.GoalID, about.GoalTitle, eu.ID, eu.Title)
	}
	direct.GoalID = elsewhere.ID
	if _, err := h.Service.RaiseActionItem(ctx, author.ID, direct); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("about a Goal the publication doesn't cover: err %v, want ErrValidation", err)
	}
}

// actionItemsWorld is two people's Action Items across two Reports: some
// about a Goal, one about a whole Report, and one closed.
type actionItemsWorld struct {
	h                      *testsupport.Harness
	owner, other           domain.Account
	eu, v2                 domain.Goal
	euSoon, v2Later, whole domain.ActionItem
	others, closed         domain.ActionItem
}

func newActionItemsWorld(t *testing.T) actionItemsWorld {
	t.Helper()
	h := testsupport.New(t)
	ctx := context.Background()
	w := actionItemsWorld{h: h}
	author := h.SignIn("author@example.com")
	w.owner = h.SignIn("owner@example.com")
	w.other = h.SignIn("other@example.com")
	w.eu = h.ActiveGoal(w.owner, "Launch in EU", "Expand the market.")
	w.v2 = h.ActiveGoal(w.owner, "Ship v2", "Win back churned accounts.")
	mbr := h.PublishReport(author, h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{
		Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{w.eu.ID},
	}))
	qbr := h.PublishReport(author, h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{
		Name: "QBR", Mode: domain.ReportModePicked, Picked: []int64{w.eu.ID, w.v2.ID},
	}))
	raise := func(pubID, goalID int64, owner domain.Account, text string, dueInDays int) domain.ActionItem {
		t.Helper()
		item, err := h.Service.RaiseActionItem(ctx, author.ID, domain.RaiseActionItemInput{
			PublicationID: pubID, GoalID: goalID, Text: text, OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, dueInDays),
		})
		if err != nil {
			t.Fatalf("RaiseActionItem %q: %v", text, err)
		}
		return item
	}
	w.v2Later = raise(qbr.ID, w.v2.ID, w.owner, "Price the v2 tiers.", 14)
	w.whole = raise(mbr.ID, 0, w.owner, "Move the MBR to Tuesdays.", 10)
	w.euSoon = raise(mbr.ID, w.eu.ID, w.owner, "Book the EU legal review.", 3)
	w.others = raise(qbr.ID, w.eu.ID, w.other, "Find an EU distributor.", 5)
	w.closed = raise(mbr.ID, w.eu.ID, w.owner, "Translate the site.", 1)
	if _, err := h.Service.CloseActionItem(ctx, w.owner.ID, w.closed.ID, "Done in-house."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	return w
}

// listedActionItems names each listing by its item's text, Report and Goal.
func listedActionItems(listings []domain.ActionItemListing) []string {
	var out []string
	for _, l := range listings {
		out = append(out, l.Item.Text+" | "+l.ReportName+" | "+l.GoalTitle)
	}
	return out
}

// A person's open Action Items, across Reports, soonest due first, each with
// its Report and Goal; closed ones and other people's are left out.
func TestOpenActionItemsOwnedByListsAPersonsOpenItems(t *testing.T) {
	t.Parallel()

	w := newActionItemsWorld(t)

	got, err := w.h.Service.OpenActionItemsOwnedBy(context.Background(), w.owner.ID)
	if err != nil {
		t.Fatalf("OpenActionItemsOwnedBy: %v", err)
	}
	want := []string{
		"Book the EU legal review. | MBR | Launch in EU",
		"Move the MBR to Tuesdays. | MBR | ",
		"Price the v2 tiers. | QBR | Ship v2",
	}
	if g := listedActionItems(got); !slices.Equal(g, want) {
		t.Errorf("owner's open items = %q, want %q", g, want)
	}
}

// A Goal's Action Items, open and closed, from every Report that covers it;
// none about another Goal or about a whole Report.
func TestActionItemsAboutListsAGoalsItems(t *testing.T) {
	t.Parallel()

	w := newActionItemsWorld(t)

	got, err := w.h.Service.ActionItemsAbout(context.Background(), w.eu.ID)
	if err != nil {
		t.Fatalf("ActionItemsAbout: %v", err)
	}
	want := []string{
		"Translate the site. | MBR | Launch in EU",
		"Book the EU legal review. | MBR | Launch in EU",
		"Find an EU distributor. | QBR | Launch in EU",
	}
	if g := listedActionItems(got); !slices.Equal(g, want) {
		t.Errorf("EU Goal's items = %q, want %q", g, want)
	}
	if len(got) > 0 && !got[0].Item.Closed {
		t.Errorf("first item %+v, want the closed one", got[0].Item)
	}
}

// Every open Action Item, by owner and then soonest due; no closed one.
func TestAllOpenActionItemsListsEveryOpenItem(t *testing.T) {
	t.Parallel()

	w := newActionItemsWorld(t)

	got, err := w.h.Service.AllOpenActionItems(context.Background())
	if err != nil {
		t.Fatalf("AllOpenActionItems: %v", err)
	}
	want := []string{
		"Book the EU legal review. | MBR | Launch in EU",
		"Move the MBR to Tuesdays. | MBR | ",
		"Price the v2 tiers. | QBR | Ship v2",
		"Find an EU distributor. | QBR | Launch in EU",
	}
	if g := listedActionItems(got); !slices.Equal(g, want) {
		t.Errorf("open items = %q, want %q", g, want)
	}
}

// Once the clock passes an open Action Item's due date it is overdue, if it
// is about a Goal; one due today, a closed one, and one about a whole Report
// are not listed.
func TestOverdueActionItemsListsOpenGoalItemsPastTheirDueDate(t *testing.T) {
	t.Parallel()

	w := newActionItemsWorld(t)
	ctx := context.Background()

	w.h.Clock.Advance(5 * day)
	got, err := w.h.Service.OverdueActionItems(ctx)
	if err != nil {
		t.Fatalf("OverdueActionItems: %v", err)
	}
	want := []string{"Book the EU legal review. | MBR | Launch in EU"}
	if g := listedActionItems(got); !slices.Equal(g, want) {
		t.Errorf("overdue items on the distributor item's due date = %q, want %q", g, want)
	}

	w.h.Clock.Advance(6 * day)
	got, err = w.h.Service.OverdueActionItems(ctx)
	if err != nil {
		t.Fatalf("OverdueActionItems: %v", err)
	}
	want = []string{
		"Book the EU legal review. | MBR | Launch in EU",
		"Find an EU distributor. | QBR | Launch in EU",
	}
	if g := listedActionItems(got); !slices.Equal(g, want) {
		t.Errorf("overdue items past the whole-Report item's due date = %q, want %q", g, want)
	}
	for _, l := range got {
		if !l.Overdue {
			t.Errorf("listed %q as not Overdue", l.Item.Text)
		}
	}
}

// A listed Action Item is Overdue the day after its due date, not on it, and
// never once closed.
func TestActionItemListingIsOverdueTheDayAfterItsDueDate(t *testing.T) {
	t.Parallel()

	w := newActionItemsWorld(t)
	ctx := context.Background()
	overdue := func() map[string]bool {
		t.Helper()
		got, err := w.h.Service.ActionItemsAbout(ctx, w.eu.ID)
		if err != nil {
			t.Fatalf("ActionItemsAbout: %v", err)
		}
		out := map[string]bool{}
		for _, l := range got {
			out[l.Item.Text] = l.Overdue
		}
		return out
	}

	w.h.Clock.Advance(3 * day)
	if got := overdue(); got["Book the EU legal review."] || got["Translate the site."] {
		t.Errorf("on the legal review's due date, overdue = %v, want neither it nor the closed item", got)
	}
	w.h.Clock.Advance(day)
	if got := overdue(); !got["Book the EU legal review."] || got["Translate the site."] {
		t.Errorf("the day after the legal review's due date, overdue = %v, want it and not the closed item", got)
	}
}

// reassignWorld is an open Action Item on a published Report, owned by
// someone other than the Report's author, and a second person to hand it to.
type reassignWorld struct {
	h                     *testsupport.Harness
	creator, owner, other domain.Account
	goal                  domain.Goal
	def                   domain.ReportDefinition
	pub                   domain.Publication
	item                  domain.ActionItem
}

// newReassignWorld publishes the Report as publisher (the creator when it is
// the zero Account) and raises the item on it. adminEmails get the Admin flag.
func newReassignWorld(t *testing.T, adminEmails ...string) reassignWorld {
	t.Helper()
	h := testsupport.New(t, adminEmails...)
	w := reassignWorld{h: h}
	w.creator = h.SignIn("creator@example.com")
	w.owner = h.SignIn("owner@example.com")
	w.other = h.SignIn("other@example.com")
	w.goal = h.ActiveGoal(w.owner, "Launch in EU", "Expand the market.")
	w.def = h.SaveReportDefinition(w.creator, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{w.goal.ID}})
	w.pub = h.PublishReport(w.creator, w.def)
	w.item = w.raise(t, w.pub, "Book the EU legal review.")
	return w
}

// raise raises an Action Item about the world's Goal on pub, owned by the
// world's owner.
func (w reassignWorld) raise(t *testing.T, pub domain.Publication, text string) domain.ActionItem {
	t.Helper()
	item, err := w.h.Service.RaiseActionItem(context.Background(), pub.PublishedBy.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: w.goal.ID, Text: text, OwnerID: w.owner.ID, DueDate: w.h.Clock.Now().AddDate(0, 0, 7),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	return item
}

// After a reassignment the item is the new owner's, it names who had it
// before, and the reassignment is recorded; only the new owner closes it.
func TestReassignedActionItemBelongsToItsNewOwner(t *testing.T) {
	t.Parallel()

	w := newReassignWorld(t)
	ctx := context.Background()
	w.h.Clock.Advance(day)

	got, err := w.h.Service.ReassignActionItemByEmail(ctx, w.creator.ID, w.item.ID, " other@example.com ")
	if err != nil {
		t.Fatalf("ReassignActionItemByEmail: %v", err)
	}
	if got.Owner.ID != w.other.ID || got.ReassignedFrom == nil || got.ReassignedFrom.ID != w.owner.ID {
		t.Errorf("reassigned item owned by %d, reassigned from %+v; want owned by %d, from %d", got.Owner.ID, got.ReassignedFrom, w.other.ID, w.owner.ID)
	}
	if fresh, err := w.h.Service.GetActionItem(ctx, w.item.ID); err != nil || fresh.Owner.ID != w.other.ID || fresh.ReassignedFrom == nil || fresh.ReassignedFrom.ID != w.owner.ID {
		t.Errorf("item read back %+v (err %v), want the new owner and who had it before", fresh, err)
	}
	if w.item.ReassignedFrom != nil {
		t.Errorf("item before any reassignment was reassigned from %+v, want nil", w.item.ReassignedFrom)
	}

	history, err := w.h.Service.ActionItemReassignments(ctx, w.item.ID)
	if err != nil {
		t.Fatalf("ActionItemReassignments: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("reassignments %+v, want one", history)
	}
	r := history[0]
	if r.ID == 0 || r.ItemID != w.item.ID || r.ItemText != "Book the EU legal review." || r.GoalID != w.goal.ID ||
		r.From.ID != w.owner.ID || r.To.ID != w.other.ID || r.By.ID != w.creator.ID || !r.At.Equal(w.h.Clock.Now()) {
		t.Errorf("reassignment %+v, want the item from owner to other, by the creator, now", r)
	}

	if _, err := w.h.Service.CloseActionItem(ctx, w.owner.ID, w.item.ID, "Done."); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("closed by the old owner: err %v, want ErrNotAuthorized", err)
	}
	if _, err := w.h.Service.CloseActionItem(ctx, w.other.ID, w.item.ID, "Done."); err != nil {
		t.Errorf("closed by the new owner: %v", err)
	}
}

// The Report Definition's creator, whoever published the publication an
// Action Item was raised on, an Admin, and the creator's Manager may each
// reassign it; nobody else may, its own owner included (CONTEXT.md: Action
// Item).
func TestWhoMayReassignAnActionItem(t *testing.T) {
	t.Parallel()

	for name, who := range map[string]func(t *testing.T, w *reassignWorld) domain.Account{
		"the Definition's creator": func(_ *testing.T, w *reassignWorld) domain.Account { return w.creator },
		"who published it": func(t *testing.T, w *reassignWorld) domain.Account {
			publisher := w.h.SignIn("publisher@example.com")
			w.h.Clock.Advance(day)
			w.pub = w.h.PublishReport(publisher, w.def)
			w.item = w.raise(t, w.pub, "Chase the vendor.")
			return publisher
		},
		"an Admin": func(_ *testing.T, w *reassignWorld) domain.Account { return w.h.SignIn("admin@example.com") },
		"the creator's Manager": func(_ *testing.T, w *reassignWorld) domain.Account {
			manager := w.h.SignIn("manager@example.com")
			w.h.SetManager(w.creator, manager)
			return manager
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newReassignWorld(t, "admin@example.com")
			actor := who(t, &w)
			ctx := context.Background()
			if ok, err := w.h.Service.CanReassignActionItem(ctx, actor.ID, w.item); err != nil || !ok {
				t.Errorf("CanReassignActionItem = %v (err %v), want true", ok, err)
			}
			got, err := w.h.Service.ReassignActionItem(ctx, actor.ID, w.item.ID, w.other.ID)
			if err != nil {
				t.Fatalf("ReassignActionItem: %v", err)
			}
			if got.Owner.ID != w.other.ID {
				t.Errorf("item owned by %d, want %d", got.Owner.ID, w.other.ID)
			}
		})
	}

	w := newReassignWorld(t, "admin@example.com")
	ctx := context.Background()
	// A later publication's publisher didn't publish the one it was raised on.
	laterPublisher := w.h.SignIn("later@example.com")
	w.h.Clock.Advance(day)
	w.h.PublishReport(laterPublisher, w.def)
	// A manager of someone other than the creator.
	ownersManager := w.h.SignIn("owners-manager@example.com")
	w.h.SetManager(w.owner, ownersManager)
	for name, actor := range map[string]domain.Account{
		"its owner":                    w.owner,
		"a stranger":                   w.other,
		"a later publication's author": laterPublisher,
		"the owner's Manager":          ownersManager,
	} {
		if ok, err := w.h.Service.CanReassignActionItem(ctx, actor.ID, w.item); err != nil || ok {
			t.Errorf("%s: CanReassignActionItem = %v (err %v), want false", name, ok, err)
		}
		if _, err := w.h.Service.ReassignActionItem(ctx, actor.ID, w.item.ID, w.other.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s: ReassignActionItem err %v, want ErrNotAuthorized", name, err)
		}
	}
	if _, err := w.h.Service.ReassignActionItem(ctx, w.creator.ID, w.item.ID+100, w.other.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("reassigning a missing item: err %v, want ErrNotFound", err)
	}
}

// Reassigning a closed item, to someone who has left, to an email with no
// account, or to its current owner is refused, and nothing is recorded.
func TestReassigningAnActionItemNeedsAnOpenItemAndANewOwner(t *testing.T) {
	t.Parallel()

	w := newReassignWorld(t, "admin@example.com")
	ctx := context.Background()
	admin := w.h.SignIn("admin@example.com")
	gone := w.h.SignIn("gone@example.com")
	if err := w.h.Service.MarkDeparted(ctx, admin.ID, gone.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	closed := w.raise(t, w.pub, "Translate the site.")
	if _, err := w.h.Service.CloseActionItem(ctx, w.owner.ID, closed.ID, "Done in-house."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}

	for name, try := range map[string]func() error{
		"a closed item": func() error {
			_, err := w.h.Service.ReassignActionItem(ctx, w.creator.ID, closed.ID, w.other.ID)
			return err
		},
		"to a Departed person": func() error {
			_, err := w.h.Service.ReassignActionItemByEmail(ctx, w.creator.ID, w.item.ID, "gone@example.com")
			return err
		},
		"to an unknown email": func() error {
			_, err := w.h.Service.ReassignActionItemByEmail(ctx, w.creator.ID, w.item.ID, "nobody@example.com")
			return err
		},
		"to its current owner": func() error {
			_, err := w.h.Service.ReassignActionItemByEmail(ctx, w.creator.ID, w.item.ID, "owner@example.com")
			return err
		},
	} {
		if err := try(); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("reassigning %s: err %v, want ErrValidation", name, err)
		}
	}
	for _, id := range []int64{w.item.ID, closed.ID} {
		if history, err := w.h.Service.ActionItemReassignments(ctx, id); err != nil || len(history) != 0 {
			t.Errorf("item %d's reassignments %+v (err %v), want none", id, history, err)
		}
	}
	if got, err := w.h.Service.GetActionItem(ctx, w.item.ID); err != nil || got.Owner.ID != w.owner.ID || got.ReassignedFrom != nil {
		t.Errorf("item %+v (err %v), want it still the owner's, never reassigned", got, err)
	}
}

// An item whose owner has left can be handed on by the Report's creator, and
// its new owner closes it; a creator who has since left can't reassign.
func TestDepartedOwnersActionItemIsReassignedButDepartedCreatorCant(t *testing.T) {
	t.Parallel()

	w := newReassignWorld(t, "admin@example.com")
	ctx := context.Background()
	admin := w.h.SignIn("admin@example.com")
	another := w.raise(t, w.pub, "Chase the vendor.")
	if err := w.h.Service.MarkDeparted(ctx, admin.ID, w.owner.ID); err != nil {
		t.Fatalf("MarkDeparted owner: %v", err)
	}
	if _, err := w.h.Service.ReassignActionItem(ctx, w.creator.ID, w.item.ID, w.other.ID); err != nil {
		t.Fatalf("reassigning the Departed owner's item: %v", err)
	}
	if _, err := w.h.Service.CloseActionItem(ctx, w.other.ID, w.item.ID, "Picked it up and booked it."); err != nil {
		t.Errorf("closed by its new owner: %v", err)
	}

	if err := w.h.Service.MarkDeparted(ctx, admin.ID, w.creator.ID); err != nil {
		t.Fatalf("MarkDeparted creator: %v", err)
	}
	if _, err := w.h.Service.ReassignActionItem(ctx, w.creator.ID, another.ID, w.other.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("reassigned by a Departed creator: err %v, want ErrNotAuthorized", err)
	}
}

// A Goal's reassignments are those of the Action Items about it, oldest
// first; none about another Goal or a whole Report.
func TestActionItemReassignmentsAboutAGoal(t *testing.T) {
	t.Parallel()

	w := newReassignWorld(t)
	ctx := context.Background()
	v2 := w.h.ActiveGoal(w.owner, "Ship v2", "Win back churned accounts.")
	def := w.h.SaveReportDefinition(w.creator, domain.SaveReportDefinitionInput{Name: "QBR", Mode: domain.ReportModePicked, Picked: []int64{w.goal.ID, v2.ID}})
	pub := w.h.PublishReport(w.creator, def)
	raise := func(goalID int64, text string) domain.ActionItem {
		t.Helper()
		item, err := w.h.Service.RaiseActionItem(ctx, w.creator.ID, domain.RaiseActionItemInput{
			PublicationID: pub.ID, GoalID: goalID, Text: text, OwnerID: w.owner.ID, DueDate: w.h.Clock.Now().AddDate(0, 0, 7),
		})
		if err != nil {
			t.Fatalf("RaiseActionItem %q: %v", text, err)
		}
		return item
	}
	reassign := func(item domain.ActionItem, to domain.Account) {
		t.Helper()
		if _, err := w.h.Service.ReassignActionItem(ctx, w.creator.ID, item.ID, to.ID); err != nil {
			t.Fatalf("ReassignActionItem %q: %v", item.Text, err)
		}
	}
	distributor := raise(w.goal.ID, "Find an EU distributor.")
	reassign(raise(v2.ID, "Price the v2 tiers."), w.other)
	reassign(raise(0, "Move the QBR to Tuesdays."), w.other)
	reassign(distributor, w.other)
	reassign(w.item, w.other)
	reassign(distributor, w.owner)

	got, err := w.h.Service.ActionItemReassignmentsAbout(ctx, w.goal.ID)
	if err != nil {
		t.Fatalf("ActionItemReassignmentsAbout: %v", err)
	}
	var names []string
	for _, r := range got {
		names = append(names, r.ItemText+": "+r.From.Email+" -> "+r.To.Email)
	}
	want := []string{
		"Find an EU distributor.: owner@example.com -> other@example.com",
		"Book the EU legal review.: owner@example.com -> other@example.com",
		"Find an EU distributor.: other@example.com -> owner@example.com",
	}
	if !slices.Equal(names, want) {
		t.Errorf("EU Goal's reassignments = %q, want %q", names, want)
	}
	if fresh, err := w.h.Service.GetActionItem(ctx, distributor.ID); err != nil || fresh.ReassignedFrom == nil || fresh.ReassignedFrom.ID != w.other.ID {
		t.Errorf("twice-reassigned item %+v (err %v), want it reassigned from its latest previous owner", fresh, err)
	}
}

// Raising an Action Item for someone else emails them what it is, who raised
// it, its Report and Goal, its due date, and a link to it.
func TestRaisingAnActionItemEmailsItsOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	svc := domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithBaseURL("https://goals.example.com/"))
	author := h.SignIn("author@example.com")
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	sentBefore := len(h.Email.Sent())

	if _, err := svc.RaiseActionItem(ctx, author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: g.ID, Text: "Book the EU\n  legal review.", OwnerID: owner.ID,
		DueDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}

	sent := h.Email.Sent()[sentBefore:]
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 to the owner: %+v", len(sent), sent)
	}
	m := sent[0]
	if m.To != "owner@example.com" {
		t.Errorf("email to %q, want the owner", m.To)
	}
	if want := "Action Item: Book the EU legal review."; m.Subject != want {
		t.Errorf("subject %q, want %q", m.Subject, want)
	}
	link := fmt.Sprintf("https://goals.example.com/reports/%d/publications/%d#action-items", def.ID, pub.ID)
	for _, want := range []string{"author (author@example.com) raised", "MBR", "Launch in EU", "2026-02-01", link} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("body does not mention %q:\n%s", want, m.Body)
		}
	}
}

// An author who raises an Action Item for themselves isn't emailed about it.
func TestRaisingAnActionItemForYourselfSendsNothing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	sentBefore := len(h.Email.Sent())

	if _, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Move the MBR to Tuesdays.", OwnerID: author.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	if sent := h.Email.Sent()[sentBefore:]; len(sent) != 0 {
		t.Errorf("sent %+v, want nothing to the author raising it for themselves", sent)
	}
}

// An Action Item, or a reassignment, whose email to its owner can't be sent
// isn't kept.
func TestActionItemIsNotKeptWhenItsEmailFails(t *testing.T) {
	t.Parallel()

	w := newReassignWorld(t)
	ctx := context.Background()
	down := domain.NewService(w.h.DB, w.h.Clock, failingSender{}, nil)

	if _, err := down.RaiseActionItem(ctx, w.creator.ID, domain.RaiseActionItemInput{
		PublicationID: w.pub.ID, Text: "Chase the vendor.", OwnerID: w.owner.ID, DueDate: w.h.Clock.Now().AddDate(0, 0, 7),
	}); err == nil {
		t.Fatal("RaiseActionItem succeeded though its email could not be sent")
	}
	items, err := w.h.Service.PublicationActionItems(ctx, w.pub.ID)
	if err != nil {
		t.Fatalf("PublicationActionItems: %v", err)
	}
	if len(items) != 1 || items[0].ID != w.item.ID {
		t.Errorf("publication's Action Items %+v, want only the one raised before mail went down", items)
	}

	if _, err := down.ReassignActionItem(ctx, w.creator.ID, w.item.ID, w.other.ID); err == nil {
		t.Fatal("ReassignActionItem succeeded though its email could not be sent")
	}
	if got, err := w.h.Service.GetActionItem(ctx, w.item.ID); err != nil || got.Owner.ID != w.owner.ID || got.ReassignedFrom != nil {
		t.Errorf("item after the failed reassignment %+v (err %v), want it still the owner's", got, err)
	}
	if history, err := w.h.Service.ActionItemReassignments(ctx, w.item.ID); err != nil || len(history) != 0 {
		t.Errorf("reassignments %+v (err %v), want none", history, err)
	}
}

// Reassigning an Action Item emails its new owner who handed it to them and
// from whom, with the same details as when it was raised. The previous owner
// gets nothing.
func TestReassigningAnActionItemEmailsItsNewOwner(t *testing.T) {
	t.Parallel()

	w := newReassignWorld(t)
	ctx := context.Background()
	svc := domain.NewService(w.h.DB, w.h.Clock, w.h.Email, nil, domain.WithBaseURL("https://goals.example.com"))
	sentBefore := len(w.h.Email.Sent())

	if _, err := svc.ReassignActionItem(ctx, w.creator.ID, w.item.ID, w.other.ID); err != nil {
		t.Fatalf("ReassignActionItem: %v", err)
	}

	sent := w.h.Email.Sent()[sentBefore:]
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 to the new owner: %+v", len(sent), sent)
	}
	m := sent[0]
	if m.To != "other@example.com" {
		t.Errorf("email to %q, want the new owner", m.To)
	}
	if want := "Action Item: Book the EU legal review."; m.Subject != want {
		t.Errorf("subject %q, want %q", m.Subject, want)
	}
	link := fmt.Sprintf("https://goals.example.com/reports/%d/publications/%d#action-items", w.def.ID, w.pub.ID)
	due := w.item.DueDate.Format("2006-01-02")
	for _, want := range []string{"creator (creator@example.com) reassigned", "from owner (owner@example.com)", "MBR", "Launch in EU", due, link} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("body does not mention %q:\n%s", want, m.Body)
		}
	}
}
