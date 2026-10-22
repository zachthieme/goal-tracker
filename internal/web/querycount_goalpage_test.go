package web_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// goalPageFixture is the query-count fixture with more on its Red Goal with
// a child, "Goal 04": a Delegate, a pending Draft Highlight, a comment and an
// Action Item reassigned once, so its page shows everything a Goal page can
// list.
type goalPageFixture struct {
	// red is "Goal 04", Owned by sam@, with a Yellow child; child is that
	// child, which contributes to it.
	red, child domain.Goal
}

// seedGoalPageFixture seeds the query-count fixture of n Goals, then makes
// morgan@ a Delegate on Goal 04 and its child, logs a Draft Highlight on Goal
// 04, and has the Admin comment on it in a fresh publication of the Report and
// raise an Action Item there, which the Admin then hands to sam@.
func seedGoalPageFixture(t *testing.T, h *testsupport.Harness, n int) goalPageFixture {
	t.Helper()
	ctx := context.Background()
	fx := seedQueryCountFixture(t, h, n)
	admin := h.SignIn(queryCountAdmin)
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	h.SignInNamed("rita@example.com", "Rita Reader")
	var out goalPageFixture
	goals, err := h.Service.ListGoals(ctx)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	for _, g := range goals {
		switch g.Title {
		case "Goal 04":
			out.red = g
		case "Goal 04 child":
			out.child = g
		}
	}
	h.AddDelegate(sam, morgan, out.red.ID)
	h.AddDelegate(sam, morgan, out.child.ID)
	if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{
		GoalID: out.red.ID, AuthorID: morgan.ID, Kind: domain.HighlightInsight, Note: "The second vendor looks faster.",
	}); err != nil {
		t.Fatalf("LogDraftHighlight: %v", err)
	}
	pub := h.PublishReport(admin, fx.report)
	if _, err := h.Service.AddComment(ctx, admin.ID, pub.ID, out.red.ID, "Who is the second vendor?"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	item, err := h.Service.RaiseActionItem(ctx, admin.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: out.red.ID, Text: "Name the second vendor.", OwnerID: admin.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	if _, err := h.Service.ReassignActionItem(ctx, admin.ID, item.ID, sam.ID); err != nil {
		t.Fatalf("ReassignActionItem: %v", err)
	}
	return out
}

// The Goal page renders the same, to its Owner, its Delegate and a reader with
// no rights on it, as it did before it loaded through one page loader (#462).
func TestGoalPageRendersAsGolden(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	fx := seedGoalPageFixture(t, h, 10)
	ts := newServer(t, h)
	for _, v := range []struct{ name, email string }{
		{"owner", "sam@example.com"},
		{"delegate", "morgan@example.com"},
		{"reader", "rita@example.com"},
	} {
		client := signInClient(t, ts.URL, v.email)
		for _, g := range []struct {
			name string
			goal domain.Goal
		}{{"red", fx.red}, {"child", fx.child}} {
			assertGolden(t, fmt.Sprintf("goal-page-%s-%s.golden.html", g.name, v.name), getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, g.goal.ID)))
		}
	}
}

// The Goal page loads through one page loader that reads each thing once
// (ADR 0010): Service.GoalPage runs no query twice for Goal 04 shown to its
// Owner, who is offered everything, and runs as many for 50 Goals as for 10.
// The page around it adds only the session and the sidebar's reads.
func TestGoalPageReadsEachThingOnce(t *testing.T) {
	t.Parallel()
	// goalPageLoaderMax is what Service.GoalPage reads for Goal 04: the Goal,
	// the viewer and the other people it names, its links, Check-ins and
	// what they recorded, Milestones, Metrics and readings, values, History
	// and discussion, and every other Goal for the "Contribute to" choices.
	const goalPageLoaderMax = 35
	// goalPageMax is the whole page: the loader's reads, plus the session's
	// and the sidebar's, which every signed-in page makes.
	const goalPageMax = 64
	type counts struct{ loader, page int }
	countFor := func(n int) counts {
		h := testsupport.New(t, queryCountAdmin)
		fx := seedGoalPageFixture(t, h, n)
		ts, svc, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, "sam@example.com")
		queries.take()
		if _, err := svc.GoalPage(context.Background(), fx.red.Owner.ID, fx.red.ID); err != nil {
			t.Fatalf("GoalPage: %v", err)
		}
		loader := queries.take()
		for name, times := range countQueryNames(loader) {
			if times > 1 {
				t.Errorf("GoalPage ran %s %d times for %d Goals; want it once", name, times, n)
			}
		}
		getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, fx.red.ID))
		return counts{loader: len(loader), page: len(queries.take())}
	}
	ten, fifty := countFor(10), countFor(50)
	if ten.loader > goalPageLoaderMax {
		t.Errorf("GoalPage ran %d queries; want at most %d", ten.loader, goalPageLoaderMax)
	}
	if ten.page > goalPageMax {
		t.Errorf("the Goal page ran %d queries; want at most %d", ten.page, goalPageMax)
	}
	if ten != fifty {
		t.Errorf("GoalPage and the Goal page ran %d and %d queries for 10 Goals, and %d and %d for 50; want the same", ten.loader, ten.page, fifty.loader, fifty.page)
	}
}

// countQueryNames is how many times each named query among statements was
// run, by the name sqlc gives it ("-- name: GetGoal :one"); a statement with
// no name counts under its own text.
func countQueryNames(statements []string) map[string]int {
	out := map[string]int{}
	for _, s := range statements {
		name := s
		if rest, ok := strings.CutPrefix(s, "-- name: "); ok {
			name, _, _ = strings.Cut(rest, " ")
		}
		out[name]++
	}
	return out
}
