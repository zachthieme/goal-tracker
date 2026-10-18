package domain_test

import (
	"context"
	"errors"
	"slices"
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
