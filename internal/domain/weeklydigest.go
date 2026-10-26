package domain

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"
)

// WeeklyDigest is one parent Owner's weekly digest: the link requests waiting
// on them, and the problems among the Goals that contribute to theirs.
type WeeklyDigest struct {
	Recipient    Account
	PendingLinks []Link
	Problems     []WeeklyDigestProblem
}

// WeeklyDigestProblem is a Goal contributing to one of the recipient's, and
// what went wrong with it.
type WeeklyDigestProblem struct {
	Parent, Child Goal
	// WorsenedTo is the Health the child got worse to this week, Yellow or
	// Red, or "" when it didn't get worse.
	WorsenedTo string
	// DateSlips are the Date Slips the child recorded this week, earliest
	// first.
	DateSlips []DateSlip
	// WentCheckinOverdue is set when the child went Check-in overdue this
	// week, judged on its own Freshness in its Owner's Timezone, which says how
	// long ago its last Check-in was (Freshness.LastCheckinAge).
	WentCheckinOverdue bool
	Freshness          Freshness
	// Ownerless is set when the child's Owner has left the company.
	Ownerless bool
	// HaltedParents are the child's parents that are On Hold or Cancelled,
	// newest parent first.
	HaltedParents []HaltedParent
}

// weeklyDigestDays is how far back a digest's week reaches, in calendar days
// of the recipient's Timezone.
const weeklyDigestDays = 7

// WeeklyDigests is the page loader for the weekly Parent digests (ADR 0010):
// one WeeklyDigest per parent Owner with something to report, leaving out
// anyone who has left the company. It reads every Goal, every accepted link,
// every pending link request, and the children's Check-ins, Date Slips and
// Freshness once each, in the same few queries however many Goals and links
// there are, and judges each child once however many parents it has. Only
// whether a child got worse and the Date Slips it recorded are judged against
// the week, which each recipient counts in their own Timezone.
//
// Recipients come in the order they are first met: those with link requests by
// their first Goal in ListGoals order, then those with problems by parent Goal
// in that order. A digest's link requests come oldest first, and its problems
// by parent, then by child newest first, as ChildrenOf orders them.
func (s *Service) WeeklyDigests(ctx context.Context) ([]WeeklyDigest, error) {
	goals, err := s.ListGoals(ctx)
	if err != nil {
		return nil, err
	}
	edges, err := s.acceptedEdges(ctx)
	if err != nil {
		return nil, err
	}
	childrenOf, children, err := s.weeklyDigestChildren(ctx, edges)
	if err != nil {
		return nil, err
	}
	pending, err := s.pendingLinksByParentOwner(ctx)
	if err != nil {
		return nil, err
	}

	var out weeklyDigestsByRecipient
	for _, g := range goals {
		if links, ok := pending[g.Owner.ID]; ok {
			out.digest(g.Owner).PendingLinks = links
			delete(pending, g.Owner.ID)
		}
	}
	now := s.Now()
	for _, parent := range goals {
		weekStart := now.In(s.weeklyDigestTimezone(parent.Owner)).AddDate(0, 0, -weeklyDigestDays)
		for _, childID := range childrenOf[parent.ID] {
			c, ok := children[childID]
			if !ok {
				continue // Done or Cancelled: nothing left at risk
			}
			if p := c.problemUnder(parent, weekStart); p.reportable() {
				d := out.digest(parent.Owner)
				d.Problems = append(d.Problems, p)
			}
		}
	}
	return out.digests(), nil
}

// pendingLinksByParentOwner reads every pending link request, oldest first,
// keyed by the Owner of its parent Goal, who must decide it.
func (s *Service) pendingLinksByParentOwner(ctx context.Context) (map[int64][]Link, error) {
	rows, err := s.queries.ListPendingLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending links: %w", err)
	}
	out := map[int64][]Link{}
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.Link.CreatedAt)
		out[r.Goal_2.OwnerID] = append(out[r.Goal_2.OwnerID], Link{
			ID:        r.Link.ID,
			Child:     goalFromRow(r.Goal, r.Account),     // child goal + its Owner
			Parent:    goalFromRow(r.Goal_2, r.Account_2), // parent goal + its Owner
			Status:    r.Link.Status,
			Note:      r.Link.Note,
			CreatedAt: createdAt,
		})
	}
	return out, nil
}

// weeklyDigestChild is a Goal contributing to another, and what a digest says
// of it under any parent: the facts read and judged once, however many
// parents it has.
type weeklyDigestChild struct {
	goal      Goal
	checkins  []Checkin
	slips     []DateSlip
	freshness Freshness
	// wentCheckinOverdue is judged on the child's own Freshness, so no
	// recipient's week changes it.
	wentCheckinOverdue bool
	halted             []HaltedParent
}

// weeklyDigestChildren reads the children of edges, every accepted link: each
// parent's child IDs, newest child first as ChildrenOf orders them, and each
// child that isn't Done or Cancelled by ID, its Check-ins, Date Slips and
// Freshness read in one batch. A Done or Cancelled child has nothing left at
// risk, so it is left out.
func (s *Service) weeklyDigestChildren(ctx context.Context, edges []edge) (map[int64][]int64, map[int64]*weeklyDigestChild, error) {
	childrenOf := map[int64][]Goal{}
	up := map[int64][]edge{}
	var live []Goal
	for _, e := range edges {
		childrenOf[e.parent.ID] = append(childrenOf[e.parent.ID], e.child)
		if LifecycleEnded(e.child.Lifecycle) {
			continue
		}
		if _, ok := up[e.child.ID]; !ok {
			live = append(live, e.child)
		}
		up[e.child.ID] = append(up[e.child.ID], e)
	}
	ids := make([]int64, 0, len(live))
	for _, c := range live {
		ids = append(ids, c.ID)
	}
	checkins, err := s.checkinsByGoal(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	slips, err := s.DateSlipsByGoal(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	freshness, err := s.FreshnessByGoal(ctx, live)
	if err != nil {
		return nil, nil, err
	}

	children := make(map[int64]*weeklyDigestChild, len(live))
	for _, c := range live {
		parents := up[c.ID]
		// A child's halted parents come newest first, as ParentsOf reads them.
		slices.SortFunc(parents, func(a, b edge) int { return weeklyDigestNewestFirst(a.parent, b.parent) })
		f := freshness[c.ID]
		children[c.ID] = &weeklyDigestChild{
			goal:               c,
			checkins:           checkins[c.ID],
			slips:              slips[c.ID],
			freshness:          f,
			wentCheckinOverdue: wentCheckinOverdueThisWeek(f),
			halted:             haltedParents(parents),
		}
	}
	ordered := make(map[int64][]int64, len(childrenOf))
	for parentID, kids := range childrenOf {
		slices.SortFunc(kids, weeklyDigestNewestFirst)
		for _, k := range kids {
			ordered[parentID] = append(ordered[parentID], k.ID)
		}
	}
	return ordered, children, nil
}

// problemUnder is what's wrong with c listed under parent, whose Owner's week
// started at weekStart. Some are changes this week: its Health got worse, to
// Yellow or to Red; it recorded a Date Slip; its Check-in went overdue. Others
// hold for as long as they last, so the digest names them every week: the
// child is Ownerless (no record says when its Owner left), or one of its
// parents is On Hold or Cancelled.
func (c *weeklyDigestChild) problemUnder(parent Goal, weekStart time.Time) WeeklyDigestProblem {
	p := WeeklyDigestProblem{
		Parent:             parent,
		Child:              c.goal,
		WentCheckinOverdue: c.wentCheckinOverdue,
		Freshness:          c.freshness,
		Ownerless:          c.goal.Ownerless,
		HaltedParents:      c.halted,
	}
	if c.goal.Lifecycle == LifecycleActive {
		if now, before := healthAt(c.checkins, time.Time{}), healthAt(c.checkins, weekStart); worsened(before, now) {
			p.WorsenedTo = now
		}
	}
	for _, sl := range c.slips {
		if sl.CreatedAt.After(weekStart) {
			p.DateSlips = append(p.DateSlips, sl)
		}
	}
	return p
}

// reportable reports whether p has anything to report.
func (p WeeklyDigestProblem) reportable() bool {
	return p.WorsenedTo != "" || len(p.DateSlips) > 0 || p.WentCheckinOverdue || p.Ownerless || len(p.HaltedParents) > 0
}

// weeklyDigestTimezone is the Timezone a's week is counted in: theirs, or the
// company's when they have none, as GoalTimezone reads a Goal's Owner's.
func (s *Service) weeklyDigestTimezone(a Account) *time.Location {
	if loc, ok := loadTimezone(a.Timezone); ok {
		return loc
	}
	return s.loc
}

// weeklyDigestNewestFirst orders Goals newest first, as ListChildGoals and
// ListParentGoals do.
func weeklyDigestNewestFirst(a, b Goal) int {
	if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
		return c
	}
	return cmp.Compare(b.ID, a.ID)
}

// wentCheckinOverdueThisWeek reports whether a Goal is Check-in overdue now
// but wasn't a week ago: it has been overdue for no more than a week's days
// (CONTEXT.md: Check-in overdue).
func wentCheckinOverdueThisWeek(f Freshness) bool {
	return f.CheckinOverdue && f.DaysSince-weeklyDigestDays <= f.CadenceDays
}

// healthAt is the Health a Goal's Check-ins (newest first) had set as of t, or
// its current Health when t is the zero time. It is "" when none had set one.
func healthAt(checkins []Checkin, t time.Time) string {
	for _, c := range checkins {
		if c.Health == "" || (!t.IsZero() && c.CreatedAt.After(t)) {
			continue
		}
		return c.Health
	}
	return ""
}

// worsened reports whether Health went from before to a worse now that is
// Yellow or Red: Green to Yellow, Yellow to Red, or no Health yet to either.
func worsened(before, now string) bool {
	return (now == HealthYellow || now == HealthRed) && healthRank(now) > healthRank(before)
}

// weeklyDigestsByRecipient collects the digests in the order their recipients
// were first met. A Departed recipient gets none.
type weeklyDigestsByRecipient struct {
	order []*WeeklyDigest
	byID  map[int64]*WeeklyDigest
}

// digest is to's digest, started if this is their first item.
func (o *weeklyDigestsByRecipient) digest(to Account) *WeeklyDigest {
	if d, ok := o.byID[to.ID]; ok {
		return d
	}
	if o.byID == nil {
		o.byID = map[int64]*WeeklyDigest{}
	}
	d := &WeeklyDigest{Recipient: to}
	o.byID[to.ID] = d
	o.order = append(o.order, d)
	return d
}

// digests is every digest collected, leaving out the Departed recipients'.
func (o *weeklyDigestsByRecipient) digests() []WeeklyDigest {
	out := make([]WeeklyDigest, 0, len(o.order))
	for _, d := range o.order {
		if !d.Recipient.Departed {
			out = append(out, *d)
		}
	}
	return out
}
