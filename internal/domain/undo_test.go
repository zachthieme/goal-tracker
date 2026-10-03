package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// wantNoLongerAvailable fails the test unless err refuses an Undo as expired
// or already used.
func wantNoLongerAvailable(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "no longer available") {
		t.Errorf("%s: err = %v, want ErrValidation saying the Undo is no longer available", what, err)
	}
}

// Removing a link hands its remover a one-time token; the Undo succeeds with
// it, and is refused without it, with someone else's, or with one for another
// action, restoring nothing.
func TestUndoingALinkRemovalTakesItsToken(t *testing.T) {
	h := testsupport.New(t)
	_, sam, child, parent, link := acceptedAcrossOwners(t, h)
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	if removal.UndoToken == "" {
		t.Fatal("RemoveLink gave no Undo token")
	}
	other := h.CreateGoal(sam, "Replace cables", "Cables fray.")
	otherLink := h.RequestLink(sam, other, child, "") // auto-accepted
	otherRemoval, err := h.Service.RemoveLink(ctx, otherLink.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink(other): %v", err)
	}

	for name, token := range map[string]string{
		"no token":                "",
		"a made-up token":         "not-a-token",
		"another removal's token": otherRemoval.UndoToken,
	} {
		if _, err := h.Service.RestoreLink(ctx, removal.ID, sam.ID, token); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("RestoreLink with %s: err = %v, want ErrNotAuthorized", name, err)
		}
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Fatalf("ParentsOf(child) = %+v after refused Undos, want none", got)
	}

	if _, err := h.Service.RestoreLink(ctx, removal.ID, sam.ID, removal.UndoToken); err != nil {
		t.Fatalf("RestoreLink with its token: %v", err)
	}
	if got := idsOf(h.ParentsOf(child)); !equalIDsUnordered(got, []int64{parent.ID}) {
		t.Errorf("ParentsOf(child) = %v, want [%d]", got, parent.ID)
	}
}

// An Undo lasts 15 minutes by the Service's clock: at 15 minutes it still
// works, a moment later it is no longer available.
func TestALinkRemovalUndoExpiresAfter15Minutes(t *testing.T) {
	h := testsupport.New(t)
	_, sam, child, _, link := acceptedAcrossOwners(t, h)
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	h.Clock.Advance(15*time.Minute + time.Second)
	_, err = h.Service.RestoreLink(ctx, removal.ID, sam.ID, removal.UndoToken)
	wantNoLongerAvailable(t, "RestoreLink after 15 minutes", err)
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after an expired Undo, want none", got)
	}

	h2 := testsupport.New(t)
	_, sam2, child2, _, link2 := acceptedAcrossOwners(t, h2)
	removal2, err := h2.Service.RemoveLink(ctx, link2.ID, sam2.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	h2.Clock.Advance(15 * time.Minute)
	if _, err := h2.Service.RestoreLink(ctx, removal2.ID, sam2.ID, removal2.UndoToken); err != nil {
		t.Errorf("RestoreLink at 15 minutes: %v", err)
	}
	if got := h2.ParentsOf(child2); len(got) != 1 {
		t.Errorf("ParentsOf(child) = %+v, want the restored link", got)
	}
}

// Sam removes a link; it is requested and accepted again, and Pat, the
// parent's Owner, removes it. Sam's first Undo, still within its 15 minutes,
// is refused: bringing the link back would override Pat's removal.
func TestALinkRemovalUndoIsRefusedOnceThePairHasMovedOn(t *testing.T) {
	h := testsupport.New(t)
	pat, sam, child, parent, link := acceptedAcrossOwners(t, h)
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	again := h.RequestLink(sam, child, parent, "")
	if _, err := h.Service.AcceptLink(ctx, again.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	if _, err := h.Service.RemoveLink(ctx, again.ID, pat.ID); err != nil {
		t.Fatalf("RemoveLink by Pat: %v", err)
	}

	if _, err := h.Service.RestoreLink(ctx, removal.ID, sam.ID, removal.UndoToken); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("replayed RestoreLink: err = %v, want ErrValidation", err)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after a replayed Undo, want none", got)
	}
}

// When the link was requested again since its removal and that request was
// rejected, the removal's Undo is refused too.
func TestALinkRemovalUndoIsRefusedOnceALaterRequestWasRejected(t *testing.T) {
	h := testsupport.New(t)
	pat, sam, child, parent, link := acceptedAcrossOwners(t, h)
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	h.Clock.Advance(time.Minute)
	again := h.RequestLink(sam, child, parent, "")
	if _, err := h.Service.RejectLink(ctx, again.ID, pat.ID); err != nil {
		t.Fatalf("RejectLink: %v", err)
	}

	if _, err := h.Service.RestoreLink(ctx, removal.ID, sam.ID, removal.UndoToken); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("RestoreLink after a later rejection: err = %v, want ErrValidation", err)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v, want none", got)
	}
}

// A refused Undo spends its token: once the refusal's reason is gone, the same
// token is no longer available.
func TestARefusedLinkRemovalUndoSpendsItsToken(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	a := h.CreateGoal(owner, "A", "why a")
	b := h.CreateGoal(owner, "B", "why b")
	link := h.RequestLink(owner, a, b, "") // A contributes to B
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, owner.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	back := h.RequestLink(owner, b, a, "") // now B contributes to A
	if _, err := h.Service.RestoreLink(ctx, removal.ID, owner.ID, removal.UndoToken); !errors.Is(err, domain.ErrCycle) {
		t.Fatalf("RestoreLink closing a cycle: err = %v, want ErrCycle", err)
	}
	if _, err := h.Service.RemoveLink(ctx, back.ID, owner.ID); err != nil {
		t.Fatalf("RemoveLink(B -> A): %v", err)
	}
	_, err = h.Service.RestoreLink(ctx, removal.ID, owner.ID, removal.UndoToken)
	wantNoLongerAvailable(t, "RestoreLink with a spent token", err)
	if got := h.ParentsOf(a); len(got) != 0 {
		t.Errorf("ParentsOf(A) = %+v, want none", got)
	}
}

// Rejecting a link request hands its rejecter a one-time token: the Undo
// succeeds with it once, is refused without it or with another person's, and
// is no longer available after 15 minutes.
func TestUndoingALinkRejectionTakesItsToken(t *testing.T) {
	ctx := context.Background()
	t.Run("token", func(t *testing.T) {
		h := testsupport.New(t)
		pat, sam, child, parent, link := pendingAcrossOwners(t, h)
		rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
		if err != nil {
			t.Fatalf("RejectLink: %v", err)
		}
		if rejection.UndoToken == "" {
			t.Fatal("RejectLink gave no Undo token")
		}
		removal, err := h.Service.RemoveLink(ctx, h.RequestLink(sam, child, h.CreateGoal(sam, "Other", "why"), "").ID, sam.ID)
		if err != nil {
			t.Fatalf("RemoveLink: %v", err)
		}
		for name, token := range map[string]string{"no token": "", "a removal's token": removal.UndoToken} {
			if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, token); !errors.Is(err, domain.ErrNotAuthorized) {
				t.Errorf("RestoreLinkRequest with %s: err = %v, want ErrNotAuthorized", name, err)
			}
		}
		if pending, _ := h.Service.PendingLinkRequests(ctx, pat.ID); len(pending) != 0 {
			t.Fatalf("pending = %+v after refused Undos, want none", pending)
		}
		restored, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken)
		if err != nil {
			t.Fatalf("RestoreLinkRequest with its token: %v", err)
		}
		if restored.Child.ID != child.ID || restored.Parent.ID != parent.ID {
			t.Errorf("restored = %+v, want %d -> %d", restored, child.ID, parent.ID)
		}
	})
	t.Run("expired", func(t *testing.T) {
		h := testsupport.New(t)
		pat, _, _, _, link := pendingAcrossOwners(t, h)
		rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
		if err != nil {
			t.Fatalf("RejectLink: %v", err)
		}
		h.Clock.Advance(16 * time.Minute)
		_, err = h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken)
		wantNoLongerAvailable(t, "RestoreLinkRequest after 15 minutes", err)
		if pending, _ := h.Service.PendingLinkRequests(ctx, pat.ID); len(pending) != 0 {
			t.Errorf("pending = %+v after an expired Undo, want none", pending)
		}
	})
	t.Run("refused once spends it", func(t *testing.T) {
		h := testsupport.New(t)
		pat, sam, child, parent, link := pendingAcrossOwners(t, h)
		rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
		if err != nil {
			t.Fatalf("RejectLink: %v", err)
		}
		// Pat links the other way round and Sam accepts, so the Undo would close
		// a cycle; once that link is gone, the spent token stays spent.
		back := h.RequestLink(pat, parent, child, "")
		if _, err := h.Service.AcceptLink(ctx, back.ID, sam.ID); err != nil {
			t.Fatalf("AcceptLink: %v", err)
		}
		if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken); !errors.Is(err, domain.ErrCycle) {
			t.Fatalf("RestoreLinkRequest closing a cycle: err = %v, want ErrCycle", err)
		}
		if _, err := h.Service.RemoveLink(ctx, back.ID, pat.ID); err != nil {
			t.Fatalf("RemoveLink: %v", err)
		}
		_, err = h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken)
		wantNoLongerAvailable(t, "RestoreLinkRequest with a spent token", err)
	})
}

// Rejecting a Handoff hands its rejecter a one-time token: the Undo succeeds
// with it, is refused without it or with one for another action, and is no
// longer available after 15 minutes or a second time.
func TestUndoingAHandoffRejectionTakesItsToken(t *testing.T) {
	ctx := context.Background()
	t.Run("token", func(t *testing.T) {
		h := testsupport.New(t)
		sam, pat, _, ho, token := rejectedHandoff(t, h)
		if token == "" {
			t.Fatal("RejectHandoff gave no Undo token")
		}
		other := startHandoff(t, h, h.CreateGoal(sam, "Migrate displays", "Displays fail.").ID, pat.ID, sam.ID)
		otherToken, err := h.Service.RejectHandoff(ctx, other.ID, pat.ID)
		if err != nil {
			t.Fatalf("RejectHandoff(other): %v", err)
		}
		for name, tok := range map[string]string{"no token": "", "another Handoff's token": otherToken} {
			if _, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID, tok); !errors.Is(err, domain.ErrNotAuthorized) {
				t.Errorf("RestoreHandoff with %s: err = %v, want ErrNotAuthorized", name, err)
			}
		}
		if pending, _ := h.Service.PendingHandoffs(ctx, pat.ID); len(pending) != 0 {
			t.Fatalf("pending = %+v after refused Undos, want none", pending)
		}
		if _, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID, token); err != nil {
			t.Fatalf("RestoreHandoff with its token: %v", err)
		}
		if pending, _ := h.Service.PendingHandoffs(ctx, pat.ID); len(pending) != 1 || pending[0].ID != ho.ID {
			t.Errorf("pending = %+v, want the restored Handoff", pending)
		}
	})
	t.Run("expired", func(t *testing.T) {
		h := testsupport.New(t)
		_, pat, _, ho, token := rejectedHandoff(t, h)
		h.Clock.Advance(16 * time.Minute)
		_, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID, token)
		wantNoLongerAvailable(t, "RestoreHandoff after 15 minutes", err)
		if pending, _ := h.Service.PendingHandoffs(ctx, pat.ID); len(pending) != 0 {
			t.Errorf("pending = %+v after an expired Undo, want none", pending)
		}
	})
	t.Run("used", func(t *testing.T) {
		h := testsupport.New(t)
		_, pat, _, ho, token := rejectedHandoff(t, h)
		if _, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID, token); err != nil {
			t.Fatalf("RestoreHandoff: %v", err)
		}
		if _, err := h.Service.RejectHandoff(ctx, ho.ID, pat.ID); err != nil {
			t.Fatalf("RejectHandoff again: %v", err)
		}
		_, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID, token)
		wantNoLongerAvailable(t, "RestoreHandoff with a used token", err)
	})
}

// A Handoff from Sam to Pat is rejected; the Goal then passes from Sam to Mel
// and back to Sam. Pat's Undo, still within its 15 minutes, is refused: the
// old Handoff must not come back pending for Pat to take the Goal.
func TestAHandoffRejectionUndoIsRefusedOnceTheGoalHasMovedOn(t *testing.T) {
	h := testsupport.New(t)
	sam, pat, goal, ho, token := rejectedHandoff(t, h)
	ctx := context.Background()
	mel := h.SignIn("mel@example.com")
	toMel := startHandoff(t, h, goal.ID, mel.ID, sam.ID)
	if _, err := h.Service.AcceptHandoff(ctx, toMel.ID, mel.ID, nil); err != nil {
		t.Fatalf("AcceptHandoff to Mel: %v", err)
	}
	back := startHandoff(t, h, goal.ID, sam.ID, mel.ID)
	if _, err := h.Service.AcceptHandoff(ctx, back.ID, sam.ID, nil); err != nil {
		t.Fatalf("AcceptHandoff back to Sam: %v", err)
	}

	if _, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID, token); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("revived RestoreHandoff: err = %v, want ErrValidation", err)
	}
	if pending, _ := h.Service.PendingHandoffs(ctx, pat.ID); len(pending) != 0 {
		t.Errorf("pending = %+v, want none after a refused Undo", pending)
	}
}

// A Handoff rejected before Undo tokens existed has none, so it can't be
// undone.
func TestAHandoffRejectedBeforeUndoTokensCantBeUndone(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)
	// Rejected as it was before, leaving nothing but its status.
	if _, err := h.DB.Exec(`UPDATE handoffs SET status = 'rejected' WHERE id = ?`, ho.ID); err != nil {
		t.Fatalf("reject as before: %v", err)
	}
	ctx := context.Background()

	if _, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID, ""); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("RestoreHandoff of an old rejection: err = %v, want ErrNotAuthorized", err)
	}
	if pending, _ := h.Service.PendingHandoffs(ctx, pat.ID); len(pending) != 0 {
		t.Errorf("pending = %+v, want none", pending)
	}
}

// retiredValue is a Dimension with values Growth and Trust, Trust retired by
// the Admin, and the token for the Admin's Undo.
func retiredValue(t *testing.T, h *testsupport.Harness) (boss domain.Account, dim domain.Dimension, trust domain.DimensionValue, token string) {
	t.Helper()
	boss = h.SignIn("boss@example.com")
	dim = h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	trust = dim.Values[1]
	token, err := h.Service.RetireDimensionValueWithUndo(context.Background(), boss.ID, trust.ID)
	if err != nil {
		t.Fatalf("RetireDimensionValueWithUndo: %v", err)
	}
	if token == "" {
		t.Fatal("RetireDimensionValueWithUndo gave no Undo token")
	}
	return boss, dim, trust, token
}

// valueRetired says whether the value valueID is Retired, failing the test
// when it doesn't exist.
func valueRetired(t *testing.T, h *testsupport.Harness, valueID int64) bool {
	t.Helper()
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		for _, v := range d.Values {
			if v.ID == valueID {
				return v.Retired
			}
		}
	}
	t.Fatalf("value %d doesn't exist", valueID)
	return false
}

// Retiring a value hands the Admin a one-time token: the Undo restores the
// value with it, and is refused without it, with another Admin's, or for
// another value.
func TestUndoingAValueRetirementTakesItsToken(t *testing.T) {
	h := testsupport.New(t, "boss@example.com", "ada@example.com")
	ctx := context.Background()
	boss, dim, trust, token := retiredValue(t, h)
	ada := h.SignIn("ada@example.com")
	growth := dim.Values[0]

	if err := h.Service.UndoRetireDimensionValue(ctx, boss.ID, trust.ID, ""); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Undo with no token: err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.UndoRetireDimensionValue(ctx, ada.ID, trust.ID, token); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Undo by another Admin: err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.UndoRetireDimensionValue(ctx, boss.ID, growth.ID, token); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Undo of another value: err = %v, want ErrNotAuthorized", err)
	}
	if !valueRetired(t, h, trust.ID) {
		t.Fatal("Trust restored by a refused Undo")
	}

	if err := h.Service.UndoRetireDimensionValue(ctx, boss.ID, trust.ID, token); err != nil {
		t.Fatalf("Undo with its token: %v", err)
	}
	if valueRetired(t, h, trust.ID) {
		t.Error("Trust still Retired after the Undo")
	}
}

// A value's Undo lasts 15 minutes and works once.
func TestAValueRetirementUndoIsOneTimeAndExpires(t *testing.T) {
	ctx := context.Background()
	t.Run("expired", func(t *testing.T) {
		h := testsupport.New(t, "boss@example.com")
		boss, _, trust, token := retiredValue(t, h)
		h.Clock.Advance(16 * time.Minute)
		wantNoLongerAvailable(t, "Undo after 15 minutes", h.Service.UndoRetireDimensionValue(ctx, boss.ID, trust.ID, token))
		if !valueRetired(t, h, trust.ID) {
			t.Error("Trust restored by an expired Undo")
		}
	})
	t.Run("used", func(t *testing.T) {
		h := testsupport.New(t, "boss@example.com")
		boss, _, trust, token := retiredValue(t, h)
		if err := h.Service.UndoRetireDimensionValue(ctx, boss.ID, trust.ID, token); err != nil {
			t.Fatalf("first Undo: %v", err)
		}
		if err := h.Service.RetireDimensionValue(ctx, boss.ID, trust.ID); err != nil {
			t.Fatalf("RetireDimensionValue: %v", err)
		}
		wantNoLongerAvailable(t, "second Undo", h.Service.UndoRetireDimensionValue(ctx, boss.ID, trust.ID, token))
		if !valueRetired(t, h, trust.ID) {
			t.Error("Trust restored by a second Undo")
		}
	})
}

// A value's Undo is refused, changing nothing, unless the value still exists,
// is still Retired, and still has the name it was retired under.
func TestAValueRetirementUndoIsRefusedOnceTheValueHasChanged(t *testing.T) {
	ctx := context.Background()
	t.Run("renamed", func(t *testing.T) {
		h := testsupport.New(t, "boss@example.com")
		boss, _, trust, token := retiredValue(t, h)
		if _, err := h.Service.RenameDimensionValue(ctx, boss.ID, trust.ID, "Reliability"); err != nil {
			t.Fatalf("RenameDimensionValue: %v", err)
		}
		if err := h.Service.UndoRetireDimensionValue(ctx, boss.ID, trust.ID, token); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("Undo of a renamed value: err = %v, want ErrValidation", err)
		}
		if !valueRetired(t, h, trust.ID) {
			t.Error("a renamed value restored by its old Undo")
		}
	})
	t.Run("restored", func(t *testing.T) {
		h := testsupport.New(t, "boss@example.com")
		boss, _, trust, token := retiredValue(t, h)
		if err := h.Service.RestoreDimensionValue(ctx, boss.ID, trust.ID); err != nil {
			t.Fatalf("RestoreDimensionValue: %v", err)
		}
		if err := h.Service.UndoRetireDimensionValue(ctx, boss.ID, trust.ID, token); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("Undo of a value no longer Retired: err = %v, want ErrValidation", err)
		}
	})
	t.Run("merged away", func(t *testing.T) {
		h := testsupport.New(t, "boss@example.com")
		boss, dim, trust, token := retiredValue(t, h)
		if err := h.Service.MergeDimensionValue(ctx, boss.ID, trust.ID, dim.Values[0].ID); err != nil {
			t.Fatalf("MergeDimensionValue: %v", err)
		}
		if err := h.Service.UndoRetireDimensionValue(ctx, boss.ID, trust.ID, token); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("Undo of a merged value: err = %v, want ErrValidation", err)
		}
	})
}
