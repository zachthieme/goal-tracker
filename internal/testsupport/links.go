package testsupport

import (
	"github.com/zachthieme/goal-tracker/internal/domain"
)

// RequestLink requests that child contribute to parent on behalf of requester,
// failing the test on error. It is a scenario builder for arranging links.
func (h *Harness) RequestLink(requester domain.Account, child, parent domain.Goal, note string) domain.Link {
	h.T.Helper()
	link, err := h.Service.RequestLink(h.T.Context(), domain.RequestLinkInput{
		ChildID:     child.ID,
		ParentID:    parent.ID,
		Note:        note,
		RequesterID: requester.ID,
	})
	if err != nil {
		h.T.Fatalf("RequestLink: %v", err)
	}
	return link
}

// ActiveChildOf creates an Active Goal owned by owner and links it as a child
// contributing to parent, failing the test on error. When owner also owns
// parent the link is accepted immediately, so the child is a real Active child
// for Rolled-up Health. It is a scenario builder for arranging a Goal graph.
func (h *Harness) ActiveChildOf(owner domain.Account, parent domain.Goal, title, whyItMatters string) domain.Goal {
	h.T.Helper()
	child := h.ActiveGoal(owner, title, whyItMatters)
	h.RequestLink(owner, child, parent, "")
	return child
}

// ParentsOf returns the accepted parents of g, failing the test on error.
func (h *Harness) ParentsOf(g domain.Goal) []domain.Goal {
	h.T.Helper()
	parents, err := h.Service.ParentsOf(h.T.Context(), g.ID)
	if err != nil {
		h.T.Fatalf("ParentsOf: %v", err)
	}
	return parents
}

// ChildrenOf returns the accepted children of g, failing the test on error.
func (h *Harness) ChildrenOf(g domain.Goal) []domain.Goal {
	h.T.Helper()
	children, err := h.Service.ChildrenOf(h.T.Context(), g.ID)
	if err != nil {
		h.T.Fatalf("ChildrenOf: %v", err)
	}
	return children
}
