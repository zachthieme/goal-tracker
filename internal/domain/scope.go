package domain

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
)

// GoalScope is the Scope of a page of Goals and the Dimension values it
// narrows them to. The Scope is Everyone, Mine (the Goals the viewer Owns or
// is a Delegate on), or an Org, by each Goal's Owner and never its Delegates
// (CONTEXT.md: Org). An Ownerless Goal's Departed Owner keeps their Manager,
// so the Goal stays in the Org its Owner was in. An Org adds no access, only
// a way to find Goals, so anyone may scope to anyone's (ADR 0002, ADR 0008).
// An address naming no scope opens on the viewer's default: My org for
// someone with anyone under them, Mine for everyone else. The Goal list, the
// goal table, Follow-ups and the sidebar's count of Follow-ups share it.
type GoalScope struct {
	// Of is the person whose Org the scope is, nil when it isn't an Org.
	Of *Account
	// in holds the Account IDs in Of's Org.
	in map[int64]bool
	// Mine is whether the scope is the Goals the viewer Owns or is a
	// Delegate on, delegated holding the latter's IDs. It is false for an
	// Org.
	Mine      bool
	delegated map[int64]bool
	// Default is whether the address named no scope, or one naming no one,
	// so the page is on the viewer's default scope.
	Default bool
	// Viewer is who is looking, so their own Org reads as My org.
	Viewer Account
	// OffersMine is whether the viewer has anyone under them, so My org is
	// offered; with no one, it would be Mine without the Delegate Goals.
	OffersMine bool
	// People are the others whose Org the page offers, by Label: every Owner
	// of a Goal and everyone above them, whose Orgs are the ones holding
	// Goals. It is empty when no one has a Manager other than themselves,
	// whoever is looking.
	People []Account
	// Values are the offered Dimensions' values the scope narrows the Goals
	// to, in the Dimensions' order and, within one, the address's: a Goal
	// is kept with any of a Dimension's, and must be for every Dimension
	// they are in. A value of a Retired Dimension is never one, as a page
	// has no control to undo it (CONTEXT.md: Retired); a Retired value of an
	// offered Dimension is.
	Values []DimensionValue
	// byDimension is Values' IDs by their Dimension's, for FilterGoals.
	byDimension map[int64][]int64
}

// GoalScopeRequest is the scope a page's address asks for: Everyone, Mine,
// or OrgOf's Org, or the viewer's default when it asks for none of them, and
// the IDs of the values it narrows to, in its order.
type GoalScopeRequest struct {
	Everyone, Mine bool
	// OrgOf is the Account whose Org is asked for, 0 for none. One naming
	// no one asks for the viewer's default, as does any when no one has a
	// Manager other than themselves: a page then offers no Org, so has no
	// control to undo it.
	OrgOf  int64
	Values []int64
}

// GoalScope resolves the scope req asks of viewer's page, over dims, every
// Dimension, and goals, the Goals whose Owners and everyone above them the
// page offers the Orgs of. It reads every Account once, and for Mine the
// Goals the viewer is a Delegate on.
func (s *Service) GoalScope(ctx context.Context, viewer Account, req GoalScopeRequest, dims []Dimension, goals []Goal) (GoalScope, error) {
	accounts, err := s.orgWalkAccounts(ctx)
	if err != nil {
		return GoalScope{}, err
	}
	sc := goalScopeAmong(viewer, req, dims, goals, accounts)
	if !sc.Mine {
		return sc, nil
	}
	delegated, err := s.DelegatedGoals(ctx, viewer.ID)
	if err != nil {
		return GoalScope{}, fmt.Errorf("load delegated goals: %w", err)
	}
	sc.delegated = make(map[int64]bool, len(delegated))
	for _, g := range delegated {
		sc.delegated[g.ID] = true
	}
	return sc, nil
}

// GoalScopeAmong is GoalScope over what a page loader has already read,
// reading nothing: accounts, every Account by ID, and delegated, the IDs of
// the Goals viewer is a Delegate on, for Mine. The Follow-ups page resolves
// its scope this way, over Service.FollowUps' reads (#592).
func GoalScopeAmong(viewer Account, req GoalScopeRequest, dims []Dimension, goals []Goal, accounts []Account, delegated map[int64]bool) GoalScope {
	sc := goalScopeAmong(viewer, req, dims, goals, accounts)
	if sc.Mine {
		sc.delegated = delegated
	}
	return sc
}

// goalScopeAmong is GoalScope over accounts, every Account by ID, reading
// nothing: Mine's Delegate Goals are left for the caller.
func goalScopeAmong(viewer Account, req GoalScopeRequest, dims []Dimension, goals []Goal, accounts []Account) GoalScope {
	sc := GoalScope{Viewer: viewer}
	byID := make(map[int64]Account, len(accounts))
	managed := false
	for _, a := range accounts {
		byID[a.ID] = a
		if a.ManagerID != nil && *a.ManagerID != a.ID {
			managed = true
			sc.OffersMine = sc.OffersMine || *a.ManagerID == viewer.ID
		}
	}
	sc.Values, sc.byDimension = goalScopeValues(req.Values, dims)
	if managed {
		sc.People = goalScopePeople(viewer.ID, goals, byID)
	}
	if of, ok := byID[req.OrgOf]; ok && managed {
		sc.scopeToOrg(of, accounts)
		return sc
	}
	switch {
	case req.Everyone:
		return sc
	case req.Mine:
	case sc.OffersMine:
		sc.Default = true
		of, ok := byID[viewer.ID]
		if !ok {
			of = viewer
		}
		sc.scopeToOrg(of, accounts)
		return sc
	default:
		sc.Default = true
	}
	sc.Mine = true
	return sc
}

// scopeToOrg scopes sc to of's Org, walked in accounts.
func (sc *GoalScope) scopeToOrg(of Account, accounts []Account) {
	sc.Of = &of
	sc.in = map[int64]bool{of.ID: true}
	for _, r := range orgWalk(of.ID, accounts) {
		sc.in[r.ID] = true
	}
}

// goalScopeValues are the values of dims' offered Dimensions ids name, in
// the Dimensions' order and, within one, ids', each once, and their IDs by
// their Dimension's. An ID naming none of them is ignored.
func goalScopeValues(ids []int64, dims []Dimension) ([]DimensionValue, map[int64][]int64) {
	var values []DimensionValue
	byDimension := map[int64][]int64{}
	for _, d := range OfferedDimensions(dims) {
		for _, id := range ids {
			for _, v := range d.Values {
				if v.ID == id && !slices.Contains(byDimension[d.ID], id) {
					values = append(values, v)
					byDimension[d.ID] = append(byDimension[d.ID], id)
				}
			}
		}
	}
	return values, byDimension
}

// goalScopePeople are the people a page's picker offers the Orgs of, but
// viewerID, by Label: each Owner of goals, then their Managers and on up,
// looked up in byID, stopping at anyone reached, as the directory's cycles
// are stored as it gives them (ADR 0008).
func goalScopePeople(viewerID int64, goals []Goal, byID map[int64]Account) []Account {
	reached := map[int64]bool{}
	var people []Account
	for _, g := range goals {
		for a, ok := g.Owner, true; ok && !reached[a.ID]; {
			reached[a.ID] = true
			people = append(people, a)
			if a.ManagerID == nil {
				break
			}
			a, ok = byID[*a.ManagerID]
		}
	}
	people = slices.DeleteFunc(people, func(a Account) bool { return a.ID == viewerID })
	slices.SortFunc(people, func(a, b Account) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Label()), strings.ToLower(b.Label())), cmp.Compare(a.Email, b.Email))
	})
	return people
}

// Keeps reports whether the scope keeps gv: it is InScope and HasValues.
func (sc GoalScope) Keeps(gv GoalWithValues) bool {
	return sc.InScope(gv.Goal) && sc.HasValues(gv)
}

// InScope reports whether g is in the Scope, whatever its values: its Owner
// is in the scope's Org, the viewer Owns it or is a Delegate on it for Mine,
// or the scope is Everyone.
func (sc GoalScope) InScope(g Goal) bool {
	switch {
	case sc.Of != nil:
		return sc.in[g.Owner.ID]
	case sc.Mine:
		return g.Owner.ID == sc.Viewer.ID || sc.delegated[g.ID]
	}
	return true
}

// HasValues reports whether gv has the scope's Values, whatever its Scope:
// any of each Dimension's that has one.
func (sc GoalScope) HasValues(gv GoalWithValues) bool {
	return goalMatchesFilter(gv, sc.byDimension)
}

// IsDefault reports whether sc is the viewer's default scope, whether the
// address named no scope or named the same one: My org when they have anyone
// under them, else Mine.
func (sc GoalScope) IsDefault() bool {
	if sc.Default {
		return true
	}
	if sc.OffersMine {
		return sc.Of != nil && sc.Of.ID == sc.Viewer.ID
	}
	return sc.Mine
}

// Offered reports whether the page offers any Org to scope to: not when no
// one has a Manager.
func (sc GoalScope) Offered() bool {
	return sc.OffersMine || len(sc.People) > 0
}

// Others are the people the picker offers beside My Org: People, and
// whoever the scope is the Org of if an address named someone else.
func (sc GoalScope) Others() []Account {
	if sc.Of == nil || sc.Of.ID == sc.Viewer.ID || slices.ContainsFunc(sc.People, func(a Account) bool { return a.ID == sc.Of.ID }) {
		return sc.People
	}
	return append([]Account{*sc.Of}, sc.People...)
}
