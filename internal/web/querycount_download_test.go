package web_test

import (
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The CSV download writes the same file for the seeded Goals as it did before
// its reads were batched (#450).
func TestGoalsDownloadWritesAsGolden(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	seedQueryCountFixture(t, h, 50)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, queryCountAdmin)

	assertGolden(t, "goals-download.golden.csv", getBody(t, client, ts.URL+"/goals/download?org=all"))
}

// Downloading the Goals as CSV runs as many queries for 50 Goals as for 10:
// each Goal's values, Fields, Milestones, Metrics and parents are read for all
// the Goals at once, not once per Goal (#450).
func TestGoalsDownloadQueriesDontGrowWithGoals(t *testing.T) {
	t.Parallel()
	countFor := func(n int) int64 {
		h := testsupport.New(t, queryCountAdmin)
		seedQueryCountFixture(t, h, n)
		ts, _, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, queryCountAdmin)
		queries.take()
		getBody(t, client, ts.URL+"/goals/download?org=all")
		return int64(len(queries.take()))
	}
	if ten, fifty := countFor(10), countFor(50); ten != fifty {
		t.Errorf("the download ran %d queries for 10 Goals and %d for 50; want the same", ten, fifty)
	}
}
