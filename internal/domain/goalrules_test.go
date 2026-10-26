package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// departedCommand is one Goal command a person makes, run on a Service bound
// to a transaction.
type departedCommand struct {
	name string
	run  func(tx *domain.Service) error
}

// A Departed person can't act (CONTEXT.md: Departed): every command the Owner
// of the Goal they leave Ownerless would make is refused them with
// ErrNotAuthorized once they leave, though each was accepted the moment
// before.
func TestGoalRulesRefuseADepartedOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	olive := h.SignIn("olive@example.com")
	nina := h.SignIn("nina@example.com")
	pat := h.SignIn("pat@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	revenue := h.ActiveGoal(pat, "Grow revenue", "The company needs it.")
	margin := h.ActiveGoal(pat, "Grow margin", "The company needs it too.")
	ownerless := h.ActiveGoal(olive, "Ship export", "People can't leave.")
	link := h.RequestLink(olive, ownerless, revenue, "")
	if _, err := h.Service.AcceptLink(ctx, link.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}

	title := "Ship export, again"
	commands := []departedCommand{
		{"AssignGoalValue", func(tx *domain.Service) error {
			return tx.AssignGoalValue(ctx, olive.ID, ownerless.ID, pillar.Values[0].ID)
		}},
		{"AddDelegate", func(tx *domain.Service) error {
			return tx.AddDelegate(ctx, olive.ID, ownerless.ID, nina.ID)
		}},
		{"AddMetric", func(tx *domain.Service) error {
			_, err := tx.AddMetric(ctx, olive.ID, domain.AddMetricInput{
				GoalID: ownerless.ID, Name: "Exports", Unit: "a day", Direction: domain.MetricUp,
				Baseline: 10, Target: 100, TargetDate: h.Clock.Now().AddDate(0, 4, 0),
			})
			return err
		}},
		{"RequestLink", func(tx *domain.Service) error {
			_, err := tx.RequestLink(ctx, domain.RequestLinkInput{ChildID: ownerless.ID, ParentID: margin.ID, RequesterID: olive.ID})
			return err
		}},
		{"RemoveLink", func(tx *domain.Service) error {
			_, err := tx.RemoveLink(ctx, link.ID, olive.ID)
			return err
		}},
		{"EditTitleAndWhyItMatters", func(tx *domain.Service) error {
			_, err := tx.EditTitleAndWhyItMatters(ctx, olive.ID, ownerless.ID, &title, ownerless.WhyItMatters)
			return err
		}},
	}

	expectAcceptedThenRefused(t, h, commands, func() { h.MarkDeparted(admin, olive) })
}

// A Departed Delegate can neither set the Goal's values nor suggest it a
// parent, though they could the moment before they left.
func TestGoalRulesRefuseADepartedDelegate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	pat := h.SignIn("pat@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	revenue := h.ActiveGoal(pat, "Grow revenue", "The company needs it.")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dana, g.ID)

	commands := []departedCommand{
		{"AssignGoalValue", func(tx *domain.Service) error {
			return tx.AssignGoalValue(ctx, dana.ID, g.ID, pillar.Values[0].ID)
		}},
		{"SuggestParent", func(tx *domain.Service) error {
			_, err := tx.SuggestParent(ctx, dana.ID, g.ID, revenue.ID, "")
			return err
		}},
	}

	expectAcceptedThenRefused(t, h, commands, func() { h.MarkDeparted(admin, dana) })
}

// A Departed Admin can no longer set a Goal's values, edit its title, reassign
// an Ownerless Goal or suggest a parent, though they could the moment before
// they left.
func TestGoalRulesRefuseADepartedAdmin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com", "ada@example.com")
	admin := h.SignIn("admin@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	olive := h.SignIn("olive@example.com")
	nina := h.SignIn("nina@example.com")
	pat := h.SignIn("pat@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	revenue := h.ActiveGoal(pat, "Grow revenue", "The company needs it.")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	ownerless := h.ActiveGoal(olive, "Ship export", "People can't leave.")
	h.MarkDeparted(admin, olive)

	title := "Ship search, again"
	commands := []departedCommand{
		{"AssignGoalValue", func(tx *domain.Service) error {
			return tx.AssignGoalValue(ctx, ada.ID, g.ID, pillar.Values[0].ID)
		}},
		{"EditTitleAndWhyItMatters", func(tx *domain.Service) error {
			_, err := tx.EditTitleAndWhyItMatters(ctx, ada.ID, g.ID, &title, g.WhyItMatters)
			return err
		}},
		{"ReassignGoal", func(tx *domain.Service) error {
			_, err := tx.ReassignGoal(ctx, ada.ID, ownerless.ID, nina.ID)
			return err
		}},
		{"SuggestParent", func(tx *domain.Service) error {
			_, err := tx.SuggestParent(ctx, ada.ID, g.ID, revenue.ID, "")
			return err
		}},
	}

	expectAcceptedThenRefused(t, h, commands, func() { h.MarkDeparted(admin, ada) })
}

// expectAcceptedThenRefused runs each of commands, rolled back, before and
// after depart: each must be accepted before and refused with
// ErrNotAuthorized after.
func expectAcceptedThenRefused(t *testing.T, h *testsupport.Harness, commands []departedCommand, depart func()) {
	t.Helper()
	for _, c := range commands {
		if err := runRolledBack(t, h, c); err != nil {
			t.Errorf("%s before departing: %v, want it accepted", c.name, err)
		}
	}
	depart()
	for _, c := range commands {
		if err := runRolledBack(t, h, c); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s after departing: %v, want ErrNotAuthorized", c.name, err)
		}
	}
}

// runRolledBack runs c in a transaction it then rolls back, returning what
// c answered.
func runRolledBack(t *testing.T, h *testsupport.Harness, c departedCommand) error {
	t.Helper()
	var got error
	if err := h.Service.WithinTx(t.Context(), func(tx *domain.Service) error {
		got = c.run(tx)
		return errRolledBack
	}); !errors.Is(err, errRolledBack) {
		t.Fatalf("%s: the transaction ended %v", c.name, err)
	}
	return got
}
