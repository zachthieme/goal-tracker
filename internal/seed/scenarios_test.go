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

// TestE2EScenarioSituations seeds the e2e suite's reference orgs, builds the
// manifest the specs read their Goals and actors from by role, as cmd/seed
// writes it for each template at e2eAppStart, and checks that every role a
// spec reads on that template has a pick, and that each pick is the situation
// its spec needs. So a seed change that breaks a scenario fails make test, not
// only make e2e; a failure names the role, or the rule that no longer holds.
func TestE2EScenarioSituations(t *testing.T) {
	t.Parallel()

	t.Run("default template", func(t *testing.T) {
		t.Parallel()
		e := seededE2EManifest(t, e2eDefaultEnd)
		t.Run("Check-in page specs", func(t *testing.T) { e2eCheckinPages(t, e) })
		t.Run("scenario 1 finding trouble", func(t *testing.T) { e2eScenario1(t, e) })
		t.Run("scenario 2 understanding trouble", func(t *testing.T) { e2eScenario2Understanding(t, e) })
		t.Run("scenario 2 watching Goals", func(t *testing.T) { e2eScenario2Watching(t, e) })
		t.Run("scenario 3 Delegate covers a Check-in overdue Goal", func(t *testing.T) { e2eScenario3Delegate(t, e) })
		t.Run("scenario 4 reporting bad news", func(t *testing.T) { e2eScenario4(t, e) })
		t.Run("scenario 5 stating a Goal", func(t *testing.T) { e2eScenario5(t, e) })
		t.Run("isolation", func(t *testing.T) { e2eIsolation(t, e) })
	})
	t.Run("due template", func(t *testing.T) {
		t.Parallel()
		e := seededE2EManifest(t, e2eDueEnd)
		t.Run("scenario 3 the week", func(t *testing.T) { e2eScenario3Week(t, e) })
		t.Run("phone width", func(t *testing.T) { e2ePhoneWidthDue(t, e) })
	})
}

// e2eManifest is a seeded org, its clock at e2eAppStart, and the manifest
// built over it.
type e2eManifest struct {
	h *testsupport.Harness
	m seed.Manifest
}

// seededE2EManifest seeds DefaultSeed into a fresh harness with the history
// ending at end, sets the clock to e2eAppStart, and builds the manifest there.
func seededE2EManifest(t *testing.T, end time.Time) *e2eManifest {
	t.Helper()
	ctx := context.Background()
	h := testsupport.New(t, admin)
	h.Clock.Set(end)
	if _, err := seed.Run(ctx, h.Service, h.Clock, seed.Options{Seed: seed.DefaultSeed, Admin: admin}); err != nil {
		t.Fatalf("seed.Run: %v", err)
	}
	h.Clock.Set(e2eAppStart)
	m, err := seed.BuildManifest(ctx, h.Service, e2eAppStart)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	return &e2eManifest{h: h, m: m}
}

// e2eRole is a manifest role a spec reads, and whether it has a pick.
type e2eRole struct {
	name   string
	picked bool
}

// requireRoles fails naming each of roles with no pick: null, or an empty
// list.
func requireRoles(t *testing.T, roles ...e2eRole) {
	t.Helper()
	var missing []string
	for _, r := range roles {
		if !r.picked {
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the manifest has no pick for %s", strings.Join(missing, ", "))
	}
}

// The domain's reads of a Goal, as its pages show them.

func (e *e2eManifest) goal(t *testing.T, id int64) domain.Goal {
	t.Helper()
	g, err := e.h.Service.ViewGoal(context.Background(), id)
	if err != nil {
		t.Fatalf("ViewGoal(%d): %v", id, err)
	}
	return g
}

// latest is the Goal's latest Check-in, the zero Check-in when it has none.
func (e *e2eManifest) latest(t *testing.T, id int64) domain.Checkin {
	t.Helper()
	cs, err := e.h.Service.ListCheckins(context.Background(), id)
	if err != nil {
		t.Fatalf("ListCheckins(%d): %v", id, err)
	}
	if len(cs) == 0 {
		return domain.Checkin{}
	}
	return cs[0]
}

func (e *e2eManifest) freshness(t *testing.T, id int64) domain.Freshness {
	t.Helper()
	f, err := e.h.Service.Freshness(context.Background(), id)
	if err != nil {
		t.Fatalf("Freshness(%d): %v", id, err)
	}
	return f
}

func (e *e2eManifest) rollup(t *testing.T, id int64) domain.RolledUpHealth {
	t.Helper()
	r, err := e.h.Service.RolledUpHealth(context.Background(), id)
	if err != nil {
		t.Fatalf("RolledUpHealth(%d): %v", id, err)
	}
	return r
}

// team is the Goal's Team value, "" when it has none.
func (e *e2eManifest) team(t *testing.T, id int64) string {
	t.Helper()
	ctx := context.Background()
	dims, err := e.h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	values, err := e.h.Service.GoalValues(ctx, id)
	if err != nil {
		t.Fatalf("GoalValues(%d): %v", id, err)
	}
	for _, d := range dims {
		for _, v := range values {
			if d.Name == "Team" && v.DimensionID == d.ID {
				return v.Value
			}
		}
	}
	return ""
}

// followUpReasons are the reasons Follow-ups flags the Goal for, sorted: its
// signals as the domain reads them. A Schedule conflict flags the child, not
// the parent, and an Active Goal's latest Check-in being Red or Yellow puts
// it under Not Green.
func (e *e2eManifest) followUpReasons(t *testing.T, id int64) []string {
	t.Helper()
	g, f := e.goal(t, id), e.freshness(t, id)
	s, err := e.h.Service.GoalSignals(context.Background(), id)
	if err != nil {
		t.Fatalf("GoalSignals(%d): %v", id, err)
	}
	active := g.Lifecycle == domain.LifecycleActive
	health := e.latest(t, id).Health
	var out []string
	for _, r := range []struct {
		reason string
		flags  bool
	}{
		{"Ownerless", g.Ownerless},
		{"Milestone overdue", f.MilestoneOverdue},
		{"Parent halted", len(s.HaltedParents) > 0},
		{"Path to Green overdue", f.PathToGreenOverdue},
		{"Red", active && health == domain.HealthRed},
		{"Schedule conflict", slices.ContainsFunc(s.ScheduleConflicts, func(c domain.ScheduleConflict) bool { return c.Child.ID == id })},
		{"Check-in overdue", f.CheckinOverdue},
		{"Unlinked", s.Unlinked},
		{"Yellow", active && health == domain.HealthYellow},
	} {
		if r.flags {
			out = append(out, r.reason)
		}
	}
	return out
}

// e2eCheckinPages is the Check-in and Goal page specs' Goals: quietGoal
// (highlight-editor, checkin-add-bar), draftlessGoal (draft-highlight-editor),
// metricGoal (checkin-submit-bar, checkin-add-bar) and metricParentGoal
// (phone-width).
func e2eCheckinPages(t *testing.T, e *e2eManifest) {
	m := e.m
	requireRoles(t,
		e2eRole{"quietGoal", m.QuietGoal != nil},
		e2eRole{"draftlessGoal", m.DraftlessGoal != nil},
		e2eRole{"metricGoal", m.MetricGoal != nil},
		e2eRole{"metricParentGoal", m.MetricParentGoal != nil},
	)
	if c := e.latest(t, m.QuietGoal.ID); c.Health != domain.HealthGreen {
		t.Errorf("quietGoal %q was last %q, want Green", m.QuietGoal.Title, c.Health)
	}
	if f := e.freshness(t, m.MetricGoal.ID); f.MilestoneOverdue {
		t.Errorf("metricGoal %q has an overdue Milestone", m.MetricGoal.Title)
	}
	if r := e.rollup(t, m.MetricParentGoal.ID); !r.Present {
		t.Errorf("metricParentGoal %q has no Rolled-up Health, want its children's", m.MetricParentGoal.Title)
	}
}

// e2eScenario1 is what 01-finding-trouble.spec.ts asserts on Follow-ups of the
// Goals the manifest picks for it.
func e2eScenario1(t *testing.T, e *e2eManifest) {
	r := e.m.FindingTrouble
	requireRoles(t,
		e2eRole{"findingTrouble.checkinOverdue", r.CheckinOverdue != nil},
		e2eRole{"findingTrouble.departing", r.Departing != nil},
		e2eRole{"findingTrouble.overdue", r.Overdue != nil},
		e2eRole{"findingTrouble.tiered", len(r.Tiered) == 2},
		e2eRole{"findingTrouble.unlinked", r.Unlinked != nil},
		e2eRole{"findingTrouble.twoReasons", r.TwoReasons != nil},
		e2eRole{"findingTrouble.ownerless", len(r.Ownerless) > 0},
		e2eRole{"findingTrouble.redWithChildren", r.RedWithChildren != nil},
		e2eRole{"findingTrouble.redSlipped", r.RedSlipped != nil},
		e2eRole{"findingTrouble.lifecycles", len(r.Lifecycles) > 0},
	)

	// Follow-ups lists Check-in overdue, Unlinked and Schedule conflict Goals.
	ctx := context.Background()
	fs, err := e.h.Service.FreshnessSignals(ctx)
	if err != nil {
		t.Fatalf("FreshnessSignals: %v", err)
	}
	gs, err := e.h.Service.GraphSignals(ctx)
	if err != nil {
		t.Fatalf("GraphSignals: %v", err)
	}
	if len(fs.CheckinOverdue) == 0 || len(gs.Unlinked) == 0 || len(gs.ScheduleConflicts) == 0 {
		t.Errorf("scenario 1: Follow-ups has %d Check-in overdue, %d Unlinked and %d Schedule conflict Goals, want some of each",
			len(fs.CheckinOverdue), len(gs.Unlinked), len(gs.ScheduleConflicts))
	}
	// Narrowed to Platform, Follow-ups has fewer rows than the whole org: some
	// flagged Goal is on another team.
	offPlatform := slices.ContainsFunc(fs.CheckinOverdue, func(f domain.GoalFreshness) bool { return e.team(t, f.Goal.ID) != "Platform" }) ||
		slices.ContainsFunc(gs.Unlinked, func(g domain.Goal) bool { return e.team(t, g.ID) != "Platform" })
	if !offPlatform {
		t.Error("scenario 1: no Check-in overdue or Unlinked Goal off Platform, so narrowing Follow-ups to Platform wouldn't narrow it")
	}
	// The departing Owner owns the Ownerless Goals, the departing one among
	// them.
	if !slices.ContainsFunc(r.Ownerless, func(g seed.ManifestGoal) bool { return g.ID == r.Departing.ID }) {
		t.Errorf("scenario 1: departing Goal %q isn't among its Owner's Ownerless Goals %+v", r.Departing.Title, r.Ownerless)
	}

	// checkinOverdue is under Waiting on a Check-in and not Placement and
	// dates. On the default seed it is Green with an overdue Milestone, so it
	// is marked Milestone overdue too, which Waiting on a Check-in also holds.
	if got := e.followUpReasons(t, r.CheckinOverdue.ID); !slices.Equal(got, []string{"Milestone overdue", "Check-in overdue"}) {
		t.Errorf("scenario 1: Check-in overdue Goal %q is on Follow-ups for %q, want Milestone overdue and Check-in overdue", r.CheckinOverdue.Title, got)
	}
	// unlinked is under Placement and dates and not Waiting on a Check-in,
	// even after the setup puts overdue past its Path to Green.
	if got := e.followUpReasons(t, r.Unlinked.ID); !slices.Equal(got, []string{"Unlinked"}) || r.Unlinked.ID == r.Overdue.ID {
		t.Errorf("scenario 1: Unlinked Goal %q is on Follow-ups for %q, or is the Goal the setup makes overdue (%t); want only Unlinked",
			r.Unlinked.Title, got, r.Unlinked.ID == r.Overdue.ID)
	}
	// twoReasons is one row with a chip for each of its two reasons, on its
	// Team.
	if got := e.followUpReasons(t, r.TwoReasons.ID); !slices.Equal(got, []string{"Schedule conflict", "Check-in overdue"}) {
		t.Errorf("scenario 1: two-reason Goal %q is on Follow-ups for %q, want Schedule conflict and Check-in overdue", r.TwoReasons.Title, got)
	}
	if team := e.team(t, r.TwoReasons.ID); team == "" || team != r.TwoReasons.Team {
		t.Errorf("scenario 1: two-reason Goal %q has Team %q, the manifest %q", r.TwoReasons.Title, team, r.TwoReasons.Team)
	}
	// redWithChildren is on Follow-ups under Not Green only: nothing but its
	// Health is wrong with it.
	if got := e.followUpReasons(t, r.RedWithChildren.ID); !slices.Equal(got, []string{"Red"}) {
		t.Errorf("scenario 1: Red Goal %q with Active children is on Follow-ups for %q, want only Red", r.RedWithChildren.Title, got)
	}
	// Step 6 reads goal-rollup-health on its page, which renders only when the
	// rollup is Present.
	if ru := e.rollup(t, r.RedWithChildren.ID); !ru.Present ||
		!slices.Contains([]string{domain.HealthRed, domain.HealthYellow, domain.HealthGreen}, ru.Health) {
		t.Errorf("scenario 1: Red Goal %q with Active children rolls up %+v, want a Rolled-up Health to show (step 6)",
			r.RedWithChildren.Title, ru)
	}
	// Each Red Goal shows the Owner's status and Path to Green.
	for _, g := range []*seed.ManifestGoal{r.RedWithChildren, r.RedSlipped} {
		if c := e.latest(t, g.ID); c.Health != domain.HealthRed || c.Status == "" || c.PathToGreen == "" {
			t.Errorf("scenario 1: Red Goal %q is %q, or has no status or Path to Green", g.Title, c.Health)
		}
	}
	for _, g := range r.Tiered {
		if e.team(t, g.ID) != "Platform" {
			t.Errorf("scenario 1: tiered Goal %q isn't on Platform", g.Title)
		}
	}
}

// e2eScenario2Understanding is 02-understanding-trouble.spec.ts's picks: the
// Platform Goals the Report is built over, and the Goals the month's changes
// happen to.
func e2eScenario2Understanding(t *testing.T, e *e2eManifest) {
	r := e.m.UnderstandingTrouble
	requireRoles(t,
		e2eRole{"understandingTrouble.platformReport", len(r.PlatformReport) > 0},
		e2eRole{"understandingTrouble.slipped", r.Slipped != nil},
		e2eRole{"understandingTrouble.turning", r.Turning != nil},
		e2eRole{"understandingTrouble.moved", r.Moved != nil},
		e2eRole{"accountIDs", len(e.m.AccountIDs) > 0},
	)
	if !slices.ContainsFunc(r.PlatformReport, func(g seed.ReportGoal) bool {
		return (g.Health == domain.HealthRed || g.Health == domain.HealthYellow) && g.PathToGreen != ""
	}) {
		t.Error("scenario 2 (understanding trouble): no Red or Yellow Platform Goal with a Path to Green, an exception in full")
	}
	// The seed writes no Highlights, so the curation lists only the scenario's.
	for _, g := range r.PlatformReport {
		hs, err := e.h.Service.ListHighlightsByGoal(context.Background(), g.ID)
		if err != nil {
			t.Fatalf("ListHighlightsByGoal: %v", err)
		}
		if len(hs) > 0 {
			t.Errorf("scenario 2 (understanding trouble): Platform Goal %q has %d seeded Highlights, want none", g.Title, len(hs))
		}
		if e.team(t, g.ID) != "Platform" {
			t.Errorf("scenario 2 (understanding trouble): Report Goal %q isn't on Platform", g.Title)
		}
	}
	if r.Slipped.Health != domain.HealthGreen || r.Slipped.DeliveryDate == "" {
		t.Errorf("scenario 2 (understanding trouble): slipped Goal %q is %q, due %q; want a Green Dated Goal",
			r.Slipped.Title, r.Slipped.Health, r.Slipped.DeliveryDate)
	}
	if ids := []int64{r.Slipped.ID, r.Turning.ID, r.Moved.ID}; ids[0] == ids[1] || ids[0] == ids[2] || ids[1] == ids[2] {
		t.Errorf("scenario 2 (understanding trouble): slipped, turning and moved are Goals %v, want three", ids)
	}
	for _, g := range []*seed.ReportGoal{r.Slipped, r.Turning} {
		if f := e.freshness(t, g.ID); f.CheckinOverdue || f.MilestoneOverdue {
			t.Errorf("scenario 2 (understanding trouble): %q can't be checked in on plainly: %+v", g.Title, f)
		}
		if _, ok := e.m.AccountIDs[g.Email]; !ok {
			t.Errorf("scenario 2 (understanding trouble): accountIDs has no %s", g.Email)
		}
	}
	if _, ok := e.m.AccountIDs["ceo@example.com"]; !ok {
		t.Error("scenario 2 (understanding trouble): accountIDs has no reader, ceo@example.com")
	}
}

// e2eScenario2Watching is 02-watching-goals.spec.ts's picks: the Growth Goals
// of the rule Report, and the hand-picked Goals.
func e2eScenario2Watching(t *testing.T, e *e2eManifest) {
	r := e.m.WatchingGoals
	p := r.Picked
	requireRoles(t,
		e2eRole{"watchingGoals.growthReport.inReport", len(r.GrowthReport.InReport) > 0},
		e2eRole{"watchingGoals.growthReport.finishedEarlier", len(r.GrowthReport.FinishedEarlier) > 0},
		e2eRole{"watchingGoals.picked.troubled", p.Troubled != nil},
		e2eRole{"watchingGoals.picked.slipped", p.Slipped != nil},
		e2eRole{"watchingGoals.picked.green", p.Green != nil},
		e2eRole{"watchingGoals.picked.parent", p.Parent != nil},
		e2eRole{"watchingGoals.picked.children", len(p.Children) > 0},
		e2eRole{"accountIDs growth-lead@example.com", e.m.AccountIDs["growth-lead@example.com"] != 0},
	)
	troubled := func(w *seed.WatchedGoal) bool {
		return w.Health == domain.HealthYellow || w.Health == domain.HealthRed
	}
	if !troubled(p.Slipped) || p.Slipped.SlippedFrom == "" {
		t.Errorf("scenario 2 (watching Goals): slipped Goal %+v isn't Yellow or Red with a slipped delivery date", p.Slipped)
	}
	if !troubled(p.Troubled) || p.Troubled.PathToGreen == "" {
		t.Errorf("scenario 2 (watching Goals): troubled Goal %+v isn't Yellow or Red with a Path to Green", p.Troubled)
	}
	if p.Green.Health != domain.HealthGreen {
		t.Errorf("scenario 2 (watching Goals): green Goal %+v isn't Green", p.Green)
	}
	picked := []int64{p.Troubled.ID, p.Slipped.ID, p.Green.ID, p.Parent.ID}
	for _, c := range p.Children {
		if slices.Contains(picked, c.ID) {
			t.Errorf("scenario 2 (watching Goals): the parent's child %q is picked too", c.Title)
		}
	}
	for _, id := range picked {
		if team := e.team(t, id); team == "" || team == "Growth" {
			t.Errorf("scenario 2 (watching Goals): picked Goal %d is on Team %q, want another than Growth", id, team)
		}
	}
}

// e2eScenario3Delegate is 03-weekly-checkin.spec.ts's second test, on the
// default template: a Check-in overdue Goal, and someone to delegate it to.
func e2eScenario3Delegate(t *testing.T, e *e2eManifest) {
	d := e.m.WeeklyCheckin.DelegateCover
	requireRoles(t,
		e2eRole{"weeklyCheckin.delegateCover.goal", d.Goal != nil},
		e2eRole{"weeklyCheckin.delegateCover.delegate", d.Delegate != nil},
	)
	if !e.freshness(t, d.Goal.ID).CheckinOverdue {
		t.Errorf("scenario 3: the Check-in overdue Goal %q isn't Check-in overdue on Follow-ups and Home", d.Goal.Title)
	}
	if d.Goal.Name == "" || d.Delegate.Name == "" || d.Delegate.Email == d.Goal.Email {
		t.Errorf("scenario 3: Owner %+v and Delegate %+v, want two named people", d.Goal, d.Delegate)
	}
}

// e2eScenario3Week is 03-weekly-checkin.spec.ts's first test, on the due
// template: the week's Owners, the first of whom Home lists two Check-ins due
// for, one on their Metric Goal.
func e2eScenario3Week(t *testing.T, e *e2eManifest) {
	owners := e.m.WeeklyCheckin.WeekOwners
	requireRoles(t, e2eRole{"weeklyCheckin.weekOwners", len(owners) > 0})
	ctx := context.Background()
	for _, w := range owners {
		account, err := e.h.Service.AccountByEmail(ctx, w.Email)
		if err != nil {
			t.Fatalf("AccountByEmail: %v", err)
		}
		home, err := e.h.Service.ActiveGoalsFor(ctx, account.ID)
		if err != nil {
			t.Fatalf("ActiveGoalsFor: %v", err)
		}
		var listed []domain.PersonalGoal
		for _, pg := range home {
			if pg.Freshness.DueBeforeNextReminder() {
				listed = append(listed, pg)
			}
		}
		var due []seed.WeekGoal
		for _, g := range w.Goals {
			if slices.ContainsFunc(listed, func(pg domain.PersonalGoal) bool { return pg.Goal.Title == g.Title }) {
				due = append(due, g)
			}
		}
		metricGoal := slices.IndexFunc(due, func(g seed.WeekGoal) bool { return g.Metric })
		if len(due) < 2 || metricGoal < 0 {
			continue
		}

		// The scenario's Priya: Home lists exactly the Owner's own due Goals,
		// each checked in before and not Check-in overdue, and no Requests.
		if len(listed) != len(due) {
			t.Errorf("scenario 3: %s's Home lists %d Check-ins due, %d of them their own", w.Email, len(listed), len(due))
		}
		for _, pg := range listed {
			if pg.Freshness.CheckinOverdue || !pg.CheckedIn {
				t.Errorf("scenario 3: %s's due Goal %q is Check-in overdue (%t) or has no Check-in to repeat (%t)",
					w.Email, pg.Goal.Title, pg.Freshness.CheckinOverdue, !pg.CheckedIn)
			}
		}
		links, err := e.h.Service.PendingLinkRequests(ctx, account.ID)
		if err != nil {
			t.Fatalf("PendingLinkRequests: %v", err)
		}
		handoffs, err := e.h.Service.PendingHandoffs(ctx, account.ID)
		if err != nil {
			t.Fatalf("PendingHandoffs: %v", err)
		}
		suggestions, err := e.h.Service.OpenParentSuggestionsFor(ctx, account.ID)
		if err != nil {
			t.Fatalf("OpenParentSuggestionsFor: %v", err)
		}
		if n := len(links) + len(handoffs) + len(suggestions); n > 0 {
			t.Errorf("scenario 3: %s has %d Requests on Home, want none", w.Email, n)
		}
		metrics, err := e.h.Service.ListMetrics(ctx, due[metricGoal].ID)
		if err != nil || len(metrics) == 0 {
			t.Fatalf("ListMetrics(%q) = %v, %v; want its Metric", due[metricGoal].Title, metrics, err)
		}
		readings, err := e.h.Service.ListMetricReadings(ctx, metrics[0].ID)
		if err != nil {
			t.Fatalf("ListMetricReadings: %v", err)
		}
		if len(readings) == 0 {
			t.Errorf("scenario 3: %s's Metric %q has no reading", w.Email, metrics[0].Name)
		}
		return
	}
	t.Error("scenario 3: no weekOwners Owner has two Check-ins due on Home, one on their Metric Goal")
}

// e2ePhoneWidthDue is phone-width.spec.ts's Goal on the due template: a 7-day
// Goal due on its Owner's Home, which offers No change.
func e2ePhoneWidthDue(t *testing.T, e *e2eManifest) {
	g := e.m.DueNoChangeGoal
	requireRoles(t, e2eRole{"dueNoChangeGoal", g != nil})
	if f := e.freshness(t, g.ID); !f.DueBeforeNextReminder() || f.CheckinOverdue {
		t.Errorf("phone width: dueNoChangeGoal %q isn't due on Home, or is Check-in overdue: %+v", g.Title, f)
	}
}

// e2eScenario4 is 04-reporting-bad-news.spec.ts's chain: a project, the team
// Goal it contributes to, and an org outcome showing Rolled-up Health Green.
func e2eScenario4(t *testing.T, e *e2eManifest) {
	c := e.m.BadNewsChain
	requireRoles(t, e2eRole{"badNewsChain", c != nil})
	if len(c.Project.Milestones) < 2 || c.Project.Team == "" || c.Project.DeliveryDate == "" {
		t.Errorf("scenario 4: project %+v needs two Planned Milestones ahead, a Team and a delivery date", c.Project)
	}
	for _, g := range []seed.BadNewsGoal{c.Project.BadNewsGoal, c.Team} {
		if h := e.latest(t, g.ID).Health; h != domain.HealthGreen {
			t.Errorf("scenario 4: %q is %q, want Green", g.Title, h)
		}
	}
	if c.Project.Email == c.Team.Email {
		t.Errorf("scenario 4: the project and the team Goal are both %s's", c.Team.Email)
	}
	// The org outcome's page shows Rolled-up Health Green before and after the
	// project turns Red.
	if r := e.rollup(t, c.OrgOutcome.ID); !r.Present || r.Health != domain.HealthGreen {
		t.Errorf("scenario 4: org outcome %q rolls up %+v, want Green", c.OrgOutcome.Title, r)
	}
}

// e2eScenario5 is 05-stating-a-goal.spec.ts's picks: a lead's team Goal
// rolling up Green or not at all, with a project Owner, and the first project
// Owner.
func e2eScenario5(t *testing.T, e *e2eManifest) {
	r := e.m.StatingAGoal
	requireRoles(t,
		e2eRole{"statingAGoal.teamGoals", len(r.TeamGoals) > 0},
		e2eRole{"statingAGoal.projectOwner", r.ProjectOwner != nil},
	)
	// The spec takes the first team Goal whose page shows Rolled-up Health
	// Green, or none, and its first project Owner.
	i := slices.IndexFunc(r.TeamGoals, func(g seed.TeamGoal) bool {
		ru := e.rollup(t, g.ID)
		return !ru.Present || ru.Health == domain.HealthGreen
	})
	if i < 0 {
		t.Fatal("scenario 5: no Active team Goal shows Rolled-up Health Green, or none")
	}
	team := r.TeamGoals[i]
	requireRoles(t, e2eRole{"statingAGoal.teamGoals[" + team.Title + "].projectOwners", len(team.ProjectOwners) > 0})
	if team.ProjectOwners[0].Email == team.Email {
		t.Errorf("scenario 5: team Goal %q's first project Owner is its own Owner", team.Title)
	}
	fields, err := e.h.Service.ListFields(context.Background())
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(fields) > 0 {
		t.Errorf("scenario 5: the seed has %d Fields, want none", len(fields))
	}
}

// e2eIsolation is isolation.spec.ts's noChangeGoal: a Goal its Owner can
// press No change on, whose latest Check-in is more than 5 minutes old.
func e2eIsolation(t *testing.T, e *e2eManifest) {
	g := e.m.NoChangeGoal
	requireRoles(t, e2eRole{"noChangeGoal", g != nil})
	if f := e.freshness(t, g.ID); f.MilestoneOverdue || f.PathToGreenOverdue {
		t.Errorf("isolation: noChangeGoal %q would refuse No change: %+v", g.Title, f)
	}
	if age := e2eAppStart.Sub(e.latest(t, g.ID).CreatedAt); age < 5*time.Minute {
		t.Errorf("isolation: Goal %q was checked in %v before the app started, want more than 5 minutes", g.Title, age)
	}
}
