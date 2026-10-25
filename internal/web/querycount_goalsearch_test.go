package web_test

import (
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Contributes to search, GET /goals/search, runs as many queries for 50
// Goals as for 10, its matches' latest Check-ins read at most once, not once
// per match (#499).
func TestGoalSearchQueriesDontGrowWithGoals(t *testing.T) {
	t.Parallel()
	countFor := func(n int) int {
		h := testsupport.New(t, queryCountAdmin)
		seedGoalPageFixture(t, h, n)
		ts, _, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, queryCountAdmin)
		queries.take()
		getBody(t, client, ts.URL+"/goals/search?q=")
		ran := queries.take()
		if times := countQueryNames(ran)["ListLatestCheckinsForGoals"]; times > 1 {
			t.Errorf("searching %d Goals ran ListLatestCheckinsForGoals %d times; want it at most once", n, times)
		}
		return len(ran)
	}
	if ten, fifty := countFor(10), countFor(50); ten != fifty {
		t.Errorf("searching ran %d queries for 10 Goals and %d for 50; want the same", ten, fifty)
	}
}
