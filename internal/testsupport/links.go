package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// RequestLink requests that child contribute to parent on behalf of requester,
// failing the test on error. It is a scenario builder for arranging links.
func (h *Harness) RequestLink(requester domain.Account, child, parent domain.Goal, note string) domain.Link {
	h.T.Helper()
	link, err := h.Service.RequestLink(context.Background(), domain.RequestLinkInput{
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

// ParentsOf returns the accepted parents of g, failing the test on error.
func (h *Harness) ParentsOf(g domain.Goal) []domain.Goal {
	h.T.Helper()
	parents, err := h.Service.ParentsOf(context.Background(), g.ID)
	if err != nil {
		h.T.Fatalf("ParentsOf: %v", err)
	}
	return parents
}

// ChildrenOf returns the accepted children of g, failing the test on error.
func (h *Harness) ChildrenOf(g domain.Goal) []domain.Goal {
	h.T.Helper()
	children, err := h.Service.ChildrenOf(context.Background(), g.ID)
	if err != nil {
		h.T.Fatalf("ChildrenOf: %v", err)
	}
	return children
}
