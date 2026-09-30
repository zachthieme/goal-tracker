package seed_test

import (
	"context"
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
// their cadence — while most are up to date, and a few are Unaligned: they
// contribute to no other Goal, and aren't org outcomes that other Goals drive.
func staleAndUnaligned(t *testing.T, h *testsupport.Harness) {
	ctx := context.Background()
	end := h.Clock.Now()
	var active, stale, unaligned int
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
		parents, err := h.Service.ParentsOf(ctx, g.ID)
		if err != nil {
			t.Fatalf("ParentsOf: %v", err)
		}
		children, err := h.Service.ChildrenOf(ctx, g.ID)
		if err != nil {
			t.Fatalf("ChildrenOf: %v", err)
		}
		if len(parents) == 0 && len(children) == 0 {
			unaligned++
		}
	}
	if stale < 3 || stale > active/4 {
		t.Errorf("want a few Stale Goals among %d Active, got %d", active, stale)
	}
	if unaligned < 3 {
		t.Errorf("want a few Unaligned Goals, got %d", unaligned)
	}
}
