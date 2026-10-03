package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Delegate submits a Check-in on an Owner's Goal without sharing the Owner's
// login: the Check-in records the Delegate as its author and the Owner it was
// written for (CONTEXT.md: each Check-in records who wrote it).
func TestDelegateSubmitsCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	if err := h.Service.AddDelegate(ctx, sam.ID, goal.ID, tpm.ID); err != nil {
		t.Fatalf("AddDelegate: %v", err)
	}

	c, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: tpm.ID,
		Health:   domain.HealthGreen,
		Status:   "Checked in for Sam.",
	})
	if err != nil {
		t.Fatalf("Delegate SubmitCheckin: %v", err)
	}
	if c.Author.ID != tpm.ID {
		t.Errorf("Author = %d, want the Delegate %d", c.Author.ID, tpm.ID)
	}
	if c.Owner.ID != sam.ID {
		t.Errorf("Owner = %d, want the Owner %d", c.Owner.ID, sam.ID)
	}
}

// A person who is neither the Owner nor a Delegate cannot submit a Check-in on
// someone else's Goal (acceptance: Non-Delegates can't submit Check-ins).
func TestNonDelegateCannotSubmitCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	mel := h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: mel.ID,
		Health:   domain.HealthGreen,
		Status:   "Meddling.",
	}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Fatalf("non-Delegate SubmitCheckin err = %v, want ErrNotAuthorized", err)
	}
}

// A removed Delegate can no longer submit Check-ins: authorization ends when the
// Owner revokes it (CONTEXT.md: a person an Owner authorizes).
func TestRemovedDelegateCannotSubmitCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	if err := h.Service.AddDelegate(ctx, sam.ID, goal.ID, tpm.ID); err != nil {
		t.Fatalf("AddDelegate: %v", err)
	}
	if err := h.Service.RemoveDelegate(ctx, sam.ID, goal.ID, tpm.ID); err != nil {
		t.Fatalf("RemoveDelegate: %v", err)
	}

	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: tpm.ID,
		Health:   domain.HealthGreen,
		Status:   "No longer allowed.",
	}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Fatalf("removed Delegate SubmitCheckin err = %v, want ErrNotAuthorized", err)
	}
	if delegates, _ := h.Service.ListDelegates(ctx, goal.ID); len(delegates) != 0 {
		t.Fatalf("Delegate still listed after removal: %+v", delegates)
	}
}

// An Owner authorizes a Delegate on their Goal, and the Goal then lists that
// Delegate (CONTEXT.md: a person an Owner authorizes to write Check-ins).
func TestOwnerAddsDelegate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if err := h.Service.AddDelegate(ctx, sam.ID, goal.ID, tpm.ID); err != nil {
		t.Fatalf("AddDelegate: %v", err)
	}

	delegates, err := h.Service.ListDelegates(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	if len(delegates) != 1 || delegates[0].ID != tpm.ID {
		t.Fatalf("delegates = %+v, want just %s", delegates, tpm.Email)
	}
}

// Only the Goal's Owner may authorize a Delegate; a stranger's attempt is
// refused and adds nobody (CONTEXT.md: a person an Owner authorizes).
func TestOnlyOwnerAddsDelegate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	mel := h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if err := h.Service.AddDelegate(ctx, mel.ID, goal.ID, tpm.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Fatalf("stranger AddDelegate err = %v, want ErrNotAuthorized", err)
	}
	delegates, err := h.Service.ListDelegates(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	if len(delegates) != 0 {
		t.Fatalf("a Delegate was added by a non-Owner: %+v", delegates)
	}
}

// A Departed Delegate can't act, so can no longer submit a Check-in on the
// Owner's Goal — but they stay listed among its Delegates, marked Departed
// (CONTEXT.md: Departed).
func TestDepartedDelegateCannotSubmitCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, goal.ID)
	if err := h.Service.MarkDeparted(ctx, boss.ID, tpm.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: tpm.ID,
		Health:   domain.HealthGreen,
		Status:   "Checked in for Sam.",
	}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Departed Delegate SubmitCheckin err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.SubmitNoChangeCheckin(ctx, goal.ID, tpm.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Departed Delegate SubmitNoChangeCheckin err = %v, want ErrNotAuthorized", err)
	}

	delegates, err := h.Service.ListDelegates(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	if len(delegates) != 1 || delegates[0].ID != tpm.ID || !delegates[0].Departed {
		t.Errorf("Delegates = %+v, want the Departed Delegate still listed", delegates)
	}
}
