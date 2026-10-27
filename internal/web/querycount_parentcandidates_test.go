package web_test

import (
	"fmt"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Searching for a Goal's parents, GET /goals/search?goal=<id>, leaves out
// the Goals that contribute to it with as many queries for 50 Goals as for
// 10: it reads the Goal's descendants once, not walking up from every
// candidate, so ListParentGoals runs only for the Goal's own parents (#638).
func TestParentCandidateSearchQueriesDontGrowWithGoals(t *testing.T) {
	t.Parallel()
	countFor := func(n int) int {
		h := testsupport.New(t, queryCountAdmin)
		fx := seedGoalPageFixture(t, h, n)
		ts, _, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, queryCountAdmin)
		queries.take()
		getBody(t, client, fmt.Sprintf("%s/goals/search?goal=%d&q=", ts.URL, fx.red.ID))
		ran := queries.take()
		if times := countQueryNames(ran)["ListParentGoals"]; times > 1 {
			t.Errorf("searching %d Goals for Goal 04's parents ran ListParentGoals %d times; want it at most once", n, times)
		}
		return len(ran)
	}
	if ten, fifty := countFor(10), countFor(50); ten != fifty {
		t.Errorf("searching for parents ran %d queries for 10 Goals and %d for 50; want the same", ten, fifty)
	}
}
