package web_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// queryCountAdmin is the Admin the query-count fixture signs in as.
const queryCountAdmin = "admin@example.com"

// queryCountFixture is a company of n Goals, cycling through every kind the Goal
// list and a Report show differently, and a Report Definition over them.
type queryCountFixture struct {
	report domain.ReportDefinition
}

// seedQueryCountFixture arranges n Goals over three Owners, the first two under
// a Manager, so the first Ask for help asks someone at any n. Every tenth Goal is of the same kind, so ten or more Goals hold
// every kind: Proposed; Active with no Check-in; Green, with a Metric read and
// a Highlight; Yellow with a Path to Green, every other one Asking for help;
// Red with its delivery date slipped and a Yellow child contributing to it;
// Check-in overdue on a daily cadence; Green with an overdue Milestone; On
// Hold; Done; and Cancelled. Some carry a Team or a Budget, which an Admin then
// marks required, so some Active Goals are Incomplete. The Report selects the
// Active and On Hold Goals by Lifecycle, also includes the first Goal, and
// shows Budget. It is published once everything is seeded, so the Done and
// Cancelled Goals then leave it, and the Green ones with a Metric turn Yellow
// after it with a Highlight.
func seedQueryCountFixture(t *testing.T, h *testsupport.Harness, n int) queryCountFixture {
	t.Helper()
	ctx := context.Background()
	admin := h.SignInNamed(queryCountAdmin, "Ada Admin")
	manager := h.SignInNamed("morgan@example.com", "Morgan Wu")
	owners := []domain.Account{
		h.SignInNamed("priya@example.com", "Priya Raman"),
		h.SignInNamed("sam@example.com", "Sam Lee"),
		admin,
	}
	h.SetManager(owners[0], manager)
	h.SetManager(owners[1], manager)
	team := h.CreateDimension(admin, "Team", "Platform", "Growth")
	budget := h.CreateField(admin, "Budget", domain.FieldNumber, "USD")

	path := func(days int) time.Time { return h.Clock.Now().AddDate(0, 0, days) }
	var first domain.Goal
	var metered []domain.Goal
	for i := range n {
		owner := owners[i%len(owners)]
		title := fmt.Sprintf("Goal %02d", i)
		var g domain.Goal
		switch i % 10 {
		case 0:
			g = h.CreateGoal(owner, title, "It matters.")
		case 1:
			g = h.ActiveGoal(owner, title, "It matters.")
		case 2:
			g = h.ActiveGoal(owner, title, "It matters.")
			metric, err := h.Service.AddMetric(ctx, owner.ID, domain.AddMetricInput{
				GoalID: g.ID, Name: "Signups", Unit: "people", Direction: domain.MetricUp,
				Baseline: 10, Target: 100, TargetDate: path(60),
			})
			if err != nil {
				t.Fatalf("AddMetric: %v", err)
			}
			if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
				GoalID:     g.ID,
				AuthorID:   owner.ID,
				Health:     domain.HealthGreen,
				Status:     "On track.",
				Readings:   []domain.MetricReadingInput{{MetricID: metric.ID, Value: float64(20 + i)}},
				Highlights: []domain.HighlightInput{{Kind: domain.HighlightAccomplishment, Note: "Shipped the beta of " + title + "."}},
			}); err != nil {
				t.Fatalf("SubmitCheckin with a reading: %v", err)
			}
			metered = append(metered, g)
		case 3:
			g = h.ActiveGoal(owner, title, "It matters.")
			if i%20 == 3 {
				h.AskForHelp(owner, g.ID, domain.HealthYellow, "A little behind.", "Add a reviewer.", path(14))
			} else {
				h.Checkin(owner, g.ID, domain.HealthYellow, "A little behind.", "Add a reviewer.", path(14))
			}
		case 4:
			g = h.ActiveGoal(owner, title, "It matters.")
			if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
				GoalID:             g.ID,
				AuthorID:           owner.ID,
				Health:             domain.HealthRed,
				Status:             "Blocked on the vendor.",
				PathToGreen:        "Find another vendor.",
				PathTargetDate:     path(3),
				DeliveryDate:       h.Clock.Now().AddDate(0, 7, 0),
				DeliveryDateReason: "The vendor slipped.",
			}); err != nil {
				t.Fatalf("SubmitCheckin slipping the delivery date: %v", err)
			}
			child := h.ActiveChildOf(owner, g, title+" child", "It helps.")
			h.Checkin(owner, child.ID, domain.HealthYellow, "Waiting on the parent.", "Unblock the vendor.", path(10))
		case 5:
			g = h.ActiveGoal(owner, title, "It matters.")
			if _, err := h.Service.SetCadence(ctx, owner.ID, g.ID, 1); err != nil {
				t.Fatalf("SetCadence: %v", err)
			}
			h.Checkin(owner, g.ID, domain.HealthGreen, "Daily so far.", "", time.Time{})
		case 6:
			g = h.ActiveGoal(owner, title, "It matters.")
			if _, err := h.Service.AddMilestone(ctx, owner.ID, domain.AddMilestoneInput{
				GoalID: g.ID, Name: "Pilot", TargetDate: path(2),
			}); err != nil {
				t.Fatalf("AddMilestone: %v", err)
			}
			h.Checkin(owner, g.ID, domain.HealthGreen, "Pilot lined up.", "", time.Time{})
		case 7:
			g = h.OnHoldGoal(owner, title, "It matters.", "Waiting on budget.")
		case 8:
			g = h.ActiveGoal(owner, title, "It matters.")
			h.EndGoalInCheckin(owner, g.ID, domain.LifecycleDone)
		case 9:
			g = h.ActiveGoal(owner, title, "It matters.")
			h.EndGoalInCheckin(owner, g.ID, domain.LifecycleCancelled)
		}
		if i == 0 {
			first = g
		}
		if i%3 == 0 {
			h.AssignGoalValue(g, team.Values[i%2])
		}
		if i%4 == 0 {
			h.SetGoalField(owner, g, budget, strconv.Itoa(1000*(i+1)))
		}
		h.Clock.Advance(time.Hour)
	}
	h.SetDimensionRequired(admin, team, true)
	h.SetFieldRequired(admin, budget, true)

	report := h.SaveReportDefinition(admin, domain.SaveReportDefinitionInput{
		Name: "Weekly",
		Mode: domain.ReportModeRules,
		Rules: []domain.ReportRule{{
			Attribute: domain.RuleLifecycle,
			Op:        domain.RuleIsAnyOf,
			Values:    []string{domain.LifecycleActive, domain.LifecycleOnHold},
		}},
		Include:  []int64{first.ID},
		FieldIDs: []int64{budget.ID},
	})
	h.Clock.Advance(24 * time.Hour)
	h.PublishReport(admin, report)
	// Days on, the daily Goals are Check-in overdue and the Pilots overdue.
	h.Clock.Advance(4 * 24 * time.Hour)
	for _, g := range metered {
		if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
			GoalID:         g.ID,
			AuthorID:       g.Owner.ID,
			Health:         domain.HealthYellow,
			Status:         "Signups stalled.",
			PathToGreen:    "Rerun the campaign.",
			PathTargetDate: path(7),
			Highlights:     []domain.HighlightInput{{Kind: domain.HighlightMiss, Note: "The campaign for " + g.Title + " underdelivered."}},
		}); err != nil {
			t.Fatalf("SubmitCheckin with a Highlight: %v", err)
		}
	}
	return queryCountFixture{report: report}
}

// assertGolden compares got with the golden file testdata/name, rewriting it
// instead when UPDATE_GOLDEN is set.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (UPDATE_GOLDEN=1 writes it): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from what was rendered; the page changed for the seeded data", path)
	}
}

// The Goal list and a Report render the same for the seeded Goals as they did
// before their reads were batched (#411).
func TestGoalListAndReportRenderAsGolden(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, queryCountAdmin)
	fx := seedQueryCountFixture(t, h, 50)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, queryCountAdmin)

	assertGolden(t, "goal-list.golden.html", getBody(t, client, ts.URL+"/goals?org=all"))
	assertGolden(t, "report.golden.html", getBody(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, fx.report.ID)))
}

// Showing the Goal list or a Report runs as many queries for 50 Goals as for
// 10: their per-Goal reads are batched, not run once per Goal (#411).
func TestGoalListAndReportQueriesDontGrowWithGoals(t *testing.T) {
	t.Parallel()
	type counts struct{ list, report int64 }
	countFor := func(n int) counts {
		h := testsupport.New(t, queryCountAdmin)
		fx := seedQueryCountFixture(t, h, n)
		ts, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, queryCountAdmin)
		var c counts
		queries.Store(0)
		getBody(t, client, ts.URL+"/goals?org=all")
		c.list = queries.Swap(0)
		getBody(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, fx.report.ID))
		c.report = queries.Swap(0)
		return c
	}
	ten, fifty := countFor(10), countFor(50)
	if ten.list != fifty.list {
		t.Errorf("the Goal list ran %d queries for 10 Goals and %d for 50; want the same", ten.list, fifty.list)
	}
	if ten.report != fifty.report {
		t.Errorf("a Report ran %d queries for 10 Goals and %d for 50; want the same", ten.report, fifty.report)
	}
}

// newQueryCountingServer serves h's database through a Service whose every
// statement is counted, returning the server and the count.
func newQueryCountingServer(t *testing.T, h *testsupport.Harness) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var path string
	if err := h.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&path); err != nil {
		t.Fatalf("read the database's path: %v", err)
	}
	queries := new(atomic.Int64)
	sqlDB := sql.OpenDB(countingConnector{
		dsn:     "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate",
		driver:  h.DB.Driver(),
		queries: queries,
	})
	t.Cleanup(func() { _ = sqlDB.Close() })
	svc := domain.NewService(sqlDB, h.Clock, h.Email, []string{queryCountAdmin})
	ts := httptest.NewServer(web.NewServer(svc))
	t.Cleanup(ts.Close)
	return ts, queries
}

// countingConnector opens connections that count each statement they run.
type countingConnector struct {
	dsn     string
	driver  driver.Driver
	queries *atomic.Int64
}

func (c countingConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.driver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return countingConn{Conn: conn, queries: c.queries}, nil
}

func (c countingConnector) Driver() driver.Driver { return c.driver }

// countingConn hides its driver's direct query and exec, so database/sql
// prepares every statement, and counts each one prepared.
type countingConn struct {
	driver.Conn
	queries *atomic.Int64
}

func (c countingConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.queries.Add(1)
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return p.PrepareContext(ctx, query)
	}
	return c.Conn.Prepare(query)
}

func (c countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.Conn.Begin() //nolint:staticcheck // the fallback for a driver without BeginTx
}

func (c countingConn) CheckNamedValue(nv *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}
