package web_test

import (
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Home and Follow-ups render the same for the seeded Goals as they did before
// their reads were batched (#449).
func TestHomeAndFollowUpsRenderAsGolden(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	seedQueryCountFixture(t, h, 50)
	ts := newServer(t, h)

	assertGolden(t, "home-lead.golden.html", getBody(t, signInClient(t, ts.URL, "morgan@example.com"), ts.URL+"/home"))
	assertGolden(t, "home-owner.golden.html", getBody(t, signInClient(t, ts.URL, "priya@example.com"), ts.URL+"/home"))
	assertGolden(t, "follow-ups.golden.html", getBody(t, signInClient(t, ts.URL, queryCountAdmin), ts.URL+"/follow-ups"))
}

// Showing Home, to a lead with an Org or to an Owner, or Follow-ups runs as
// many queries for 50 Goals as for 10: their per-Goal reads are batched, not
// run once per Goal (#449).
func TestHomeAndFollowUpsQueriesDontGrowWithGoals(t *testing.T) {
	t.Parallel()
	// morgan@ manages two of the fixture's Owners; priya@ Owns a third of its
	// Goals.
	pages := []struct{ name, email, path string }{
		{"Home, as a lead with an Org", "morgan@example.com", "/home"},
		{"Home, as an Owner", "priya@example.com", "/home"},
		{"Follow-ups", queryCountAdmin, "/follow-ups"},
	}
	countFor := func(n int) []int64 {
		h := testsupport.New(t, queryCountAdmin)
		seedQueryCountFixture(t, h, n)
		ts, _, queries := newQueryCountingServer(t, h)
		counts := make([]int64, len(pages))
		for i, p := range pages {
			client := signInClient(t, ts.URL, p.email)
			queries.take()
			getBody(t, client, ts.URL+p.path)
			counts[i] = int64(len(queries.take()))
		}
		return counts
	}
	ten, fifty := countFor(10), countFor(50)
	for i, p := range pages {
		if ten[i] != fifty[i] {
			t.Errorf("%s ran %d queries for 10 Goals and %d for 50; want the same", p.name, ten[i], fifty[i])
		}
	}
}
