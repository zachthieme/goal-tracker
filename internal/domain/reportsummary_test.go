package domain_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A rules definition's scope summary reads its rules in order, joined with
// " · ": a Dimension, Owner or Health rule as "<name>: <values>" ("not
// <values>" for "is not"), a Lifecycle rule as its values alone, and a
// Top-level rule as "Top-level" or "Not top-level". Also include and Leave out
// close it as "n added, m left out", each left off when empty.
func TestReportSummaryScopeOfARulesDefinition(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Dana Okafor")
	lee := h.SignInNamed("lee@example.com", "Lee Chen")
	team := h.CreateDimension(boss, "Team", "Platform", "Identity", "Growth")
	platform, identity := team.Values[0], team.Values[1]
	extra := h.ActiveGoal(boss, "Also included", "why")
	out := h.ActiveGoal(boss, "Left out", "why")
	out2 := h.ActiveGoal(boss, "Left out too", "why")

	for _, c := range []struct {
		name             string
		rules            []domain.ReportRule
		include, exclude []int64
		want             string
	}{
		{
			name:    "dimension and lifecycle with overrides",
			rules:   []domain.ReportRule{dimensionRule(team, domain.RuleIsAnyOf, platform, identity), namedRule(domain.RuleLifecycle, domain.RuleIs, domain.LifecycleActive)},
			include: []int64{extra.ID},
			exclude: []int64{out.ID},
			want:    "Team: Platform or Identity · Active · 1 added, 1 left out",
		},
		{
			name:  "no overrides",
			rules: []domain.ReportRule{namedRule(domain.RuleLifecycle, domain.RuleIsAnyOf, domain.LifecycleActive, domain.LifecycleOnHold)},
			want:  "Active or On Hold",
		},
		{
			name:    "only added",
			rules:   []domain.ReportRule{namedRule(domain.RuleLifecycle, domain.RuleIsNot, domain.LifecycleDone)},
			include: []int64{extra.ID},
			want:    "not Done · 1 added",
		},
		{
			name:    "only left out",
			rules:   []domain.ReportRule{dimensionRule(team, domain.RuleIsNot, platform)},
			exclude: []int64{out.ID, out2.ID},
			want:    "Team: not Platform · 2 left out",
		},
		{
			name:  "owner and health",
			rules: []domain.ReportRule{ownerRule(domain.RuleIsAnyOf, boss, lee), namedRule(domain.RuleHealth, domain.RuleIsAnyOf, domain.HealthRed, domain.HealthYellow)},
			want:  "Owner: Dana Okafor or Lee Chen · Health: Red or Yellow",
		},
		{
			name:  "top-level",
			rules: []domain.ReportRule{{Attribute: domain.RuleTopLevel, Op: domain.RuleIs}, ownerRule(domain.RuleIsNot, lee)},
			want:  "Top-level · Owner: not Lee Chen",
		},
		{
			name:  "not top-level",
			rules: []domain.ReportRule{{Attribute: domain.RuleTopLevel, Op: domain.RuleIsNot}},
			want:  "Not top-level",
		},
	} {
		def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
			Name: c.name, Mode: domain.ReportModeRules, Rules: c.rules, Include: c.include, Exclude: c.exclude,
		})
		if got := reportSummary(t, h, def).Scope; got != c.want {
			t.Errorf("%s: scope %q, want %q", c.name, got, c.want)
		}
	}
}

// A picked definition's scope summary counts the Goals picked by hand.
func TestReportSummaryScopeOfAPickedDefinition(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	var picked []int64
	for _, title := range []string{"One", "Two", "Three", "Four", "Five"} {
		picked = append(picked, h.ActiveGoal(boss, title, "why").ID)
	}
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: picked})
	if got, want := reportSummary(t, h, def).Scope, "5 Goals picked by hand"; got != want {
		t.Errorf("scope %q, want %q", got, want)
	}
	one := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "One", Mode: domain.ReportModePicked, Picked: picked[:1]})
	if got, want := reportSummary(t, h, one).Scope, "1 Goal picked by hand"; got != want {
		t.Errorf("scope %q, want %q", got, want)
	}
}

// A never-published definition's summary counts the Goals its draft selects
// and their Health — Green, Yellow and Red by the Owner-set Health, and those
// with none, a Proposed Goal among them, so the counts add up to the Goals —
// and has no last publication and no count of changes since one.
func TestReportSummaryOfANeverPublishedDefinition(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	green := h.ActiveGoal(boss, "Green", "why")
	yellow := h.ActiveGoal(boss, "Yellow", "why")
	red := h.ActiveGoal(boss, "Red", "why")
	red2 := h.ActiveGoal(boss, "Red too", "why")
	unchecked := h.ActiveGoal(boss, "Active, no Check-in", "why")
	proposed := h.CreateGoal(boss, "Proposed", "why")
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(boss, yellow.ID, domain.HealthYellow, "Wobbly.", "Steady it.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked.", "Unblock it.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, red2.ID, domain.HealthRed, "Blocked.", "Unblock it.", h.Clock.Now().AddDate(0, 1, 0))
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:   "MBR",
		Mode:   domain.ReportModePicked,
		Picked: []int64{green.ID, yellow.ID, red.ID, red2.ID, unchecked.ID, proposed.ID},
	})

	sum := reportSummary(t, h, def)

	if sum.Goals != 6 {
		t.Errorf("Goals %d, want 6", sum.Goals)
	}
	if got, want := sum.Health, (domain.HealthCounts{Green: 1, Yellow: 1, Red: 2, None: 2}); got != want {
		t.Errorf("Health %+v, want %+v", got, want)
	}
	if sum.LastPublication != nil {
		t.Errorf("last publication %+v, want none", sum.LastPublication)
	}
	if sum.Changes != nil {
		t.Errorf("changes %d, want none", *sum.Changes)
	}
}

// A published definition's summary carries its latest publication: when it
// was published and by whom.
func TestReportSummaryOfAPublishedDefinitionHasItsLastPublication(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	lee := h.SignInNamed("lee@example.com", "Lee Chen")
	g := h.ActiveGoal(boss, "Goal", "why")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.PublishReport(boss, def)
	h.Clock.Advance(day)
	latest := h.PublishReport(lee, def)
	h.Clock.Advance(day)

	got := reportSummary(t, h, def).LastPublication
	if got == nil {
		t.Fatal("last publication none, want the latest")
	}
	if got.ID != latest.ID || !got.PublishedAt.Equal(latest.PublishedAt) {
		t.Errorf("last publication %d at %v, want %d at %v", got.ID, got.PublishedAt, latest.ID, latest.PublishedAt)
	}
	if got.PublishedBy.Label() != "Lee Chen" {
		t.Errorf("published by %q, want Lee Chen", got.PublishedBy.Label())
	}
}

// Since its last publication, a definition's summary counts each Goal that
// changed — was created, slipped a date, gained a Milestone, changed Lifecycle
// or changed Health — or entered or left the Report, once however many of
// those it did. A standing condition is no change: a Goal that stays Red adds
// nothing until it recovers.
func TestReportSummaryCountsEachGoalChangedSinceTheLastPublicationOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	lee := h.SignIn("lee@example.com")
	staysRed := h.ActiveGoal(boss, "Stays Red", "why")
	slipped := h.ActiveGoal(boss, "Slipped", "why")
	gainedMilestone := h.ActiveGoal(boss, "Gained a Milestone", "why")
	paused := h.ActiveGoal(boss, "Paused", "why")
	leftOut := h.ActiveGoal(boss, "Left out", "why")
	entered := h.ActiveGoal(lee, "Also included", "why")
	turnsYellow := h.ActiveGoal(boss, "Turns Yellow", "why")
	h.Checkin(boss, turnsYellow.ID, domain.HealthGreen, "On track.", "", time.Time{})
	red := func(g domain.Goal) {
		h.Checkin(boss, g.ID, domain.HealthRed, "Blocked.", "Unblock it.", h.Clock.Now().AddDate(0, 1, 0))
	}
	red(staysRed)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:  "MBR",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{ownerRule(domain.RuleIs, boss)},
	})
	h.PublishReport(boss, def)
	h.Clock.Advance(day)
	changes := func(step string, want int) {
		t.Helper()
		got := reportSummary(t, h, def).Changes
		switch {
		case got == nil:
			t.Fatalf("%s: changes none, want %d", step, want)
		case *got != want:
			t.Errorf("%s: changes %d, want %d", step, *got, want)
		}
	}

	red(staysRed)
	changes("stayed Red", 0)
	slipDelivery(h, boss, slipped, slipped.DeliveryDate.AddDate(0, 0, 14))
	changes("slipped", 1)
	listGoal(t, h, def, "include", entered)
	changes("entered", 2)
	h.ActiveGoal(boss, "New, and so entered", "why")
	changes("created and entered", 3)
	if _, err := h.Service.AddMilestone(ctx, domain.AddMilestoneInput{GoalID: gainedMilestone.ID, Name: "GA", TargetDate: h.Clock.Now().AddDate(0, 2, 0)}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	changes("gained a Milestone", 4)
	moveLifecycle(t, h, boss, paused, domain.LifecycleOnHold)
	changes("changed Lifecycle", 5)
	listGoal(t, h, def, "exclude", leftOut)
	changes("left", 6)
	h.Checkin(boss, turnsYellow.ID, domain.HealthYellow, "Slipping.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0))
	changes("changed Health", 7)
	h.Checkin(boss, staysRed.ID, domain.HealthGreen, "Unblocked.", "", time.Time{})
	changes("recovered", 8)
}

// ReportSummaries summarises every saved definition, as ListReportDefinitions
// orders them.
func TestReportSummariesSummariseEveryDefinition(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	a := h.ActiveGoal(boss, "A", "why")
	b := h.ActiveGoal(boss, "B", "why")
	h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Weekly", Mode: domain.ReportModePicked, Picked: []int64{a.ID}})
	monthly := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Monthly", Mode: domain.ReportModePicked, Picked: []int64{a.ID, b.ID}})
	h.PublishReport(boss, monthly)

	sums, err := h.Service.ReportSummaries(context.Background())
	if err != nil {
		t.Fatalf("ReportSummaries: %v", err)
	}
	var got []string
	for _, s := range sums {
		got = append(got, fmt.Sprintf("%s: %d, published %t", s.Definition.Name, s.Goals, s.LastPublication != nil))
	}
	if want := []string{"Monthly: 2, published true", "Weekly: 1, published false"}; !slices.Equal(got, want) {
		t.Errorf("summaries %q, want %q", got, want)
	}
}

// reportSummary is def's summary, re-read as stored, failing the test on
// error.
func reportSummary(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition) domain.ReportSummary {
	t.Helper()
	ctx := context.Background()
	def, err := h.Service.GetReportDefinition(ctx, def.ID)
	if err != nil {
		t.Fatalf("GetReportDefinition: %v", err)
	}
	sum, err := h.Service.ReportSummary(ctx, def)
	if err != nil {
		t.Fatalf("ReportSummary: %v", err)
	}
	return sum
}
