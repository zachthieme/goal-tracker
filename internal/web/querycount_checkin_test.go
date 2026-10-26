package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Check-in page and every response that renders its form read through
// one page loader, once (ADR 0010, #560): Service.CheckinPage runs no query
// twice for Goal 04 shown to its Owner, and the page, Add another, a submit
// the form's own parsing refuses, one the domain refuses and a refused No
// change each run it once, and as many queries for 50 Goals as for 10.
//
// Around the loader each adds the session's and the sidebar's reads, which
// share one query with it: every Account, which the loader walks the Ask for
// help up and the sidebar finds the viewer's Org and their Asks for help in.
// A post the domain refuses adds its command's own reads, made again inside
// the transaction that writes, of which these repeat the loader's or the
// session's:
//   - SubmitCheckin, refusing a Green for Goal 04's overdue Milestone: the
//     Goal (GetGoal), its author (GetAccount), and its children and their
//     latest Check-ins for its Rolled-up Health (ListChildGoals,
//     ListLatestCheckinsForGoals);
//   - SubmitNoChangeCheckin, refusing for the same Milestone: the Goal
//     (GetGoal), its author (GetAccount), its and its children's latest
//     Check-ins (ListLatestCheckinsForGoals) and its Metrics' readings
//     (ListMetricReadingsForGoals).
//
// No other query runs twice.
func TestCheckinPageReadsEachThingOnce(t *testing.T) {
	t.Parallel()
	// checkinPageLoaderMax is what Service.CheckinPage reads for Goal 04
	// shown to its Owner: the Goal, the viewer and the Owner's Manager, its
	// children and its and their latest Check-ins, its Check-ins for the Run
	// off Green, Milestones, Metrics and readings, pending Draft Highlights,
	// open Ask for help, and every Account, for the ask walks past the
	// Owner's Departed Manager.
	const checkinPageLoaderMax = 11
	// The Check-in page, and the paths that render it or its form without a
	// command, are the loader's reads and the session's and sidebar's.
	const checkinPageMax = 24
	// A post the domain refuses adds its command's reads.
	const checkinSubmitRefusedMax = 28
	const checkinNoChangeRefusedMax = 31
	sidebarShared := []string{"ListAskWalkAccounts"}
	submitCheckinRereads := []string{"GetGoal", "GetAccount", "ListChildGoals", "ListLatestCheckinsForGoals"}
	noChangeRereads := []string{"GetGoal", "GetAccount", "ListLatestCheckinsForGoals", "ListMetricReadingsForGoals"}

	type checkinPath struct {
		name    string
		max     int
		rereads []string
		// visit makes the request, returning its status.
		visit func(t *testing.T, client *http.Client, base string, goalID int64) int
	}
	submit := func(form url.Values) func(t *testing.T, client *http.Client, base string, goalID int64) int {
		return func(t *testing.T, client *http.Client, base string, goalID int64) int {
			_, status := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", base, goalID), form)
			return status
		}
	}
	paths := []checkinPath{
		{"the page", checkinPageMax, nil, func(t *testing.T, client *http.Client, base string, goalID int64) int {
			resp, err := ctxGet(t, client, fmt.Sprintf("%s/goals/%d/checkin", base, goalID))
			if err != nil {
				t.Fatalf("GET the Check-in page: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			_ = readBody(t, resp)
			return resp.StatusCode
		}},
		// A plain post's Add another renders the whole page around the form.
		{"Add another", checkinPageMax, nil, func(t *testing.T, client *http.Client, base string, goalID int64) int {
			resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/checkins", base, goalID), url.Values{
				"health": {domain.HealthRed}, "status": {"Still blocked."},
				"highlight_kind": {domain.HighlightMiss}, "highlight_note": {"The vendor slipped."}, "add_highlight": {"1"},
			})
			defer func() { _ = resp.Body.Close() }()
			_ = readBody(t, resp)
			return resp.StatusCode
		}},
		{"a date that isn't one", checkinPageMax, nil, submit(url.Values{
			"health": {domain.HealthRed}, "status": {"Still blocked."}, "path_to_green": {"A second vendor."}, "path_target_date": {"not-a-date"},
		})},
		{"a Green the overdue Milestone refuses", checkinSubmitRefusedMax, submitCheckinRereads, submit(url.Values{
			"health": {domain.HealthGreen}, "status": {"On track."},
		})},
		{"a refused No change", checkinNoChangeRefusedMax, noChangeRereads, func(t *testing.T, client *http.Client, base string, goalID int64) int {
			resp, _ := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", base, goalID), true)
			defer func() { _ = resp.Body.Close() }()
			return resp.StatusCode
		}},
	}

	type counts struct {
		loader int
		paths  map[string]int
	}
	countFor := func(n int) counts {
		h := testsupport.New(t, queryCountAdmin)
		fx := seedGoalPageFixture(t, h, n)
		owner := h.SignIn("sam@example.com")
		ts, svc, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, "sam@example.com")
		queries.take()
		if _, err := svc.CheckinPage(context.Background(), owner.ID, fx.red.ID); err != nil {
			t.Fatalf("CheckinPage: %v", err)
		}
		loader := queries.take()
		for name, times := range countQueryNames(loader) {
			if times > 1 {
				t.Errorf("CheckinPage ran %s %d times for %d Goals; want it once", name, times, n)
			}
		}
		out := counts{loader: len(loader), paths: map[string]int{}}
		for _, p := range paths {
			if status := p.visit(t, client, ts.URL, fx.red.ID); status != http.StatusOK {
				t.Fatalf("%s for %d Goals: status %d, want 200", p.name, n, status)
			}
			ran := queries.take()
			twice := map[string]bool{}
			for _, name := range append(append([]string{}, sidebarShared...), p.rereads...) {
				twice[name] = true
			}
			names := countQueryNames(ran)
			for name, times := range names {
				want := 1
				if twice[name] {
					want = 2
				}
				if times > want {
					t.Errorf("%s ran %s %d times for %d Goals; want it at most %d", p.name, name, times, n, want)
				}
			}
			// Only the loader reads the pending Draft Highlights, so it ran
			// once.
			if got := names["ListPendingDraftHighlights"]; got != 1 {
				t.Errorf("%s read the Check-in page %d times for %d Goals; want once", p.name, got, n)
			}
			out.paths[p.name] = len(ran)
		}
		return out
	}
	ten, fifty := countFor(10), countFor(50)
	if ten.loader > checkinPageLoaderMax {
		t.Errorf("CheckinPage ran %d queries; want at most %d", ten.loader, checkinPageLoaderMax)
	}
	if ten.loader != fifty.loader {
		t.Errorf("CheckinPage ran %d queries for 10 Goals and %d for 50; want the same", ten.loader, fifty.loader)
	}
	for _, p := range paths {
		if got := ten.paths[p.name]; got > p.max {
			t.Errorf("%s ran %d queries; want at most %d", p.name, got, p.max)
		}
		if ten.paths[p.name] != fifty.paths[p.name] {
			t.Errorf("%s ran %d queries for 10 Goals and %d for 50; want the same", p.name, ten.paths[p.name], fifty.paths[p.name])
		}
	}
}
