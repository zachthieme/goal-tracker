package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// goalFormFixtureGoal is the query-count fixture's Goal titled title.
func goalFormFixtureGoal(t *testing.T, h *testsupport.Harness, title string) domain.Goal {
	t.Helper()
	goals, err := h.Service.ListGoals(t.Context())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	for _, g := range goals {
		if g.Title == title {
			return g
		}
	}
	t.Fatalf("no Goal titled %q", title)
	return domain.Goal{}
}

// The New goal page offers the values its picked parents carry, read in one
// query for all of them (#629): picking 3 parents, each carrying a Team
// value, runs as many queries as picking 1.
func TestNewGoalPageReadsThePickedParentsValuesOnce(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	seedQueryCountFixture(t, h, 10)
	// Every third Goal carries a Team value; Goals 03 and 06 are Active, so
	// either way the picked parents' Health is read, once.
	var parents []string
	for _, title := range []string{"Goal 03", "Goal 06", "Goal 09"} {
		parents = append(parents, fmt.Sprint(goalFormFixtureGoal(t, h, title).ID))
	}
	ts, _, queries := newQueryCountingServer(t, h)
	client := signInClient(t, ts.URL, queryCountAdmin)

	countFor := func(ids []string) int {
		queries.take()
		getBody(t, client, ts.URL+"/goals/new?parent="+strings.Join(ids, "&parent="))
		ran := queries.take()
		names := countQueryNames(ran)
		if times := names["ListGoalValuesForGoals"]; times > 1 {
			t.Errorf("the New goal page with %d parents ran ListGoalValuesForGoals %d times; want it at most once", len(ids), times)
		}
		if times := names["ListGoalValues"]; times != 0 {
			t.Errorf("the New goal page with %d parents ran ListGoalValues %d times; want it not at all", len(ids), times)
		}
		return len(ran)
	}
	if one, three := countFor(parents[:1]), countFor(parents); one != three {
		t.Errorf("the New goal page ran %d queries with 1 picked parent and %d with 3; want the same", one, three)
	}
}

// The define page's live checklist reads only what its Ready to activate
// card judges, the Goal's Milestones and Metrics and the offered Dimensions
// and Fields, not the parents nor the candidates the page offers (#629).
func TestDefineChecklistReadsNoParents(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	seedQueryCountFixture(t, h, 10)
	// Goal 00 is the fixture's Proposed Goal, Priya's.
	proposed := goalFormFixtureGoal(t, h, "Goal 00")
	ts, _, queries := newQueryCountingServer(t, h)
	client := signInClient(t, ts.URL, proposed.Owner.Email)

	queries.take()
	form := url.Values{"title": {proposed.Title}, "why_it_matters": {proposed.WhyItMatters}}
	card, status := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/define/checklist", ts.URL, proposed.ID), form)
	if status != http.StatusOK || findByTestID(t, parsePage(t, card), "goal-form-ready") == nil {
		t.Fatalf("the live checklist answered %d without its Ready to activate card:\n%s", status, card)
	}
	names := countQueryNames(queries.take())
	for _, q := range []string{"ListParentGoals", "ListPendingParentLinks", "ListGoals", "ListLatestCheckinsForGoals"} {
		if times := names[q]; times != 0 {
			t.Errorf("the live checklist ran %s %d times; want it not at all", q, times)
		}
	}
}

// The define page reads through one page loader, once (ADR 0010, #629):
// Service.GoalDefinitionPage reads the Goal's values, Fields, Milestones,
// Metrics and parents, Accepted and Pending, once each, and the candidates
// and offered Dimensions and Fields from them, so the parent links aren't
// read again for the candidates. No query runs twice but, at most,
// ListParentGoals, which ParentCandidates' cycle check once ran for every
// candidate (#638).
func TestDefinePageReadsEachThingOnce(t *testing.T) {
	t.Parallel()
	// defineMax is the define page's queries: the session's 2 and the
	// sidebar's 11, DefinableGoal's Goal, and the loader's 11: the Goal's
	// values, Fields, Milestones, Metrics, accepted and Pending parents,
	// every Goal and its contributors for the candidates, and the Dimensions,
	// their values and the Fields.
	const defineMax = 25
	h := testsupport.New(t, queryCountAdmin)
	seedQueryCountFixture(t, h, 10)
	// Goal 00 is the fixture's Proposed Goal, Priya's.
	proposed := goalFormFixtureGoal(t, h, "Goal 00")
	ts, _, queries := newQueryCountingServer(t, h)
	client := signInClient(t, ts.URL, proposed.Owner.Email)

	queries.take()
	getBody(t, client, fmt.Sprintf("%s/goals/%d/define", ts.URL, proposed.ID))
	ran := queries.take()
	if len(ran) > defineMax {
		t.Errorf("the define page ran %d queries; want at most %d", len(ran), defineMax)
	}
	for name, times := range countQueryNames(ran) {
		if times > 1 && name != "ListParentGoals" {
			t.Errorf("the define page ran %s %d times; want it once", name, times)
		}
	}
}

// A refused define, read through the same loader, shows the form as typed,
// not as saved, with the parent it posted picked (#629).
func TestRefusedDefineKeepsWhatWasTyped(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	seedQueryCountFixture(t, h, 10)
	proposed := goalFormFixtureGoal(t, h, "Goal 00")
	parent := goalFormFixtureGoal(t, h, "Goal 03")
	ts, _, _ := newQueryCountingServer(t, h)
	client := signInClient(t, ts.URL, proposed.Owner.Email)

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/define", ts.URL, proposed.ID), url.Values{
		"title":          {proposed.Title},
		"why_it_matters": {""},
		"parent_id":      {fmt.Sprint(parent.ID)},
	})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("the refused define answered %d, want 422:\n%s", resp.StatusCode, page)
	}
	doc := parsePage(t, page)
	if why := formField(t, doc, "why_it_matters"); why.Value != "" {
		t.Errorf("Why it matters reads %q, want it as typed, blank", why.Value)
	}
	// Goal 03 checked in Yellow.
	if chip := textOf(findByTestID(t, doc, "parent-chip")); !strings.Contains(chip, parent.Title) || !strings.Contains(chip, "Yellow") {
		t.Errorf("the picked parent reads %q, want %s with its Health, Yellow", chip, parent.Title)
	}
}
