package domain_test

import (
	"context"
	"errors"
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
