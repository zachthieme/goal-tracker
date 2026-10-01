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
