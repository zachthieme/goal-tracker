package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// ActionItem is a follow-up raised while a Report is discussed (CONTEXT.md:
// Action Item). It has an owner and a due date, belongs to a Report
// Definition, and carries into each new publication until its owner closes it.
type ActionItem struct {
	ID           int64
	DefinitionID int64
	// PublicationID is the publication it was raised on, and CommentID the
	// comment it was turned from; CommentID is 0 when it was created directly.
	PublicationID int64
	CommentID     int64
	Text          string
	Owner         Account
	DueDate       time.Time
	CreatedBy     Account
	CreatedAt     time.Time
	Closed        bool
	ClosedAt      time.Time
	ClosingNote   string
	// GoalID is the Goal in the Report it is about, and GoalTitle that Goal's
	// title; they are 0 and "" when it is about the Report as a whole. One
	// raised from a comment is about the comment's Goal. A publication's
	// snapshot freezes GoalTitle as it read at publishing.
	GoalID    int64
	GoalTitle string
	// ReassignedFrom is who owned it before its most recent reassignment, or
	// nil if it has never been reassigned. A publication's snapshot freezes it
	// as it stood at publishing.
	ReassignedFrom *Account
}

// RaiseActionItemInput is the raise-an-Action-Item command's input. Set
// CommentID to turn a comment into the Action Item; its Text then defaults to
// the comment's, and it is about the comment's Goal. Leave it 0 to create one
// directly, which needs Text; GoalID then names the Goal it is about, or is 0
// for the Report as a whole.
type RaiseActionItemInput struct {
	PublicationID int64
	CommentID     int64
	GoalID        int64
	Text          string
	OwnerID       int64
	DueDate       time.Time
}

// RaiseActionItem raises an Action Item on the published Report
// in.PublicationID, for its Report Definition. Only the Report's author — who
// saved its Definition, or who published this publication — may raise one. It
// needs an owner who has not left the company and a due date; a comment it is
// turned from must be on the same publication, and a Goal it names must be one
// the publication covers. The owner is emailed it unless they raised it
// themselves; an Action Item whose email can't be sent is not kept.
func (s *Service) RaiseActionItem(ctx context.Context, actorID int64, in RaiseActionItemInput) (ActionItem, error) {
	pub, err := s.GetPublication(ctx, in.PublicationID)
	if err != nil {
		return ActionItem{}, err
	}
	author, err := s.isReportAuthor(ctx, actorID, pub)
	if err != nil {
		return ActionItem{}, err
	}
	if !author {
		return ActionItem{}, fmt.Errorf("%w: only the Report's author raises Action Items", ErrNotAuthorized)
	}
	text := strings.TrimSpace(in.Text)
	goalID := in.GoalID
	if in.CommentID != 0 {
		c, err := s.GetComment(ctx, in.CommentID)
		if err != nil {
			return ActionItem{}, err
		}
		if c.PublicationID != pub.ID {
			return ActionItem{}, fmt.Errorf("%w: comment %d is not on this publication", ErrValidation, c.ID)
		}
		if goalID != 0 && goalID != c.GoalID {
			return ActionItem{}, fmt.Errorf("%w: an Action Item raised from a comment is about the comment's Goal", ErrValidation)
		}
		goalID = c.GoalID
		if text == "" {
			text = c.Body
		}
	} else if goalID != 0 && !pub.Report.covers(goalID) {
		return ActionItem{}, fmt.Errorf("%w: goal %d is not in this publication", ErrValidation, goalID)
	}
	if text == "" {
		return ActionItem{}, fmt.Errorf("%w: an Action Item needs a description", ErrValidation)
	}
	if in.DueDate.IsZero() {
		return ActionItem{}, fmt.Errorf("%w: an Action Item needs a due date", ErrValidation)
	}
	owner, err := s.queries.GetAccount(ctx, in.OwnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ActionItem{}, fmt.Errorf("%w: an Action Item needs an owner", ErrValidation)
		}
		return ActionItem{}, fmt.Errorf("look up owner: %w", err)
	}
	if owner.Departed != 0 {
		return ActionItem{}, fmt.Errorf("%w: %s has left the company", ErrValidation, owner.Email)
	}
	var item ActionItem
	err = s.WithinTx(ctx, func(tx *Service) error {
		row, err := tx.queries.CreateActionItem(ctx, db.CreateActionItemParams{
			ReportDefinitionID: pub.DefinitionID,
			PublicationID:      pub.ID,
			CommentID:          in.CommentID,
			GoalID:             goalID,
			Text:               text,
			OwnerID:            owner.ID,
			DueDate:            in.DueDate.Format(dateFormat),
			CreatedBy:          actorID,
			CreatedAt:          s.clock.Now().Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create action item: %w", err)
		}
		if item, err = tx.actionItemFromRow(ctx, row); err != nil {
			return err
		}
		if item.Owner.ID == actorID {
			return nil
		}
		var people Mentions
		return tx.emailActionItemOwner(ctx, item, fmt.Sprintf("%s raised an Action Item for you", people.Of(item.CreatedBy)))
	})
	return item, err
}

// emailActionItemOwner emails the Action Item to its owner: intro, saying who
// gave it to them, then what it is, its Report and Goal, its due date, and a
// link to it.
func (s *Service) emailActionItemOwner(ctx context.Context, item ActionItem, intro string) error {
	def, err := s.queries.GetReportDefinition(ctx, item.DefinitionID)
	if err != nil {
		return fmt.Errorf("get report definition: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s in the Report %s:\n\n%s\n\n", intro, def.Name, item.Text)
	if item.GoalTitle != "" {
		fmt.Fprintf(&b, "Goal: %s\n", item.GoalTitle)
	}
	fmt.Fprintf(&b, "Due: %s\n\n", item.DueDate.Format(dateFormat))
	fmt.Fprintf(&b, "%s/reports/%d/publications/%d#action-items\n", s.baseURL, item.DefinitionID, item.PublicationID)
	subject := "Action Item: " + strings.Join(strings.Fields(item.Text), " ")
	if err := s.email.Send(ctx, email.Message{To: item.Owner.Email, Subject: subject, Body: b.String()}); err != nil {
		return fmt.Errorf("send action item email: %w", err)
	}
	return nil
}

// IsReportAuthor reports whether actorID is the author of the published Report
// pubID — who saved its Definition, or who published it — and so may raise
// Action Items on it.
func (s *Service) IsReportAuthor(ctx context.Context, actorID, pubID int64) (bool, error) {
	pub, err := s.GetPublication(ctx, pubID)
	if err != nil {
		return false, err
	}
	return s.isReportAuthor(ctx, actorID, pub)
}

func (s *Service) isReportAuthor(ctx context.Context, actorID int64, pub Publication) (bool, error) {
	if actorID == pub.PublishedBy.ID {
		return true, nil
	}
	def, err := s.queries.GetReportDefinition(ctx, pub.DefinitionID)
	if err != nil {
		return false, fmt.Errorf("get report definition: %w", err)
	}
	return actorID == def.CreatedBy, nil
}

// RaiseActionItemByEmail raises an Action Item owned by the Account with the
// given email. It is the web-facing convenience over RaiseActionItem, since
// the tool identifies people by email. An email with no account is rejected.
func (s *Service) RaiseActionItemByEmail(ctx context.Context, actorID int64, in RaiseActionItemInput, ownerEmail string) (ActionItem, error) {
	acc, err := s.queries.GetAccountByEmail(ctx, strings.TrimSpace(ownerEmail))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ActionItem{}, fmt.Errorf("%w: no account with email %q", ErrValidation, ownerEmail)
		}
		return ActionItem{}, fmt.Errorf("look up owner: %w", err)
	}
	in.OwnerID = acc.ID
	return s.RaiseActionItem(ctx, actorID, in)
}

// CloseActionItem closes an open Action Item with a note on how it was
// resolved. Only its owner may close it, and the note cannot be blank.
func (s *Service) CloseActionItem(ctx context.Context, actorID, id int64, note string) (ActionItem, error) {
	item, err := s.GetActionItem(ctx, id)
	if err != nil {
		return ActionItem{}, err
	}
	if actorID != item.Owner.ID {
		return ActionItem{}, fmt.Errorf("%w: only the Action Item's owner closes it", ErrNotAuthorized)
	}
	if item.Closed {
		return ActionItem{}, fmt.Errorf("%w: the Action Item is already closed", ErrValidation)
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return ActionItem{}, fmt.Errorf("%w: closing an Action Item needs a note", ErrValidation)
	}
	if err := s.queries.CloseActionItem(ctx, db.CloseActionItemParams{
		ClosedAt:    s.clock.Now().Format(timeFormat),
		ClosingNote: note,
		ID:          id,
	}); err != nil {
		return ActionItem{}, fmt.Errorf("close action item: %w", err)
	}
	return s.GetActionItem(ctx, id)
}

// ActionItemReassignment records one open Action Item handed to a new owner
// (CONTEXT.md: Action Item): who had it, who has it now, who reassigned it,
// and when.
type ActionItemReassignment struct {
	ID       int64
	ItemID   int64
	ItemText string
	// GoalID is the Goal the item is about, or 0 for a whole Report.
	GoalID int64
	From   Account
	To     Account
	By     Account
	At     time.Time
}

// ReassignActionItem hands the open Action Item itemID to newOwnerID and
// records the reassignment. Only those CanReassignActionItem allows may do
// it, and the new owner must not have left the company or already own it.
// The new owner is emailed it unless they reassigned it to themselves; a
// reassignment whose email can't be sent is not kept.
func (s *Service) ReassignActionItem(ctx context.Context, actorID, itemID, newOwnerID int64) (ActionItem, error) {
	item, err := s.reassignableActionItem(ctx, actorID, itemID)
	if err != nil {
		return ActionItem{}, err
	}
	owner, err := s.queries.GetAccount(ctx, newOwnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ActionItem{}, fmt.Errorf("%w: an Action Item needs an owner", ErrValidation)
		}
		return ActionItem{}, fmt.Errorf("look up new owner: %w", err)
	}
	if owner.Departed != 0 {
		return ActionItem{}, fmt.Errorf("%w: %s has left the company", ErrValidation, owner.Email)
	}
	if owner.ID == item.Owner.ID {
		return ActionItem{}, fmt.Errorf("%w: %s already owns this Action Item", ErrValidation, owner.Email)
	}
	var reassigned ActionItem
	if err := s.WithinTx(ctx, func(tx *Service) error {
		n, err := tx.queries.ReassignActionItem(ctx, db.ReassignActionItemParams{OwnerID: owner.ID, ID: item.ID})
		if err != nil {
			return fmt.Errorf("reassign action item: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: the Action Item is already closed", ErrValidation)
		}
		if _, err := tx.queries.CreateActionItemReassignment(ctx, db.CreateActionItemReassignmentParams{
			ActionItemID: item.ID,
			FromOwnerID:  item.Owner.ID,
			ToOwnerID:    owner.ID,
			ReassignedBy: actorID,
			ReassignedAt: s.clock.Now().Format(timeFormat),
		}); err != nil {
			return fmt.Errorf("record action item reassignment: %w", err)
		}
		if reassigned, err = tx.GetActionItem(ctx, item.ID); err != nil {
			return err
		}
		if owner.ID == actorID {
			return nil
		}
		by, err := tx.queries.GetAccount(ctx, actorID)
		if err != nil {
			return fmt.Errorf("look up reassigner: %w", err)
		}
		var people Mentions
		intro := fmt.Sprintf("%s reassigned an Action Item to you from %s", people.Of(accountFromRow(by)), people.Of(item.Owner))
		return tx.emailActionItemOwner(ctx, reassigned, intro)
	}); err != nil {
		return ActionItem{}, err
	}
	return reassigned, nil
}

// ReassignActionItemByEmail hands the Action Item itemID to the Account with
// the given email, the web-facing convenience over ReassignActionItem. An
// email with no account is rejected.
func (s *Service) ReassignActionItemByEmail(ctx context.Context, actorID, itemID int64, email string) (ActionItem, error) {
	acc, err := s.queries.GetAccountByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return ActionItem{}, fmt.Errorf("look up new owner: %w", err)
		}
		// Report a missing or unreassignable item before the email.
		if _, err := s.reassignableActionItem(ctx, actorID, itemID); err != nil {
			return ActionItem{}, err
		}
		return ActionItem{}, fmt.Errorf("%w: no account with email %q", ErrValidation, email)
	}
	return s.ReassignActionItem(ctx, actorID, itemID, acc.ID)
}

// reassignableActionItem returns the Action Item itemID if it exists, is open,
// and actorID may reassign it.
func (s *Service) reassignableActionItem(ctx context.Context, actorID, itemID int64) (ActionItem, error) {
	item, err := s.GetActionItem(ctx, itemID)
	if err != nil {
		return ActionItem{}, err
	}
	if item.Closed {
		return ActionItem{}, fmt.Errorf("%w: the Action Item is already closed", ErrValidation)
	}
	ok, err := s.CanReassignActionItem(ctx, actorID, item)
	if err != nil {
		return ActionItem{}, err
	}
	if !ok {
		return ActionItem{}, fmt.Errorf("%w: only the Report's creator, a Report Delegate or its publisher, an Admin, or the creator's Manager reassigns an Action Item", ErrNotAuthorized)
	}
	return item, nil
}

// CanReassignActionItem reports whether actorID may hand the Action Item to a
// new owner (CONTEXT.md: Action Item): it is open, and they are its Report
// Definition's creator or one of its Report Delegates, who published the
// publication it was raised on, an Admin, or the creator's Manager. A Departed
// person can't.
func (s *Service) CanReassignActionItem(ctx context.Context, actorID int64, item ActionItem) (bool, error) {
	if item.Closed {
		return false, nil
	}
	actor, err := s.queries.GetAccount(ctx, actorID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("look up actor: %w", err)
	}
	if actor.Departed != 0 {
		return false, nil
	}
	if actor.IsAdmin != 0 {
		return true, nil
	}
	def, err := s.queries.GetReportDefinition(ctx, item.DefinitionID)
	if err != nil {
		return false, fmt.Errorf("get report definition: %w", err)
	}
	if def.CreatedBy == actorID {
		return true, nil
	}
	delegates, err := s.ListReportDelegates(ctx, def.ID)
	if err != nil {
		return false, err
	}
	if slices.ContainsFunc(delegates, func(d Account) bool { return d.ID == actorID }) {
		return true, nil
	}
	pub, err := s.queries.GetReportPublication(ctx, item.PublicationID)
	if err != nil {
		return false, fmt.Errorf("get report publication: %w", err)
	}
	if pub.PublishedBy == actorID {
		return true, nil
	}
	creator, err := s.queries.GetAccount(ctx, def.CreatedBy)
	if err != nil {
		return false, fmt.Errorf("look up report creator: %w", err)
	}
	return creator.ManagerID != nil && *creator.ManagerID == actorID, nil
}

// ActionItemReassignments returns the Action Item itemID's reassignments,
// oldest first.
func (s *Service) ActionItemReassignments(ctx context.Context, itemID int64) ([]ActionItemReassignment, error) {
	rows, err := s.queries.ListActionItemReassignments(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list action item reassignments: %w", err)
	}
	return s.actionItemReassignmentsFromRows(ctx, rows)
}

// ActionItemReassignmentsAbout returns the reassignments of the Action Items
// about the Goal goalID, oldest first.
func (s *Service) ActionItemReassignmentsAbout(ctx context.Context, goalID int64) ([]ActionItemReassignment, error) {
	rows, err := s.queries.ListActionItemReassignmentsForGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list goal's action item reassignments: %w", err)
	}
	return s.actionItemReassignmentsFromRows(ctx, rows)
}

func (s *Service) actionItemReassignmentsFromRows(ctx context.Context, rows []db.ActionItemReassignment) ([]ActionItemReassignment, error) {
	out := make([]ActionItemReassignment, 0, len(rows))
	for _, row := range rows {
		item, err := s.queries.GetActionItem(ctx, row.ActionItemID)
		if err != nil {
			return nil, fmt.Errorf("get reassigned action item: %w", err)
		}
		r := ActionItemReassignment{ID: row.ID, ItemID: item.ID, ItemText: item.Text, GoalID: item.GoalID}
		for _, p := range []struct {
			id  int64
			acc *Account
		}{{row.FromOwnerID, &r.From}, {row.ToOwnerID, &r.To}, {row.ReassignedBy, &r.By}} {
			acc, err := s.queries.GetAccount(ctx, p.id)
			if err != nil {
				return nil, fmt.Errorf("look up reassignment account: %w", err)
			}
			*p.acc = accountFromRow(acc)
		}
		r.At, _ = time.Parse(timeFormat, row.ReassignedAt)
		out = append(out, r)
	}
	return out, nil
}

// GetActionItem returns the Action Item with the given id, or ErrNotFound if
// none exists.
func (s *Service) GetActionItem(ctx context.Context, id int64) (ActionItem, error) {
	row, err := s.queries.GetActionItem(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ActionItem{}, fmt.Errorf("%w: action item %d", ErrNotFound, id)
		}
		return ActionItem{}, fmt.Errorf("get action item: %w", err)
	}
	return s.actionItemFromRow(ctx, row)
}

// OpenActionItems returns the Report Definition defID's open Action Items,
// soonest due first: what the next publication carries at its top.
func (s *Service) OpenActionItems(ctx context.Context, defID int64) ([]ActionItem, error) {
	rows, err := s.queries.ListOpenActionItems(ctx, defID)
	if err != nil {
		return nil, fmt.Errorf("list open action items: %w", err)
	}
	return s.actionItemsFromRows(ctx, rows)
}

// PublicationActionItems returns the Action Items raised on the published
// Report pubID, open or closed, in the order they were raised.
func (s *Service) PublicationActionItems(ctx context.Context, pubID int64) ([]ActionItem, error) {
	rows, err := s.queries.ListPublicationActionItems(ctx, pubID)
	if err != nil {
		return nil, fmt.Errorf("list publication action items: %w", err)
	}
	return s.actionItemsFromRows(ctx, rows)
}

func (s *Service) actionItemsFromRows(ctx context.Context, rows []db.ActionItem) ([]ActionItem, error) {
	var out []ActionItem
	for _, row := range rows {
		item, err := s.actionItemFromRow(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) actionItemFromRow(ctx context.Context, row db.ActionItem) (ActionItem, error) {
	owner, err := s.queries.GetAccount(ctx, row.OwnerID)
	if err != nil {
		return ActionItem{}, fmt.Errorf("look up action item owner: %w", err)
	}
	by, err := s.queries.GetAccount(ctx, row.CreatedBy)
	if err != nil {
		return ActionItem{}, fmt.Errorf("look up action item author: %w", err)
	}
	var goalTitle string
	if row.GoalID != 0 {
		g, err := s.queries.GetGoal(ctx, row.GoalID)
		if err != nil {
			return ActionItem{}, fmt.Errorf("look up action item goal: %w", err)
		}
		goalTitle = g.Goal.Title
	}
	due, _ := time.Parse(dateFormat, row.DueDate)
	createdAt, _ := time.Parse(timeFormat, row.CreatedAt)
	item := ActionItem{
		ID:            row.ID,
		DefinitionID:  row.ReportDefinitionID,
		PublicationID: row.PublicationID,
		CommentID:     row.CommentID,
		GoalID:        row.GoalID,
		GoalTitle:     goalTitle,
		Text:          row.Text,
		Owner:         accountFromRow(owner),
		DueDate:       due,
		CreatedBy:     accountFromRow(by),
		CreatedAt:     createdAt,
		ClosingNote:   row.ClosingNote,
	}
	if row.ClosedAt != "" {
		item.Closed = true
		item.ClosedAt, _ = time.Parse(timeFormat, row.ClosedAt)
	}
	latest, err := s.queries.GetLatestActionItemReassignment(ctx, row.ID)
	switch {
	case err == nil:
		from, err := s.queries.GetAccount(ctx, latest.FromOwnerID)
		if err != nil {
			return ActionItem{}, fmt.Errorf("look up action item's previous owner: %w", err)
		}
		prev := accountFromRow(from)
		item.ReassignedFrom = &prev
	case !errors.Is(err, sql.ErrNoRows):
		return ActionItem{}, fmt.Errorf("get latest action item reassignment: %w", err)
	}
	return item, nil
}

// ActionItemListing is an Action Item listed outside its Report — on Home, a
// Goal's page, Follow-ups, or an email — with what a reader needs to place it.
type ActionItemListing struct {
	Item ActionItem
	// ReportName is the name of the Report Definition it belongs to.
	ReportName string
	// GoalTitle is the same as Item.GoalTitle: "" when it is about the whole
	// Report.
	GoalTitle string
	// Overdue is whether it is open and its due date is before today, in the
	// org's timezone. One due today is not overdue.
	Overdue bool
}

// OpenActionItemsOwnedBy returns ownerID's open Action Items across every
// Report, soonest due first.
func (s *Service) OpenActionItemsOwnedBy(ctx context.Context, ownerID int64) ([]ActionItemListing, error) {
	rows, err := s.queries.ListOpenActionItemsForOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list owner's open action items: %w", err)
	}
	return actionItemListings(ctx, s, rows, func(r db.ListOpenActionItemsForOwnerRow) (db.ActionItem, string) {
		return r.ActionItem, r.ReportName
	})
}

// ActionItemsAbout returns the Action Items about the Goal goalID, open and
// closed, from every Report, soonest due first.
func (s *Service) ActionItemsAbout(ctx context.Context, goalID int64) ([]ActionItemListing, error) {
	rows, err := s.queries.ListActionItemsForGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list goal's action items: %w", err)
	}
	return actionItemListings(ctx, s, rows, func(r db.ListActionItemsForGoalRow) (db.ActionItem, string) {
		return r.ActionItem, r.ReportName
	})
}

// AllOpenActionItems returns every open Action Item, by owner and then
// soonest due.
func (s *Service) AllOpenActionItems(ctx context.Context) ([]ActionItemListing, error) {
	rows, err := s.queries.ListAllOpenActionItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all open action items: %w", err)
	}
	return actionItemListings(ctx, s, rows, func(r db.ListAllOpenActionItemsRow) (db.ActionItem, string) {
		return r.ActionItem, r.ReportName
	})
}

// OverdueActionItems returns the open Action Items about a Goal whose due
// date is before today, in the org's timezone, soonest due first. One about a
// whole Report is never listed: it has no Goal to put at risk.
func (s *Service) OverdueActionItems(ctx context.Context) ([]ActionItemListing, error) {
	today := orgDate(s.clock.Now(), s.loc).Format(dateFormat)
	rows, err := s.queries.ListOverdueActionItems(ctx, today)
	if err != nil {
		return nil, fmt.Errorf("list overdue action items: %w", err)
	}
	return actionItemListings(ctx, s, rows, func(r db.ListOverdueActionItemsRow) (db.ActionItem, string) {
		return r.ActionItem, r.ReportName
	})
}

// actionItemListings lists rows in order, split by row into each one's Action
// Item and its Report's name.
func actionItemListings[R any](ctx context.Context, s *Service, rows []R, row func(R) (db.ActionItem, string)) ([]ActionItemListing, error) {
	out := make([]ActionItemListing, 0, len(rows))
	for _, r := range rows {
		item, reportName := row(r)
		l, err := s.actionItemListing(ctx, item, reportName)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// actionItemListing loads row's Action Item and places it in its Report,
// judging it overdue against today in the org's timezone.
func (s *Service) actionItemListing(ctx context.Context, row db.ActionItem, reportName string) (ActionItemListing, error) {
	item, err := s.actionItemFromRow(ctx, row)
	if err != nil {
		return ActionItemListing{}, err
	}
	today := orgDate(s.clock.Now(), s.loc).Format(dateFormat)
	return ActionItemListing{
		Item:       item,
		ReportName: reportName,
		GoalTitle:  item.GoalTitle,
		Overdue:    !item.Closed && row.DueDate < today,
	}, nil
}

// OpenActionItemsToChase returns the open Action Items viewerID has reason to
// chase, across every Report, split into two groups. An item is theirs to
// chase when they created its Report Definition, or someone in their Org
// (CONTEXT.md: Org) published the publication it was raised on, raised it, or
// owns it. yours holds those where the viewer is that someone; org holds the
// rest. Each group is soonest due first, so its overdue items lead.
func (s *Service) OpenActionItemsToChase(ctx context.Context, viewerID int64) (yours, org []ActionItemListing, err error) {
	people, err := s.Org(ctx, viewerID)
	if err != nil {
		return nil, nil, err
	}
	inOrg := make(map[int64]bool, len(people))
	for _, p := range people {
		inOrg[p.ID] = true
	}
	rows, err := s.queries.ListOpenActionItemsWithParties(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list open action items with parties: %w", err)
	}
	for _, r := range rows {
		item := r.ActionItem
		var group *[]ActionItemListing
		switch {
		case slices.Contains([]int64{r.DefinitionCreatedBy, r.PublishedBy, item.CreatedBy, item.OwnerID}, viewerID):
			group = &yours
		case inOrg[r.PublishedBy] || inOrg[item.CreatedBy] || inOrg[item.OwnerID]:
			group = &org
		default:
			continue
		}
		l, err := s.actionItemListing(ctx, item, r.ReportName)
		if err != nil {
			return nil, nil, err
		}
		*group = append(*group, l)
	}
	return yours, org, nil
}
