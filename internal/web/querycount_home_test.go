package web_test

import (
	"context"
	"fmt"
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

// seedOwnerlessGoals adds n Active Goals whose Owner has left the company,
// each an Ownerless row on Follow-ups. It seeds them before the query-count
// fixture, which makes values required to activate a Goal.
func seedOwnerlessGoals(t *testing.T, h *testsupport.Harness, n int) {
	t.Helper()
	gone := h.SignInNamed("olga@example.com", "Olga Berg")
	for i := range n {
		h.ActiveGoal(gone, fmt.Sprintf("Orphaned %02d", i), "It matters.")
	}
	h.MarkDeparted(h.SignInNamed(queryCountAdmin, "Ada Admin"), gone)
}

// Follow-ups loads through one page loader that reads each thing once (ADR
// 0010): Service.FollowUps runs no query twice, for an Admin or for a lead
// with an Org, and as many for 50 Goals, 10 of them Ownerless, as for 10, 2 of
// them Ownerless. The whole page, its sidebar and the session's reads
// included, runs twice only what the loader and the sidebar both need, and
// its Scope reads nothing the loader holds: not every Account, nor the
// viewer's Delegate Goals (#592).
func TestFollowUpsReadsEachThingOnce(t *testing.T) {
	t.Parallel()
	// followUpsMax is what Service.FollowUps reads: every Active Goal with
	// its latest Check-in, the overdue Milestones, the accepted links, the
	// overdue Action Items, the open Asks for help and every Account to walk
	// them up, the Not Green Goals' Check-ins and Date Slips, the Ownerless
	// Goals' Delegates, today's Nudges, every Goal, and the viewer's Delegate
	// Goals.
	const followUpsMax = 12
	// followUpsShared are the reads the loader and the sidebar both make, so
	// the whole page runs each twice. The page's Scope reads none of them
	// again, its Accounts and Delegate Goals being the loader's.
	followUpsShared := map[string]int{
		"ListActiveGoalsWithLatestCheckin": 2,
		"ListOverdueMilestones":            2,
		"ListAcceptedLinkGoals":            2,
		"ListOverdueActionItems":           2,
		"ListOpenAsksForHelp":              2,
		"ListAskWalkAccounts":              2,
		"ListDelegatedGoals":               2,
	}
	for _, v := range []struct {
		email     string
		pageCount int
		// scopeReads are what the page's Scope reads more than once to
		// resolve a lead's default scope, My org: whom they manage, and the
		// people under them.
		scopeReads map[string]int
	}{
		{queryCountAdmin, 27, nil},
		{"morgan@example.com", 27, map[string]int{"ListAccountsUnder": 3, "GetAccount": 2}},
	} {
		t.Run(v.email, func(t *testing.T) {
			t.Parallel()
			type counts struct{ loader, page int }
			countFor := func(n int) counts {
				h := testsupport.New(t, queryCountAdmin)
				seedOwnerlessGoals(t, h, n/5)
				seedQueryCountFixture(t, h, n)
				viewer := h.SignIn(v.email)
				ts, svc, queries := newQueryCountingServer(t, h)
				client := signInClient(t, ts.URL, v.email)
				queries.take()
				if _, err := svc.FollowUps(context.Background(), viewer.ID); err != nil {
					t.Fatalf("FollowUps: %v", err)
				}
				loader := queries.take()
				for name, times := range countQueryNames(loader) {
					if times > 1 {
						t.Errorf("FollowUps ran %s %d times for %d Goals; want it once", name, times, n)
					}
				}
				getBody(t, client, ts.URL+"/follow-ups")
				page := queries.take()
				if times := countQueryNames(page)["ListOrgWalkAccounts"]; times > 0 {
					t.Errorf("Follow-ups ran ListOrgWalkAccounts %d times for %d Goals; want the loader's Accounts", times, n)
				}
				for name, times := range countQueryNames(page) {
					want := max(1, followUpsShared[name], v.scopeReads[name])
					if times > want {
						t.Errorf("Follow-ups ran %s %d times for %d Goals; want it at most %d", name, times, n, want)
					}
				}
				return counts{loader: len(loader), page: len(page)}
			}
			ten, fifty := countFor(10), countFor(50)
			if ten.loader > followUpsMax {
				t.Errorf("FollowUps ran %d queries; want at most %d", ten.loader, followUpsMax)
			}
			if ten.page != v.pageCount {
				t.Errorf("Follow-ups ran %d queries; want %d", ten.page, v.pageCount)
			}
			if ten != fifty {
				t.Errorf("FollowUps and Follow-ups ran %d and %d queries for 10 Goals, and %d and %d for 50; want the same", ten.loader, ten.page, fifty.loader, fifty.page)
			}
		})
	}
}

// With no Ask for help open, Follow-ups still reads every Account in its
// loader, so its Scope reads them no more than when one is (#592).
func TestFollowUpsScopeReadsNoAccountsWithNoAskForHelp(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	admin := h.SignInNamed(queryCountAdmin, "Ada Admin")
	lead := h.SignInNamed("morgan@example.com", "Morgan Wu")
	dev := h.SignInNamed("priya@example.com", "Priya Raman")
	h.SetManager(dev, lead)
	h.ActiveGoal(dev, "Dev's goal", "It matters.")
	h.ActiveGoal(admin, "Admin's goal", "It matters.")
	ts, _, queries := newQueryCountingServer(t, h)
	for _, email := range []string{queryCountAdmin, "morgan@example.com"} {
		client := signInClient(t, ts.URL, email)
		queries.take()
		getBody(t, client, ts.URL+"/follow-ups")
		names := countQueryNames(queries.take())
		if names["ListOpenAsksForHelp"] == 0 {
			t.Fatalf("Follow-ups for %s read no open Asks for help; the test no longer sees the no-Ask path", email)
		}
		if times := names["ListOrgWalkAccounts"]; times > 0 {
			t.Errorf("Follow-ups for %s ran ListOrgWalkAccounts %d times with no Ask for help open; want none", email, times)
		}
		if times := names["ListDelegatedGoals"]; times > 2 {
			t.Errorf("Follow-ups for %s ran ListDelegatedGoals %d times; want at most 2", email, times)
		}
	}
}
