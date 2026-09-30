package domain

import (
	"context"
	"fmt"
)

// Graph traversal over the "contributes to" edges lives behind the domain
// service (ADR-0004), so the storage of the graph can change without touching
// callers. The traversal walks the direct-edge queries breadth-first in Go
// rather than in SQL; it stays correct because accepted links never form a
// cycle (ADR-0001).

// Ancestors returns the Goals reachable by following accepted contributes-to
// edges up from goalID (the Goals it contributes to, then their parents, and so
// on), out to depth levels. depth 1 is the direct parents. A depth below 1
// yields nothing.
func (s *Service) Ancestors(ctx context.Context, goalID int64, depth int) ([]Goal, error) {
	return s.traverse(ctx, goalID, depth, s.ParentsOf)
}

// Descendants returns the Goals reachable by following accepted contributes-to
// edges down from goalID (the Goals that contribute to it, then their children,
// and so on), out to depth levels. depth 1 is the direct children.
func (s *Service) Descendants(ctx context.Context, goalID int64, depth int) ([]Goal, error) {
	return s.traverse(ctx, goalID, depth, s.ChildrenOf)
}

// traverse walks the graph breadth-first from goalID for depth levels, using
// next to expand one Goal into its neighbours in one direction. Each reachable
// Goal is returned once, at its shallowest depth, in breadth-first order.
func (s *Service) traverse(
	ctx context.Context,
	goalID int64,
	depth int,
	next func(context.Context, int64) ([]Goal, error),
) ([]Goal, error) {
	seen := map[int64]bool{goalID: true}
	var out []Goal
	frontier := []int64{goalID}
	for level := 0; level < depth && len(frontier) > 0; level++ {
		var nextFrontier []int64
		for _, id := range frontier {
			neighbours, err := next(ctx, id)
			if err != nil {
				return nil, err
			}
			for _, g := range neighbours {
				if seen[g.ID] {
					continue
				}
				seen[g.ID] = true
				out = append(out, g)
				nextFrontier = append(nextFrontier, g.ID)
			}
		}
		frontier = nextFrontier
	}
	return out, nil
}

// ensureNoCycle returns ErrCycle if accepting a link from childID to parentID
// would make the graph cyclic — that is, if parentID already contributes,
// directly or transitively, to childID (ADR-0001).
func (s *Service) ensureNoCycle(ctx context.Context, childID, parentID int64) error {
	reaches, err := s.contributesTo(ctx, parentID, childID)
	if err != nil {
		return err
	}
	if reaches {
		return fmt.Errorf("%w: %d already contributes to %d", ErrCycle, parentID, childID)
	}
	return nil
}

// contributesTo reports whether fromID contributes, directly or transitively, to
// toID through accepted links.
func (s *Service) contributesTo(ctx context.Context, fromID, toID int64) (bool, error) {
	seen := map[int64]bool{fromID: true}
	frontier := []int64{fromID}
	for len(frontier) > 0 {
		var nextFrontier []int64
		for _, id := range frontier {
			parents, err := s.ParentsOf(ctx, id)
			if err != nil {
				return false, err
			}
			for _, p := range parents {
				if p.ID == toID {
					return true, nil
				}
				if seen[p.ID] {
					continue
				}
				seen[p.ID] = true
				nextFrontier = append(nextFrontier, p.ID)
			}
		}
		frontier = nextFrontier
	}
	return false, nil
}
