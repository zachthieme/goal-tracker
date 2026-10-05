package seed_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/seed"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The e2e suite's reference instants. The default template is the seed ending
// at e2eDefaultEnd, the instant `cmd/seed -end 2026-10-05` uses; the due
// template is the seed ending 4 days earlier, at e2eDueEnd. Both are viewed
// from e2eAppStart, the instant the suite's app starts at. The e2e fixtures
// (e2e/global-setup.ts, #230) seed at the same dates: the two move together.
var (
	e2eDefaultEnd = time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC)
	e2eDueEnd     = time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)
	e2eAppStart   = time.Date(2026, 10, 5, 18, 5, 0, 0, time.UTC)
)

// TestE2EScenarioSituations seeds the e2e suite's reference orgs and checks
// that each scenario spec finds the situations its seedLookup choosers look
// for, so a seed change that breaks a scenario fails make test, not only make
// e2e. Each check encodes its spec's chooser; a failure names the scenario and
// the rule that no longer holds.
func TestE2EScenarioSituations(t *testing.T) {
	t.Parallel()

	t.Run("default template", func(t *testing.T) {
		t.Parallel()
		org := seededE2EOrg(t, e2eDefaultEnd)
		t.Run("scenario 1 finding trouble", func(t *testing.T) { e2eScenario1(t, org) })
		t.Run("scenario 2 understanding trouble", func(t *testing.T) { e2eScenario2Understanding(t, org) })
		t.Run("scenario 2 watching Goals", func(t *testing.T) { e2eScenario2Watching(t, org) })
		t.Run("scenario 3 Delegate covers a Stale Goal", func(t *testing.T) { e2eScenario3Delegate(t, org) })
		t.Run("scenario 4 reporting bad news", func(t *testing.T) { e2eScenario4(t, org) })
		t.Run("scenario 5 stating a Goal", func(t *testing.T) { e2eScenario5(t, org) })
		t.Run("isolation", func(t *testing.T) { e2eIsolation(t, org) })
	})
	t.Run("due template", func(t *testing.T) {
		t.Parallel()
		org := seededE2EOrg(t, e2eDueEnd)
		t.Run("scenario 3 the week", func(t *testing.T) { e2eScenario3Week(t, org) })
	})
}

// e2eOrg is a seeded org as the e2e choosers read it, through the domain
// Service, viewed from e2eAppStart.
type e2eOrg struct {
	h     *testsupport.Harness
	now   time.Time
	goals []*e2eGoal // by id
	byID  map[int64]*e2eGoal
}

// e2eGoal is one Goal with what the choosers read about it.
type e2eGoal struct {
	domain.Goal
	checkins   []domain.Checkin // newest first
	milestones []domain.Milestone
	slips      []domain.DateSlip
	metrics    []domain.Metric
	delegates  []domain.Account
	children   []*e2eGoal // accepted links, any Lifecycle
	parents    []*e2eGoal // accepted links, any Lifecycle
	team       string     // "" when it has no Team value
}

// seededE2EOrg seeds DefaultSeed into a fresh harness with the history ending
// at end, sets the clock to e2eAppStart, and reads the org back.
func seededE2EOrg(t *testing.T, end time.Time) *e2eOrg {
	t.Helper()
	ctx := context.Background()
	h := testsupport.New(t, admin)
	h.Clock.Set(end)
	if _, err := seed.Run(ctx, h.Service, h.Clock, seed.Options{Seed: seed.DefaultSeed, Admin: admin}); err != nil {
		t.Fatalf("seed.Run: %v", err)
	}
	h.Clock.Set(e2eAppStart)

	org := &e2eOrg{h: h, now: e2eAppStart, byID: map[int64]*e2eGoal{}}
	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	var teamDim int64
	for _, d := range dims {
		if d.Name == "Team" {
			teamDim = d.ID
		}
	}
	goals := listGoals(t, h)
	slices.SortFunc(goals, func(a, b domain.Goal) int { return int(a.ID - b.ID) })
	for _, g := range goals {
		eg := &e2eGoal{Goal: g}
		org.goals = append(org.goals, eg)
		org.byID[g.ID] = eg
	}
	for _, g := range org.goals {
		must := func(err error, what string) {
			t.Helper()
			if err != nil {
				t.Fatalf("%s(%q): %v", what, g.Title, err)
			}
		}
		g.checkins, err = h.Service.ListCheckins(ctx, g.ID)
		must(err, "ListCheckins")
		g.milestones, err = h.Service.ListMilestones(ctx, g.ID)
		must(err, "ListMilestones")
		g.slips, err = h.Service.ListDateSlips(ctx, g.ID)
		must(err, "ListDateSlips")
		g.metrics, err = h.Service.ListMetrics(ctx, g.ID)
		must(err, "ListMetrics")
		g.delegates, err = h.Service.ListDelegates(ctx, g.ID)
		must(err, "ListDelegates")
		children, err := h.Service.ChildrenOf(ctx, g.ID)
		must(err, "ChildrenOf")
		for _, c := range children {
			child := org.byID[c.ID]
			g.children = append(g.children, child)
			child.parents = append(child.parents, g)
		}
		values, err := h.Service.GoalValues(ctx, g.ID)
		must(err, "GoalValues")
		for _, v := range values {
			if v.DimensionID == teamDim {
				g.team = v.Value
			}
		}
	}
	for _, g := range org.goals {
		byID := func(a, b *e2eGoal) int { return int(a.ID - b.ID) }
		slices.SortFunc(g.children, byID)
		slices.SortFunc(g.parents, byID)
	}
	return org
}

// today is the e2e app's date, in UTC: the server's timezone in the suite.
func (o *e2eOrg) today() time.Time {
	y, m, d := o.now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// find is the first Goal, by id, that meets every one of rules, as a
// seedLookup query's "order by g.id limit 1" picks it.
func (o *e2eOrg) find(rules ...func(*e2eGoal) bool) *e2eGoal {
	for _, g := range o.goals {
		if meetsAll(g, rules) {
			return g
		}
	}
	return nil
}

func meetsAll[T any](v T, rules []func(T) bool) bool {
	for _, r := range rules {
		if !r(v) {
			return false
		}
	}
	return true
}

func (g *e2eGoal) active() bool { return g.Lifecycle == domain.LifecycleActive }

// health is the Health of the Goal's latest Check-in, "" when it has none.
func (g *e2eGoal) health() string {
	if len(g.checkins) == 0 {
		return ""
	}
	return g.checkins[0].Health
}

// plannedMilestones are the Goal's Planned Milestones.
func (g *e2eGoal) plannedMilestones() []domain.Milestone {
	var out []domain.Milestone
	for _, m := range g.milestones {
		if m.Status == domain.MilestonePlanned {
			out = append(out, m)
		}
	}
	return out
}

// overdueMilestone is the choosers' "a Planned Milestone dated before today".
func (o *e2eOrg) overdueMilestone(g *e2eGoal) bool {
	return slices.ContainsFunc(g.plannedMilestones(), func(m domain.Milestone) bool { return m.TargetDate.Before(o.today()) })
}

// e2eScenario4 is chooseBadNewsChain (04-reporting-bad-news.spec.ts): a
// project, the team Goal it contributes to, and an org outcome the team Goal
// contributes to whose other Active children are all Green. The rules filter
// the candidate links in the spec's order, and the first that leaves none is
// the failure.
func e2eScenario4(t *testing.T, o *e2eOrg) {
	type chain struct{ project, team *e2eGoal }
	activeGreen := func(g *e2eGoal) bool { return g.active() && g.health() == domain.HealthGreen }
	greenOrgOutcomes := func(team *e2eGoal) []*e2eGoal {
		var out []*e2eGoal
		for _, org := range team.parents {
			if !slices.ContainsFunc(org.children, func(c *e2eGoal) bool {
				return c != team && c.active() && len(c.checkins) > 0 && c.health() != domain.HealthGreen
			}) {
				out = append(out, org)
			}
		}
		return out
	}
	var candidates []chain
	for _, team := range o.goals {
		for _, project := range team.children {
			if !project.Owner.Departed && !team.Owner.Departed {
				candidates = append(candidates, chain{project, team})
			}
		}
	}
	rules := []struct {
		rule  string
		meets func(chain) bool
	}{
		{"the project is Active, Dated and Green", func(c chain) bool {
			return activeGreen(c.project) && c.project.Kind == domain.GoalDated
		}},
		{"the project has at least two Planned Milestones dated after today", func(c chain) bool {
			n := 0
			for _, m := range c.project.plannedMilestones() {
				if m.TargetDate.After(o.today()) {
					n++
				}
			}
			return n >= 2
		}},
		{"the project has no overdue Planned Milestone", func(c chain) bool { return !o.overdueMilestone(c.project) }},
		{"the project has a Team", func(c chain) bool { return c.project.team != "" }},
		{"the team Goal is Active and Green", func(c chain) bool { return activeGreen(c.team) }},
		{"the team Goal has another Owner than the project", func(c chain) bool {
			return c.project.Owner.Email != c.team.Owner.Email
		}},
		{"the team Goal's other Active children are all Green", func(c chain) bool {
			return !slices.ContainsFunc(c.team.children, func(k *e2eGoal) bool {
				return k != c.project && k.active() && k.health() != domain.HealthGreen
			})
		}},
		{"the team Goal has no overdue Planned Milestone", func(c chain) bool { return !o.overdueMilestone(c.team) }},
		{"the team Goal contributes to an org outcome", func(c chain) bool { return len(c.team.parents) > 0 }},
		{"the team Goal contributes to an org outcome whose other Active children are all Green", func(c chain) bool {
			return len(greenOrgOutcomes(c.team)) > 0
		}},
	}
	for _, r := range rules {
		candidates = slices.DeleteFunc(candidates, func(c chain) bool { return !r.meets(c) })
		if len(candidates) == 0 {
			t.Fatalf("scenario 4: no project and team Goal meet the rule: %s", r.rule)
		}
	}

	// The org outcome's page shows Rolled-up Health Green before and after the
	// project turns Red.
	c := candidates[0]
	org := greenOrgOutcomes(c.team)[0]
	if r := o.rollup(t, org); !r.Present || r.Health != domain.HealthGreen {
		t.Errorf("scenario 4: org outcome %q rolls up %+v, want Green", org.Title, r)
	}
}

// ownedBy is every Goal the Account owns.
func (o *e2eOrg) ownedBy(id int64) []*e2eGoal {
	var out []*e2eGoal
	for _, g := range o.goals {
		if g.Owner.ID == id {
			out = append(out, g)
		}
	}
	return out
}

// lastUpdate is the choosers' coalesce of the Goal's last Check-in, its
// activation and its creation.
func (g *e2eGoal) lastUpdate() time.Time {
	switch {
	case len(g.checkins) > 0:
		return g.checkins[0].CreatedAt
	case !g.ActivatedAt.IsZero():
		return g.ActivatedAt
	default:
		return g.CreatedAt
	}
}

// staleBy is the choosers' Stale test, julianday('now') - julianday(last
// update) > cadence + extra: whole days of time, not of the calendar.
func (o *e2eOrg) staleBy(g *e2eGoal, extraDays int) bool {
	return o.now.Sub(g.lastUpdate()) > time.Duration(g.CadenceDays+extraDays)*24*time.Hour
}

// isStale is 01-finding-trouble's isStale.
func (o *e2eOrg) isStale(g *e2eGoal) bool { return o.staleBy(g, 0) }

// checkedInWithinCadence is pickCheckinGoal's freshness rule: the Goal's last
// Check-in is less than its cadence ago. A Goal with no Check-in fails it.
func (o *e2eOrg) checkedInWithinCadence(g *e2eGoal) bool {
	return len(g.checkins) > 0 && g.checkins[0].CreatedAt.After(o.now.Add(-time.Duration(g.CadenceDays)*24*time.Hour))
}

func (g *e2eGoal) activeChildren() []*e2eGoal {
	var out []*e2eGoal
	for _, c := range g.children {
		if c.active() {
			out = append(out, c)
		}
	}
	return out
}

// deliverySlips are the Goal's Date Slips of its delivery date, oldest first.
func (g *e2eGoal) deliverySlips() []domain.DateSlip {
	var out []domain.DateSlip
	for _, s := range g.slips {
		if s.MilestoneID == 0 {
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b domain.DateSlip) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

func (g *e2eGoal) troubled() bool {
	return g.health() == domain.HealthYellow || g.health() == domain.HealthRed
}

func (g *e2eGoal) pathToGreen() string {
	if len(g.checkins) == 0 {
		return ""
	}
	return g.checkins[0].PathToGreen
}

// The domain's reads of a Goal, as its pages show them.

func (o *e2eOrg) freshness(t *testing.T, g *e2eGoal) domain.Freshness {
	t.Helper()
	f, err := o.h.Service.Freshness(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("Freshness(%q): %v", g.Title, err)
	}
	return f
}

func (o *e2eOrg) signals(t *testing.T, g *e2eGoal) domain.GoalSignals {
	t.Helper()
	s, err := o.h.Service.GoalSignals(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("GoalSignals(%q): %v", g.Title, err)
	}
	return s
}

func (o *e2eOrg) rollup(t *testing.T, g *e2eGoal) domain.RolledUpHealth {
	t.Helper()
	r, err := o.h.Service.RolledUpHealth(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("RolledUpHealth(%q): %v", g.Title, err)
	}
	return r
}

// riskReasons are the reasons Risks flags g for, sorted: its signals as the
// domain reads them. A Schedule conflict flags the child, not the parent.
func (o *e2eOrg) riskReasons(t *testing.T, g *e2eGoal) []string {
	t.Helper()
	f, s := o.freshness(t, g), o.signals(t, g)
	var out []string
	for _, r := range []struct {
		reason string
		flags  bool
	}{
		{"Ownerless", g.Ownerless},
		{"Parent halted", len(s.HaltedParents) > 0},
		{"Path to Green overdue", f.PathToGreenOverdue},
		{"Schedule conflict", slices.ContainsFunc(s.ScheduleConflicts, func(c domain.ScheduleConflict) bool { return c.Child.ID == g.ID })},
		{"Stale", f.Stale},
		{"Unaligned", s.Unaligned},
	} {
		if r.flags {
			out = append(out, r.reason)
		}
	}
	return out
}

// e2eScenario1 is 01-finding-trouble.spec.ts's choosers, and what it asserts
// on Risks of the Goals they choose.
func e2eScenario1(t *testing.T, o *e2eOrg) {
	const leader = "cto@example.com"
	platform := func(g *e2eGoal) bool { return g.team == "Platform" }
	active := (*e2eGoal).active

	// stale: a Stale Platform Goal the leader doesn't own.
	stale := o.find(active, platform, o.isStale, func(g *e2eGoal) bool {
		return g.Owner.Email != leader && !g.Owner.Departed
	})
	if stale == nil {
		t.Fatal("scenario 1: no Stale Platform Goal the leader doesn't own (stale)")
	}
	// departing: an Owner of an Active Platform Goal who owns no Top-level and
	// no Stale Active Goal, and is neither the leader nor an Admin.
	departing := o.find(active, platform, func(g *e2eGoal) bool {
		a := g.Owner
		if a.Email == leader || a.Email == admin || a.IsAdmin {
			return false
		}
		return !slices.ContainsFunc(o.ownedBy(a.ID), func(h *e2eGoal) bool {
			return h.TopLevel || (h.active() && o.isStale(h))
		})
	})
	if departing == nil {
		t.Fatal("scenario 1: no Platform Owner to depart, owning no Top-level or Stale Goal (departing)")
	}
	// overdue: an Active Platform Goal that can take a plain Check-in.
	overdue := o.find(active, platform, func(g *e2eGoal) bool {
		return g.Owner.Email != leader && g.Owner.Email != departing.Owner.Email && !g.Owner.Departed &&
			g != stale && !o.isStale(g) && len(g.activeChildren()) == 0 && !o.overdueMilestone(g)
	})
	if overdue == nil {
		t.Fatal("scenario 1: no Platform Goal to put past its Path to Green: not Stale, no Active children, no overdue Milestone (overdue)")
	}
	tiered := 0
	for _, g := range o.goals {
		if g.active() && platform(g) {
			tiered++
		}
	}
	if tiered < 2 {
		t.Errorf("scenario 1: %d Active Platform Goals, want two to carry the second Dimension", tiered)
	}
	unaligned := o.find(active, func(g *e2eGoal) bool {
		return !g.TopLevel && g.Owner.Email != departing.Owner.Email && len(g.parents) == 0
	})
	if unaligned == nil {
		t.Fatal("scenario 1: no Unaligned Goal (unaligned)")
	}
	twoReasons := o.find(active, o.isStale, func(g *e2eGoal) bool {
		return g.Owner.Email != departing.Owner.Email && g.team != "" && !g.DeliveryDate.IsZero() &&
			slices.ContainsFunc(g.parents, func(p *e2eGoal) bool {
				return !p.DeliveryDate.IsZero() && g.DeliveryDate.After(p.DeliveryDate)
			})
	})
	if twoReasons == nil {
		t.Fatal("scenario 1: no Stale Goal with a Team and a Schedule conflict, the two-reason row (twoReasons)")
	}
	redWithChildren := o.find(active, func(g *e2eGoal) bool {
		return g.health() == domain.HealthRed && !o.isStale(g) && g.Owner.Email != departing.Owner.Email &&
			len(g.parents) > 0 && len(g.activeChildren()) > 0
	})
	if redWithChildren == nil {
		t.Fatal("scenario 1: no Red Goal, not Stale and contributing to a Goal, with Active child Goals (redWithChildren)")
	}
	redSlipped := o.find(active, func(g *e2eGoal) bool {
		return g.health() == domain.HealthRed && !g.DeliveryDate.IsZero() && len(g.deliverySlips()) > 0
	})
	if redSlipped == nil {
		t.Fatal("scenario 1: no Red Goal with a Date Slip of its delivery date (redSlipped)")
	}

	// Risks lists Stale, Unaligned and Schedule conflict Goals.
	ctx := context.Background()
	fs, err := o.h.Service.FreshnessSignals(ctx)
	if err != nil {
		t.Fatalf("FreshnessSignals: %v", err)
	}
	gs, err := o.h.Service.GraphSignals(ctx)
	if err != nil {
		t.Fatalf("GraphSignals: %v", err)
	}
	if len(fs.Stale) == 0 || len(gs.Unaligned) == 0 || len(gs.ScheduleConflicts) == 0 {
		t.Errorf("scenario 1: Risks has %d Stale, %d Unaligned and %d Schedule conflict Goals, want some of each",
			len(fs.Stale), len(gs.Unaligned), len(gs.ScheduleConflicts))
	}
	// Narrowed to Platform, Risks has fewer rows than the whole org: some
	// flagged Goal is on another team.
	offPlatform := slices.ContainsFunc(fs.Stale, func(f domain.GoalFreshness) bool { return o.byID[f.Goal.ID].team != "Platform" }) ||
		slices.ContainsFunc(gs.Unaligned, func(g domain.Goal) bool { return o.byID[g.ID].team != "Platform" })
	if !offPlatform {
		t.Error("scenario 1: no Stale or Unaligned Goal off Platform, so narrowing Risks to Platform wouldn't narrow it")
	}

	// stale is under Owner needs to update and not Plan doesn't fit.
	if got := o.riskReasons(t, stale); !slices.Equal(got, []string{"Stale"}) {
		t.Errorf("scenario 1: stale Goal %q is on Risks for %q, want only Stale", stale.Title, got)
	}
	// unaligned is under Plan doesn't fit and not Owner needs to update, even
	// after the setup puts overdue past its Path to Green.
	if got := o.riskReasons(t, unaligned); !slices.Equal(got, []string{"Unaligned"}) || unaligned == overdue {
		t.Errorf("scenario 1: Unaligned Goal %q is on Risks for %q, or is the Goal the setup makes overdue (%t); want only Unaligned",
			unaligned.Title, got, unaligned == overdue)
	}
	// twoReasons is one row with a chip for each of its two reasons.
	if got := o.riskReasons(t, twoReasons); !slices.Equal(got, []string{"Schedule conflict", "Stale"}) {
		t.Errorf("scenario 1: two-reason Goal %q is on Risks for %q, want Schedule conflict and Stale", twoReasons.Title, got)
	}
	// redWithChildren isn't on Risks: nothing but its Health is wrong with it.
	if got := o.riskReasons(t, redWithChildren); len(got) > 0 {
		t.Errorf("scenario 1: Red Goal %q with Active children is on Risks for %q, want it off Risks", redWithChildren.Title, got)
	}
	// Each Red Goal shows the Owner's status and Path to Green.
	for _, g := range []*e2eGoal{redWithChildren, redSlipped} {
		if g.checkins[0].Status == "" || g.pathToGreen() == "" {
			t.Errorf("scenario 1: Red Goal %q has no status or Path to Green", g.Title)
		}
	}
}

// e2eTeamReportGoals are the Goals a "Team is any of team" Report lists
// against its default baseline, 30 days back (platformGoals and growthGoals
// in the scenario 2 specs): inReport every Goal on the team less those Done
// or Cancelled before the baseline, and finishedEarlier those.
func (o *e2eOrg) e2eTeamReportGoals(team string) (inReport, finishedEarlier []*e2eGoal) {
	baseline := o.now.AddDate(0, 0, -30)
	for _, g := range o.goals {
		if g.team != team {
			continue
		}
		if g.Lifecycle == domain.LifecycleDone || g.Lifecycle == domain.LifecycleCancelled {
			var finished time.Time
			for _, c := range g.checkins {
				if c.LifecycleChange.To == g.Lifecycle && c.CreatedAt.After(finished) {
					finished = c.CreatedAt
				}
			}
			if finished.Before(baseline) {
				finishedEarlier = append(finishedEarlier, g)
				continue
			}
		}
		inReport = append(inReport, g)
	}
	return inReport, finishedEarlier
}

// e2eScenario2Understanding is 02-understanding-trouble.spec.ts's choosers:
// the Platform Goals the Report is built over, and pickCheckinGoal's Goals.
func e2eScenario2Understanding(t *testing.T, o *e2eOrg) {
	platform, _ := o.e2eTeamReportGoals("Platform")
	if len(platform) == 0 {
		t.Fatal("scenario 2 (understanding trouble): no Platform Goals for the Report")
	}
	if !slices.ContainsFunc(platform, func(g *e2eGoal) bool { return g.troubled() && g.pathToGreen() != "" }) {
		t.Error("scenario 2 (understanding trouble): no Red or Yellow Platform Goal with a Path to Green, an exception in full")
	}
	// The seed writes no Highlights, so the curation lists only the scenario's.
	for _, g := range platform {
		hs, err := o.h.Service.ListHighlightsByGoal(context.Background(), g.ID)
		if err != nil {
			t.Fatalf("ListHighlightsByGoal: %v", err)
		}
		if len(hs) > 0 {
			t.Errorf("scenario 2 (understanding trouble): Platform Goal %q has %d seeded Highlights, want none", g.Title, len(hs))
		}
	}

	// pickCheckinGoal: Active, Owner still here, checked in within its
	// cadence, no accepted children and no overdue Milestone.
	plain := func(g *e2eGoal) bool {
		return g.active() && !g.Owner.Departed && o.checkedInWithinCadence(g) &&
			len(g.children) == 0 && !o.overdueMilestone(g)
	}
	var slipped *e2eGoal
	for _, g := range platform {
		if plain(g) && g.Kind == domain.GoalDated && g.health() == domain.HealthGreen {
			slipped = g
			break
		}
	}
	if slipped == nil {
		t.Fatal("scenario 2 (understanding trouble): no Green Dated Platform Goal its Owner can check in on plainly (pickCheckinGoal), to slip")
	}
	var turning *e2eGoal
	for _, g := range platform {
		if plain(g) && g != slipped {
			turning = g
			break
		}
	}
	if turning == nil {
		t.Fatal("scenario 2 (understanding trouble): no second Platform Goal its Owner can check in on plainly (pickCheckinGoal), to turn")
	}
	if !slices.ContainsFunc(platform, func(g *e2eGoal) bool { return g != slipped && g != turning }) {
		t.Error("scenario 2 (understanding trouble): no third Platform Goal to move to another Team")
	}
}

// e2eScenario2Watching is 02-watching-goals.spec.ts's choosers: the Growth
// Goals of the rule Report, and pickedGoals.
func e2eScenario2Watching(t *testing.T, o *e2eOrg) {
	const manager = "growth-lead@example.com"
	inReport, finishedEarlier := o.e2eTeamReportGoals("Growth")
	if len(inReport) == 0 {
		t.Error("scenario 2 (watching Goals): no Growth Goals for the rule Report")
	}
	if len(finishedEarlier) == 0 {
		t.Error("scenario 2 (watching Goals): no Growth Goal finished more than 30 days ago")
	}

	// pickedGoals' candidates: Active, checked in, on a Team other than Growth,
	// neither owned by nor delegated to the manager.
	var candidates []*e2eGoal
	for _, g := range o.goals {
		if g.active() && len(g.checkins) > 0 && g.team != "" && g.team != "Growth" && g.Owner.Email != manager &&
			!slices.ContainsFunc(g.delegates, func(a domain.Account) bool { return a.Email == manager }) {
			candidates = append(candidates, g)
		}
	}
	first := func(meets func(*e2eGoal) bool) *e2eGoal {
		if i := slices.IndexFunc(candidates, meets); i >= 0 {
			return candidates[i]
		}
		return nil
	}
	slipped := first(func(g *e2eGoal) bool { return len(g.deliverySlips()) > 0 && g.troubled() })
	troubled := first(func(g *e2eGoal) bool { return g != slipped && g.troubled() && g.pathToGreen() != "" })
	green := first(func(g *e2eGoal) bool {
		return g.health() == domain.HealthGreen && len(g.activeChildren()) == 0 && !o.overdueMilestone(g)
	})
	others := []*e2eGoal{slipped, troubled, green}
	parent := first(func(g *e2eGoal) bool {
		return len(g.activeChildren()) > 0 && !slices.Contains(others, g) &&
			!slices.ContainsFunc(g.activeChildren(), func(c *e2eGoal) bool { return slices.Contains(others, c) })
	})
	if slipped == nil {
		t.Error("scenario 2 (watching Goals): no Yellow or Red Goal off Growth that slipped its delivery date")
	}
	if troubled == nil {
		t.Error("scenario 2 (watching Goals): no other Yellow or Red Goal off Growth with a Path to Green")
	}
	if green == nil {
		t.Error("scenario 2 (watching Goals): no Green Goal off Growth with no Active children or overdue Milestone")
	}
	if parent == nil {
		t.Error("scenario 2 (watching Goals): no Goal off Growth with Active children, disjoint from the other picks")
	}
}

// e2eScenario3Delegate is 03-weekly-checkin.spec.ts's second test, on the
// default template: staleGoal, and someone to delegate it to.
func e2eScenario3Delegate(t *testing.T, o *e2eOrg) {
	var goal *e2eGoal
	for _, g := range o.goals {
		if !g.active() || g.Owner.Departed || g.Owner.Name == "" || len(g.checkins) == 0 || !o.staleBy(g, 1) ||
			len(g.children) > 0 || o.overdueMilestone(g) || len(g.delegates) > 0 {
			continue
		}
		if goal == nil || g.checkins[0].CreatedAt.Before(goal.checkins[0].CreatedAt) {
			goal = g
		}
	}
	if goal == nil {
		t.Fatal("scenario 3: no Goal Stale by more than its cadence and a day, with a named Owner and no Delegates (staleGoal)")
	}
	if !o.freshness(t, goal).Stale {
		t.Errorf("scenario 3: staleGoal %q isn't Stale on Risks and Home", goal.Title)
	}
	if !slices.ContainsFunc(o.goals, func(g *e2eGoal) bool {
		a := g.Owner
		return !a.Departed && !a.IsAdmin && a.Name != "" && a.Email != goal.Owner.Email
	}) {
		t.Error("scenario 3: no one to delegate the Stale Goal to (delegateFor)")
	}
}

// e2eScenario3Week is 03-weekly-checkin.spec.ts's first test, on the due
// template: weekOwners, and the first of them whose Home lists two Check-ins
// due, one on their Metric Goal.
func e2eScenario3Week(t *testing.T, o *e2eOrg) {
	ctx := context.Background()
	type owner struct {
		account domain.Account
		active  []*e2eGoal
	}
	byOwner := map[int64]*owner{}
	var owners []*owner
	for _, g := range o.goals {
		if !g.active() || g.Owner.Departed || g.Owner.Email == "cto@example.com" {
			continue
		}
		if byOwner[g.Owner.ID] == nil {
			byOwner[g.Owner.ID] = &owner{account: g.Owner}
			owners = append(owners, byOwner[g.Owner.ID])
		}
		byOwner[g.Owner.ID].active = append(byOwner[g.Owner.ID].active, g)
	}
	// weekOwners: at least two Active Goals, exactly one with a Metric, and
	// none a No change would be refused on. Most Active Goals first.
	owners = slices.DeleteFunc(owners, func(w *owner) bool {
		metrics := 0
		for _, g := range w.active {
			if len(g.metrics) > 0 {
				metrics++
			}
			if len(g.children) > 0 || o.overdueMilestone(g) {
				return true
			}
		}
		return len(w.active) < 2 || metrics != 1
	})
	slices.SortStableFunc(owners, func(a, b *owner) int {
		if len(a.active) != len(b.active) {
			return len(b.active) - len(a.active)
		}
		return int(a.account.ID - b.account.ID)
	})

	for _, w := range owners {
		home, err := o.h.Service.ActiveGoalsFor(ctx, w.account.ID)
		if err != nil {
			t.Fatalf("ActiveGoalsFor: %v", err)
		}
		var listed []domain.PersonalGoal
		for _, pg := range home {
			if pg.Freshness.DueBeforeNextReminder() {
				listed = append(listed, pg)
			}
		}
		var due []*e2eGoal
		for _, g := range w.active {
			if slices.ContainsFunc(listed, func(pg domain.PersonalGoal) bool { return pg.Goal.Title == g.Title }) {
				due = append(due, g)
			}
		}
		metricGoal := slices.IndexFunc(due, func(g *e2eGoal) bool { return len(g.metrics) > 0 })
		if len(due) < 2 || metricGoal < 0 {
			continue
		}

		// The scenario's Priya: Home lists exactly the Owner's own due Goals,
		// each checked in before and not Stale, and no Requests.
		if len(listed) != len(due) {
			t.Errorf("scenario 3: %s's Home lists %d Check-ins due, %d of them their own", w.account.Email, len(listed), len(due))
		}
		for _, pg := range listed {
			if pg.Freshness.Stale || !pg.CheckedIn {
				t.Errorf("scenario 3: %s's due Goal %q is Stale (%t) or has no Check-in to repeat (%t)",
					w.account.Email, pg.Goal.Title, pg.Freshness.Stale, !pg.CheckedIn)
			}
		}
		links, err := o.h.Service.PendingLinkRequests(ctx, w.account.ID)
		if err != nil {
			t.Fatalf("PendingLinkRequests: %v", err)
		}
		handoffs, err := o.h.Service.PendingHandoffs(ctx, w.account.ID)
		if err != nil {
			t.Fatalf("PendingHandoffs: %v", err)
		}
		suggestions, err := o.h.Service.OpenParentSuggestionsFor(ctx, w.account.ID)
		if err != nil {
			t.Fatalf("OpenParentSuggestionsFor: %v", err)
		}
		if n := len(links) + len(handoffs) + len(suggestions); n > 0 {
			t.Errorf("scenario 3: %s has %d Requests on Home, want none", w.account.Email, n)
		}
		metric := due[metricGoal].metrics[0]
		readings, err := o.h.Service.ListMetricReadings(ctx, metric.ID)
		if err != nil {
			t.Fatalf("ListMetricReadings: %v", err)
		}
		if len(readings) == 0 {
			t.Errorf("scenario 3: %s's Metric %q has no reading", w.account.Email, metric.Name)
		}
		return
	}
	t.Error("scenario 3: no weekOwners Owner has two Check-ins due on Home, one on their Metric Goal")
}

// e2eScenario5 is 05-stating-a-goal.spec.ts's choosers: a lead's team Goal
// rolling up Green or not at all, and a project Owner.
func e2eScenario5(t *testing.T, o *e2eOrg) {
	lead := func(a domain.Account) bool { return strings.HasSuffix(a.Email, "-lead@example.com") }
	team := o.find((*e2eGoal).active, func(g *e2eGoal) bool {
		if g.Owner.Departed || !lead(g.Owner) || g.team == "" {
			return false
		}
		r := o.rollup(t, g)
		return !r.Present || r.Health == domain.HealthGreen
	})
	if team == nil {
		t.Error("scenario 5: no Active team Goal shows Rolled-up Health Green, or none")
	}
	leadership := []string{admin, "ceo@example.com", "cto@example.com", "cpo@example.com"}
	if o.find((*e2eGoal).active, func(g *e2eGoal) bool {
		return !g.Owner.Departed && !lead(g.Owner) && !slices.Contains(leadership, g.Owner.Email) &&
			slices.ContainsFunc(g.parents, func(p *e2eGoal) bool { return lead(p.Owner) })
	}) == nil {
		t.Error("scenario 5: no project Owner, contributing an Active Goal to a lead's Goal (projectOwners)")
	}
	fields, err := o.h.Service.ListFields(context.Background())
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(fields) > 0 {
		t.Errorf("scenario 5: the seed has %d Fields, want none", len(fields))
	}
}

// e2eIsolation is isolation.spec.ts's noChangeGoal: a Goal its Owner can
// press No change on, whose latest Check-in is more than 5 minutes old.
func e2eIsolation(t *testing.T, o *e2eOrg) {
	g := o.find((*e2eGoal).active, func(g *e2eGoal) bool {
		return !g.Owner.Departed && len(g.checkins) > 0 && len(g.children) == 0 && !o.overdueMilestone(g)
	})
	if g == nil {
		t.Fatal("isolation: no Goal its Owner can press No change on (noChangeGoal)")
	}
	if o.now.Sub(g.checkins[0].CreatedAt) < 5*time.Minute {
		t.Errorf("isolation: Goal %q was checked in %v before the app started, want more than 5 minutes",
			g.Title, o.now.Sub(g.checkins[0].CreatedAt))
	}
}
