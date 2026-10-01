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

// startHandoff starts a Handoff of goalID to toID on behalf of actorID, failing
// the test on error.
func startHandoff(t *testing.T, h *testsupport.Harness, goalID, toID, actorID int64) domain.Handoff {
	t.Helper()
	ho, err := h.Service.StartHandoff(context.Background(), domain.StartHandoffInput{
		GoalID: goalID, ToOwnerID: toID, ActorID: actorID,
	})
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	return ho
}

// Rejecting a Handoff keeps it in the Goal's ownership history with the outcome
// rejected; the Owner is unchanged and a new Handoff may be started
// (CONTEXT.md: every Handoff is kept with its outcome).
func TestRejectedHandoffIsKeptInHistory(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	mel := h.SignIn("mel@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)
	if err := h.Service.RejectHandoff(context.Background(), ho.ID, pat.ID); err != nil {
		t.Fatalf("RejectHandoff: %v", err)
	}

	g, _ := h.Service.ViewGoal(context.Background(), goal.ID)
	if g.Owner.ID != sam.ID {
		t.Errorf("Owner after reject = %d, want %d", g.Owner.ID, sam.ID)
	}
	history, err := h.Service.OwnershipHistory(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("OwnershipHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history = %+v, want the one rejected Handoff", history)
	}
	got := history[0]
	if got.Status != domain.HandoffRejected || got.From.ID != sam.ID || got.To.ID != pat.ID || got.InitiatedBy.ID != sam.ID {
		t.Errorf("history[0] = %+v, want rejected Handoff from Sam to Pat started by Sam", got)
	}

	// Only a pending Handoff blocks another: Sam may now hand the Goal to Mel.
	startHandoff(t, h, goal.ID, mel.ID, sam.ID)
}

// Marking someone Departed cancels the pending Handoffs to them: each is kept
// with the outcome cancelled and can no longer be accepted.
func TestMarkDepartedCancelsHandoffsToThePerson(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)

	if err := h.Service.MarkDeparted(context.Background(), boss.ID, pat.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	history, err := h.Service.OwnershipHistory(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("OwnershipHistory: %v", err)
	}
	if len(history) != 1 || history[0].Status != domain.HandoffCancelled {
		t.Fatalf("history = %+v, want the one cancelled Handoff", history)
	}
	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("accept cancelled Handoff err = %v, want ErrNotFound", err)
	}
	if g, _ := h.Service.ViewGoal(context.Background(), goal.ID); g.Owner.ID != sam.ID {
		t.Errorf("Owner = %d, want %d", g.Owner.ID, sam.ID)
	}
}

// A Handoff the Owner started before leaving stays pending when they depart; the
// new Owner accepts it, which ends the Goal's Ownerless state (CONTEXT.md:
// Ownerless).
func TestHandoffFromDepartedOwnerStillAccepted(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)

	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	if g, _ := h.Service.ViewGoal(context.Background(), goal.ID); !g.Ownerless {
		t.Fatal("Goal not Ownerless after its Owner departed")
	}
	if pending, _ := h.Service.PendingHandoffs(context.Background(), pat.ID); len(pending) != 1 {
		t.Fatalf("pending for Pat = %+v, want the Handoff from Sam", pending)
	}

	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID); err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}
	g, _ := h.Service.ViewGoal(context.Background(), goal.ID)
	if g.Owner.ID != pat.ID || g.Ownerless {
		t.Errorf("after accept: Owner = %d, Ownerless = %v; want Pat and not Ownerless", g.Owner.ID, g.Ownerless)
	}
}

// An Admin Reassign is recorded in the Goal's ownership history after the
// Handoffs before it: who did it, from whom, to whom, and the outcome
// reassigned.
func TestReassignAppearsInOwnershipHistory(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	mel := h.SignIn("mel@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho := startHandoff(t, h, goal.ID, mel.ID, sam.ID)
	if err := h.Service.RejectHandoff(context.Background(), ho.ID, mel.ID); err != nil {
		t.Fatalf("RejectHandoff: %v", err)
	}
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	if _, err := h.Service.ReassignGoal(context.Background(), boss.ID, goal.ID, pat.ID); err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}

	history, err := h.Service.OwnershipHistory(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("OwnershipHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history = %+v, want the rejected Handoff then the Reassign", history)
	}
	if history[0].Status != domain.HandoffRejected {
		t.Errorf("history[0].Status = %q, want %q", history[0].Status, domain.HandoffRejected)
	}
	got := history[1]
	if got.Status != domain.HandoffReassigned || got.From.ID != sam.ID || got.To.ID != pat.ID || got.InitiatedBy.ID != boss.ID {
		t.Errorf("history[1] = %+v, want Reassign from Sam to Pat by the Admin", got)
	}
}
