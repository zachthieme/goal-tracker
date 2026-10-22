package web_test

import (
	"context"
	"fmt"
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
