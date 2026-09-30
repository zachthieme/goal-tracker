package seed_test

import (
	"context"
	"testing"

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

// The seed builds a realistic org: about 50 Goals spread across several teams
// through a Team Dimension, arranged three levels deep in the goal graph.
func TestSeedBuildsAnOrgAcrossTeamsAndThreeLevels(t *testing.T) {
	h := seeded(t)
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
