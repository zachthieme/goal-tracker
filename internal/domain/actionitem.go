package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
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
}

// RaiseActionItemInput is the raise-an-Action-Item command's input. Set
// CommentID to turn a comment into the Action Item; its Text then defaults to
// the comment's. Leave it 0 to create one directly, which needs Text.
type RaiseActionItemInput struct {
	PublicationID int64
	CommentID     int64
	Text          string
	OwnerID       int64
	DueDate       time.Time
}

// RaiseActionItem raises an Action Item on the published Report
// in.PublicationID, for its Report Definition. Only the Report's author — who
// saved its Definition, or who published this publication — may raise one. It
// needs an owner who has not left the org and a due date; a comment it is
// turned from must be on the same publication.
func (s *Service) RaiseActionItem(ctx context.Context, actorID int64, in RaiseActionItemInput) (ActionItem, error) {
	pub, err := s.GetPublication(ctx, in.PublicationID)
	if err != nil {
		return ActionItem{}, err
	}
	def, err := s.queries.GetReportDefinition(ctx, pub.DefinitionID)
	if err != nil {
		return ActionItem{}, fmt.Errorf("get report definition: %w", err)
	}
	if actorID != def.CreatedBy && actorID != pub.PublishedBy.ID {
		return ActionItem{}, fmt.Errorf("%w: only the Report's author raises Action Items", ErrNotAuthorized)
	}
	text := strings.TrimSpace(in.Text)
	if in.CommentID != 0 {
		c, err := s.GetComment(ctx, in.CommentID)
		if err != nil {
			return ActionItem{}, err
		}
		if c.PublicationID != pub.ID {
			return ActionItem{}, fmt.Errorf("%w: comment %d is not on this publication", ErrValidation, c.ID)
		}
		if text == "" {
			text = c.Body
		}
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
		return ActionItem{}, fmt.Errorf("%w: %s has left the org", ErrValidation, owner.Email)
	}
	row, err := s.queries.CreateActionItem(ctx, db.CreateActionItemParams{
		ReportDefinitionID: def.ID,
		PublicationID:      pub.ID,
		CommentID:          in.CommentID,
		Text:               text,
		OwnerID:            owner.ID,
		DueDate:            in.DueDate.Format(dateFormat),
		CreatedBy:          actorID,
		CreatedAt:          s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return ActionItem{}, fmt.Errorf("create action item: %w", err)
	}
	return s.actionItemFromRow(ctx, row)
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
	due, _ := time.Parse(dateFormat, row.DueDate)
	createdAt, _ := time.Parse(timeFormat, row.CreatedAt)
	item := ActionItem{
		ID:            row.ID,
		DefinitionID:  row.ReportDefinitionID,
		PublicationID: row.PublicationID,
		CommentID:     row.CommentID,
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
	return item, nil
}
