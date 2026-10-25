package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A closure that panics inside WithinTx leaves nothing behind: its writes are
// rolled back and the write lock released, even on a background context that
// is never cancelled, as the weekly emails and the directory sync run on
// (#504). The panic itself carries on up to the caller.
func TestWithinTxRollsBackAndReleasesTheWriteLockWhenItsClosurePanics(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	recovered := func() (r any) {
		defer func() { r = recover() }()
		_ = h.Service.WithinTx(context.Background(), func(tx *domain.Service) error {
			if _, err := tx.EditTitle(context.Background(), g.ID, "Ship v3", owner.ID); err != nil {
				t.Fatalf("EditTitle in the transaction: %v", err)
			}
			panic("closure failed part-way")
		})
		return nil
	}()
	if recovered != "closure failed part-way" {
		t.Fatalf("recovered %v, want the closure's panic carried up", recovered)
	}

	got, err := h.Service.ViewGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Title != "Ship v2" {
		t.Errorf("title after the panic = %q, want the write not kept", got.Title)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = h.Service.WithinTx(ctx, func(tx *domain.Service) error {
		_, err := tx.EditTitle(ctx, g.ID, "Ship v4", owner.ID)
		return err
	})
	if err != nil {
		t.Fatalf("a write after the panic: %v, want the write lock released", err)
	}
}
