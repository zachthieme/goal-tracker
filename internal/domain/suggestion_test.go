package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Anyone but a Goal's Owner may suggest a parent for it, with a note; the
// suggestion waits open for the Owner, on their Goal and among theirs to
// decide. The Owner can't suggest one: they request the link themselves.
func TestSuggestParentByANonOwnerWaitsOpenForTheOwner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com") // owns the Goal
	pat := h.SignIn("pat@example.com") // suggests a parent
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	parent := h.ActiveGoal(pat, "Reduce outages", "Outages cost trust.")

	s, err := h.Service.SuggestParent(ctx, pat.ID, goal.ID, parent.ID, "  displays cause outages  ")
	if err != nil {
		t.Fatalf("SuggestParent: %v", err)
	}
	if s.Status != domain.SuggestionOpen || s.Goal.ID != goal.ID || s.Parent.ID != parent.ID ||
		s.SuggestedBy.ID != pat.ID || s.Note != "displays cause outages" {
		t.Errorf("suggestion = %+v, want pat's open suggestion of the parent with the trimmed note", s)
	}

	open, err := h.Service.OpenParentSuggestions(ctx, goal.ID)
	if err != nil {
		t.Fatalf("OpenParentSuggestions: %v", err)
	}
	if len(open) != 1 || open[0].ID != s.ID {
		t.Errorf("open on the Goal = %+v, want just the suggestion", open)
	}
	mine, err := h.Service.OpenParentSuggestionsFor(ctx, sam.ID)
	if err != nil {
		t.Fatalf("OpenParentSuggestionsFor: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != s.ID {
		t.Errorf("open for the Owner = %+v, want just the suggestion", mine)
	}
	if theirs, _ := h.Service.OpenParentSuggestionsFor(ctx, pat.ID); len(theirs) != 0 {
		t.Errorf("the suggester has %d to decide, want 0", len(theirs))
	}
	// Suggesting isn't linking.
	if got := h.ParentsOf(goal); len(got) != 0 {
		t.Errorf("ParentsOf(goal) = %+v, want none", got)
	}

	if _, err := h.Service.SuggestParent(ctx, sam.ID, goal.ID, parent.ID, ""); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("the Owner suggesting: err = %v, want ErrNotAuthorized", err)
	}
}

// endGoal ends g in a Check-in by its Owner: Done with an outcome, or
// Cancelled with a reason.
func endGoal(t *testing.T, h *testsupport.Harness, owner domain.Account, g domain.Goal, lifecycle string) {
	t.Helper()
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          g.ID,
		AuthorID:        owner.ID,
		Status:          "Wrapping up.",
		Lifecycle:       lifecycle,
		LifecycleReason: "No longer needed.",
		Outcome:         "It shipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin %s: %v", lifecycle, err)
	}
}

// A Delegate on the Goal isn't its Owner, so may suggest a parent too.
func TestSuggestParentByADelegate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	parent := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.") // Proposed
	h.AddDelegate(sam, dee, goal.ID)

	if _, err := h.Service.SuggestParent(context.Background(), dee.ID, goal.ID, parent.ID, ""); err != nil {
		t.Fatalf("a Delegate suggesting: %v", err)
	}
}

// A suggestion is refused, recording nothing, when the parent is already
// linked or Pending for the Goal, already suggested and still open (naming who
// suggested it), the Goal itself, or would make a cycle; and when the Goal or
// the parent is anything but Active or Proposed.
func TestSuggestParentRefusals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com") // owns the Goal
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	kim := h.SignIn("kim@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")

	linked := h.ActiveGoal(sam, "Linked parent", "Already linked.")
	h.RequestLink(sam, goal, linked, "")
	pending := h.ActiveGoal(pat, "Pending parent", "Already requested.")
	h.RequestLink(sam, goal, pending, "")
	suggested := h.ActiveGoal(pat, "Suggested parent", "Already suggested.")
	if _, err := h.Service.SuggestParent(ctx, pat.ID, goal.ID, suggested.ID, ""); err != nil {
		t.Fatalf("SuggestParent: %v", err)
	}
	below := h.ActiveChildOf(sam, goal, "Below the Goal", "Contributes to it.")
	onHold := h.OnHoldGoal(pat, "On Hold parent", "Paused.", "Waiting on budget.")
	done := h.ActiveGoal(pat, "Done parent", "Finished.")
	endGoal(t, h, pat, done, domain.LifecycleDone)
	cancelled := h.ActiveGoal(pat, "Cancelled parent", "Dropped.")
	endGoal(t, h, pat, cancelled, domain.LifecycleCancelled)
	heldGoal := h.OnHoldGoal(sam, "Held Goal", "Paused.", "Waiting on budget.")
	open := h.ActiveGoal(pat, "Open parent", "Fine to suggest.")

	for _, c := range []struct {
		name     string
		goal     domain.Goal
		parent   domain.Goal
		want     error
		mentions string
	}{
		{"already linked", goal, linked, domain.ErrValidation, ""},
		{"already Pending", goal, pending, domain.ErrValidation, ""},
		{"already suggested", goal, suggested, domain.ErrValidation, "Pat Lee"},
		{"itself", goal, goal, domain.ErrValidation, ""},
		{"a cycle", goal, below, domain.ErrCycle, ""},
		{"an On Hold parent", goal, onHold, domain.ErrValidation, ""},
		{"a Done parent", goal, done, domain.ErrValidation, ""},
		{"a Cancelled parent", goal, cancelled, domain.ErrValidation, ""},
		{"an On Hold Goal", heldGoal, open, domain.ErrValidation, ""},
	} {
		_, err := h.Service.SuggestParent(ctx, kim.ID, c.goal.ID, c.parent.ID, "")
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
			continue
		}
		if c.mentions != "" && !strings.Contains(err.Error(), c.mentions) {
			t.Errorf("%s: err = %q, want it to name %s", c.name, err, c.mentions)
		}
	}
	all, err := h.Service.ParentSuggestions(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ParentSuggestions: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("the Goal has %d suggestions, want just the first", len(all))
	}
}

// Accepting a suggestion requests the link as if the Owner had, so it waits
// Pending for the parent's Owner; but when the suggester owns the parent, both
// people who must agree have, so the link is accepted. Only the Goal's Owner
// may accept, and only an open suggestion.
func TestAcceptParentSuggestion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com") // owns the Goal
	pat := h.SignIn("pat@example.com") // suggests
	kim := h.SignIn("kim@example.com") // owns the other parent
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	patsParent := h.ActiveGoal(pat, "Reduce outages", "Outages cost trust.")
	kimsParent := h.ActiveGoal(kim, "Cut costs", "Budgets are tight.")

	toKim, err := h.Service.SuggestParent(ctx, pat.ID, goal.ID, kimsParent.ID, "")
	if err != nil {
		t.Fatalf("SuggestParent: %v", err)
	}
	toPat, err := h.Service.SuggestParent(ctx, pat.ID, goal.ID, patsParent.ID, "")
	if err != nil {
		t.Fatalf("SuggestParent: %v", err)
	}

	if _, err := h.Service.AcceptParentSuggestion(ctx, pat.ID, toKim.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("the suggester accepting: err = %v, want ErrNotAuthorized", err)
	}

	link, err := h.Service.AcceptParentSuggestion(ctx, sam.ID, toKim.ID)
	if err != nil {
		t.Fatalf("AcceptParentSuggestion: %v", err)
	}
	if link.Status != domain.LinkPending || link.Parent.ID != kimsParent.ID {
		t.Errorf("link = %+v, want Pending on kim's parent", link)
	}
	if pending, _ := h.Service.PendingLinkRequests(ctx, kim.ID); len(pending) != 1 {
		t.Errorf("kim has %d requests to decide, want the link", len(pending))
	}

	link, err = h.Service.AcceptParentSuggestion(ctx, sam.ID, toPat.ID)
	if err != nil {
		t.Fatalf("AcceptParentSuggestion: %v", err)
	}
	if link.Status != domain.LinkAccepted {
		t.Errorf("Status = %q, want accepted when the suggester owns the parent", link.Status)
	}
	if got := h.ParentsOf(goal); len(got) != 1 || got[0].ID != patsParent.ID {
		t.Errorf("ParentsOf(goal) = %+v, want pat's parent", got)
	}

	for _, id := range []int64{toKim.ID, toPat.ID} {
		s, err := h.Service.ParentSuggestion(ctx, id)
		if err != nil {
			t.Fatalf("ParentSuggestion: %v", err)
		}
		if s.Status != domain.SuggestionAccepted || !s.ClosedAt.Equal(testsupport.Epoch) {
			t.Errorf("suggestion = %+v, want accepted now", s)
		}
	}
	if _, err := h.Service.AcceptParentSuggestion(ctx, sam.ID, toKim.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("accepting again: err = %v, want ErrValidation", err)
	}
	if open, _ := h.Service.OpenParentSuggestionsFor(ctx, sam.ID); len(open) != 0 {
		t.Errorf("sam has %d open suggestions, want 0", len(open))
	}
}
