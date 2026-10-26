package web_test

import (
	"context"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// weeklyDigestsMaxQueries is the most statements Service.WeeklyDigests may run:
// every Goal, every accepted link and every pending link request, then the
// children's Check-ins and Date Slips, and their Freshness, which reads their
// latest Check-ins and, for a Green child, its Milestones, each once.
const weeklyDigestsMaxQueries = 7

// The weekly digests' loader runs as many queries for 50 Goals as for 10, no
// more than weeklyDigestsMaxQueries, and none twice: a child is read once
// however many parents it has (#604).
func TestWeeklyDigestsQueriesDontGrowWithGoals(t *testing.T) {
	t.Parallel()
	countFor := func(n int) int {
		h := testsupport.New(t, queryCountAdmin)
		seedQueryCountFixture(t, h, n)
		_, svc, queries := newQueryCountingServer(t, h)
		queries.take()
		digests, err := svc.WeeklyDigests(context.Background())
		if err != nil {
			t.Fatalf("WeeklyDigests: %v", err)
		}
		if len(digests) == 0 {
			t.Fatalf("WeeklyDigests for %d Goals made no digest; want the fixture's children reported", n)
		}
		ran := queries.take()
		for query, times := range countStatements(ran) {
			if times > 1 {
				t.Errorf("WeeklyDigests for %d Goals ran %q %d times; want it once", n, query, times)
			}
		}
		if len(ran) > weeklyDigestsMaxQueries {
			t.Errorf("WeeklyDigests for %d Goals ran %d queries; want at most %d", n, len(ran), weeklyDigestsMaxQueries)
		}
		return len(ran)
	}
	if ten, fifty := countFor(10), countFor(50); ten != fifty {
		t.Errorf("WeeklyDigests ran %d queries for 10 Goals and %d for 50; want the same", ten, fifty)
	}
}
