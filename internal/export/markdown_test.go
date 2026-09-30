package export_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/export"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

var boss = domain.Account{ID: 1, Email: "boss@example.com"}

// publication is a published Report with the given exceptions and lines, read
// against a chosen date.
func publication(exceptions []domain.ReportBlock, lines []domain.SelectedGoal) domain.Publication {
	return domain.Publication{
		ID:           7,
		DefinitionID: 3,
		PublishedBy:  boss,
		PublishedAt:  time.Date(2026, 2, 21, 9, 30, 0, 0, time.UTC),
		Report: domain.Report{
			Definition: domain.ReportDefinition{ID: 3, Name: "EU MBR", Introduction: "Quarterly business review."},
			Baseline:   date(2026, 1, 12),
			Exceptions: exceptions,
			Lines:      lines,
		},
	}
}

// The export opens with the Report's name, who published it and when, what it
// read its changes against, and the introduction; every Goal that is not an
// exception takes one line with its title, Owner, Health, and due date.
func TestMarkdownHeaderAndOneLineGoals(t *testing.T) {
	pub := publication(nil, []domain.SelectedGoal{
		{Goal: domain.Goal{Title: "Cut churn", Owner: boss, DeliveryDate: date(2026, 6, 30)}, Health: domain.HealthGreen},
		{Goal: domain.Goal{Title: "Hire a PM", Owner: boss}},
	})

	want := `# EU MBR

Published 2026-02-21 09:30 UTC by boss@example.com. Changes since 2026-01-12.

Quarterly business review.

## Other Goals

- Cut churn — boss@example.com — Green — due 2026-06-30
- Hire a PM — boss@example.com — —
`
	if got := export.Markdown(pub); got != want {
		t.Errorf("Markdown:\n%s\nwant:\n%s", got, want)
	}
}

// A publication that read its changes against the previous publication says
// so, as its snapshot page does.
func TestMarkdownNamesThePreviousPublication(t *testing.T) {
	pub := publication(nil, nil)
	pub.Report.Previous = domain.PreviousPublication{ID: 6, PublishedAt: time.Date(2026, 1, 20, 14, 5, 0, 0, time.UTC)}

	want := "Published 2026-02-21 09:30 UTC by boss@example.com. Changes since the previous publication, 2026-01-20 14:05 UTC.\n"
	if got := export.Markdown(pub); !strings.Contains(got, want) {
		t.Errorf("Markdown does not name the previous publication:\n%s\nwant a line:\n%s", got, want)
	}
	if got := export.Markdown(pub); !strings.Contains(got, "No Goals selected.") {
		t.Errorf("Markdown of an empty Report does not say so:\n%s", got)
	}
}

// An exception gets the full MBR block the snapshot page shows: badges, its
// delivery date with the dates it slipped from struck through, Health, So What,
// status, Path to Green, Milestones — a removed one struck through — Metrics
// against target, and the Rolled-up Health with the Owner's explanation.
func TestMarkdownExceptionBlockKeepsStrikethroughsAndBadges(t *testing.T) {
	pub := publication([]domain.ReportBlock{{
		Goal: domain.Goal{
			Title: "Launch in EU", SoWhat: "Expand the market.", Owner: boss,
			Lifecycle: domain.LifecycleActive, DeliveryDate: date(2026, 5, 1),
		},
		Health:         domain.HealthRed,
		Badges:         []string{domain.BadgeNew, domain.BadgeNewDate, domain.BadgeStale},
		PriorDueDates:  []time.Time{date(2026, 3, 1), date(2026, 4, 1)},
		Status:         "Blocked on legal.",
		PathToGreen:    "Hire counsel.",
		PathTargetDate: date(2026, 3, 15),
		Explanation:    "A child is recovering.",
		Milestones: []domain.ReportMilestone{
			{Milestone: domain.Milestone{Name: "Beta", TargetDate: date(2026, 2, 15), Status: domain.MilestonePlanned}, New: true, PriorDates: []time.Time{date(2026, 2, 1)}},
			{Milestone: domain.Milestone{Name: "Pilot", TargetDate: date(2026, 1, 30), Status: domain.MilestoneDone}},
			{Milestone: domain.Milestone{Name: "Launch party", TargetDate: date(2026, 3, 1), Status: domain.MilestoneRemoved, RemovedReason: "Budget cut."}},
		},
		Metrics: []domain.ReportMetric{
			{Metric: domain.Metric{Name: "Countries live", Unit: "countries", Direction: domain.MetricUp, Baseline: 0, Target: 5, TargetDate: date(2026, 6, 30)}, Current: 2, Read: true},
			{Metric: domain.Metric{Name: "NPS", Unit: "points", Direction: domain.MetricUp, Baseline: 30, Target: 50.5, TargetDate: date(2026, 6, 30)}},
		},
		RolledUp: domain.RolledUpHealth{Health: domain.HealthYellow, Present: true, StaleChildren: 1, ActiveChildren: 3},
	}}, nil)

	want := `## Exceptions

### Launch in EU **[New]** **[New Date]** **[Stale]**

boss@example.com · Active · Health: **Red** · due ~~2026-03-01~~ ~~2026-04-01~~ 2026-05-01

**So What:** Expand the market.

**Status:** Blocked on legal.

**Path to Green:** Hire counsel. (back to Green by 2026-03-15)

**Milestones:**

- Beta — ~~2026-02-01~~ 2026-02-15 **[New]**
- Pilot — 2026-01-30 **[Done]**
- ~~Launch party~~ — 2026-03-01 **[Removed]** — Budget cut.

**Metrics:**

- Countries live: 2 against a target of 5 countries by 2026-06-30 (baseline 0, up)
- NPS: no reading yet against a target of 50.5 points by 2026-06-30 (baseline 30, up)

**Rolled-up Health:** Yellow · 1 of 3 Stale

**Why the Health differs:** A child is recovering.
`
	got := export.Markdown(pub)
	if !strings.HasSuffix(got, "\n\n"+want) {
		t.Errorf("Markdown:\n%s\nwant it to end with the block:\n%s", got, want)
	}
}

// What people type is exported as written: Markdown in a title or status is
// escaped so it cannot strike through, embolden, link, or start a list, and a
// line break reads as a space, as it does on the snapshot page.
func TestMarkdownEscapesWhatPeopleType(t *testing.T) {
	pub := publication([]domain.ReportBlock{{
		Goal:   domain.Goal{Title: "Ship *fast* ~~now~~", SoWhat: "See [docs](http://x).", Owner: boss, Lifecycle: domain.LifecycleActive},
		Health: domain.HealthYellow,
		Status: "Line one.\n# not a heading <b>",
	}}, []domain.SelectedGoal{
		{Goal: domain.Goal{Title: "1. Hire", Owner: boss}, Health: domain.HealthGreen},
		{Goal: domain.Goal{Title: "- Retain `all`", Owner: boss}, Health: domain.HealthGreen},
	})

	got := export.Markdown(pub)
	for _, want := range []string{
		"### Ship \\*fast\\* \\~\\~now\\~\\~\n",
		"**So What:** See \\[docs\\](http://x).\n",
		"**Status:** Line one. # not a heading \\<b>\n",
		"- 1\\. Hire — boss@example.com — Green\n",
		"- \\- Retain \\`all\\` — boss@example.com — Green\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown missing %q:\n%s", want, got)
		}
	}
}
