package web_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// goalPageFixture is the query-count fixture with more on its Red Goal with
// a child, "Goal 04": a Delegate, a pending Draft Highlight, a comment, an
// Action Item reassigned once, an off-target Metric, an overdue and a slipped
// Milestone, an open Ask for help and a pending Handoff; and on its child a
// Delegate, an open Parent suggestion and a Nudge, so their pages show
// everything a Goal page can list.
type goalPageFixture struct {
	// red is "Goal 04", Owned by sam@, with a Yellow child; child is that
	// child, which contributes to it.
	red, child domain.Goal
}

// seedGoalPageFixture seeds the query-count fixture of n Goals, then makes
// morgan@ a Delegate on Goal 04 and its child, logs a Draft Highlight on Goal
// 04, and has the Admin comment on it in a fresh publication of the Report and
// raise an Action Item there, which the Admin then hands to sam@. Then, all at
// the same moment, it puts mia@, Departed, between sam@ and morgan@, and on
// Goal 04 adds a Metric past its target date, an overdue Milestone and one
// that slips, and two Check-ins repeating the Red one but for an explanation,
// each with a reading short of the target and the second asking for help, so
// the ask walks past mia@ to morgan@; then sam@ offers Goal 04 to priya@. On
// the child rita@ suggests Goal 01 as a parent, and the Admin nudges it while
// a one-day cadence makes it Check-in overdue.
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
	seedGoalPageExtras(t, h, &out, goals)
	return out
}

// seedGoalPageExtras is the second half of seedGoalPageFixture: what Goal 04
// and its child have beyond the query-count fixture's Goals, so the Goal
// page's goldens pin how each renders (#466).
func seedGoalPageExtras(t *testing.T, h *testsupport.Harness, fx *goalPageFixture, goals []domain.Goal) {
	t.Helper()
	ctx := context.Background()
	admin := h.SignIn(queryCountAdmin)
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	rita := h.SignIn("rita@example.com")
	priya := h.SignIn("priya@example.com")
	day := func(days int) time.Time { return h.Clock.Now().AddDate(0, 0, days) }

	// sam@'s Manager left, so the Ask for help goes past them to morgan@.
	mia := h.SignInNamed("mia@example.com", "Mia Park")
	h.SetManager(sam, mia)
	h.SetManager(mia, morgan)
	h.MarkDeparted(admin, mia)

	metric, err := h.Service.AddMetric(ctx, sam.ID, domain.AddMetricInput{
		GoalID: fx.red.ID, Name: "Vendor quotes", Unit: "quotes", Direction: domain.MetricUp,
		Baseline: 0, Target: 3, TargetDate: day(-1),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.AddMilestone(ctx, sam.ID, domain.AddMilestoneInput{
		GoalID: fx.red.ID, Name: "Security review", TargetDate: day(-2),
	}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	contract, err := h.Service.AddMilestone(ctx, sam.ID, domain.AddMilestoneInput{
		GoalID: fx.red.ID, Name: "Vendor contract", TargetDate: day(14),
	})
	if err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	// The Check-ins repeat the Red one's Path to Green and its date, so
	// neither moves it.
	red, ok, err := h.Service.LatestCheckin(ctx, fx.red.ID)
	if err != nil || !ok {
		t.Fatalf("LatestCheckin = %v, %v; want the Red Check-in", ok, err)
	}
	for _, c := range []struct {
		quotes     float64
		milestones []domain.MilestoneChangeInput
		ask        bool
	}{
		{1, []domain.MilestoneChangeInput{{MilestoneID: contract.ID, TargetDate: day(28), DateReason: "The vendor slipped."}}, false},
		{2, nil, true},
	} {
		if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
			GoalID:         fx.red.ID,
			AuthorID:       sam.ID,
			Health:         domain.HealthRed,
			Status:         red.Status,
			PathToGreen:    red.PathToGreen,
			PathTargetDate: red.PathTargetDate,
			AsksForHelp:    c.ask,
			Explanation:    "The vendor blocks it whatever the child does.",
			Readings:       []domain.MetricReadingInput{{MetricID: metric.ID, Value: c.quotes}},
			Milestones:     c.milestones,
		}); err != nil {
			t.Fatalf("SubmitCheckin with %v quotes: %v", c.quotes, err)
		}
	}
	h.StartHandoff(sam, fx.red.ID, priya)

	for _, g := range goals {
		if g.Title == "Goal 01" {
			h.SuggestParent(rita, fx.child, g, "It unblocks Goal 01 too.")
		}
	}
	// A one-day cadence makes the child Check-in overdue for as long as the
	// Nudge takes; its weekly one is back before the page is read.
	if _, err := h.Service.SetCadence(ctx, sam.ID, fx.child.ID, 1); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	h.Nudge(admin, fx.child.ID)
	if _, err := h.Service.SetCadence(ctx, sam.ID, fx.child.ID, 7); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
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
		// History's first page shows only the latest entries, so the Red
		// Goal's whole History is pinned too, as its Owner sees it.
		if v.name == "owner" {
			assertGolden(t, "goal-page-red-owner-history.golden.html", getBody(t, client, fmt.Sprintf("%s/goals/%d?shown=25", ts.URL, fx.red.ID)))
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
	// and discussion, every other Goal for the "Contribute to" choices, and
	// the people above its Owner its open Ask for help walks past (#466 gave
	// it one, adding that read).
	const goalPageLoaderMax = 36
	// goalPageMax is the whole page: the loader's reads, plus the session's
	// and the sidebar's, which every signed-in page makes.
	const goalPageMax = 65
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

// Walking up past Departed Managers to the Ask for help's addressee reads
// everyone once, however many it passes: GoalPage still runs no query twice
// for an Owner with three Departed Managers above them, and names the fourth.
func TestGoalPageWalksPastDepartedManagersInOneRead(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	admin := h.SignIn(queryCountAdmin)
	var line []domain.Account
	for _, e := range []string{"sam@example.com", "mia@example.com", "dir@example.com", "svp@example.com", "vp@example.com"} {
		line = append(line, h.SignIn(e))
	}
	for i := 0; i+1 < len(line); i++ {
		h.SetManager(line[i], line[i+1])
	}
	for _, departed := range line[1:4] {
		if err := h.Service.MarkDeparted(context.Background(), admin.ID, departed.ID); err != nil {
			t.Fatalf("MarkDeparted(%s): %v", departed.Email, err)
		}
	}
	g := h.ActiveGoal(line[0], "Launch in EU", "Expand the market.")
	_, svc, queries := newQueryCountingServer(t, h)
	queries.take()
	page, err := svc.GoalPage(context.Background(), line[0].ID, g.ID)
	if err != nil {
		t.Fatalf("GoalPage: %v", err)
	}
	for name, times := range countQueryNames(queries.take()) {
		if times > 1 {
			t.Errorf("GoalPage ran %s %d times walking past three Departed Managers; want it once", name, times)
		}
	}
	if page.AskAddressee == nil || page.AskAddressee.ID != line[4].ID {
		t.Errorf("GoalPage's Ask for help addressee = %v, want %s", page.AskAddressee, line[4].Email)
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
