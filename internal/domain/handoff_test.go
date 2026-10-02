package domain_test

import (
	"context"
	"errors"
	"slices"
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

	accepted, err := h.Service.AcceptHandoff(context.Background(), handoff.ID, pat.ID, nil)
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
	if _, err := h.Service.AcceptHandoff(context.Background(), handoff.ID, mel.ID, nil); !errors.Is(err, domain.ErrNotAuthorized) {
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
	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID, nil); !errors.Is(err, domain.ErrNotFound) {
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

	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID, nil); err != nil {
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

// delegateIDs returns the IDs of the Goal's Delegates, failing the test on error.
func delegateIDs(t *testing.T, h *testsupport.Harness, goalID int64) []int64 {
	t.Helper()
	ds, err := h.Service.ListDelegates(context.Background(), goalID)
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	ids := make([]int64, 0, len(ds))
	for _, d := range ds {
		ids = append(ids, d.ID)
	}
	return ids
}

// Accepting a Handoff keeps the Delegates the new Owner chose and removes the
// rest, while the Goal moves to the new Owner (CONTEXT.md: Delegate — the new
// Owner chooses which Delegates to keep).
func TestAcceptHandoffRemovesTheDelegatesNotKept(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com") // current Owner
	pat := h.SignIn("pat@example.com") // new Owner
	ann := h.SignIn("ann@example.com") // Delegate Pat keeps
	bob := h.SignIn("bob@example.com") // Delegate Pat drops
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	h.AddDelegate(sam, bob, goal.ID)
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)

	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID, []int64{ann.ID}); err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}

	if got := delegateIDs(t, h, goal.ID); !slices.Equal(got, []int64{ann.ID}) {
		t.Errorf("Delegates after accept = %v, want only Ann (%d)", got, ann.ID)
	}
	if g, _ := h.Service.ViewGoal(context.Background(), goal.ID); g.Owner.ID != pat.ID {
		t.Errorf("Owner after accept = %d, want Pat (%d)", g.Owner.ID, pat.ID)
	}
}

// Accepting with every Delegate kept leaves them all on the Goal.
func TestAcceptHandoffKeepingEveryDelegate(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	bob := h.SignIn("bob@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	h.AddDelegate(sam, bob, goal.ID)
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)

	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID, []int64{ann.ID, bob.ID}); err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}

	if got := delegateIDs(t, h, goal.ID); !slices.Equal(got, []int64{ann.ID, bob.ID}) {
		t.Errorf("Delegates after accept = %v, want Ann and Bob (%d, %d)", got, ann.ID, bob.ID)
	}
}

// A Departed Delegate isn't offered to the new Owner, so accepting leaves them on
// the Goal, still Departed, whatever the new Owner kept.
func TestAcceptHandoffLeavesDepartedDelegates(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com") // Departed Delegate
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, ann.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)

	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID, nil); err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}

	if got := delegateIDs(t, h, goal.ID); !slices.Equal(got, []int64{ann.ID}) {
		t.Errorf("Delegates after accept = %v, want the Departed Ann (%d) still there", got, ann.ID)
	}
}

// The Owner already writes a Goal's Check-ins, so a Delegate who accepts a
// Handoff of that Goal stops being its Delegate on becoming its Owner.
func TestAcceptHandoffByADelegateEndsTheirDelegation(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com") // Delegate, then new Owner
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, pat, goal.ID)
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)

	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID, []int64{pat.ID}); err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}

	if got := delegateIDs(t, h, goal.ID); len(got) != 0 {
		t.Errorf("Delegates after accept = %v, want none: Pat is now the Owner", got)
	}
}

// Accepting is all-or-nothing: if recording the outcome fails, the Owner, the
// Delegates and the Handoff all stay as they were.
func TestAcceptHandoffChangesNothingWhenAStepFails(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)
	// Fail the last step, after ownership has moved and Ann has been removed.
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_accept BEFORE UPDATE OF status ON handoffs
		WHEN NEW.status = 'accepted' BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}

	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID, nil); err == nil {
		t.Fatal("AcceptHandoff succeeded despite the failing step")
	}

	if g, _ := h.Service.ViewGoal(context.Background(), goal.ID); g.Owner.ID != sam.ID {
		t.Errorf("Owner after a failed accept = %d, want Sam (%d)", g.Owner.ID, sam.ID)
	}
	if got := delegateIDs(t, h, goal.ID); !slices.Equal(got, []int64{ann.ID}) {
		t.Errorf("Delegates after a failed accept = %v, want Ann (%d) still there", got, ann.ID)
	}
	if pending, _ := h.Service.PendingHandoffs(context.Background(), pat.ID); len(pending) != 1 || pending[0].ID != ho.ID {
		t.Errorf("pending after a failed accept = %+v, want the Handoff still pending", pending)
	}
}

// Rejecting a Handoff leaves the Goal's Delegates alone.
func TestRejectHandoffLeavesDelegates(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)

	if err := h.Service.RejectHandoff(context.Background(), ho.ID, pat.ID); err != nil {
		t.Fatalf("RejectHandoff: %v", err)
	}

	if got := delegateIDs(t, h, goal.ID); !slices.Equal(got, []int64{ann.ID}) {
		t.Errorf("Delegates after reject = %v, want Ann (%d)", got, ann.ID)
	}
}

// An Admin Reassign of an Ownerless Goal keeps every Delegate, present or
// Departed.
func TestReassignKeepsEveryDelegate(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	bob := h.SignIn("bob@example.com") // Departed Delegate
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	h.AddDelegate(sam, bob, goal.ID)
	for _, gone := range []int64{sam.ID, bob.ID} {
		if err := h.Service.MarkDeparted(context.Background(), boss.ID, gone); err != nil {
			t.Fatalf("MarkDeparted: %v", err)
		}
	}

	if _, err := h.Service.ReassignGoal(context.Background(), boss.ID, goal.ID, pat.ID); err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}

	if got := delegateIDs(t, h, goal.ID); !slices.Equal(got, []int64{ann.ID, bob.ID}) {
		t.Errorf("Delegates after reassign = %v, want Ann and Bob (%d, %d)", got, ann.ID, bob.ID)
	}
}

// Each pending Handoff offers the new Owner the Goal's Delegates to keep: every
// Delegate who isn't Departed, other than the new Owner themselves.
func TestPendingHandoffOffersTheDelegatesToKeep(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com") // new Owner, also a Delegate
	ann := h.SignIn("ann@example.com") // offered
	bob := h.SignIn("bob@example.com") // Departed, not offered
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	h.AddDelegate(sam, bob, goal.ID)
	h.AddDelegate(sam, pat, goal.ID)
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, bob.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	startHandoff(t, h, goal.ID, pat.ID, sam.ID)

	pending, err := h.Service.PendingHandoffs(context.Background(), pat.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("PendingHandoffs = %+v, %v; want the one Handoff", pending, err)
	}
	offered := pending[0].KeepableDelegates
	if len(offered) != 1 || offered[0].ID != ann.ID {
		t.Errorf("KeepableDelegates = %+v, want only Ann (%d)", offered, ann.ID)
	}
}

// An Ownerless Goal is replaced by whichever comes first, the Admin's Reassign
// or the acceptance of the Handoff the Owner started before leaving: a Reassign
// cancels that Handoff, which keeps its place in the Ownership history, so its
// recipient can no longer take the Goal from the Owner the Admin chose, and the
// new Owner may start a Handoff of their own.
func TestReassignCancelsThePendingHandoff(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com") // departing Owner
	pat := h.SignIn("pat@example.com") // offered the Goal by Sam
	cal := h.SignIn("cal@example.com") // chosen by the Admin
	mel := h.SignIn("mel@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if _, err := h.Service.ReassignGoal(context.Background(), boss.ID, goal.ID, cal.ID); err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}

	history, err := h.Service.OwnershipHistory(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("OwnershipHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history = %+v, want Sam's Handoff then the Reassign", history)
	}
	if got := history[0]; got.ID != ho.ID || got.Status != domain.HandoffCancelled || got.From.ID != sam.ID || got.To.ID != pat.ID {
		t.Errorf("history[0] = %+v, want Sam's Handoff to Pat cancelled", got)
	}
	if got := history[1]; got.Status != domain.HandoffReassigned || got.From.ID != sam.ID || got.To.ID != cal.ID {
		t.Errorf("history[1] = %+v, want the Reassign from Sam to Cal", got)
	}
	if pending, _ := h.Service.PendingHandoffs(context.Background(), pat.ID); len(pending) != 0 {
		t.Errorf("pending for Pat after the Reassign = %+v, want none", pending)
	}
	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Pat accepting the old Handoff err = %v, want ErrNotFound", err)
	}
	if g, _ := h.Service.ViewGoal(context.Background(), goal.ID); g.Owner.ID != cal.ID {
		t.Errorf("Owner = %d, want Cal (%d)", g.Owner.ID, cal.ID)
	}

	startHandoff(t, h, goal.ID, mel.ID, cal.ID)
}

// Reassigning the Goal to the very person the pending Handoff was offered to
// still cancels that Handoff: they own the Goal by the Reassign, not by
// accepting.
func TestReassignToTheHandoffRecipientCancelsTheHandoff(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)
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
	if len(history) != 2 || history[0].ID != ho.ID || history[0].Status != domain.HandoffCancelled ||
		history[1].Status != domain.HandoffReassigned || history[1].To.ID != pat.ID {
		t.Fatalf("history = %+v, want Sam's Handoff cancelled then the Reassign to Pat", history)
	}
	if pending, _ := h.Service.PendingHandoffs(context.Background(), pat.ID); len(pending) != 0 {
		t.Errorf("pending for Pat after the Reassign = %+v, want none", pending)
	}
	if g, _ := h.Service.ViewGoal(context.Background(), goal.ID); g.Owner.ID != pat.ID || g.Ownerless {
		t.Errorf("Owner = %d, Ownerless = %v; want Pat (%d) and not Ownerless", g.Owner.ID, g.Ownerless, pat.ID)
	}
}

// A pending Handoff whose from-Owner no longer owns the Goal is stale: accepting
// it is refused and nothing changes. The Service can't reach this state (a
// Reassign cancels the pending Handoff), so the test moves the Goal directly in
// the database.
func TestAcceptStaleHandoffIsRefused(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	cal := h.SignIn("cal@example.com")
	ann := h.SignIn("ann@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	ho := startHandoff(t, h, goal.ID, pat.ID, sam.ID)
	if _, err := h.DB.Exec(`UPDATE goals SET owner_id = ? WHERE id = ?`, cal.ID, goal.ID); err != nil {
		t.Fatalf("move the Goal to Cal: %v", err)
	}

	if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, pat.ID, nil); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("accept stale Handoff err = %v, want ErrValidation", err)
	}

	if g, _ := h.Service.ViewGoal(context.Background(), goal.ID); g.Owner.ID != cal.ID {
		t.Errorf("Owner after a refused accept = %d, want Cal (%d)", g.Owner.ID, cal.ID)
	}
	if got := delegateIDs(t, h, goal.ID); !slices.Equal(got, []int64{ann.ID}) {
		t.Errorf("Delegates after a refused accept = %v, want Ann (%d) still there", got, ann.ID)
	}
	if pending, _ := h.Service.PendingHandoffs(context.Background(), pat.ID); len(pending) != 1 || pending[0].ID != ho.ID {
		t.Errorf("pending after a refused accept = %+v, want the Handoff still pending", pending)
	}
}

// rejectedHandoff is Sam's Goal with a Handoff to Pat that Pat rejected.
func rejectedHandoff(t *testing.T, h *testsupport.Harness) (sam, pat domain.Account, goal domain.Goal, ho domain.Handoff) {
	t.Helper()
	sam = h.SignIn("sam@example.com")
	pat = h.SignIn("pat@example.com")
	goal = h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho = startHandoff(t, h, goal.ID, pat.ID, sam.ID)
	if err := h.Service.RejectHandoff(context.Background(), ho.ID, pat.ID); err != nil {
		t.Fatalf("RejectHandoff: %v", err)
	}
	return sam, pat, goal, ho
}

// Undoing a rejection puts the Handoff back as pending with the same proposed
// Owner, and the Goal's ownership history shows it pending, not as a rejection
// followed by something else.
func TestRestoreHandoffPutsTheRejectedHandoffBackAsPending(t *testing.T) {
	h := testsupport.New(t)
	sam, pat, goal, ho := rejectedHandoff(t, h)
	ctx := context.Background()

	restored, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID)
	if err != nil {
		t.Fatalf("RestoreHandoff: %v", err)
	}
	if restored.ID != ho.ID || restored.Status != domain.HandoffPending || restored.To.ID != pat.ID {
		t.Errorf("restored = %+v, want Handoff %d pending to Pat", restored, ho.ID)
	}
	if pending, _ := h.Service.PendingHandoffs(ctx, pat.ID); len(pending) != 1 || pending[0].ID != ho.ID {
		t.Errorf("pending = %+v, want the restored Handoff", pending)
	}
	history, err := h.Service.OwnershipHistory(ctx, goal.ID)
	if err != nil {
		t.Fatalf("OwnershipHistory: %v", err)
	}
	if len(history) != 1 || history[0].Status != domain.HandoffPending || history[0].From.ID != sam.ID {
		t.Errorf("history = %+v, want just the one Handoff, pending", history)
	}
}

// Only the person who rejected a Handoff may undo it: the Owner who started it
// may not.
func TestRestoreHandoffRefusesAnyoneButTheRejecter(t *testing.T) {
	h := testsupport.New(t)
	sam, pat, _, ho := rejectedHandoff(t, h)
	ctx := context.Background()

	if _, err := h.Service.RestoreHandoff(ctx, ho.ID, sam.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("RestoreHandoff by the Owner: err = %v, want ErrNotAuthorized", err)
	}
	if pending, _ := h.Service.PendingHandoffs(ctx, pat.ID); len(pending) != 0 {
		t.Errorf("pending = %+v, want none after a refused Undo", pending)
	}
}

// A rejection is undone once: a second Undo is refused and changes nothing.
func TestRestoreHandoffRefusesASecondUndo(t *testing.T) {
	h := testsupport.New(t)
	_, pat, goal, ho := rejectedHandoff(t, h)
	ctx := context.Background()

	if _, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID); err != nil {
		t.Fatalf("first RestoreHandoff: %v", err)
	}
	if _, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("second RestoreHandoff: err = %v, want ErrValidation", err)
	}
	if history, _ := h.Service.OwnershipHistory(ctx, goal.ID); len(history) != 1 || history[0].Status != domain.HandoffPending {
		t.Errorf("history = %+v, want the one Handoff, pending", history)
	}
}

// Undo works only for a Handoff that was rejected: one still pending, one
// accepted, or an id that names none is refused and changes nothing.
func TestRestoreHandoffRefusesAHandoffThatWasNeverRejected(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	pending := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	accepted := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	stillPending := startHandoff(t, h, pending.ID, pat.ID, sam.ID)
	taken := startHandoff(t, h, accepted.ID, pat.ID, sam.ID)
	ctx := context.Background()
	if _, err := h.Service.AcceptHandoff(ctx, taken.ID, pat.ID, nil); err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}

	if _, err := h.Service.RestoreHandoff(ctx, stillPending.ID, pat.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("RestoreHandoff of a pending Handoff: err = %v, want ErrValidation", err)
	}
	if _, err := h.Service.RestoreHandoff(ctx, taken.ID, pat.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("RestoreHandoff of an accepted Handoff: err = %v, want ErrValidation", err)
	}
	if _, err := h.Service.RestoreHandoff(ctx, 4242, pat.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RestoreHandoff of no Handoff: err = %v, want ErrNotFound", err)
	}
	if history, _ := h.Service.OwnershipHistory(ctx, accepted.ID); len(history) != 1 || history[0].Status != domain.HandoffAccepted {
		t.Errorf("history = %+v, want the one accepted Handoff", history)
	}
	if g, _ := h.Service.ViewGoal(ctx, accepted.ID); g.Owner.ID != pat.ID {
		t.Errorf("Owner = %d, want Pat (%d) still", g.Owner.ID, pat.ID)
	}
}

// Undo is refused, changing nothing, when it no longer fits: the Goal has
// another pending Handoff, its Owner has changed, or the proposed Owner has
// since Departed.
func TestRestoreHandoffRefusesWhenItNoLongerFits(t *testing.T) {
	cases := map[string]func(t *testing.T, h *testsupport.Harness, sam, pat domain.Account, goal domain.Goal){
		"another pending": func(t *testing.T, h *testsupport.Harness, sam, pat domain.Account, goal domain.Goal) {
			mel := h.SignIn("mel@example.com")
			startHandoff(t, h, goal.ID, mel.ID, sam.ID)
		},
		"Owner changed": func(t *testing.T, h *testsupport.Harness, sam, pat domain.Account, goal domain.Goal) {
			mel := h.SignIn("mel@example.com")
			ho := startHandoff(t, h, goal.ID, mel.ID, sam.ID)
			if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, mel.ID, nil); err != nil {
				t.Fatalf("AcceptHandoff: %v", err)
			}
		},
		"proposed Owner Departed": func(t *testing.T, h *testsupport.Harness, sam, pat domain.Account, goal domain.Goal) {
			boss := h.SignIn("boss@example.com")
			if err := h.Service.MarkDeparted(context.Background(), boss.ID, pat.ID); err != nil {
				t.Fatalf("MarkDeparted: %v", err)
			}
		},
	}
	for name, since := range cases {
		t.Run(name, func(t *testing.T) {
			h := testsupport.New(t, "boss@example.com")
			sam, pat, goal, ho := rejectedHandoff(t, h)
			ctx := context.Background()
			since(t, h, sam, pat, goal)
			before, _ := h.Service.OwnershipHistory(ctx, goal.ID)
			owner, _ := h.Service.ViewGoal(ctx, goal.ID)

			if _, err := h.Service.RestoreHandoff(ctx, ho.ID, pat.ID); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("RestoreHandoff: err = %v, want ErrValidation", err)
			}
			after, _ := h.Service.OwnershipHistory(ctx, goal.ID)
			if !slices.EqualFunc(before, after, func(a, b domain.Handoff) bool { return a.ID == b.ID && a.Status == b.Status }) {
				t.Errorf("history = %+v, want unchanged %+v", after, before)
			}
			if g, _ := h.Service.ViewGoal(ctx, goal.ID); g.Owner.ID != owner.Owner.ID {
				t.Errorf("Owner = %d, want unchanged %d", g.Owner.ID, owner.Owner.ID)
			}
		})
	}
}
