package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// When the requester owns the parent too, the link is accepted immediately
// (CONTEXT.md: creating a child directly under a Goal you own).
func TestRequestLinkAutoAcceptsWhenRequesterOwnsParent(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	parent := h.CreateGoal(owner, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(owner, "Migrate displays", "Old displays fail often.")

	link, err := h.Service.RequestLink(context.Background(), domain.RequestLinkInput{
		ChildID:     child.ID,
		ParentID:    parent.ID,
		RequesterID: owner.ID,
	})
	if err != nil {
		t.Fatalf("RequestLink: %v", err)
	}
	if link.Status != domain.LinkAccepted {
		t.Errorf("Status = %q, want %q", link.Status, domain.LinkAccepted)
	}

	parents := h.ParentsOf(child)
	if len(parents) != 1 || parents[0].ID != parent.ID {
		t.Fatalf("ParentsOf(child) = %+v, want just the parent", parents)
	}
	children := h.ChildrenOf(parent)
	if len(children) != 1 || children[0].ID != child.ID {
		t.Fatalf("ChildrenOf(parent) = %+v, want just the child", children)
	}
}

// The requester must own the child Goal they are offering up.
func TestRequestLinkRequiresRequesterOwnsChild(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	parent := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")

	_, err := h.Service.RequestLink(context.Background(), domain.RequestLinkInput{
		ChildID:     child.ID,
		ParentID:    parent.ID,
		RequesterID: pat.ID,
	})
	if !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("err = %v, want ErrNotAuthorized", err)
	}
}

// When the parent is owned by someone else, the request waits Pending for that
// Owner, who sees it and can accept it into the graph.
func TestRequestLinkPendsThenParentOwnerAccepts(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com") // owns the parent
	sam := h.SignIn("sam@example.com") // owns the child
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")

	link, err := h.Service.RequestLink(context.Background(), domain.RequestLinkInput{
		ChildID:     child.ID,
		ParentID:    parent.ID,
		Note:        "displays cause a third of outages",
		RequesterID: sam.ID,
	})
	if err != nil {
		t.Fatalf("RequestLink: %v", err)
	}
	if link.Status != domain.LinkPending {
		t.Fatalf("Status = %q, want %q", link.Status, domain.LinkPending)
	}
	// Not yet part of the graph.
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v, want none before acceptance", got)
	}

	// The parent's Owner sees the pending request, with its note.
	pending, err := h.Service.PendingLinkRequests(context.Background(), pat.ID)
	if err != nil {
		t.Fatalf("PendingLinkRequests: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("len(pending) = %d, want 1", len(pending))
	}
	if pending[0].Child.ID != child.ID || pending[0].Parent.ID != parent.ID {
		t.Errorf("pending request = %+v, want child->parent", pending[0])
	}
	if pending[0].Note != "displays cause a third of outages" {
		t.Errorf("Note = %q, want the request note", pending[0].Note)
	}
	// The child's Owner is not the one who decides, so sees nothing to act on.
	if other, _ := h.Service.PendingLinkRequests(context.Background(), sam.ID); len(other) != 0 {
		t.Errorf("child Owner sees %d pending requests, want 0", len(other))
	}

	accepted, err := h.Service.AcceptLink(context.Background(), link.ID, pat.ID)
	if err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	if accepted.Status != domain.LinkAccepted {
		t.Errorf("Status = %q, want %q", accepted.Status, domain.LinkAccepted)
	}
	if got := h.ParentsOf(child); len(got) != 1 || got[0].ID != parent.ID {
		t.Errorf("ParentsOf(child) = %+v, want the parent after acceptance", got)
	}
	// Once accepted it is no longer pending.
	if got, _ := h.Service.PendingLinkRequests(context.Background(), pat.ID); len(got) != 0 {
		t.Errorf("pending after accept = %d, want 0", len(got))
	}
}

// Only the parent's Owner may accept a pending request.
func TestAcceptLinkRequiresParentOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	link := h.RequestLink(sam, child, parent, "")

	if _, err := h.Service.AcceptLink(context.Background(), link.ID, sam.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("err = %v, want ErrNotAuthorized", err)
	}
}

// The parent's Owner can reject a request, which removes it entirely.
func TestRejectLinkRemovesTheRequest(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	link := h.RequestLink(sam, child, parent, "")

	if _, err := h.Service.RejectLink(context.Background(), link.ID, pat.ID); err != nil {
		t.Fatalf("RejectLink: %v", err)
	}
	if got, _ := h.Service.PendingLinkRequests(context.Background(), pat.ID); len(got) != 0 {
		t.Errorf("pending after reject = %d, want 0", len(got))
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v, want none after reject", got)
	}
}

// An accepted link can be removed, and then the Goals are no longer linked.
func TestRemoveLinkUnlinksTheGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	parent := h.CreateGoal(owner, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(owner, "Migrate displays", "Old displays fail often.")
	link := h.RequestLink(owner, child, parent, "") // auto-accepted

	if _, err := h.Service.RemoveLink(context.Background(), link.ID, owner.ID); err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v, want none after remove", got)
	}
	if got := h.ChildrenOf(parent); len(got) != 0 {
		t.Errorf("ChildrenOf(parent) = %+v, want none after remove", got)
	}
}

// A link that would immediately close a cycle is rejected at request time.
func TestRequestLinkRejectsAnImmediateCycle(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	a := h.CreateGoal(owner, "A", "why a")
	b := h.CreateGoal(owner, "B", "why b")
	h.RequestLink(owner, a, b, "") // A contributes to B, auto-accepted

	// B contributing to A would make A->B->A.
	_, err := h.Service.RequestLink(context.Background(), domain.RequestLinkInput{
		ChildID:     b.ID,
		ParentID:    a.ID,
		RequesterID: owner.ID,
	})
	if !errors.Is(err, domain.ErrCycle) {
		t.Errorf("err = %v, want ErrCycle", err)
	}
}

// A cycle that only appears after the request was made — because another link
// was accepted in between — is caught again at accept time (ADR-0001).
func TestAcceptLinkRejectsACycleThatAppearedSinceTheRequest(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	a1 := h.SignIn("a1@example.com")
	b1 := h.SignIn("b1@example.com")
	c1 := h.SignIn("c1@example.com")
	a := h.CreateGoal(a1, "A", "why a")
	b := h.CreateGoal(b1, "B", "why b")
	c := h.CreateGoal(c1, "C", "why c")

	// Accepted so far: A -> B (A contributes to B).
	ab := h.RequestLink(a1, a, b, "")
	if _, err := h.Service.AcceptLink(context.Background(), ab.ID, b1.ID); err != nil {
		t.Fatalf("accept A->B: %v", err)
	}

	// C requests to contribute to A. No cycle yet: A does not contribute to C.
	ca := h.RequestLink(c1, c, a, "")

	// In between, B -> C is requested and accepted, so now A -> B -> C, i.e. A
	// contributes to C.
	bc := h.RequestLink(b1, b, c, "")
	if _, err := h.Service.AcceptLink(context.Background(), bc.ID, c1.ID); err != nil {
		t.Fatalf("accept B->C: %v", err)
	}

	// Accepting C -> A now would close A -> B -> C -> A.
	if _, err := h.Service.AcceptLink(context.Background(), ca.ID, a1.ID); !errors.Is(err, domain.ErrCycle) {
		t.Errorf("err = %v, want ErrCycle at accept time", err)
	}
}

// A Goal may have many parents and many children.
func TestGoalCanHaveManyParentsAndChildren(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	mid := h.CreateGoal(owner, "Displays migration", "cuts cost and outages")
	costParent := h.CreateGoal(owner, "Cut cost", "why cost")
	outageParent := h.CreateGoal(owner, "Reduce outages", "why outages")
	childOne := h.CreateGoal(owner, "Phase one", "why one")
	childTwo := h.CreateGoal(owner, "Phase two", "why two")

	h.RequestLink(owner, mid, costParent, "")
	h.RequestLink(owner, mid, outageParent, "")
	h.RequestLink(owner, childOne, mid, "")
	h.RequestLink(owner, childTwo, mid, "")

	if got := h.ParentsOf(mid); len(got) != 2 {
		t.Errorf("ParentsOf(mid) = %d, want 2", len(got))
	}
	if got := h.ChildrenOf(mid); len(got) != 2 {
		t.Errorf("ChildrenOf(mid) = %d, want 2", len(got))
	}
}

// Ancestor and descendant traversal reaches through the graph out to a depth.
func TestAncestorsAndDescendantsToDepth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	// A -> B -> C -> D (each contributes to the next).
	a := h.CreateGoal(owner, "A", "why a")
	b := h.CreateGoal(owner, "B", "why b")
	c := h.CreateGoal(owner, "C", "why c")
	d := h.CreateGoal(owner, "D", "why d")
	h.RequestLink(owner, a, b, "")
	h.RequestLink(owner, b, c, "")
	h.RequestLink(owner, c, d, "")

	ancestors, err := h.Service.Ancestors(context.Background(), a.ID, 2)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if ids := idsOf(ancestors); !equalIDsUnordered(ids, []int64{b.ID, c.ID}) {
		t.Errorf("Ancestors(a, 2) = %v, want B and C", ids)
	}

	deep, err := h.Service.Ancestors(context.Background(), a.ID, 10)
	if err != nil {
		t.Fatalf("Ancestors deep: %v", err)
	}
	if ids := idsOf(deep); !equalIDsUnordered(ids, []int64{b.ID, c.ID, d.ID}) {
		t.Errorf("Ancestors(a, 10) = %v, want B, C and D", ids)
	}

	descendants, err := h.Service.Descendants(context.Background(), d.ID, 2)
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	if ids := idsOf(descendants); !equalIDsUnordered(ids, []int64{c.ID, b.ID}) {
		t.Errorf("Descendants(d, 2) = %v, want C and B", ids)
	}
}

func idsOf(goals []domain.Goal) []int64 {
	ids := make([]int64, len(goals))
	for i, g := range goals {
		ids[i] = g.ID
	}
	return ids
}

func equalIDsUnordered(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[int64]int{}
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		seen[id]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// A Goal cannot contribute to itself.
func TestRequestLinkRejectsSelfLink(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Reduce outages", "Outages cost trust.")

	_, err := h.Service.RequestLink(context.Background(), domain.RequestLinkInput{
		ChildID:     g.ID,
		ParentID:    g.ID,
		RequesterID: owner.ID,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

// acceptedAcrossOwners arranges an accepted link from Sam's child Goal to Pat's
// parent Goal, the case where undoing a removal must not ask Pat again.
func acceptedAcrossOwners(t *testing.T, h *testsupport.Harness) (pat, sam domain.Account, child, parent domain.Goal, link domain.Link) {
	t.Helper()
	pat = h.SignIn("pat@example.com")
	sam = h.SignIn("sam@example.com")
	parent = h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child = h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	link = h.RequestLink(sam, child, parent, "displays cause outages")
	if _, err := h.Service.AcceptLink(context.Background(), link.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	return pat, sam, child, parent, link
}

// Undoing a removal puts the link back as accepted between the same two Goals,
// without asking the parent's Owner to accept it again.
func TestRestoreLinkPutsTheRemovedLinkBackAsAccepted(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat, sam, child, parent, link := acceptedAcrossOwners(t, h)
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	restored, err := h.Service.RestoreLink(ctx, removal.ID, sam.ID, removal.UndoToken)
	if err != nil {
		t.Fatalf("RestoreLink: %v", err)
	}
	if restored.Status != domain.LinkAccepted || restored.Child.ID != child.ID || restored.Parent.ID != parent.ID {
		t.Errorf("restored = %+v, want accepted %d -> %d", restored, child.ID, parent.ID)
	}
	if restored.Note != "displays cause outages" {
		t.Errorf("restored note = %q, want the original note", restored.Note)
	}
	if got := idsOf(h.ParentsOf(child)); !equalIDsUnordered(got, []int64{parent.ID}) {
		t.Errorf("ParentsOf(child) = %v, want [%d]", got, parent.ID)
	}
	if got, _ := h.Service.PendingLinkRequests(ctx, pat.ID); len(got) != 0 {
		t.Errorf("pending for the parent's Owner = %d, want 0", len(got))
	}
}

// Only the person who removed a link may undo it: the other Goal's Owner may
// not, even though they could have removed it themselves.
func TestRestoreLinkRefusesAnyoneButTheRemover(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat, sam, child, _, link := acceptedAcrossOwners(t, h)
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	if _, err := h.Service.RestoreLink(ctx, removal.ID, pat.ID, removal.UndoToken); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("RestoreLink by the other Owner: err = %v, want ErrNotAuthorized", err)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v, want none after a refused Undo", got)
	}
}

// A removal is undone once; a second Undo is refused and adds nothing.
func TestRestoreLinkRefusesASecondUndo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	_, sam, child, _, link := acceptedAcrossOwners(t, h)
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	restored, err := h.Service.RestoreLink(ctx, removal.ID, sam.ID, removal.UndoToken)
	if err != nil {
		t.Fatalf("first RestoreLink: %v", err)
	}
	// Remove the restored link again by its own removal, then replay the first
	// Undo: it was spent, so it must not bring the link back.
	if _, err := h.Service.RemoveLink(ctx, restored.ID, sam.ID); err != nil {
		t.Fatalf("second RemoveLink: %v", err)
	}
	if _, err := h.Service.RestoreLink(ctx, removal.ID, sam.ID, removal.UndoToken); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("second RestoreLink: err = %v, want ErrValidation", err)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v, want none after a spent Undo", got)
	}
}

// Undo works only against a recorded removal: an id that names none is refused
// and creates no link.
func TestRestoreLinkRefusesARemovalThatNeverHappened(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")

	if _, err := h.Service.RestoreLink(context.Background(), 4242, sam.ID, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RestoreLink of no removal: err = %v, want ErrNotFound", err)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v, want none", got)
	}
}

// When the link has been made again since it was removed, Undo is refused
// rather than duplicating it.
func TestRestoreLinkRefusesWhenTheLinkExistsAgain(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	parent := h.CreateGoal(owner, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(owner, "Migrate displays", "Old displays fail often.")
	link := h.RequestLink(owner, child, parent, "")
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, owner.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	h.RequestLink(owner, child, parent, "") // linked again, auto-accepted
	if _, err := h.Service.RestoreLink(ctx, removal.ID, owner.ID, removal.UndoToken); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("RestoreLink of a link that exists again: err = %v, want ErrValidation", err)
	}
	if got := h.ParentsOf(child); len(got) != 1 {
		t.Errorf("ParentsOf(child) = %+v, want just the one link", got)
	}
}

// When restoring the link would now close a cycle, because another link was
// made since it was removed, Undo is refused and nothing changes (ADR-0001).
func TestRestoreLinkRefusesACycleThatAppearedSinceTheRemoval(t *testing.T) {
	t.Parallel()

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
	h.RequestLink(owner, b, a, "") // now B contributes to A
	if _, err := h.Service.RestoreLink(ctx, removal.ID, owner.ID, removal.UndoToken); !errors.Is(err, domain.ErrCycle) {
		t.Errorf("RestoreLink closing a cycle: err = %v, want ErrCycle", err)
	}
	if got := h.ParentsOf(a); len(got) != 0 {
		t.Errorf("ParentsOf(A) = %+v, want none after a refused Undo", got)
	}
}

// The remover must still own one of the Goals: after Handing off the Goal they
// owned, their Undo is refused.
func TestRestoreLinkRefusesARemoverWhoNoLongerOwnsEitherGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	_, sam, child, _, link := acceptedAcrossOwners(t, h)
	ctx := context.Background()

	removal, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	lee := h.SignIn("lee@example.com")
	ho, err := h.Service.StartHandoff(ctx, domain.StartHandoffInput{GoalID: child.ID, ToOwnerID: lee.ID, ActorID: sam.ID})
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	if _, err := h.Service.AcceptHandoff(ctx, ho.ID, lee.ID, nil); err != nil {
		t.Fatalf("AcceptHandoff: %v", err)
	}
	if _, err := h.Service.RestoreLink(ctx, removal.ID, sam.ID, removal.UndoToken); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("RestoreLink by a former Owner: err = %v, want ErrNotAuthorized", err)
	}
}

// pendingAcrossOwners is a pending request from Sam's child Goal to Pat's
// parent Goal, with a note.
func pendingAcrossOwners(t *testing.T, h *testsupport.Harness) (pat, sam domain.Account, child, parent domain.Goal, link domain.Link) {
	t.Helper()
	pat = h.SignIn("pat@example.com")
	sam = h.SignIn("sam@example.com")
	parent = h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child = h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	link = h.RequestLink(sam, child, parent, "displays cause outages")
	return pat, sam, child, parent, link
}

// Undoing a rejection puts the request back as pending between the same two
// Goals, with its original note and requester, for the parent's Owner to
// decide again.
func TestRestoreLinkRequestPutsTheRejectedRequestBackAsPending(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat, sam, child, parent, link := pendingAcrossOwners(t, h)
	ctx := context.Background()

	rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
	if err != nil {
		t.Fatalf("RejectLink: %v", err)
	}
	restored, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken)
	if err != nil {
		t.Fatalf("RestoreLinkRequest: %v", err)
	}
	if restored.Status != domain.LinkPending || restored.Child.ID != child.ID || restored.Parent.ID != parent.ID {
		t.Errorf("restored = %+v, want pending %d -> %d", restored, child.ID, parent.ID)
	}
	pending, _ := h.Service.PendingLinkRequests(ctx, pat.ID)
	if len(pending) != 1 || pending[0].Note != "displays cause outages" || pending[0].Child.Owner.ID != sam.ID {
		t.Errorf("pending = %+v, want Sam's request with its original note", pending)
	}
	if !pending[0].CreatedAt.Equal(link.CreatedAt) {
		t.Errorf("pending made at %v, want the original %v", pending[0].CreatedAt, link.CreatedAt)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v, want none: the request is only pending", got)
	}
}

// Only the person who rejected a request may undo it: the requester may not.
func TestRestoreLinkRequestRefusesAnyoneButTheRejecter(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat, sam, _, _, link := pendingAcrossOwners(t, h)
	ctx := context.Background()

	rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
	if err != nil {
		t.Fatalf("RejectLink: %v", err)
	}
	if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, sam.ID, rejection.UndoToken); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("RestoreLinkRequest by the requester: err = %v, want ErrNotAuthorized", err)
	}
	if got, _ := h.Service.PendingLinkRequests(ctx, pat.ID); len(got) != 0 {
		t.Errorf("pending = %+v, want none after a refused Undo", got)
	}
}

// A rejection is undone once: after rejecting the restored request again, the
// first Undo is spent and brings nothing back.
func TestRestoreLinkRequestRefusesASecondUndo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat, _, _, _, link := pendingAcrossOwners(t, h)
	ctx := context.Background()

	rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
	if err != nil {
		t.Fatalf("RejectLink: %v", err)
	}
	if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken); err != nil {
		t.Fatalf("first RestoreLinkRequest: %v", err)
	}
	if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("second RestoreLinkRequest while pending: err = %v, want ErrValidation", err)
	}
	pending, _ := h.Service.PendingLinkRequests(ctx, pat.ID)
	if len(pending) != 1 {
		t.Fatalf("pending = %+v, want just the one restored request", pending)
	}
	if _, err := h.Service.RejectLink(ctx, pending[0].ID, pat.ID); err != nil {
		t.Fatalf("second RejectLink: %v", err)
	}
	if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("replayed RestoreLinkRequest: err = %v, want ErrValidation", err)
	}
	if got, _ := h.Service.PendingLinkRequests(ctx, pat.ID); len(got) != 0 {
		t.Errorf("pending = %+v, want none after a spent Undo", got)
	}
}

// Undo works only against a recorded rejection: an id that names none is
// refused and creates no request.
func TestRestoreLinkRequestRefusesARejectionThatNeverHappened(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat, _, _, _, _ := pendingAcrossOwners(t, h)
	ctx := context.Background()

	if _, err := h.Service.RestoreLinkRequest(ctx, 4242, pat.ID, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RestoreLinkRequest of no rejection: err = %v, want ErrNotFound", err)
	}
	if got, _ := h.Service.PendingLinkRequests(ctx, pat.ID); len(got) != 1 {
		t.Errorf("pending = %+v, want just the untouched request", got)
	}
}

// When the same link has been requested or accepted again since the rejection,
// Undo is refused and changes nothing: whether the new request is still
// pending, was accepted, was rejected in turn, or was accepted and removed.
func TestRestoreLinkRequestRefusesWhenTheLinkWasRequestedAgain(t *testing.T) {
	t.Parallel()

	cases := map[string]func(t *testing.T, h *testsupport.Harness, pat, sam domain.Account, child, parent domain.Goal){
		"pending again": func(t *testing.T, h *testsupport.Harness, pat, sam domain.Account, child, parent domain.Goal) {
			h.RequestLink(sam, child, parent, "please")
		},
		"accepted again": func(t *testing.T, h *testsupport.Harness, pat, sam domain.Account, child, parent domain.Goal) {
			again := h.RequestLink(sam, child, parent, "please")
			if _, err := h.Service.AcceptLink(context.Background(), again.ID, pat.ID); err != nil {
				t.Fatalf("AcceptLink: %v", err)
			}
		},
		"rejected again": func(t *testing.T, h *testsupport.Harness, pat, sam domain.Account, child, parent domain.Goal) {
			again := h.RequestLink(sam, child, parent, "please")
			if _, err := h.Service.RejectLink(context.Background(), again.ID, pat.ID); err != nil {
				t.Fatalf("RejectLink: %v", err)
			}
		},
		"accepted then removed": func(t *testing.T, h *testsupport.Harness, pat, sam domain.Account, child, parent domain.Goal) {
			again := h.RequestLink(sam, child, parent, "please")
			if _, err := h.Service.AcceptLink(context.Background(), again.ID, pat.ID); err != nil {
				t.Fatalf("AcceptLink: %v", err)
			}
			if _, err := h.Service.RemoveLink(context.Background(), again.ID, sam.ID); err != nil {
				t.Fatalf("RemoveLink: %v", err)
			}
		},
	}
	for name, since := range cases {
		t.Run(name, func(t *testing.T) {
			h := testsupport.New(t)
			pat, sam, child, parent, link := pendingAcrossOwners(t, h)
			ctx := context.Background()

			rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
			if err != nil {
				t.Fatalf("RejectLink: %v", err)
			}
			since(t, h, pat, sam, child, parent)
			pendingBefore, _ := h.Service.PendingLinkRequests(ctx, pat.ID)
			parentsBefore := h.ParentsOf(child)

			if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("RestoreLinkRequest: err = %v, want ErrValidation", err)
			}
			if got, _ := h.Service.PendingLinkRequests(ctx, pat.ID); len(got) != len(pendingBefore) {
				t.Errorf("pending = %+v, want unchanged %+v", got, pendingBefore)
			}
			if got := h.ParentsOf(child); len(got) != len(parentsBefore) {
				t.Errorf("ParentsOf(child) = %+v, want unchanged %+v", got, parentsBefore)
			}
		})
	}
}

// A link removed before the request was made and rejected doesn't block the
// Undo: it isn't a request since.
func TestRestoreLinkRequestIgnoresARemovalBeforeTheRequest(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat, sam, child, parent, link := acceptedAcrossOwners(t, h)
	ctx := context.Background()

	if _, err := h.Service.RemoveLink(ctx, link.ID, sam.ID); err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	h.Clock.Advance(time.Hour)
	again := h.RequestLink(sam, child, parent, "once more")
	rejection, err := h.Service.RejectLink(ctx, again.ID, pat.ID)
	if err != nil {
		t.Fatalf("RejectLink: %v", err)
	}
	if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken); err != nil {
		t.Errorf("RestoreLinkRequest: %v", err)
	}
}

// When the request would now close a cycle, because the parent was linked
// under the child since, Undo is refused and nothing changes (ADR-0001).
func TestRestoreLinkRequestRefusesACycleThatAppearedSinceTheRejection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat, sam, child, parent, link := pendingAcrossOwners(t, h)
	ctx := context.Background()

	rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
	if err != nil {
		t.Fatalf("RejectLink: %v", err)
	}
	back := h.RequestLink(pat, parent, child, "") // now the parent contributes to the child
	if _, err := h.Service.AcceptLink(ctx, back.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken); !errors.Is(err, domain.ErrCycle) {
		t.Errorf("RestoreLinkRequest closing a cycle: err = %v, want ErrCycle", err)
	}
	if got, _ := h.Service.PendingLinkRequests(ctx, pat.ID); len(got) != 0 {
		t.Errorf("pending = %+v, want none after a refused Undo", got)
	}
}

// A Goal's Pending parent requests are listed apart from its Accepted parents,
// each with its link, so the New goal form can show both and request neither
// again.
func TestPendingParentLinksListsAGoalsPendingRequests(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	g := h.CreateGoal(sam, "Ship v2", "Customers wait too long for v2.")
	own := h.CreateGoal(sam, "Win enterprise", "Enterprise deals stall.")
	theirs := h.CreateGoal(kim, "Grow revenue", "Revenue is flat.")
	h.RequestLink(sam, g, own, "")
	link := h.RequestLink(sam, g, theirs, "")
	other := h.CreateGoal(sam, "Fix search", "Search is slow.")
	h.RequestLink(sam, other, theirs, "")

	pending, err := h.Service.PendingParentLinks(ctx, g.ID)
	if err != nil {
		t.Fatalf("PendingParentLinks: %v", err)
	}

	if len(pending) != 1 || pending[0].LinkID != link.ID || pending[0].Goal.ID != theirs.ID || pending[0].Goal.Owner.ID != kim.ID {
		t.Errorf("pending parents = %+v, want only %s, owned by Kim", pending, theirs.Title)
	}
}
