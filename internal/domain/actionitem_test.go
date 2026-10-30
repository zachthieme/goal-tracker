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
	if got, err := h.Service.GetPublication(ctx, first.ID); err != nil || slices.ContainsFunc(got.Facts, isFact(domain.ReportFactActionItems)) {
		t.Errorf("the publication it was raised on now lists Action Items (err %v), want it unchanged: %v", err, factOutline(got.Facts))
	}

	h.Clock.Advance(7 * day)
	second := h.PublishReport(author, def)
	if items := h.PublicationFact(second, domain.ReportFactActionItems).ActionItems; len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("next publication's Action Items %+v, want the open item", items)
	}
	h.Clock.Advance(7 * day)
	third := h.PublishReport(author, def)
	if items := h.PublicationFact(third, domain.ReportFactActionItems).ActionItems; len(items) != 1 {
		t.Fatalf("third publication's Action Items %+v, want the still-open item", items)
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
	if slices.ContainsFunc(fourth.Facts, isFact(domain.ReportFactActionItems)) {
		t.Errorf("publication after closing lists Action Items, want none: %v", factOutline(fourth.Facts))
	}
	got, err := h.Service.GetPublication(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if items := h.PublicationFact(got, domain.ReportFactActionItems).ActionItems; len(items) != 1 || items[0].ID != item.ID || items[0].Closed {
		t.Errorf("earlier publication's frozen Action Items %+v, want the item as it stood, open", items)
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
	h.AddReportDelegate(author, reader, def)
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
	if _, err := h.Service.RaiseActionItemByEmail(ctx, author.ID, direct, "nobody@example.com"); !errors.Is(err, domain.ErrValidation) || domain.Reason(err) != `no account with email "nobody@example.com"` {
		t.Errorf("owner with no account: err %v, want ErrValidation that no account has it", err)
	}
}

// Raising an Action Item by email asks whether the actor may raise one on the
// publication before it looks the owner's email up: a missing publication is
// not found, and someone who isn't the Report's author, or a Departed author,
// naming an email no one has is refused as not permitted.
func TestRaiseActionItemByEmailAuthorizesBeforeLookingUpTheEmail(t *testing.T) {
	t.Parallel()

	w := newReassignWorld(t, "admin@example.com")
	ctx := context.Background()
	admin := w.h.SignIn("admin@example.com")
	in := func(pubID int64) domain.RaiseActionItemInput {
		return domain.RaiseActionItemInput{PublicationID: pubID, Text: "Chase the vendor.", DueDate: w.h.Clock.Now().AddDate(0, 0, 7)}
	}

	if _, err := w.h.Service.RaiseActionItemByEmail(ctx, w.creator.ID, in(w.pub.ID+100), "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("raised on no such publication: err %v, want ErrNotFound", err)
	}
	if _, err := w.h.Service.RaiseActionItemByEmail(ctx, w.other.ID, in(w.pub.ID), "nobody@example.com"); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("raised by someone who is not the author: err %v, want ErrNotAuthorized", err)
	}
	if err := w.h.Service.MarkDeparted(ctx, admin.ID, w.creator.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	if _, err := w.h.Service.RaiseActionItemByEmail(ctx, w.creator.ID, in(w.pub.ID), "nobody@example.com"); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("raised by a Departed author: err %v, want ErrNotAuthorized", err)
	}
	if items, err := w.h.Service.PublicationActionItems(ctx, w.pub.ID); err != nil || len(items) != 1 {
		t.Errorf("publication's Action Items %+v (err %v), want only the world's item", items, err)
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

// A Comment on the Goal itself belongs to no Report, so it can't be turned
// into an Action Item on a publication (ticket #350).
func TestActionItemIsNotRaisedFromACommentOnTheGoalItself(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	author := h.SignIn("author@example.com")
	owner := h.SignIn("owner@example.com")
	eu := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID}})
	pub := h.PublishReport(author, def)
	question, err := h.Service.AddGoalComment(ctx, owner.ID, eu.ID, "Can we get a second vendor quote?")
	if err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}

	_, err = h.Service.RaiseActionItem(ctx, author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, CommentID: question.ID, OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("Action Item from a Goal Comment: err %v, want ErrValidation", err)
	}
	if items, err := h.Service.PublicationActionItems(ctx, pub.ID); err != nil || len(items) != 0 {
		t.Errorf("Action Items %+v (err %v), want none raised", items, err)
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

	got, err := domain.AllOpenActionItems(context.Background(), w.h.Service)
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

// publishAsFormerDelegate publishes the world's Report as publisher, made a
// Report Delegate to publish it and no longer one after, so they are only its
// publisher.
func (w reassignWorld) publishAsFormerDelegate(t *testing.T, publisher domain.Account) domain.Publication {
	t.Helper()
	w.h.AddReportDelegate(w.creator, publisher, w.def)
	pub := w.h.PublishReport(publisher, w.def)
	if err := w.h.Service.RemoveReportDelegate(context.Background(), w.creator.ID, w.def.ID, publisher.ID); err != nil {
		t.Fatalf("RemoveReportDelegate: %v", err)
	}
	return pub
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

// The Report Definition's creator, a Report Delegate on it, whoever published
// the publication an Action Item was raised on, an Admin, and the creator's
// Manager may each reassign it; nobody else may, its own owner included
// (CONTEXT.md: Action Item).
func TestWhoMayReassignAnActionItem(t *testing.T) {
	t.Parallel()

	for name, who := range map[string]func(t *testing.T, w *reassignWorld) domain.Account{
		"the Definition's creator": func(_ *testing.T, w *reassignWorld) domain.Account { return w.creator },
		"a Report Delegate": func(_ *testing.T, w *reassignWorld) domain.Account {
			delegate := w.h.SignIn("delegate@example.com")
			w.h.AddReportDelegate(w.creator, delegate, w.def)
			return delegate
		},
		"who published it": func(t *testing.T, w *reassignWorld) domain.Account {
			publisher := w.h.SignIn("publisher@example.com")
			w.h.Clock.Advance(day)
			w.pub = w.publishAsFormerDelegate(t, publisher)
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
			if p, err := w.h.Service.ActionItemPermissions(ctx, actor.ID, []domain.ActionItem{w.item}); err != nil || !p[w.item.ID].Reassign {
				t.Errorf("ActionItemPermissions = %+v (err %v), want Reassign", p, err)
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
	w.publishAsFormerDelegate(t, laterPublisher)
	// A manager of someone other than the creator.
	ownersManager := w.h.SignIn("owners-manager@example.com")
	w.h.SetManager(w.owner, ownersManager)
	for name, actor := range map[string]domain.Account{
		"its owner":                    w.owner,
		"a stranger":                   w.other,
		"a later publication's author": laterPublisher,
		"the owner's Manager":          ownersManager,
	} {
		if p, err := w.h.Service.ActionItemPermissions(ctx, actor.ID, []domain.ActionItem{w.item}); err != nil || p[w.item.ID].Reassign {
			t.Errorf("%s: ActionItemPermissions = %+v (err %v), want no Reassign", name, p, err)
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
	// The rule asks who first: someone who may not reassign any of the
	// Report's items is refused for that, not for the item being closed.
	if _, err := w.h.Service.ReassignActionItem(ctx, w.other.ID, closed.ID, w.creator.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("a stranger reassigning a closed item: err %v, want ErrNotAuthorized", err)
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
	h.DeliverEmail()

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
// is kept, and the email goes out once mail is back (ticket #412).
func TestActionItemIsKeptWhenItsEmailFails(t *testing.T) {
	t.Parallel()

	w := newReassignWorld(t)
	ctx := context.Background()
	down := domain.NewService(w.h.DB, w.h.Clock, failingSender{}, nil, domain.WithEmailDeliveredOnCommit())
	sentBefore := len(w.h.Email.Sent())

	raised, err := down.RaiseActionItem(ctx, w.creator.ID, domain.RaiseActionItemInput{
		PublicationID: w.pub.ID, Text: "Chase the vendor.", OwnerID: w.owner.ID, DueDate: w.h.Clock.Now().AddDate(0, 0, 7),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v, want it kept though its email could not be sent", err)
	}
	items, err := w.h.Service.PublicationActionItems(ctx, w.pub.ID)
	if err != nil {
		t.Fatalf("PublicationActionItems: %v", err)
	}
	if len(items) != 2 || items[1].ID != raised.ID {
		t.Errorf("publication's Action Items %+v, want the one raised while mail was down too", items)
	}

	if _, err := down.ReassignActionItem(ctx, w.creator.ID, w.item.ID, w.other.ID); err != nil {
		t.Fatalf("ReassignActionItem: %v, want it kept though its email could not be sent", err)
	}
	if got, err := w.h.Service.GetActionItem(ctx, w.item.ID); err != nil || got.Owner.ID != w.other.ID {
		t.Errorf("item after the reassignment %+v (err %v), want it the new owner's", got, err)
	}

	w.h.Clock.Advance(time.Minute)
	w.h.DeliverEmail()
	assertSentTo(t, w.h.Email.Sent()[sentBefore:], "owner@example.com", "other@example.com")
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
	w.h.DeliverEmail()

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

// chaseWorld is a viewer over an Org two levels deep (mid under the viewer,
// deep under mid) and two outsiders, with Action Items raised on Reports the
// outsiders and the Org create and publish.
type chaseWorld struct {
	h                 *testsupport.Harness
	viewer, mid, deep domain.Account
	outA, outB        domain.Account
	goal              domain.Goal
	n                 int
}

func newChaseWorld(t *testing.T) *chaseWorld {
	t.Helper()
	h := testsupport.New(t)
	w := &chaseWorld{h: h}
	w.viewer = h.SignIn("viewer@example.com")
	w.mid = h.SignIn("mid@example.com")
	w.deep = h.SignIn("deep@example.com")
	w.outA = h.SignIn("out-a@example.com")
	w.outB = h.SignIn("out-b@example.com")
	h.SetManager(w.mid, w.viewer)
	h.SetManager(w.deep, w.mid)
	w.goal = h.ActiveGoal(w.outA, "Launch in EU", "Expand the market.")
	return w
}

// raise saves a Report Definition as creator, publishes it as publisher (made a
// Report Delegate to, if not its creator), and raises text on it as raiser (who must be one of the two), owned by owner and
// due in dueInDays.
func (w *chaseWorld) raise(t *testing.T, creator, publisher, raiser, owner domain.Account, text string, dueInDays int) domain.ActionItem {
	t.Helper()
	w.n++
	def := w.h.SaveReportDefinition(creator, domain.SaveReportDefinitionInput{
		Name: fmt.Sprintf("Report %d", w.n), Mode: domain.ReportModePicked, Picked: []int64{w.goal.ID},
	})
	if publisher.ID != creator.ID {
		w.h.AddReportDelegate(creator, publisher, def)
	}
	pub := w.h.PublishReport(publisher, def)
	item, err := w.h.Service.RaiseActionItem(context.Background(), raiser.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: text, OwnerID: owner.ID, DueDate: w.h.Clock.Now().AddDate(0, 0, dueInDays),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem %q: %v", text, err)
	}
	return item
}

// openFor lists the viewer's two groups of open Action Items by their text.
func (w *chaseWorld) openFor(t *testing.T, viewer domain.Account) (yours, org []string) {
	t.Helper()
	y, o, err := w.h.Service.OpenActionItemsToChase(context.Background(), viewer.ID)
	if err != nil {
		t.Fatalf("OpenActionItemsToChase: %v", err)
	}
	for _, l := range y {
		yours = append(yours, l.Item.Text)
	}
	for _, l := range o {
		org = append(org, l.Item.Text)
	}
	return yours, org
}

// An open Action Item on a Report Definition the viewer created is theirs to
// chase, though someone outside their Org published, raised and owns it; one
// nobody in their Org is party to is left out.
func TestOpenActionItemsToChaseIncludeThoseOnDefinitionsTheViewerCreated(t *testing.T) {
	t.Parallel()

	w := newChaseWorld(t)
	w.raise(t, w.viewer, w.outA, w.outA, w.outB, "On the viewer's Definition.", 5)
	w.raise(t, w.outA, w.outB, w.outB, w.outA, "Nobody in the Org's.", 5)

	yours, org := w.openFor(t, w.viewer)
	if want := []string{"On the viewer's Definition."}; !slices.Equal(yours, want) || len(org) != 0 {
		t.Errorf("yours = %q, org = %q; want yours %q and no org", yours, org, want)
	}
}

// An open Action Item is in the viewer's My org group when someone in their
// Org, at any depth, published the publication it was raised on, raised it,
// or owns it, though nobody else in the Org is party to it. Only a Report's
// author raises an Action Item: its publication's publisher or its
// Definition's creator. So an item raised in the Org always has one of those
// in the Org too, and the raised-by case can't be isolated from both: its
// raiser created the Definition, which alone doesn't put an item in My org
// (TestOpenActionItemsToChaseLeaveOutThoseOnlyADefinitionByTheirOrgIsPartyTo).
func TestOpenActionItemsToChaseIncludeTheirOrgs(t *testing.T) {
	t.Parallel()

	for name, raise := range map[string]func(t *testing.T, w *chaseWorld) domain.ActionItem{
		"published two levels below the viewer": func(t *testing.T, w *chaseWorld) domain.ActionItem {
			return w.raise(t, w.outA, w.deep, w.outA, w.outB, "The item.", 5)
		},
		"raised by someone in the viewer's Org": func(t *testing.T, w *chaseWorld) domain.ActionItem {
			return w.raise(t, w.mid, w.outA, w.mid, w.outB, "The item.", 5)
		},
		"owned by someone in the viewer's Org": func(t *testing.T, w *chaseWorld) domain.ActionItem {
			return w.raise(t, w.outA, w.outA, w.outA, w.deep, "The item.", 5)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := newChaseWorld(t)
			raise(t, w)

			yours, org := w.openFor(t, w.viewer)
			if want := []string{"The item."}; len(yours) != 0 || !slices.Equal(org, want) {
				t.Errorf("yours = %q, org = %q; want no yours and org %q", yours, org, want)
			}
		})
	}
}

// An open Action Item on a Report Definition someone in the viewer's Org
// created, though someone outside the Org published, raised and owns it, is
// in neither group: only the viewer's own Definitions count. Nor can someone
// in the Org who neither created the Definition nor published the
// publication raise an item there, so none is relevant only for its raiser.
func TestOpenActionItemsToChaseLeaveOutThoseOnlyADefinitionByTheirOrgIsPartyTo(t *testing.T) {
	t.Parallel()

	w := newChaseWorld(t)
	w.raise(t, w.mid, w.outA, w.outA, w.outB, "On the Org's Definition.", 5)

	yours, org := w.openFor(t, w.viewer)
	if len(yours) != 0 || len(org) != 0 {
		t.Errorf("yours = %q, org = %q; want neither", yours, org)
	}

	def := w.h.SaveReportDefinition(w.outA, domain.SaveReportDefinitionInput{Name: "Outsiders' report", Mode: domain.ReportModePicked, Picked: []int64{w.goal.ID}})
	w.h.AddReportDelegate(w.outA, w.outB, def)
	pub := w.h.PublishReport(w.outB, def)
	_, err := w.h.Service.RaiseActionItem(context.Background(), w.mid.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Raised by the Org alone.", OwnerID: w.outB.ID, DueDate: w.h.Clock.Now().AddDate(0, 0, 5),
	})
	if !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("someone in the Org raising on a Report outsiders created and published: err %v, want ErrNotAuthorized", err)
	}
}

// An item the viewer owns, published, raised or whose Definition they created
// is in Yours; one only their Org is party to is in My org; none is in both.
// A closed item is in neither.
func TestOpenActionItemsToChaseSplitYoursFromTheirOrgs(t *testing.T) {
	t.Parallel()

	w := newChaseWorld(t)
	w.raise(t, w.outA, w.outA, w.outA, w.viewer, "Owned by the viewer.", 5)
	w.raise(t, w.outA, w.viewer, w.viewer, w.outB, "Published by the viewer.", 6)
	w.raise(t, w.mid, w.deep, w.mid, w.viewer, "Owned by the viewer, the rest by the Org.", 7)
	w.raise(t, w.mid, w.deep, w.deep, w.mid, "Only the Org's.", 8)
	closed := w.raise(t, w.viewer, w.viewer, w.viewer, w.mid, "Closed.", 9)
	if _, err := w.h.Service.CloseActionItem(context.Background(), w.mid.ID, closed.ID, "Done."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}

	yours, org := w.openFor(t, w.viewer)
	wantYours := []string{"Owned by the viewer.", "Published by the viewer.", "Owned by the viewer, the rest by the Org."}
	wantOrg := []string{"Only the Org's."}
	if !slices.Equal(yours, wantYours) || !slices.Equal(org, wantOrg) {
		t.Errorf("yours = %q, org = %q; want yours %q, org %q", yours, org, wantYours, wantOrg)
	}
}

// Within each group, overdue items come first, then the rest soonest due
// first, each marked overdue or not.
func TestOpenActionItemsToChaseListOverdueFirstThenSoonestDue(t *testing.T) {
	t.Parallel()

	w := newChaseWorld(t)
	w.raise(t, w.viewer, w.viewer, w.viewer, w.outA, "Due in 9.", 9)
	w.raise(t, w.viewer, w.viewer, w.viewer, w.outA, "Due in 1.", 1)
	w.raise(t, w.viewer, w.viewer, w.viewer, w.outA, "Due in 4.", 4)
	w.raise(t, w.viewer, w.viewer, w.viewer, w.outA, "Due in 2.", 2)
	w.raise(t, w.outA, w.outA, w.outA, w.deep, "Org due in 6.", 6)
	w.raise(t, w.outA, w.outA, w.outA, w.deep, "Org due in 3.", 3)
	w.h.Clock.Advance(4 * day)

	yours, org, err := w.h.Service.OpenActionItemsToChase(context.Background(), w.viewer.ID)
	if err != nil {
		t.Fatalf("OpenActionItemsToChase: %v", err)
	}
	listed := func(ls []domain.ActionItemListing) []string {
		var out []string
		for _, l := range ls {
			out = append(out, fmt.Sprintf("%s overdue=%t", l.Item.Text, l.Overdue))
		}
		return out
	}
	wantYours := []string{"Due in 1. overdue=true", "Due in 2. overdue=true", "Due in 4. overdue=false", "Due in 9. overdue=false"}
	if got := listed(yours); !slices.Equal(got, wantYours) {
		t.Errorf("yours = %q, want %q", got, wantYours)
	}
	wantOrg := []string{"Org due in 3. overdue=true", "Org due in 6. overdue=false"}
	if got := listed(org); !slices.Equal(got, wantOrg) {
		t.Errorf("org = %q, want %q", got, wantOrg)
	}
}

// Each refusal of a raise, close or reassign that is about one field names
// it, so a form can show the reason beside that field; one about the whole
// form names none. Each says what it always has (#660).
func TestActionItemRefusalsNameTheirInput(t *testing.T) {
	t.Parallel()

	w := newReassignWorld(t, "admin@example.com")
	ctx := context.Background()
	admin := w.h.SignIn("admin@example.com")
	gone := w.h.SignIn("gone@example.com")
	if err := w.h.Service.MarkDeparted(ctx, admin.ID, gone.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	elsewhere := w.h.ActiveGoal(w.owner, "Hire a CFO", "Get the books in order.")
	onGoal, err := w.h.Service.AddGoalComment(ctx, w.owner.ID, w.goal.ID, "Who is the vendor?")
	if err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	onPub, err := w.h.Service.AddComment(ctx, w.owner.ID, w.pub.ID, w.goal.ID, "Why did the vendor slip?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	closed := w.raise(t, w.pub, "Translate the site.")
	if _, err := w.h.Service.CloseActionItem(ctx, w.owner.ID, closed.ID, "Done in-house."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	due := w.h.Clock.Now().AddDate(0, 0, 7)
	raise := func(in domain.RaiseActionItemInput) error {
		in.PublicationID = w.pub.ID
		_, err := w.h.Service.RaiseActionItem(ctx, w.creator.ID, in)
		return err
	}

	for _, c := range []struct {
		name, input, message string
		try                  func() error
	}{
		{"no description", domain.InputActionItemText, "an Action Item needs a description", func() error {
			return raise(domain.RaiseActionItemInput{OwnerID: w.owner.ID, DueDate: due})
		}},
		{"no due date", domain.InputActionItemDue, "an Action Item needs a due date", func() error {
			return raise(domain.RaiseActionItemInput{Text: "x", OwnerID: w.owner.ID})
		}},
		{"no owner", domain.InputActionItemOwner, "an Action Item needs an owner", func() error {
			return raise(domain.RaiseActionItemInput{Text: "x", DueDate: due})
		}},
		{"a Departed owner", domain.InputActionItemOwner, "gone@example.com has left the company", func() error {
			return raise(domain.RaiseActionItemInput{Text: "x", OwnerID: gone.ID, DueDate: due})
		}},
		{"an owner by an email with no account", domain.InputActionItemOwner, `no account with email "nobody@example.com"`, func() error {
			_, err := w.h.Service.RaiseActionItemByEmail(ctx, w.creator.ID, domain.RaiseActionItemInput{PublicationID: w.pub.ID, Text: "x", DueDate: due}, "nobody@example.com")
			return err
		}},
		{"a Goal the publication doesn't cover", domain.InputActionItemGoal, fmt.Sprintf("goal %d is not in this publication", elsewhere.ID), func() error {
			return raise(domain.RaiseActionItemInput{GoalID: elsewhere.ID, Text: "x", OwnerID: w.owner.ID, DueDate: due})
		}},
		{"a close with no note", domain.InputClosingNote, "closing an Action Item needs a note", func() error {
			_, err := w.h.Service.CloseActionItem(ctx, w.owner.ID, w.item.ID, " ")
			return err
		}},
		{"a reassign to no one", domain.InputActionItemOwner, "an Action Item needs an owner", func() error {
			_, err := w.h.Service.ReassignActionItem(ctx, w.creator.ID, w.item.ID, 99999)
			return err
		}},
		{"a reassign to a Departed person", domain.InputActionItemOwner, "gone@example.com has left the company", func() error {
			_, err := w.h.Service.ReassignActionItemByEmail(ctx, w.creator.ID, w.item.ID, "gone@example.com")
			return err
		}},
		{"a reassign to its owner", domain.InputActionItemOwner, "owner@example.com already owns this Action Item", func() error {
			_, err := w.h.Service.ReassignActionItemByEmail(ctx, w.creator.ID, w.item.ID, "owner@example.com")
			return err
		}},
		{"a reassign to an email with no account", domain.InputActionItemOwner, `no account with email "nobody@example.com"`, func() error {
			_, err := w.h.Service.ReassignActionItemByEmail(ctx, w.creator.ID, w.item.ID, "nobody@example.com")
			return err
		}},
		{"a comment on the Goal itself", "", fmt.Sprintf("comment %d is not on this publication", onGoal.ID), func() error {
			return raise(domain.RaiseActionItemInput{CommentID: onGoal.ID, OwnerID: w.owner.ID, DueDate: due})
		}},
		{"a comment's item about another Goal", "", "an Action Item raised from a comment is about the comment's Goal", func() error {
			return raise(domain.RaiseActionItemInput{CommentID: onPub.ID, GoalID: elsewhere.ID, OwnerID: w.owner.ID, DueDate: due})
		}},
		{"a closed item", "", "the Action Item is already closed", func() error {
			_, err := w.h.Service.ReassignActionItem(ctx, w.creator.ID, closed.ID, w.other.ID)
			return err
		}},
	} {
		err := c.try()
		if !errors.Is(err, domain.ErrValidation) || domain.Reason(err) != c.message {
			t.Errorf("%s: err %v, want ErrValidation saying %q", c.name, err, c.message)
		}
		var named []string
		for _, p := range domain.InputErrors(err) {
			named = append(named, p.Input+": "+p.Message)
		}
		var want []string
		if c.input != "" {
			want = []string{c.input + ": " + c.message}
		}
		if !slices.Equal(named, want) {
			t.Errorf("%s: names inputs %q, want %q", c.name, named, want)
		}
	}
}
