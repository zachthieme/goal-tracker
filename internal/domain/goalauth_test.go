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

// Only the Admin running an import may link Goals without the parent's Owner
// accepting: the child's Owner, a stranger, and an Admin outside an import are
// refused, and no link is made.
func TestImportLinkAdmitsOnlyTheImportingAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	ctx := context.Background()
	imp, err := h.Service.ForImportBy(ctx, admin.ID)
	if err != nil {
		t.Fatalf("ForImportBy: %v", err)
	}

	for name, try := range map[string]struct {
		svc   *domain.Service
		actor domain.Account
	}{
		"the child's Owner":                  {h.Service, sam},
		"a stranger":                         {h.Service, h.SignIn("kim@example.com")},
		"an Admin outside an import":         {h.Service, admin},
		"someone else in the Admin's import": {imp, sam},
	} {
		if _, err := try.svc.ImportLink(ctx, try.actor.ID, child.ID, parent.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s: err = %v, want ErrNotAuthorized", name, err)
		}
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("parents after refusals = %+v, want none", got)
	}
	if _, err := imp.ImportLink(ctx, admin.ID, child.ID, parent.ID); err != nil {
		t.Errorf("the importing Admin: %v", err)
	}
}

// importLinkAsAdmin links childID to parentID as an Admin's import does, as
// admin@example.com, whom the harness must list as an Admin.
func importLinkAsAdmin(t *testing.T, h *testsupport.Harness, childID, parentID int64) (domain.Link, error) {
	t.Helper()
	admin := h.SignIn("admin@example.com")
	imp, err := h.Service.ForImportBy(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("ForImportBy: %v", err)
	}
	return imp.ImportLink(context.Background(), admin.ID, childID, parentID)
}

// RequireCheckinAuthor admits a Goal's Owner and its Delegates, as
// SubmitCheckin does, and refuses anyone else, an Admin and a Departed
// Delegate included; a missing Goal is not found.
func TestRequireCheckinAuthor(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	lee := h.SignIn("lee@example.com")
	g := h.CreateGoal(sam, "Ship v2", "why")
	h.AddDelegate(sam, dana, g.ID)
	h.AddDelegate(sam, lee, g.ID)
	ctx := context.Background()
	if err := h.Service.MarkDeparted(ctx, admin.ID, lee.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	for name, actor := range map[string]domain.Account{"the Owner": sam, "a Delegate": dana} {
		if err := h.Service.RequireCheckinAuthor(ctx, actor.ID, g.ID); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, actor := range map[string]domain.Account{
		"a stranger":          h.SignIn("pat@example.com"),
		"an Admin":            admin,
		"a Departed Delegate": lee,
	} {
		if err := h.Service.RequireCheckinAuthor(ctx, actor.ID, g.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s: err = %v, want ErrNotAuthorized", name, err)
		}
	}
	if err := h.Service.RequireCheckinAuthor(ctx, sam.ID, g.ID+100); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a missing Goal: err = %v, want ErrNotFound", err)
	}
}

// RequireGoalValueSetter admits a Goal's Owner, its Delegates and an Admin, as
// setting its Dimension values does, and refuses anyone else.
func TestRequireGoalValueSetter(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	g := h.CreateGoal(sam, "Ship v2", "why")
	h.AddDelegate(sam, dana, g.ID)
	ctx := context.Background()

	for name, actor := range map[string]domain.Account{
		"the Owner":  sam,
		"a Delegate": dana,
		"an Admin":   h.SignIn("admin@example.com"),
	} {
		if err := h.Service.RequireGoalValueSetter(ctx, actor.ID, g.ID); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := h.Service.RequireGoalValueSetter(ctx, h.SignIn("pat@example.com").ID, g.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("a stranger: err = %v, want ErrNotAuthorized", err)
	}
}
