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

// Only the Goal's Owner may remove a Delegate: neither a stranger nor a
// Delegate may revoke one, and the refused attempt leaves the Delegate
// authorized (CONTEXT.md: a person an Owner authorizes).
func TestOnlyOwnerRemovesDelegate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	pm := h.SignIn("pm@example.com")
	mel := h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, goal.ID)
	h.AddDelegate(sam, pm, goal.ID)

	if err := h.Service.RemoveDelegate(ctx, mel.ID, goal.ID, tpm.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("stranger RemoveDelegate err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RemoveDelegate(ctx, pm.ID, goal.ID, tpm.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Delegate RemoveDelegate err = %v, want ErrNotAuthorized", err)
	}
	delegates, err := h.Service.ListDelegates(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	if len(delegates) != 2 || delegates[0].ID != pm.ID || delegates[1].ID != tpm.ID {
		t.Fatalf("delegates = %+v, want both pm and tpm still authorized", delegates)
	}
}

// ListDelegatesForGoals reads the Delegates of many Goals at once, keyed by
// Goal ID and ordered by email as ListDelegates orders one Goal's, and has no
// entry for a Goal with none.
func TestListDelegatesForGoalsReadsEachGoalsDelegates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	zed := h.SignIn("zed@example.com")
	amy := h.SignIn("amy@example.com")
	search := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	revenue := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	alone := h.ActiveGoal(sam, "Work alone", "Nobody helps.")
	h.AddDelegate(sam, zed, search.ID)
	h.AddDelegate(sam, amy, search.ID)
	h.AddDelegate(sam, zed, revenue.ID)

	got, err := h.Service.ListDelegatesForGoals(ctx, []int64{search.ID, revenue.ID, alone.ID})
	if err != nil {
		t.Fatalf("ListDelegatesForGoals: %v", err)
	}
	emails := func(goalID int64) []string {
		var out []string
		for _, a := range got[goalID] {
			out = append(out, a.Email)
		}
		return out
	}
	if e := emails(search.ID); len(e) != 2 || e[0] != "amy@example.com" || e[1] != "zed@example.com" {
		t.Errorf("Ship search's Delegates %v, want amy then zed", e)
	}
	if e := emails(revenue.ID); len(e) != 1 || e[0] != "zed@example.com" {
		t.Errorf("Grow revenue's Delegates %v, want zed", e)
	}
	if d, ok := got[alone.ID]; ok {
		t.Errorf("Work alone's Delegates %+v, want no entry", d)
	}
}

// Adding or removing a Delegate by email asks whether the actor may manage
// the Goal's Delegates before it looks the email up: someone else naming an
// email no one has is refused as not permitted, and only the Owner hears that
// no Account has it.
func TestDelegateByEmailAuthorizesBeforeLookingUpTheEmail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	rita := h.SignIn("rita@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")

	for _, c := range []struct {
		name string
		run  func(actorID int64) error
	}{
		{"AddDelegateByEmail", func(actorID int64) error {
			return h.Service.AddDelegateByEmail(ctx, actorID, goal.ID, "nobody@example.com")
		}},
		{"RemoveDelegateByEmail", func(actorID int64) error {
			return h.Service.RemoveDelegateByEmail(ctx, actorID, goal.ID, "nobody@example.com")
		}},
	} {
		if err := c.run(rita.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s of an unknown email by someone else = %v, want ErrNotAuthorized", c.name, err)
		}
		if err := c.run(sam.ID); !errors.Is(err, domain.ErrValidation) || domain.Reason(err) != `no account with email "nobody@example.com" — check the address or pick from the list` {
			t.Errorf("%s of an unknown email by the Owner = %v, want ErrValidation that no account has it", c.name, err)
		}
	}
}

// Adding or removing a Delegate by email on a Goal that doesn't exist is
// not found, as AddDelegate and RemoveDelegate answer it, whatever the email.
func TestDelegateByEmailRefusesAMissingGoal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")

	if err := h.Service.AddDelegateByEmail(ctx, sam.ID, 9999, "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("AddDelegateByEmail on a missing Goal = %v, want ErrNotFound", err)
	}
	if err := h.Service.RemoveDelegateByEmail(ctx, sam.ID, 9999, "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RemoveDelegateByEmail on a missing Goal = %v, want ErrNotFound", err)
	}
}
