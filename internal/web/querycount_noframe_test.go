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

// The sidebar is read only when the page frame is drawn (#628): a POST that
// redirects, an htmx fragment and a download run none of the queries
// Service.Sidebar runs, while a page that draws the frame still runs every one
// of them. None of the three reads any of those queries for its own work: the
// Check-in form's loader reads every Account only when the ask walks past a
// Departed Manager (see TestCheckinPageReadsEachThingOnce), and the fixture
// has none.
func TestSidebarIsReadOnlyForAPageThatDrawsTheFrame(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	seedQueryCountFixture(t, h, 10)
	admin := h.SignIn(queryCountAdmin)
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	goalNamed := func(title string) domain.Goal {
		for _, g := range goals {
			if g.Title == title {
				return g
			}
		}
		t.Fatalf("the fixture has no Goal %q", title)
		return domain.Goal{}
	}
	// Goal 06 is priya@'s, its Pilot Milestone overdue, so the Nudge rule
	// admits it; Goal 05 is the Admin's own.
	nudged, checkedIn := goalNamed("Goal 06"), goalNamed("Goal 05")

	ts, svc, queries := newQueryCountingServer(t, h)
	client := signInClient(t, ts.URL, queryCountAdmin)
	queries.take()
	if _, err := svc.Sidebar(context.Background(), admin.ID); err != nil {
		t.Fatalf("Sidebar: %v", err)
	}
	sidebarQueries := countQueryNames(queries.take())
	if len(sidebarQueries) == 0 {
		t.Fatal("Sidebar ran no queries; want the ones a framed page runs")
	}

	type noFramePath struct {
		name string
		// visit makes the request, failing unless it answers as it should.
		visit func(t *testing.T)
	}
	paths := []noFramePath{
		{"a Nudge that redirects", func(t *testing.T) {
			status := postFormStatus(t, noRedirects(client), fmt.Sprintf("%s/goals/%d/nudge", ts.URL, nudged.ID), url.Values{})
			if status != http.StatusSeeOther {
				t.Fatalf("POST the Nudge: status %d, want 303", status)
			}
		}},
		{"an htmx Check-in the form refuses", func(t *testing.T) {
			body, status := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, checkedIn.ID), url.Values{
				"health": {domain.HealthRed}, "status": {"Still blocked."}, "path_to_green": {"A second vendor."}, "path_target_date": {"not-a-date"},
			})
			if status != http.StatusOK {
				t.Fatalf("POST the Check-in: status %d, want 200", status)
			}
			page := parsePage(t, body)
			if !hasTestID(page, "checkin-error") {
				t.Fatalf("the refused Check-in answered without the form's refusal:\n%s", body)
			}
			if hasTestID(page, "nav-home") {
				t.Fatalf("the refused Check-in answered with the page frame, want the form alone:\n%s", body)
			}
		}},
		{"the Goals download", func(t *testing.T) {
			resp, err := ctxGet(t, client, ts.URL+"/goals/download?org=all")
			if err != nil {
				t.Fatalf("GET the download: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			_ = readBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET the download: status %d, want 200", resp.StatusCode)
			}
		}},
	}
	for _, p := range paths {
		p.visit(t)
		ran := countQueryNames(queries.take())
		for name := range sidebarQueries {
			if ran[name] > 0 {
				t.Errorf("%s ran the sidebar's %s %d times; want none", p.name, name, ran[name])
			}
		}
	}

	getBody(t, client, ts.URL+"/goals")
	ran := countQueryNames(queries.take())
	for name := range sidebarQueries {
		if ran[name] == 0 {
			t.Errorf("the Goal list, which draws the frame, didn't run the sidebar's %s", name)
		}
	}
}
