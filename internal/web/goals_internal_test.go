package web

import (
	"slices"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// goalRowAt is a Goal list row for a Goal titled title in lifecycle whose
// latest Check-in set health, or that has none when health is "".
func goalRowAt(title, lifecycle, health string) goalRow {
	row := goalRow{GoalWithValues: domain.GoalWithValues{Goal: domain.Goal{Title: title, Lifecycle: lifecycle}}}
	if health != "" {
		row.Latest = &domain.Checkin{Health: health}
		row.Health = health
	}
	return row
}

// healthGroupsOf is the Health groups of rows, once sorted as the Goal list
// sorts them: each group's name and its rows' titles.
func healthGroupsOf(rows []goalRow) (names []string, titles [][]string) {
	sortGoalRows(rows)
	for _, g := range groupGoalRowsByHealth(rows) {
		names = append(names, g.name())
		var ts []string
		for _, r := range g.Rows {
			ts = append(ts, r.Goal.Title)
		}
		titles = append(titles, ts)
	}
	return names, titles
}

// A Goal with no Health that needs a decision or an update — Ownerless, Check-in
// overdue or Milestone overdue — is surfaced as prominently as Red
// (CONTEXT.md), so the whole No Health group leads the list, above Red, with
// that Goal first in it.
func TestGoalHealthGroupsLeadWithNoHealthWhenItHoldsAProblem(t *testing.T) {
	t.Parallel()

	for name, mark := range map[string]func(*goalRow){
		"Check-in overdue":  func(r *goalRow) { r.Freshness.CheckinOverdue = true },
		"Ownerless":         func(r *goalRow) { r.Goal.Ownerless = true },
		"Milestone overdue": func(r *goalRow) { r.Freshness.MilestoneOverdue = true },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			problem := goalRowAt("Zulu problem", domain.LifecycleActive, "")
			mark(&problem)
			names, titles := healthGroupsOf([]goalRow{
				goalRowAt("Alpha idea", domain.LifecycleProposed, ""),
				goalRowAt("Fine", domain.LifecycleActive, domain.HealthGreen),
				problem,
				goalRowAt("Wobbly", domain.LifecycleActive, domain.HealthYellow),
				goalRowAt("On fire", domain.LifecycleActive, domain.HealthRed),
			})
			if want := []string{"No Health", "Red", "Yellow", "Green"}; !slices.Equal(names, want) {
				t.Fatalf("groups = %q, want %q", names, want)
			}
			if want := []string{"Zulu problem", "Alpha idea"}; !slices.Equal(titles[0], want) {
				t.Errorf("No Health rows = %q, want %q", titles[0], want)
			}
		})
	}
}

// Goals with no Health that need nothing yet, like a Proposed one, leave the No
// Health group last, after Green.
func TestGoalHealthGroupsKeepQuietNoHealthLast(t *testing.T) {
	t.Parallel()

	names, _ := healthGroupsOf([]goalRow{
		goalRowAt("Alpha idea", domain.LifecycleProposed, ""),
		goalRowAt("Beta idea", domain.LifecycleProposed, ""),
		goalRowAt("Fine", domain.LifecycleActive, domain.HealthGreen),
		goalRowAt("Wobbly", domain.LifecycleActive, domain.HealthYellow),
		goalRowAt("On fire", domain.LifecycleActive, domain.HealthRed),
	})
	if want := []string{"Red", "Yellow", "Green", "No Health"}; !slices.Equal(names, want) {
		t.Errorf("groups = %q, want %q", names, want)
	}
}
