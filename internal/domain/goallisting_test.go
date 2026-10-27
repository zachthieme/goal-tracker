package domain_test

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// goalListings is GoalListings of goals, failing the test on error.
func goalListings(t *testing.T, h *testsupport.Harness, goals ...domain.Goal) map[int64]domain.GoalListing {
	t.Helper()
	listings, err := h.Service.GoalListings(context.Background(), goals)
	if err != nil {
		t.Fatalf("GoalListings: %v", err)
	}
	return listings
}

// Each Goal's listing carries its latest Check-in, none when it has none, and
// its current Health: the latest Check-in's while it is Active, and none
// while it is Proposed, Active with no Check-in or On Hold (CONTEXT.md:
// Health).
func TestGoalListingsCarryTheLatestCheckinAndCurrentHealth(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	cases := healthCases(h, sam)
	var goals []domain.Goal
	for _, c := range cases {
		goals = append(goals, c.goal)
	}

	listings := goalListings(t, h, goals...)
	for _, c := range cases {
		l, ok := listings[c.goal.ID]
		if !ok {
			t.Fatalf("GoalListings has no listing for %s", c.name)
		}
		if l.Health != c.health {
			t.Errorf("Health of %s = %q, want %q", c.name, l.Health, c.health)
		}
		latest, ok, err := h.Service.LatestCheckin(context.Background(), c.goal.ID)
		if err != nil {
			t.Fatalf("LatestCheckin of %s: %v", c.name, err)
		}
		switch {
		case !ok && l.Latest != nil:
			t.Errorf("Latest of %s = %+v, want none", c.name, *l.Latest)
		case ok && (l.Latest == nil || !reflect.DeepEqual(*l.Latest, latest)):
			t.Errorf("Latest of %s = %+v, want %+v", c.name, l.Latest, latest)
		}
	}
	if l := listings[cases[3].goal.ID]; l.Latest == nil || l.Latest.Status != "Slipping." {
		t.Errorf("Latest of %s = %+v, want the Yellow Check-in", cases[3].name, l.Latest)
	}
}

// Each Goal's listing carries the delivery dates its Date Slips moved it
// from, earliest first, leaving out its Milestones' slips and its
// back-to-Green date's, and none for a Goal whose delivery date never slipped
// (CONTEXT.md: Date Slip).
func TestGoalListingsCarryThePriorDueDates(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	slipped := slippedDueDateGoal(t, h, sam)
	steady := h.ActiveGoal(sam, "Ship billing", "People can't pay.")

	listings := goalListings(t, h, slipped, steady)
	want := []time.Time{time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)}
	if got := listings[slipped.ID].PriorDueDates; !slices.Equal(got, want) {
		t.Errorf("PriorDueDates of the slipped Goal = %v, want %v", got, want)
	}
	if got := listings[steady.ID].PriorDueDates; len(got) != 0 {
		t.Errorf("PriorDueDates of a Goal that never slipped = %v, want none", got)
	}
}

// Each Goal's listing carries its freshness and what it lacks, as
// FreshnessByGoal and IncompleteByGoal read them: here a Goal Check-in
// overdue and Incomplete beside one checked in on lately with its required
// value set (CONTEXT.md: Incomplete).
func TestGoalListingsCarryFreshnessAndIncompleteAsTheirReadersDo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(admin, "Team", "Platform")
	stale := h.ActiveGoal(sam, "Stale", "It matters.")
	fresh := h.ActiveGoal(sam, "Fresh", "It matters.")
	h.AssignGoalValue(fresh, team.Values[0])
	h.SetDimensionRequired(admin, team, true)
	h.Checkin(sam, stale.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(30 * 24 * time.Hour)
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})
	goals := []domain.Goal{stale, fresh}

	listings := goalListings(t, h, goals...)
	freshness, err := h.Service.FreshnessByGoal(ctx, goals)
	if err != nil {
		t.Fatalf("FreshnessByGoal: %v", err)
	}
	incomplete, err := h.Service.IncompleteByGoal(ctx, goals)
	if err != nil {
		t.Fatalf("IncompleteByGoal: %v", err)
	}
	if !listings[stale.ID].Freshness.CheckinOverdue || listings[fresh.ID].Freshness.CheckinOverdue {
		t.Errorf("Check-in overdue = %t for Stale and %t for Fresh, want only Stale",
			listings[stale.ID].Freshness.CheckinOverdue, listings[fresh.ID].Freshness.CheckinOverdue)
	}
	if want := []string{"Team"}; !slices.Equal(listings[stale.ID].Incomplete, want) || len(listings[fresh.ID].Incomplete) != 0 {
		t.Errorf("Incomplete = %q for Stale and %q for Fresh, want %q and none",
			listings[stale.ID].Incomplete, listings[fresh.ID].Incomplete, want)
	}
	for _, g := range goals {
		if got := listings[g.ID].Freshness; !reflect.DeepEqual(got, freshness[g.ID]) {
			t.Errorf("Freshness of %s = %+v, want FreshnessByGoal's %+v", g.Title, got, freshness[g.ID])
		}
		if got := listings[g.ID].Incomplete; !slices.Equal(got, incomplete[g.ID]) {
			t.Errorf("Incomplete of %s = %q, want IncompleteByGoal's %q", g.Title, got, incomplete[g.ID])
		}
	}
}
