package web_test

import (
	"fmt"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Report builder, new and editing a saved definition, runs as many
// statements for 50 Goals as for 10, and no more than it does now (#652).
// Some run more than once:
//   - the builder selects its Goals twice, once for what Also include, Leave
//     out and Picked offer and once for the rail's matches, so each read
//     selecting them runs twice;
//   - the Goals are listed once by the builder and once by each selection,
//     and once more on a new builder, whose starting rule checks for the
//     author's own Goals;
//   - people are looked up one at a time: the session's viewer, each
//     Manager the Owner's org offers, and on a new builder the person its
//     starting Owner rule names, or on an edit the editor whose right to edit
//     is checked.
//
// No other statement runs twice.
func TestReportBuilderPagesRunTheStatementsTheyDo(t *testing.T) {
	t.Parallel()
	// reportBuilderSelection is why the reads selecting the builder's Goals
	// run twice.
	const reportBuilderSelection = "the builder selects its Goals twice: for what its Goal lists offer, and for the rail's matches"
	type reportBuilderReread struct {
		times int
		why   string
	}
	selections := map[string]reportBuilderReread{
		"LatestReportPublication":    {2, reportBuilderSelection},
		"ListLatestCheckinsForGoals": {2, reportBuilderSelection},
		"ListAllGoalValues":          {2, reportBuilderSelection},
		"ListCheckinsForGoals":       {2, reportBuilderSelection},
	}
	withRereads := func(more map[string]reportBuilderReread) map[string]reportBuilderReread {
		out := map[string]reportBuilderReread{}
		for _, m := range []map[string]reportBuilderReread{selections, more} {
			for name, r := range m {
				out[name] = r
			}
		}
		return out
	}
	pages := []struct {
		name string
		path func(fx queryCountFixture) string
		// max is the statements the page runs: the session's 2 and the
		// sidebar's 11, and the builder's own.
		max     int
		rereads map[string]reportBuilderReread
	}{
		{
			// The builder's 19: the starting rule's check for anyone under
			// the author and for their own Goals, the Goals, Fields,
			// Dimensions and their values, the Managers and each one, the
			// person the starting rule names, and two selections of 5.
			"/reports/new", func(queryCountFixture) string { return "/reports/new" }, 32,
			withRereads(map[string]reportBuilderReread{
				"ListGoals":  {4, "listed for the starting rule's check of the author's own Goals, by the builder and by each selection"},
				"GetAccount": {3, "people looked up one at a time: the session's viewer, the Owner's org's Manager and the person the starting Owner rule names"},
			}),
		},
		{
			// The builder's 23: the definition with its rules, their values,
			// its Goals, Fields and Report Delegates, the editor, the Goals,
			// Fields, Dimensions and their values, the Managers and each one,
			// and two selections of 5.
			"a definition's edit page", func(fx queryCountFixture) string { return fmt.Sprintf("/reports/%d/edit", fx.report.ID) }, 36,
			withRereads(map[string]reportBuilderReread{
				"ListGoals":  {3, "listed by the builder and by each selection"},
				"GetAccount": {3, "people looked up one at a time: the session's viewer, the editor whose right to edit is checked and the Owner's org's Manager"},
			}),
		},
	}

	countFor := func(n int) map[string]int {
		h := testsupport.New(t, queryCountAdmin)
		fx := seedQueryCountFixture(t, h, n)
		ts, _, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, queryCountAdmin)
		out := map[string]int{}
		for _, p := range pages {
			queries.take()
			getPage(t, client, ts.URL+p.path(fx))
			ran := queries.take()
			if len(ran) > p.max {
				t.Errorf("%s ran %d statements for %d Goals; want at most %d", p.name, len(ran), n, p.max)
			}
			for name, times := range countQueryNames(ran) {
				r, ok := p.rereads[name]
				switch {
				case !ok && times > 1:
					t.Errorf("%s ran %s %d times for %d Goals; want it once", p.name, name, times, n)
				case ok && times > r.times:
					t.Errorf("%s ran %s %d times for %d Goals; want at most %d: %s", p.name, name, times, n, r.times, r.why)
				}
			}
			out[p.name] = len(ran)
		}
		return out
	}
	ten, fifty := countFor(10), countFor(50)
	for _, p := range pages {
		if ten[p.name] != fifty[p.name] {
			t.Errorf("%s ran %d statements for 10 Goals and %d for 50; want the same", p.name, ten[p.name], fifty[p.name])
		}
	}
}
