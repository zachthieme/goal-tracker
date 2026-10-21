package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Admin's import makes the creation-time writes to the Goals it creates as
// their Owner would, but a Goal it didn't create stays the Owner's alone, and
// only an Admin may run one.
func TestAnImportWritesOnlyTheGoalsItCreates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	existing := h.CreateGoal(sam, "Cut checkout latency", "why")
	ctx := context.Background()
	due := testsupport.Epoch.AddDate(0, 6, 0)

	if _, err := h.Service.ForImportBy(ctx, sam.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("an import by a non-Admin: err = %v, want ErrNotAuthorized", err)
	}

	err := h.Service.WithinTx(ctx, func(tx *domain.Service) error {
		imp, err := tx.ForImportBy(ctx, admin.ID)
		if err != nil {
			return err
		}
		imported, err := imp.CreateGoal(ctx, domain.CreateGoalInput{Title: "Ship v2", WhyItMatters: "why", OwnerID: sam.ID})
		if err != nil {
			return err
		}
		if _, err := imp.MarkGoalDated(ctx, admin.ID, imported.ID, due); err != nil {
			t.Errorf("the importing Admin on a Goal the import created: %v", err)
		}
		if _, err := imp.MarkGoalDated(ctx, admin.ID, existing.ID, due); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("the importing Admin on a Goal the import didn't create: err = %v, want ErrNotAuthorized", err)
		}
		if _, err := tx.MarkGoalDated(ctx, admin.ID, imported.ID, due); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("the Admin outside the import: err = %v, want ErrNotAuthorized", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
}
