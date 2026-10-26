package domain

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// UndoWindow is how long an Undo lasts after the action that offered it, by
// the Service's clock.
const UndoWindow = 15 * time.Minute

// The actions that offer an Undo, each naming what its token's subject is.
//
// Each name starts undo: the CHECK test on undo_tokens.kind finds its values by
// that prefix (enumchecks_test.go), so a value named otherwise goes unchecked.
const (
	undoLinkRemoval      = "link-removal"      // a LinkRemoval
	undoLinkRejection    = "link-rejection"    // a LinkRejection
	undoHandoffRejection = "handoff-rejection" // a Handoff
	undoValueRetirement  = "value-retire"      // a Dimension value
)

// errUndoUnavailable refuses a token that has expired or been used.
var errUndoUnavailable = Refuse(ErrValidation, "this Undo is no longer available")

// issueUndo records a fresh one-time token that lets accountID undo the kind
// of action done to subjectID, and returns it. detail is what the Undo must
// find unchanged (a retired value's name), or empty.
func (s *Service) issueUndo(ctx context.Context, kind string, subjectID, accountID int64, detail string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make undo token: %w", err)
	}
	token := hex.EncodeToString(raw)
	if _, err := s.queries.CreateUndoToken(ctx, db.CreateUndoTokenParams{
		Token:     token,
		Kind:      kind,
		SubjectID: subjectID,
		AccountID: accountID,
		Detail:    detail,
		IssuedAt:  s.clock.Now().Format(timeFormat),
	}); err != nil {
		return "", fmt.Errorf("record undo token: %w", err)
	}
	return token, nil
}

// spendUndo checks token is actorID's Undo of the kind of action done to
// subjectID, unused and no older than UndoWindow, and spends it. It commits
// the spending in a transaction of its own, before the caller attempts the
// restore, so a refused restore can't roll it back: a token is presented once.
// It returns the token's detail. A missing token, or one for another action or
// person, is ErrNotAuthorized; an expired or used one is ErrValidation.
func (s *Service) spendUndo(ctx context.Context, token, kind string, subjectID, actorID int64) (string, error) {
	notYours := Refuse(ErrNotAuthorized, "this Undo isn't yours to use")
	if token == "" {
		return "", notYours
	}
	now := s.clock.Now()
	var row db.UndoToken
	var spent bool
	err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		row, err = tx.queries.GetUndoToken(ctx, token)
		if errors.Is(err, sql.ErrNoRows) {
			return notYours
		}
		if err != nil {
			return fmt.Errorf("look up undo token: %w", err)
		}
		if row.Kind != kind || row.SubjectID != subjectID || row.AccountID != actorID {
			return notYours
		}
		usedAt := now.Format(timeFormat)
		n, err := tx.queries.UseUndoToken(ctx, db.UseUndoTokenParams{
			UsedAt: &usedAt,
			ID:     row.ID,
		})
		if err != nil {
			return fmt.Errorf("spend undo token: %w", err)
		}
		spent = n > 0
		return nil
	})
	if err != nil {
		return "", err
	}
	if !spent {
		return "", errUndoUnavailable
	}
	issuedAt, err := time.Parse(timeFormat, row.IssuedAt)
	if err != nil {
		return "", fmt.Errorf("read undo token time: %w", err)
	}
	if now.Sub(issuedAt) > UndoWindow {
		return "", errUndoUnavailable
	}
	return row.Detail, nil
}

// RetireDimensionValueWithUndo retires a value as RetireDimensionValue does,
// and returns the token that lets the Admin undo it (UndoRetireDimensionValue)
// for UndoWindow. The token keeps the value's name as retired.
func (s *Service) RetireDimensionValueWithUndo(ctx context.Context, actorID, valueID int64) (string, error) {
	var token string
	err := s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.RetireDimensionValue(ctx, actorID, valueID); err != nil {
			return err
		}
		val, _, err := tx.valueInDimension(ctx, valueID)
		if err != nil {
			return err
		}
		token, err = tx.issueUndo(ctx, undoValueRetirement, valueID, actorID, val.Value)
		return err
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// UndoRetireDimensionValue undoes retiring a value, restoring it as
// RestoreDimensionValue does, but only for the Admin who retired it, with the
// token RetireDimensionValueWithUndo gave them, within UndoWindow, and only
// once. Presenting the token spends it, even when the Undo is then refused. It
// is refused, changing nothing, unless the value still exists, is still
// Retired, and still has the name it was retired under. RestoreDimensionValue
// stays for restoring a Retired value at any time.
func (s *Service) UndoRetireDimensionValue(ctx context.Context, actorID, valueID int64, token string) error {
	name, err := s.spendUndo(ctx, token, undoValueRetirement, valueID, actorID)
	if err != nil {
		return err
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		val, _, err := tx.valueInDimension(ctx, valueID)
		if err != nil {
			return err
		}
		if !dimensionValueFromRow(val).Retired || val.Value != name {
			return Refuse(ErrValidation, "this value has changed since it was retired, so there is nothing to undo")
		}
		return tx.RestoreDimensionValue(ctx, actorID, valueID)
	})
}
