package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

func TestSignInCreatesAccountOnFirstUse(t *testing.T) {
	h := testsupport.New(t)

	acc, err := h.Service.SignIn(context.Background(), "sam@example.com")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if acc.ID == 0 {
		t.Error("want a persisted account with a non-zero ID")
	}
	if acc.Email != "sam@example.com" {
		t.Errorf("Email = %q, want sam@example.com", acc.Email)
	}
	if acc.IsAdmin {
		t.Error("account not in the admin list should not be an Admin")
	}
}

func TestSignInIsIdempotentForTheSameEmail(t *testing.T) {
	h := testsupport.New(t)

	first := h.SignIn("sam@example.com")
	second := h.SignIn("sam@example.com")

	if first.ID != second.ID {
		t.Errorf("second sign-in made a new account: %d != %d", first.ID, second.ID)
	}
}

func TestSignInSetsAdminFromConfig(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")

	admin := h.SignIn("boss@example.com")
	if !admin.IsAdmin {
		t.Error("email in the admin list should get the Admin flag")
	}

	regular := h.SignIn("sam@example.com")
	if regular.IsAdmin {
		t.Error("email not in the admin list should not get the Admin flag")
	}
}

// A Departed person can't sign in (CONTEXT.md: Departed).
func TestSignInRefusesADepartedAccount(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if _, err := h.Service.SignIn(context.Background(), "sam@example.com"); !errors.Is(err, domain.ErrDeparted) {
		t.Errorf("SignIn as a Departed Account: err = %v, want ErrDeparted", err)
	}
}

// An Admin reverses a departure: the person's remaining Goals stop being
// Ownerless, they can sign in again, and their Delegate rights come back as they
// were. A Goal reassigned while they were away stays with its new Owner
// (CONTEXT.md: Departed).
func TestMarkReturnedReversesADeparture(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	kept := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	moved := h.ActiveGoal(sam, "Migrate displays", "Displays fail often.")
	delegated := h.ActiveGoal(pat, "Grow revenue", "Revenue funds the rest.")
	h.AddDelegate(pat, sam, delegated.ID)
	if err := h.Service.MarkDeparted(ctx, boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	if _, err := h.Service.ReassignGoal(ctx, boss.ID, moved.ID, pat.ID); err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}

	if err := h.Service.MarkReturned(ctx, boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkReturned: %v", err)
	}

	if g, err := h.Service.ViewGoal(ctx, kept.ID); err != nil || g.Ownerless || g.Owner.ID != sam.ID {
		t.Errorf("Goal Sam still owns = %+v, %v; want Sam's and not Ownerless", g, err)
	}
	if g, err := h.Service.ViewGoal(ctx, moved.ID); err != nil || g.Owner.ID != pat.ID {
		t.Errorf("Goal reassigned while Sam was away = %+v, %v; want it still Pat's", g, err)
	}
	if acc, err := h.Service.SignIn(ctx, "sam@example.com"); err != nil || acc.Departed {
		t.Errorf("SignIn after return = %+v, %v; want a present Account", acc, err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   delegated.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Checked in for Pat.",
	}); err != nil {
		t.Errorf("returned Delegate SubmitCheckin: %v", err)
	}
}

// Only an Admin may mark a person returned; the refusal is enforced in the
// domain, not just hidden in the UI.
func TestMarkReturnedOnlyByAdmin(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	if err := h.Service.MarkDeparted(ctx, boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if err := h.Service.MarkReturned(ctx, pat.ID, sam.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin MarkReturned err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.SignIn(ctx, "sam@example.com"); !errors.Is(err, domain.ErrDeparted) {
		t.Errorf("Sam can sign in after a refused MarkReturned: err = %v", err)
	}
}

// A Handoff cancelled when its new Owner departed stays cancelled after they
// return, so the Goal stays with the Owner who started it.
func TestMarkReturnedLeavesACancelledHandoffCancelled(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho, err := h.Service.StartHandoff(ctx, domain.StartHandoffInput{GoalID: goal.ID, ToOwnerID: pat.ID, ActorID: sam.ID})
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	if err := h.Service.MarkDeparted(ctx, boss.ID, pat.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if err := h.Service.MarkReturned(ctx, boss.ID, pat.ID); err != nil {
		t.Fatalf("MarkReturned: %v", err)
	}

	history, err := h.Service.OwnershipHistory(ctx, goal.ID)
	if err != nil {
		t.Fatalf("OwnershipHistory: %v", err)
	}
	if len(history) != 1 || history[0].Status != domain.HandoffCancelled {
		t.Errorf("history = %+v, want the one Handoff still cancelled", history)
	}
	if _, err := h.Service.AcceptHandoff(ctx, ho.ID, pat.ID); err == nil {
		t.Error("the returned person accepted a Handoff cancelled at their departure")
	}
}
