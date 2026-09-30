package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// When the requester owns the parent too, the link is accepted immediately
// (CONTEXT.md: creating a child directly under a Goal you own).
func TestRequestLinkAutoAcceptsWhenRequesterOwnsParent(t *testing.T) {
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
	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	link := h.RequestLink(sam, child, parent, "")

	if err := h.Service.RejectLink(context.Background(), link.ID, pat.ID); err != nil {
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
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	parent := h.CreateGoal(owner, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(owner, "Migrate displays", "Old displays fail often.")
	link := h.RequestLink(owner, child, parent, "") // auto-accepted

	if err := h.Service.RemoveLink(context.Background(), link.ID, owner.ID); err != nil {
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
