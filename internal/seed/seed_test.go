package seed_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/seed"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const admin = "admin@example.com"

// seeded runs the seed into a fresh harness database and returns the harness.
func seeded(t *testing.T) *testsupport.Harness {
	t.Helper()
	h := testsupport.New(t, admin)
	if _, err := seed.Run(context.Background(), h.Service, h.Clock, seed.Options{Seed: seed.DefaultSeed, Admin: admin}); err != nil {
		t.Fatalf("seed.Run: %v", err)
	}
	return h
}

func listGoals(t *testing.T, h *testsupport.Harness) []domain.Goal {
	t.Helper()
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	return goals
}

// depth is how many levels a Goal sits below its highest ancestor: a Goal that
// contributes to nothing is at depth 1.
func depth(t *testing.T, h *testsupport.Harness, goalID int64) int {
	t.Helper()
	parents, err := h.Service.ParentsOf(context.Background(), goalID)
	if err != nil {
		t.Fatalf("ParentsOf: %v", err)
	}
	deepest := 0
	for _, p := range parents {
		deepest = max(deepest, depth(t, h, p.ID))
	}
	return deepest + 1
}

// TestSeededOrg seeds one org and checks it from each angle a demo needs. The
// subtests share the org because seeding writes weeks of Check-ins and takes a
// few seconds.
func TestSeededOrg(t *testing.T) {
	h := seeded(t)
	t.Run("about 50 Goals across teams and three levels", func(t *testing.T) { orgAcrossTeamsAndThreeLevels(t, h) })
	t.Run("Check-in history with mixed Health", func(t *testing.T) { checkinHistoryWithMixedHealth(t, h) })
	t.Run("Date Slips", func(t *testing.T) { dateSlips(t, h) })
	t.Run("Milestone Churn", func(t *testing.T) { milestoneChurn(t, h) })
	t.Run("Stale and Unaligned Goals", func(t *testing.T) { staleAndUnaligned(t, h) })
	t.Run("Lifecycle changes", func(t *testing.T) { lifecycleChanges(t, h) })
	t.Run("every person has a Name", func(t *testing.T) { everyPersonNamed(t, h) })
	t.Run("only into a fresh database", func(t *testing.T) { onlyIntoAFreshDatabase(t, h) })
	t.Run("deterministic", func(t *testing.T) { deterministic(t, h) })
}

// The seed builds a realistic org: about 50 Goals spread across several teams
// through a Team Dimension, arranged three levels deep in the goal graph.
func orgAcrossTeamsAndThreeLevels(t *testing.T, h *testsupport.Harness) {
	ctx := context.Background()

	goals := listGoals(t, h)
	if len(goals) < 45 || len(goals) > 55 {
		t.Fatalf("want about 50 Goals, got %d", len(goals))
	}

	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	var team domain.Dimension
	for _, d := range dims {
		if d.Name == "Team" {
			team = d
		}
	}
	if team.ID == 0 {
		t.Fatalf("want a Team Dimension, got %+v", dims)
	}

	teamsInUse := map[string]int{}
	levels := map[int]int{}
	for _, g := range goals {
		values, err := h.Service.GoalValues(ctx, g.ID)
		if err != nil {
			t.Fatalf("GoalValues: %v", err)
		}
		for _, v := range values {
			if v.DimensionID == team.ID {
				teamsInUse[v.Value]++
			}
		}
		levels[depth(t, h, g.ID)]++
	}
	if len(teamsInUse) < 4 {
		t.Errorf("want Goals across several teams, got %v", teamsInUse)
	}
	if levels[1] == 0 || levels[2] == 0 || levels[3] == 0 || len(levels) != 3 {
		t.Errorf("want Goals on exactly three levels of the graph, got %v", levels)
	}
}

// Owners have been checking in for weeks: most Goals are Active with a history
// of Check-ins spanning the seeded weeks, and their current Health is a mix of
// Green, Yellow, and Red.
func checkinHistoryWithMixedHealth(t *testing.T, h *testsupport.Harness) {
	ctx := context.Background()
	end := h.Clock.Now()

	health := map[string]int{}
	var active, longHistory int
	for _, g := range listGoals(t, h) {
		if g.Lifecycle != domain.LifecycleActive {
			continue
		}
		active++
		checkins, err := h.Service.ListCheckins(ctx, g.ID)
		if err != nil {
			t.Fatalf("ListCheckins: %v", err)
		}
		if len(checkins) == 0 {
			t.Errorf("Active Goal %q has no Check-ins", g.Title)
			continue
		}
		health[checkins[0].Health]++
		oldest := checkins[len(checkins)-1].CreatedAt
		if end.Sub(oldest) >= 8*7*24*time.Hour && len(checkins) >= 4 {
			longHistory++
		}
		if checkins[0].CreatedAt.After(end) {
			t.Errorf("Goal %q has a Check-in after the seed's end %v", g.Title, end)
		}
	}
	if active < 35 {
		t.Errorf("want most Goals Active, got %d", active)
	}
	if longHistory < active/2 {
		t.Errorf("want most Active Goals to have weeks of Check-in history, got %d of %d", longHistory, active)
	}
	for _, want := range []string{domain.HealthGreen, domain.HealthYellow, domain.HealthRed} {
		if health[want] == 0 {
			t.Errorf("want some Goals currently %s, got %v", want, health)
		}
	}
}

// Some Goals' delivery dates have slipped and some Milestones' dates have
// moved, each Date Slip moving the date later with a reason, and the Goal's
// delivery date now reads as the latest slip's new date.
func dateSlips(t *testing.T, h *testsupport.Harness) {
	ctx := context.Background()
	var deliverySlipped, milestoneSlipped int
	for _, g := range listGoals(t, h) {
		slips, err := h.Service.ListDateSlips(ctx, g.ID)
		if err != nil {
			t.Fatalf("ListDateSlips: %v", err)
		}
		var lastDelivery time.Time
		var delivery, milestone bool
		for _, s := range slips {
			if s.Reason == "" || !s.NewDate.After(s.OldDate) {
				t.Errorf("Goal %q: want each Date Slip later and with a reason, got %+v", g.Title, s)
			}
			if s.MilestoneID == 0 {
				delivery, lastDelivery = true, s.NewDate
			} else {
				milestone = true
			}
		}
		if delivery {
			deliverySlipped++
			if !g.DeliveryDate.Equal(lastDelivery) {
				t.Errorf("Goal %q: delivery date %v, want the last slip's %v", g.Title, g.DeliveryDate, lastDelivery)
			}
		}
		if milestone {
			milestoneSlipped++
		}
	}
	if deliverySlipped < 3 {
		t.Errorf("want several Goals whose delivery date slipped, got %d", deliverySlipped)
	}
	if milestoneSlipped < 3 {
		t.Errorf("want several Goals with a Milestone date slip, got %d", milestoneSlipped)
	}
}

// Some Goals have Milestone Churn: Milestones added in a Check-in after the Goal
// became Active, and Milestones removed with a reason.
func milestoneChurn(t *testing.T, h *testsupport.Harness) {
	ctx := context.Background()
	var churned, removed int
	for _, g := range listGoals(t, h) {
		churn, err := h.Service.MilestoneChurn(ctx, g.ID)
		if err != nil {
			t.Fatalf("MilestoneChurn: %v", err)
		}
		if churn > 0 {
			churned++
		}
		milestones, err := h.Service.ListMilestones(ctx, g.ID)
		if err != nil {
			t.Fatalf("ListMilestones: %v", err)
		}
		for _, m := range milestones {
			if m.Status == domain.MilestoneRemoved {
				removed++
				if m.RemovedReason == "" {
					t.Errorf("Goal %q: removed Milestone %q has no reason", g.Title, m.Name)
				}
			}
		}
	}
	if churned < 3 {
		t.Errorf("want several Goals with Milestone Churn, got %d", churned)
	}
	if removed == 0 {
		t.Errorf("want some Milestones removed, got none")
	}
}

// Among the Active Goals, a few are Stale — their last Check-in is older than
// their cadence — while most are up to date. The org outcomes are Top-level
// Goals, so the Unaligned list the signals page shows is exactly the five side
// projects.
func staleAndUnaligned(t *testing.T, h *testsupport.Harness) {
	ctx := context.Background()
	end := h.Clock.Now()
	var active, stale int
	for _, g := range listGoals(t, h) {
		if g.Lifecycle != domain.LifecycleActive {
			continue
		}
		active++
		latest, ok, err := h.Service.LatestCheckin(ctx, g.ID)
		if err != nil {
			t.Fatalf("LatestCheckin: %v", err)
		}
		if ok && end.Sub(latest.CreatedAt) > time.Duration(g.CadenceDays)*24*time.Hour {
			stale++
		}
	}
	if stale < 3 || stale > active/4 {
		t.Errorf("want a few Stale Goals among %d Active, got %d", active, stale)
	}

	topLevel := map[string]bool{}
	for _, g := range listGoals(t, h) {
		if g.TopLevel {
			topLevel[g.Title] = true
		}
	}
	wantTopLevel := []string{
		"Be trustworthy at scale",
		"Delight customers in their first week",
		"Grow net revenue retention",
	}
	if got := sortedKeys(topLevel); !slices.Equal(got, wantTopLevel) {
		t.Errorf("Top-level Goals = %q, want the org outcomes %q", got, wantTopLevel)
	}

	signals, err := h.Service.GraphSignals(ctx)
	if err != nil {
		t.Fatalf("GraphSignals: %v", err)
	}
	unaligned := map[string]bool{}
	for _, g := range signals.Unaligned {
		unaligned[g.Title] = true
	}
	wantUnaligned := []string{
		"Evaluate ARM build agents",
		"Prototype a natural-language query assistant",
		"Refresh brand illustrations",
		"Spike: instant payouts",
		"Tablet layout exploration",
	}
	if got := sortedKeys(unaligned); len(signals.Unaligned) != len(got) || !slices.Equal(got, wantUnaligned) {
		t.Errorf("Unaligned Goals = %q, want exactly the side projects %q", got, wantUnaligned)
	}
}

func sortedKeys(set map[string]bool) []string {
	return slices.Sorted(maps.Keys(set))
}

// Not every Goal is Active: some were finished Done with an outcome, some put
// On Hold or Cancelled with a reason, and a couple are still Proposed.
func lifecycleChanges(t *testing.T, h *testsupport.Harness) {
	ctx := context.Background()
	lifecycles := map[string]int{}
	for _, g := range listGoals(t, h) {
		lifecycles[g.Lifecycle]++
		if g.Lifecycle == domain.LifecycleActive || g.Lifecycle == domain.LifecycleProposed {
			continue
		}
		latest, ok, err := h.Service.LatestCheckin(ctx, g.ID)
		if err != nil || !ok {
			t.Fatalf("LatestCheckin(%q): ok=%v err=%v", g.Title, ok, err)
		}
		change := latest.LifecycleChange
		if change.To != g.Lifecycle {
			t.Errorf("Goal %q is %s but its latest Check-in moved it to %q", g.Title, g.Lifecycle, change.To)
		}
		if g.Lifecycle == domain.LifecycleDone && change.Outcome == "" {
			t.Errorf("Done Goal %q has no outcome", g.Title)
		}
		if g.Lifecycle != domain.LifecycleDone && change.Reason == "" {
			t.Errorf("%s Goal %q has no reason", g.Lifecycle, g.Title)
		}
	}
	for _, want := range []string{domain.LifecycleProposed, domain.LifecycleDone, domain.LifecycleOnHold, domain.LifecycleCancelled} {
		if lifecycles[want] == 0 {
			t.Errorf("want some %s Goals, got %v", want, lifecycles)
		}
	}
}

// Every seeded person has a realistic Name (CONTEXT.md: Name); one whose email
// spells out a name has that name, ada.okafor@example.com being Ada Okafor.
func everyPersonNamed(t *testing.T, h *testsupport.Harness) {
	people := map[string]domain.Account{}
	for _, g := range listGoals(t, h) {
		people[g.Owner.Email] = g.Owner
	}
	people[admin] = h.SignIn(admin)
	if ada, ok := people["ada.okafor@example.com"]; !ok || ada.Name != "Ada Okafor" {
		t.Errorf("ada.okafor@example.com is named %q, want Ada Okafor", ada.Name)
	}
	for addr, p := range people {
		if p.Name == "" {
			t.Errorf("%s has no Name", addr)
			continue
		}
		local, _, _ := strings.Cut(addr, "@")
		if spelled := strings.ToLower(strings.ReplaceAll(p.Name, " ", ".")); strings.Contains(local, ".") && spelled != local {
			t.Errorf("%s is named %q, which doesn't match the email", addr, p.Name)
		}
	}
}

// Seeding a database that already has Goals is refused, and leaves it as it
// was: the seed builds an org into a fresh database only.
func onlyIntoAFreshDatabase(t *testing.T, h *testsupport.Harness) {
	before := len(listGoals(t, h))
	_, err := seed.Run(context.Background(), h.Service, h.Clock, seed.Options{Seed: seed.DefaultSeed, Admin: admin})
	if !errors.Is(err, seed.ErrNotFresh) {
		t.Fatalf("seeding a seeded database: want ErrNotFresh, got %v", err)
	}
	if after := len(listGoals(t, h)); after != before {
		t.Errorf("refused seed changed the Goal count from %d to %d", before, after)
	}
}

// The same seed rerun into another fresh database, ending at the same time,
// builds the same org, Check-in for Check-in.
func deterministic(t *testing.T, h *testsupport.Harness) {
	again := seeded(t)
	if got, want := fingerprint(t, again), fingerprint(t, h); got != want {
		t.Errorf("reseeding built a different org:\n got: %s\nwant: %s", got, want)
	}
}

// fingerprint renders every Goal with its Lifecycle, dates, Top-level mark,
// parents, and full Check-in, Date Slip, and Milestone history.
func fingerprint(t *testing.T, h *testsupport.Harness) string {
	t.Helper()
	ctx := context.Background()
	var b strings.Builder
	for _, g := range listGoals(t, h) {
		fmt.Fprintf(&b, "%d %q owner=%s (%s) %s %s due=%s cadence=%d top=%t\n", g.ID, g.Title, g.Owner.Email, g.Owner.Name, g.Lifecycle, g.Kind, g.DeliveryDate.Format(time.DateOnly), g.CadenceDays, g.TopLevel)
		parents, _ := h.Service.ParentsOf(ctx, g.ID)
		for _, p := range parents {
			fmt.Fprintf(&b, "  parent %d\n", p.ID)
		}
		checkins, _ := h.Service.ListCheckins(ctx, g.ID)
		for _, c := range checkins {
			fmt.Fprintf(&b, "  checkin %s %s %q %q %s %+v\n", c.CreatedAt.Format(time.RFC3339), c.Health, c.Status, c.PathToGreen, c.PathTargetDate.Format(time.DateOnly), c.LifecycleChange)
		}
		slips, _ := h.Service.ListDateSlips(ctx, g.ID)
		for _, s := range slips {
			fmt.Fprintf(&b, "  slip %d %s->%s %q\n", s.MilestoneID, s.OldDate.Format(time.DateOnly), s.NewDate.Format(time.DateOnly), s.Reason)
		}
		milestones, _ := h.Service.ListMilestones(ctx, g.ID)
		for _, m := range milestones {
			fmt.Fprintf(&b, "  milestone %q %s %s\n", m.Name, m.TargetDate.Format(time.DateOnly), m.Status)
		}
		metrics, _ := h.Service.ListMetrics(ctx, g.ID)
		for _, m := range metrics {
			readings, _ := h.Service.ListMetricReadings(ctx, m.ID)
			for _, r := range readings {
				fmt.Fprintf(&b, "  reading %q %v\n", m.Name, r.Value)
			}
		}
	}
	return b.String()
}
