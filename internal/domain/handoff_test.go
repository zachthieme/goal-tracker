package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Owner starts a Handoff; it stays Pending and ownership does not move until
// the new Owner accepts it (CONTEXT.md: Handoff takes effect only when the new
// Owner accepts).
func TestHandoffTakesEffectOnlyWhenNewOwnerAccepts(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com") // current Owner
	pat := h.SignIn("pat@example.com") // proposed new Owner
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	handoff, err := h.Service.StartHandoff(context.Background(), domain.StartHandoffInput{
		GoalID:    goal.ID,
		ToOwnerID: pat.ID,
		ActorID:   sam.ID,
	})
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	if handoff.Status != domain.HandoffPending {
		t.Fatalf("Status = %q, want %q", handoff.Status, domain.HandoffPending)
	}

	// Ownership has not moved yet.
	before, _ := h.Service.ViewGoal(context.Background(), goal.ID)
	if before.Owner.ID != sam.ID {
		t.Fatalf("Owner moved before acceptance: got %d, want %d", before.Owner.ID, sam.ID)
	}

	// The proposed new Owner sees the pending Handoff and accepts it.
	pending, err := h.Service.PendingHandoffs(context.Background(), pat.ID)
	if err != nil {
		t.Fatalf("PendingHandoffs: %v", err)
	}
	if len(pending) != 1 || pending[0].Goal.ID != goal.ID {
		t.Fatalf("PendingHandoffs = %+v, want the one Handoff for the Goal", pending)
	}

	accepted, err := h.Service.AcceptHandoff(context.Background(), handoff.ID, pat.ID)
	if err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}
	if accepted.Status != domain.HandoffAccepted {
		t.Errorf("Status = %q, want %q", accepted.Status, domain.HandoffAccepted)
	}

	// Ownership has now moved to Pat, and nothing is pending.
	after, _ := h.Service.ViewGoal(context.Background(), goal.ID)
	if after.Owner.ID != pat.ID {
		t.Errorf("Owner after accept = %d, want %d", after.Owner.ID, pat.ID)
	}
	if got, _ := h.Service.PendingHandoffs(context.Background(), pat.ID); len(got) != 0 {
		t.Errorf("pending after accept = %d, want 0", len(got))
	}
}

// Only the proposed new Owner may accept a Handoff.
func TestAcceptHandoffOnlyByNewOwner(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	mel := h.SignIn("mel@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	handoff, err := h.Service.StartHandoff(context.Background(), domain.StartHandoffInput{
		GoalID:    goal.ID,
		ToOwnerID: pat.ID,
		ActorID:   sam.ID,
	})
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	if _, err := h.Service.AcceptHandoff(context.Background(), handoff.ID, mel.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("err = %v, want ErrNotAuthorized", err)
	}
}

// An Admin may start a Handoff for a Goal they do not own; a non-Owner non-Admin
// may not (CONTEXT.md: an Owner or an Admin starts a Handoff).
func TestStartHandoffAuthorization(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com") // Admin
	sam := h.SignIn("sam@example.com")   // Owner
	pat := h.SignIn("pat@example.com")   // proposed new Owner
	mel := h.SignIn("mel@example.com")   // uninvolved
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.StartHandoff(context.Background(), domain.StartHandoffInput{
		GoalID: goal.ID, ToOwnerID: pat.ID, ActorID: boss.ID,
	}); err != nil {
		t.Errorf("Admin StartHandoff: %v, want success", err)
	}

	other := h.CreateGoal(sam, "Ship faster", "Slow ships lose deals.")
	if _, err := h.Service.StartHandoff(context.Background(), domain.StartHandoffInput{
		GoalID: other.ID, ToOwnerID: pat.ID, ActorID: mel.ID,
	}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("stranger StartHandoff err = %v, want ErrNotAuthorized", err)
	}
}

// An Admin marks a person departed; the Goals they still own become Ownerless.
func TestMarkDepartedMakesGoalsOwnerless(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	if g, _ := h.Service.ViewGoal(context.Background(), goal.ID); g.Ownerless {
		t.Fatal("Goal Ownerless before its Owner departed")
	}
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	g, _ := h.Service.ViewGoal(context.Background(), goal.ID)
	if !g.Ownerless {
		t.Error("Goal not Ownerless after its Owner departed")
	}
}

// Only an Admin may mark a person departed.
func TestMarkDepartedOnlyByAdmin(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	if err := h.Service.MarkDeparted(context.Background(), sam.ID, pat.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("err = %v, want ErrNotAuthorized", err)
	}
}

// An Admin reassigns an Ownerless Goal to a present Owner, clearing Ownerless.
func TestAdminReassignsOwnerlessGoal(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	reassigned, err := h.Service.ReassignGoal(context.Background(), boss.ID, goal.ID, pat.ID)
	if err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}
	if reassigned.Owner.ID != pat.ID {
		t.Errorf("Owner = %d, want %d", reassigned.Owner.ID, pat.ID)
	}
	if reassigned.Ownerless {
		t.Error("Goal still Ownerless after reassignment to a present Owner")
	}
}

// Reassignment is an Admin power over Ownerless Goals: a non-Admin may not, and
// a Goal whose Owner is present is handed off, not reassigned.
func TestReassignGoalGuards(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	// Goal still owned by a present person: not an Ownerless reassignment.
	if _, err := h.Service.ReassignGoal(context.Background(), boss.ID, goal.ID, pat.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("reassign of an owned Goal err = %v, want ErrValidation", err)
	}

	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	// Now Ownerless, but a non-Admin still may not reassign it.
	if _, err := h.Service.ReassignGoal(context.Background(), pat.ID, goal.ID, pat.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin reassign err = %v, want ErrNotAuthorized", err)
	}
}
