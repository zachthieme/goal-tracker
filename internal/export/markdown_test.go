package export_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/export"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
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
// read its changes against, its Goals counted by Health, and the introduction;
// every Goal that is not an
// exception takes one line with its Health first, then its title, Owner and
// due date; with no Health yet, a dash takes its place (#453).
func TestMarkdownHeaderAndOneLineGoals(t *testing.T) {
	t.Parallel()

	pub := publication(nil, []domain.SelectedGoal{
		{Goal: domain.Goal{Title: "Cut churn", Owner: boss, DeliveryDate: date(2026, 6, 30)}, Health: domain.HealthGreen},
		{Goal: domain.Goal{Title: "Hire a PM", Owner: boss}},
	})

	want := `# EU MBR

Published 2026-02-21 09:30 UTC by boss (boss@example.com). Changes since 2026-01-12.

**Health:** 0 Red · 0 Yellow · 1 Green · 0 Check-in overdue

Quarterly business review.

## On track

- Green — Cut churn — boss — due 2026-06-30
- — — Hire a PM — boss
`
	if got := export.Markdown(pub); got != want {
		t.Errorf("Markdown:\n%s\nwant:\n%s", got, want)
	}
}

// A publication that read its changes against the previous publication says
// so, as its snapshot page does.
func TestMarkdownNamesThePreviousPublication(t *testing.T) {
	t.Parallel()

	pub := publication(nil, nil)
	pub.Report.Previous = domain.PreviousPublication{ID: 6, PublishedAt: time.Date(2026, 1, 20, 14, 5, 0, 0, time.UTC)}

	want := "Published 2026-02-21 09:30 UTC by boss (boss@example.com). Changes since the previous publication, 2026-01-20 14:05 UTC.\n"
	if got := export.Markdown(pub); !strings.Contains(got, want) {
		t.Errorf("Markdown does not name the previous publication:\n%s\nwant a line:\n%s", got, want)
	}
	if got := export.Markdown(pub); !strings.Contains(got, "No Goals selected.") {
		t.Errorf("Markdown of an empty Report does not say so:\n%s", got)
	}
}

// An exception gets the full MBR block the snapshot page shows: badges, its
// delivery date with the dates it slipped from struck through, Health, Why it matters,
// status, Path to Green, Milestones — a removed one struck through — Metrics
// against target, and the Rolled-up Health with the Owner's explanation.
func TestMarkdownExceptionBlockKeepsStrikethroughsAndBadges(t *testing.T) {
	t.Parallel()

	pub := publication([]domain.ReportBlock{{
		Goal: domain.Goal{
			Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss,
			Lifecycle: domain.LifecycleActive, DeliveryDate: date(2026, 5, 1),
		},
		Health:         domain.HealthRed,
		Badges:         []string{domain.BadgeNew, domain.BadgeNewDate, domain.BadgeCheckinOverdue},
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
		RolledUp: domain.RolledUpHealth{Health: domain.HealthYellow, Present: true, CheckinOverdueChildren: 1, ActiveChildren: 3},
	}}, nil)

	want := `## Exceptions

### Red · Launch in EU **[New]** **[New Date]** **[Check-in overdue]**

boss · Active · due ~~2026-03-01~~ ~~2026-04-01~~ 2026-05-01

**Why it matters:** Expand the market.

**Status:** Blocked on legal.

**Path to Green:** Hire counsel. (back to Green by 2026-03-15)

**Milestones:**

- **[Overdue]** 2026-02-15 ~~2026-02-01~~ Beta
- **[Done]** 2026-01-30 Pilot
- **[Removed]** 2026-03-01 ~~Launch party~~ — Budget cut.

**Metrics:**

- Countries live: 2 against a target of 5 countries by 2026-06-30 (baseline 0, up)
- NPS: no reading yet against a target of 50.5 points by 2026-06-30 (baseline 30, up)

**Rolled-up Health:** Yellow · 1 of 3 overdue

**Why the Health differs:** A child is recovering.
`
	got := export.Markdown(pub)
	if !strings.HasSuffix(got, "\n\n"+want) {
		t.Errorf("Markdown:\n%s\nwant it to end with the block:\n%s", got, want)
	}
}

// Each Milestone reads mark, date, name: the mark as of the day the Report was
// published, then the current date with the most recent date it slipped from
// struck through, and how many times it slipped once that is more than one. A
// Milestone on track has no mark.
func TestMarkdownListsMilestonesAsMarkDateName(t *testing.T) {
	t.Parallel()

	pub := publication([]domain.ReportBlock{{
		Goal:   domain.Goal{Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive},
		Health: domain.HealthYellow,
		Milestones: []domain.ReportMilestone{
			{Milestone: domain.Milestone{Name: "Vendor sign-off", TargetDate: date(2026, 4, 20), Status: domain.MilestonePlanned},
				New: true, PriorDates: []time.Time{date(2026, 3, 1), date(2026, 3, 15), date(2026, 4, 3)}},
			{Milestone: domain.Milestone{Name: "Security review", TargetDate: date(2026, 4, 1), Status: domain.MilestonePlanned},
				PriorDates: []time.Time{date(2026, 3, 20)}},
			{Milestone: domain.Milestone{Name: "Docs", TargetDate: date(2026, 3, 10), Status: domain.MilestonePlanned}, New: true},
			{Milestone: domain.Milestone{Name: "Due on the day", TargetDate: date(2026, 2, 21), Status: domain.MilestonePlanned}},
			{Milestone: domain.Milestone{Name: "GA launch", TargetDate: date(2026, 6, 15), Status: domain.MilestonePlanned}},
		},
	}}, nil)

	want := `**Milestones:**

- **[Slipped]** 2026-04-20 ~~2026-04-03~~ · 3 slips · Vendor sign-off
- **[Slipped]** 2026-04-01 ~~2026-03-20~~ Security review
- **[New]** 2026-03-10 Docs
- 2026-02-21 Due on the day
- 2026-06-15 GA launch
`
	got := export.Markdown(pub)
	if !strings.Contains(got, want) {
		t.Errorf("Markdown:\n%s\nwant the Milestones to read:\n%s", got, want)
	}
	for _, older := range []string{"2026-03-01", "2026-03-15"} {
		if strings.Contains(got, older) {
			t.Errorf("Markdown lists the older prior date %s; only the most recent shows:\n%s", older, got)
		}
	}
}

// A Milestone is marked by the date the publication was published in the zone
// its block's Goal was judged in: at 09:30 UTC on Feb 21 it is still Feb 20 in
// Honolulu, so a Milestone due Feb 20 has passed only in a block frozen
// without a timezone, which reads as UTC.
func TestMarkdownMarksMilestonesInTheBlocksTimezone(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ tz, want string }{
		{"", "- **[Overdue]** 2026-02-20 Docs\n"},
		{"Pacific/Honolulu", "- 2026-02-20 Docs\n"},
	} {
		pub := publication([]domain.ReportBlock{{
			Goal:     domain.Goal{Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive},
			Health:   domain.HealthYellow,
			Timezone: tc.tz,
			Milestones: []domain.ReportMilestone{
				{Milestone: domain.Milestone{Name: "Docs", TargetDate: date(2026, 2, 20), Status: domain.MilestonePlanned}},
			},
		}}, nil)

		if got := export.Markdown(pub); !strings.Contains(got, tc.want) {
			t.Errorf("Markdown of a block with Timezone %q:\n%s\nwant the Milestone to read %q", tc.tz, got, tc.want)
		}
	}
}

// A publication frozen before Milestones read mark, date, name exports in that
// form: its snapshot already holds all the mark needs.
func TestMarkdownListsTheMilestonesOfAnOlderSnapshotAsMarkDateName(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[{"Goal":{"ID":1,"Title":"Launch in EU","WhyItMatters":"Expand the market.",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"},"Lifecycle":"Active"},"Health":"Red","Milestones":[` +
		`{"Milestone":{"ID":1,"Name":"Beta","TargetDate":"2025-12-20T00:00:00Z","Status":"Planned"},"New":false,"PriorDates":["2025-12-01T00:00:00Z"]},` +
		`{"Milestone":{"ID":2,"Name":"GA","TargetDate":"2026-03-01T00:00:00Z","Status":"Planned"},"New":true,"PriorDates":null}]}]}`
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write an older snapshot: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	md := export.Markdown(got)
	want := "- **[Overdue]** 2025-12-20 ~~2025-12-01~~ Beta\n- **[New]** 2026-03-01 GA\n"
	if !strings.Contains(md, want) {
		t.Errorf("Markdown:\n%s\nwant the Milestones to read:\n%s", md, want)
	}
}

// What people type is exported as written: Markdown in a title or status is
// escaped so it cannot strike through, embolden, link, or start a list, and a
// line break reads as a space, as it does on the snapshot page.
func TestMarkdownEscapesWhatPeopleType(t *testing.T) {
	t.Parallel()

	pub := publication([]domain.ReportBlock{{
		Goal:   domain.Goal{Title: "Ship *fast* ~~now~~", WhyItMatters: "See [docs](http://x).", Owner: boss, Lifecycle: domain.LifecycleActive},
		Health: domain.HealthYellow,
		Status: "Line one.\n# not a heading <b>",
	}}, []domain.SelectedGoal{
		{Goal: domain.Goal{Title: "1. Hire", Owner: boss}, Health: domain.HealthGreen},
		{Goal: domain.Goal{Title: "- Retain `all`", Owner: boss}, Health: domain.HealthGreen},
	})

	got := export.Markdown(pub)
	for _, want := range []string{
		"### Yellow · Ship \\*fast\\* \\~\\~now\\~\\~\n",
		"**Why it matters:** See \\[docs\\](http://x).\n",
		"**Status:** Line one. # not a heading \\<b>\n",
		"- Green — 1\\. Hire — boss\n",
		"- Green — \\- Retain \\`all\\` — boss\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown missing %q:\n%s", want, got)
		}
	}
}

// A publication's open Action Items open the export, ahead of the
// introduction, each with its owner and due date (CONTEXT.md: Action Item).
func TestMarkdownOpensWithActionItems(t *testing.T) {
	t.Parallel()

	pub := publication(nil, nil)
	pub.Report.ActionItems = []domain.ActionItem{
		{Text: "Get a *second* vendor quote.", Owner: boss, DueDate: date(2026, 3, 1)},
	}

	want := `# EU MBR

Published 2026-02-21 09:30 UTC by boss (boss@example.com). Changes since 2026-01-12.

## Open Action Items

- Get a \*second\* vendor quote. — boss — due 2026-03-01

Quarterly business review.

No Goals selected.
`
	if got := export.Markdown(pub); got != want {
		t.Errorf("Markdown:\n%s\nwant:\n%s", got, want)
	}
}

// The narrative follows the introduction: each section's heading, the
// author's notes, each its own paragraph in the order entered, and then the
// Highlights they picked, each crediting the Goal's Owner.
func TestMarkdownCarriesTheNarrative(t *testing.T) {
	t.Parallel()

	alice := domain.Account{ID: 2, Email: "alice@example.com"}
	pub := publication(nil, nil)
	pub.Report.Narrative = []domain.NarrativeSection{
		{Kind: domain.HighlightInsight, Notes: []string{"Pricing drives churn.", "Discounts don't save accounts."}},
		{Kind: domain.HighlightAccomplishment, Notes: []string{"EU is open for business."}, Highlights: []domain.NarrativeHighlight{{
			Highlight: domain.Highlight{Kind: domain.HighlightAccomplishment, Note: "Signed the *first* EU customer.", Owner: alice},
			GoalID:    5,
			GoalTitle: "Launch in EU",
		}}},
	}

	want := `# EU MBR

Published 2026-02-21 09:30 UTC by boss (boss@example.com). Changes since 2026-01-12.

Quarterly business review.

## Insights

Pricing drives churn.

Discounts don't save accounts.

## Accomplishments

EU is open for business.

- Signed the \*first\* EU customer. — alice (alice@example.com), Launch in EU

No Goals selected.
`
	if got := export.Markdown(pub); got != want {
		t.Errorf("Markdown:\n%s\nwant:\n%s", got, want)
	}
}

// A publication frozen when a section held at most one of the author's notes
// exports that note as before: its own paragraph under the section heading,
// before the Highlights picked into it.
func TestMarkdownKeepsTheNoteOfASnapshotFromBeforeSeveralNotes(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Narrative":[{"Kind":"Accomplishment","Text":"EU is open for business.",` +
		`"Highlights":[{"Highlight":{"ID":1,"Kind":"Accomplishment","Note":"Signed the first EU customer.",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"}},"GoalID":1,"GoalTitle":"Launch in EU","Section":"Accomplishment"}]}]}`
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write a pre-several-notes snapshot: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	md := export.Markdown(got)
	want := "\n## Accomplishments\n\nEU is open for business.\n\n- Signed the first EU customer. — boss, Launch in EU\n"
	if !strings.Contains(md, want) {
		t.Errorf("Markdown:\n%s\nwant the Accomplishments to read:\n%s", md, want)
	}
}

// The export has no hover, so a person's first mention reads Name (email) and
// every later one their Name alone; someone without a Name reads as their
// email's local part (CONTEXT.md: Name).
func TestMarkdownIntroducesEachPersonAtFirstMention(t *testing.T) {
	t.Parallel()

	ada := domain.Account{ID: 2, Email: "ada.okafor@example.com", Name: "Ada Okafor"}
	pub := publication(nil, []domain.SelectedGoal{
		{Goal: domain.Goal{Title: "Cut churn", Owner: ada}, Health: domain.HealthGreen},
		{Goal: domain.Goal{Title: "Hire a PM", Owner: ada}, Health: domain.HealthGreen},
		{Goal: domain.Goal{Title: "Close the books", Owner: boss}, Health: domain.HealthGreen},
	})

	want := `Published 2026-02-21 09:30 UTC by boss (boss@example.com). Changes since 2026-01-12.

**Health:** 0 Red · 0 Yellow · 3 Green · 0 Check-in overdue

Quarterly business review.

## On track

- Green — Cut churn — Ada Okafor (ada.okafor@example.com)
- Green — Hire a PM — Ada Okafor
- Green — Close the books — boss
`
	if got := export.Markdown(pub); !strings.HasSuffix(got, want) {
		t.Errorf("Markdown:\n%s\nwant it to end:\n%s", got, want)
	}
}

// A publisher renamed after publishing is introduced by the Name they had when
// they published, the same Name the Goals they own read by, so the export
// never shows one person under two Names.
func TestMarkdownBylineKeepsThePublishersNameAfterARename(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ceo@example.com")
	ctx := context.Background()
	ceo := h.SignInNamed("ceo@example.com", "Dana Whitfield")
	g := h.ActiveGoal(ceo, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(ceo, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(ceo, def)
	if err := h.Service.SetName(ctx, ceo.ID, "Dana Renamed"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	md := export.Markdown(got)
	if !strings.Contains(md, " by Dana Whitfield (ceo@example.com). ") {
		t.Errorf("Markdown:\n%s\nwant the byline to introduce Dana Whitfield (ceo@example.com)", md)
	}
	if strings.Contains(md, "Dana Renamed") {
		t.Errorf("Markdown:\n%s\nshows the Name given after publishing", md)
	}
}

// The Fields a Report chose show beside each Goal that has a value in them, as
// labelled values: under an exception's header line, and at the end of a
// one-line Goal. A number reads with its unit, a long text on one line, and
// numbers are never totalled (ADR 0005). A Goal with none shows none (ticket
// #78).
func TestMarkdownCarriesTheChosenFields(t *testing.T) {
	t.Parallel()

	budget := domain.Field{Name: "Budget", Type: domain.FieldNumber, Unit: "$"}
	notes := domain.Field{Name: "Notes", Type: domain.FieldLongText}
	pub := publication([]domain.ReportBlock{{
		Goal:   domain.Goal{Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive},
		Health: domain.HealthRed,
		Fields: []domain.FieldValue{{Field: budget, Value: "120"}, {Field: notes, Value: "Counsel hired.\nLegal *unblocked*."}},
	}}, []domain.SelectedGoal{
		{Goal: domain.Goal{Title: "Cut churn", Owner: boss}, Health: domain.HealthGreen, Fields: []domain.FieldValue{{Field: budget, Value: "40"}}},
		{Goal: domain.Goal{Title: "Hire a PM", Owner: boss}, Health: domain.HealthGreen},
	})

	want := `### Red · Launch in EU

boss · Active

**Budget:** 120 $ · **Notes:** Counsel hired. Legal \*unblocked\*.

**Why it matters:** Expand the market.

## On track

- Green — Cut churn — boss — Budget: 40 $
- Green — Hire a PM — boss
`
	got := export.Markdown(pub)
	if !strings.HasSuffix(got, want) {
		t.Errorf("Markdown:\n%s\nwant it to end with:\n%s", got, want)
	}
	if strings.Contains(got, "160") {
		t.Errorf("Markdown totals Budget across Goals:\n%s", got)
	}
}

// A Goal whose Health changed since the baseline shows what it was in the meta
// line under its Health-led heading; one whose Health didn't change shows no
// earlier Health.
func TestMarkdownShowsTheEarlierHealthOfAChangedGoal(t *testing.T) {
	t.Parallel()

	pub := publication([]domain.ReportBlock{
		{
			Goal:        domain.Goal{Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive, DeliveryDate: date(2026, 5, 1)},
			Health:      domain.HealthYellow,
			PriorHealth: domain.HealthGreen,
		},
		{
			Goal:   domain.Goal{Title: "Cut churn", WhyItMatters: "Keep customers.", Owner: boss, Lifecycle: domain.LifecycleActive},
			Health: domain.HealthRed,
		},
	}, nil)

	got := export.Markdown(pub)
	for _, want := range []string{
		"### Yellow · Launch in EU\n\nboss · Active · was Green · due 2026-05-01\n",
		"### Red · Cut churn\n\nboss · Active\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, got)
		}
	}
}

// A block's Run off Green, frozen with the publication, sits in the meta line
// under its Health-led heading, before what the Health was ("### Red · Launch
// in EU" over "boss · Active · 5 wks · was Green"); a block with none says
// nothing of weeks (CONTEXT.md: Run off Green).
func TestMarkdownShowsTheRunOffGreenBesideTheHealth(t *testing.T) {
	t.Parallel()

	pub := publication([]domain.ReportBlock{
		{
			Goal:             domain.Goal{Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive},
			Health:           domain.HealthRed,
			RunOffGreenWeeks: 5,
			PriorHealth:      domain.HealthGreen,
		},
	}, nil)
	if got, want := export.Markdown(pub), "### Red · Launch in EU\n\nboss · Active · 5 wks · was Green\n"; !strings.Contains(got, want) {
		t.Errorf("Markdown lacks %q:\n%s", want, got)
	}

	pub = publication([]domain.ReportBlock{
		{
			Goal:   domain.Goal{Title: "Cut churn", WhyItMatters: "Keep customers.", Owner: boss, Lifecycle: domain.LifecycleActive},
			Health: domain.HealthGreen,
		},
	}, nil)
	if got := export.Markdown(pub); strings.Contains(got, "wk") {
		t.Errorf("Markdown of a block with no Run off Green mentions weeks:\n%s", got)
	}
}

// The export opens, under its byline and before its Action Items, with the
// line counting what moved since the baseline, as the snapshot page does; a
// publication frozen before Reports counted it has none (CONTEXT.md: Report).
func TestMarkdownOpensWithWhatMovedSinceTheBaseline(t *testing.T) {
	t.Parallel()

	byline := "Published 2026-02-21 09:30 UTC by boss (boss@example.com). "
	for _, tc := range []struct {
		name     string
		previous bool
		movement *domain.ReportMovement
		want     string
	}{
		{
			name:     "against the previous publication",
			previous: true,
			movement: &domain.ReportMovement{TurnedRed: 2, Left: 1},
			want: byline + "Changes since the previous publication, 2026-01-20 14:05 UTC.\n" +
				"\n**Since the previous publication:** 2 turned Red · 1 left\n" +
				"\n## Open Action Items\n",
		},
		{
			name:     "against a date",
			movement: &domain.ReportMovement{New: 1},
			want:     byline + "Changes since 2026-01-12.\n\n**Since 2026-01-12:** 1 new\n\n## Open Action Items\n",
		},
		{
			name:     "with nothing moved",
			previous: true,
			movement: &domain.ReportMovement{},
			want: byline + "Changes since the previous publication, 2026-01-20 14:05 UTC.\n" +
				"\nNo movement since the previous publication.\n\n## Open Action Items\n",
		},
		{
			name: "frozen before Reports counted it",
			want: byline + "Changes since 2026-01-12.\n\n## Open Action Items\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub := publication(nil, nil)
			if tc.previous {
				pub.Report.Previous = domain.PreviousPublication{ID: 6, PublishedAt: time.Date(2026, 1, 20, 14, 5, 0, 0, time.UTC)}
			}
			pub.Report.Movement = tc.movement
			pub.Report.ActionItems = []domain.ActionItem{{Text: "Call legal.", Owner: boss, DueDate: date(2026, 3, 1)}}
			if got := export.Markdown(pub); !strings.Contains(got, tc.want) {
				t.Errorf("Markdown:\n%s\nwant it to contain:\n%s", got, tc.want)
			}
		})
	}
}

// The export counts the Report's Goals by Health — Red, Yellow, Green — and
// how many are Check-in overdue in one line under its selection and before
// what moved, as the snapshot page's Health summary does, zeros included; a
// Report that selects no Goals says so instead.
func TestMarkdownCountsTheReportsGoalsByHealth(t *testing.T) {
	t.Parallel()

	block := func(health string, badges ...string) domain.ReportBlock {
		return domain.ReportBlock{Goal: domain.Goal{Title: health + " Goal", Owner: boss, Lifecycle: domain.LifecycleActive}, Health: health, Badges: badges}
	}
	pub := publication(
		[]domain.ReportBlock{block(domain.HealthRed, domain.BadgeCheckinOverdue), block(domain.HealthRed), block(domain.HealthYellow)},
		[]domain.SelectedGoal{
			{Goal: domain.Goal{Title: "Cut churn", Owner: boss}, Health: domain.HealthGreen},
			{Goal: domain.Goal{Title: "Ship billing", Owner: boss}, Health: domain.HealthGreen},
			{Goal: domain.Goal{Title: "Hire a PM", Owner: boss}, Health: domain.HealthGreen},
		},
	)
	pub.Scope = "Team: Platform"
	pub.Report.Movement = &domain.ReportMovement{New: 1}

	want := "Changes since 2026-01-12.\n\n**Selection:** Team: Platform\n\n" +
		"**Health:** 2 Red · 1 Yellow · 3 Green · 1 Check-in overdue\n\n" +
		"**Since 2026-01-12:** 1 new\n"
	if got := export.Markdown(pub); !strings.Contains(got, want) {
		t.Errorf("Markdown:\n%s\nwant it to contain:\n%s", got, want)
	}

	green := publication(nil, []domain.SelectedGoal{{Goal: domain.Goal{Title: "Cut churn", Owner: boss}, Health: domain.HealthGreen}})
	if got, want := export.Markdown(green), "\n**Health:** 0 Red · 0 Yellow · 1 Green · 0 Check-in overdue\n"; !strings.Contains(got, want) {
		t.Errorf("Markdown:\n%s\nwant it to contain:\n%s", got, want)
	}

	if got := export.Markdown(publication(nil, nil)); strings.Contains(got, "**Health:**") {
		t.Errorf("Markdown of a Report with no Goals counts their Health:\n%s", got)
	}
}

// An exception block says what changed on it since the baseline under its
// Owner line, before its Fields; a block where nothing changed says nothing.
func TestMarkdownSaysWhatChangedOnABlock(t *testing.T) {
	t.Parallel()

	pub := publication([]domain.ReportBlock{
		{
			Goal:    domain.Goal{Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive, DeliveryDate: date(2026, 5, 1)},
			Health:  domain.HealthRed,
			Changes: []string{"Turned Red", "Delivery date moved 3 weeks later"},
			Fields:  []domain.FieldValue{{Field: domain.Field{Name: "Tier"}, Value: "1"}},
		},
		{
			Goal:   domain.Goal{Title: "Cut churn", WhyItMatters: "Keep customers.", Owner: boss, Lifecycle: domain.LifecycleActive},
			Health: domain.HealthRed,
		},
	}, nil)

	got := export.Markdown(pub)
	if want := "\n\nboss · Active · due 2026-05-01\n\n**Changed:** Turned Red · Delivery date moved 3 weeks later\n\n**Tier:** 1\n"; !strings.Contains(got, want) {
		t.Errorf("Markdown lacks %q:\n%s", want, got)
	}
	if n := strings.Count(got, "**Changed:**"); n != 1 {
		t.Errorf("Markdown says what changed %d times, want once, on the changed block:\n%s", n, got)
	}
}

// A publication says how it selected its Goals directly under its byline,
// before what moved, escaped like anything else people typed; one frozen
// before selections were says nothing about it.
func TestMarkdownSaysHowThePublicationSelects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, scope, want string
	}{
		{
			name:  "rules",
			scope: "Team: Platform or Identity · Active",
			want:  "Changes since 2026-01-12.\n\n**Selection:** Team: Platform or Identity · Active\n\n**Since 2026-01-12:** 1 new\n",
		},
		{
			name:  "markdown in a value",
			scope: "Team: R&D_ops",
			want:  "Changes since 2026-01-12.\n\n**Selection:** Team: R&D\\_ops\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub := publication(nil, nil)
			pub.Scope = tc.scope
			pub.Report.Movement = &domain.ReportMovement{New: 1}
			if got := export.Markdown(pub); !strings.Contains(got, tc.want) {
				t.Errorf("Markdown:\n%s\nwant it to contain:\n%s", got, tc.want)
			}
		})
	}

	if got := export.Markdown(publication(nil, nil)); strings.Contains(got, "Selection") {
		t.Errorf("Markdown of a publication with no selection mentions one:\n%s", got)
	}
}

// An exception block counts the Red, Yellow and Check-in overdue children
// beside its Rolled-up Health, names the Goals behind it, and under its head
// the Goals it contributes to, each led by its Health and marked ↓ when the
// Report holds it ("Red Growth ↓"); with no base URL to link from, it names them without links
// (#246).
func TestMarkdownExceptionBlockNamesItsLinkedGoals(t *testing.T) {
	t.Parallel()

	pub := publication([]domain.ReportBlock{{
		Goal:     domain.Goal{Title: "Referral program", WhyItMatters: "Customers bring customers.", Owner: boss, Lifecycle: domain.LifecycleActive},
		Health:   domain.HealthRed,
		Fields:   []domain.FieldValue{{Field: domain.Field{Name: "Region", Type: domain.FieldShortText}, Value: "EMEA"}},
		RolledUp: domain.RolledUpHealth{Health: domain.HealthRed, Present: true, RedChildren: 2, YellowChildren: 1, CheckinOverdueChildren: 1, ActiveChildren: 6},
		ContributesTo: []domain.LinkedGoal{
			{GoalID: 1, Title: "Growth", Health: domain.HealthRed, InReport: true},
		},
		Behind: []domain.LinkedGoal{
			{GoalID: 3, Title: "Alpha ledger", Health: domain.HealthRed, InReport: true},
			{GoalID: 4, Title: "Beta_payouts", Health: domain.HealthRed},
		},
	}}, nil)

	got := export.Markdown(pub)
	for _, want := range []string{
		"**Rolled-up Health:** Red · 2 of 6 Red · 1 Yellow · 1 overdue\n",
		"**Supported by:** Red Alpha ledger ↓ · Red Beta\\_payouts\n",
		"boss · Active\n\n**Contributes to:** Red Growth ↓\n\n**Region:** EMEA",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown:\n%s\nwant it to contain %q", got, want)
		}
	}
}

// An exception block names each Schedule conflict its Goal is in after what
// changed, grouped by side under each side's words, the child side first, with
// the other Goal by name, its Owner introduced at their first mention, and its
// delivery date as Markdown writes dates, never the block's own.
func TestMarkdownNamesEachScheduleConflictOfABlock(t *testing.T) {
	t.Parallel()

	pat := domain.Account{ID: 2, Email: "pat@example.com", Name: "Pat Parker"}
	sam := domain.Account{ID: 3, Email: "sam@example.com", Name: "Sam Lee"}
	own := date(2026, 11, 3)
	pub := publication([]domain.ReportBlock{{
		Goal:    domain.Goal{Title: "Lisbon office", Owner: boss, Lifecycle: domain.LifecycleActive, DeliveryDate: own},
		Health:  domain.HealthGreen,
		Changes: []string{"Back to Green"},
		ScheduleConflicts: []domain.ReportScheduleConflict{
			{GoalID: 7, Title: "Open in Iberia", Owner: pat, Due: date(2026, 10, 15), OwnDue: own},
			{AsParent: true, GoalID: 8, Title: "Fit_out the floor", Owner: sam, Due: date(2026, 12, 1), OwnDue: own},
		},
	}}, nil)

	got := export.Markdown(pub)
	want := "\n\n**Changed:** Back to Green\n\n**Schedule conflict:** " +
		"Parent Goal due earlier · Open in Iberia (Pat Parker (pat@example.com)), due 2026-10-15; " +
		"Contributing Goal due later · Fit\\_out the floor (Sam Lee (sam@example.com)), due 2026-12-01\n"
	if !strings.Contains(got, want) {
		t.Errorf("Markdown:\n%s\nwant it to contain %q", got, want)
	}
}

// An exception block that asks for help says so after its Path to Green,
// introducing whom it asks at their first mention, or saying no one is above
// the Owner.
func TestMarkdownSaysWhomABlockAsksForHelp(t *testing.T) {
	t.Parallel()

	mia := domain.Account{ID: 2, Email: "mia@example.com", Name: "Mia Chen"}
	block := func(title string, of *domain.Account) domain.ReportBlock {
		return domain.ReportBlock{
			Goal:           domain.Goal{Title: title, Owner: boss, Lifecycle: domain.LifecycleActive, DeliveryDate: date(2026, 6, 1)},
			Health:         domain.HealthRed,
			PathToGreen:    "Need counsel budget.",
			PathTargetDate: date(2026, 3, 1),
			AsksForHelp:    true,
			AskAddressee:   of,
		}
	}
	md := export.Markdown(publication([]domain.ReportBlock{block("Launch in EU", &mia), block("Cut churn", nil)}, nil))

	for _, want := range []string{
		"\n**Path to Green:** Need counsel budget. (back to Green by 2026-03-01)\n\n**Asks for help** of Mia Chen (mia@example.com)\n",
		"\n**Path to Green:** Need counsel budget. (back to Green by 2026-03-01)\n\n**Asks for help** (no one above the Owner)\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown:\n%s\nwant it to hold:\n%s", md, want)
		}
	}
}

// A block frozen before Reports showed asks says nothing of one.
func TestMarkdownOfASnapshotFromBeforeAsksSaysNoAsk(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[{"Goal":{"ID":1,"Title":"Launch in EU","Lifecycle":"Active",` +
		`"Owner":{"ID":1,"Email":"boss@example.com","Name":"boss"}},"Health":"Red","PathToGreen":"Hire counsel.","PathTargetDate":"2026-03-01T00:00:00Z"}]}`
	if _, err := h.DB.Exec(`UPDATE report_publications SET snapshot = ? WHERE id = ?`, before, pub.ID); err != nil {
		t.Fatalf("write a pre-asks snapshot: %v", err)
	}

	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	md := export.Markdown(got)
	if !strings.Contains(md, "**Path to Green:** Hire counsel.") || strings.Contains(md, "Asks for help") {
		t.Errorf("Markdown:\n%s\nwant the Path to Green and no ask", md)
	}
}

// A back-to-Green date that slipped within the Run off Green reads as a
// Milestone's slipped date does: the current date, the one it last slipped
// from struck through, and the count once it slipped more than once.
func TestMarkdownStrikesTheBackToGreenDateItSlippedFrom(t *testing.T) {
	t.Parallel()

	block := func(prior []time.Time) domain.ReportBlock {
		return domain.ReportBlock{
			Goal:                 domain.Goal{Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive},
			Health:               domain.HealthRed,
			PathToGreen:          "Hire counsel.",
			PathTargetDate:       date(2026, 10, 3),
			PriorPathTargetDates: prior,
		}
	}
	for _, tc := range []struct {
		prior []time.Time
		want  string
	}{
		{[]time.Time{date(2026, 9, 5), date(2026, 9, 12)}, "**Path to Green:** Hire counsel. (back to Green by 2026-10-03 ~~2026-09-12~~ · 2 slips)\n"},
		{[]time.Time{date(2026, 9, 12)}, "**Path to Green:** Hire counsel. (back to Green by 2026-10-03 ~~2026-09-12~~)\n"},
		{nil, "**Path to Green:** Hire counsel. (back to Green by 2026-10-03)\n"},
	} {
		if got := export.Markdown(publication([]domain.ReportBlock{block(tc.prior)}, nil)); !strings.Contains(got, tc.want) {
			t.Errorf("Markdown of a block slipped from %v lacks %q:\n%s", tc.prior, tc.want, got)
		}
	}
}

// A Path to Green whose back-to-Green date had passed when the Report was
// published is marked Overdue, flagged as prominently as Red (CONTEXT.md: Path
// to Green), as its page and PDF mark it; one still on time has no mark.
func TestMarkdownMarksAnOverduePathToGreen(t *testing.T) {
	t.Parallel()

	block := func(target time.Time) domain.ReportBlock {
		return domain.ReportBlock{
			Goal:           domain.Goal{Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive},
			Health:         domain.HealthRed,
			PathToGreen:    "Hire counsel.",
			PathTargetDate: target,
		}
	}
	for _, tc := range []struct {
		target time.Time
		want   string
	}{
		{date(2026, 2, 12), "\n**Path to Green:** Hire counsel. (back to Green by 2026-02-12) — **Overdue**\n"},
		{date(2026, 2, 21), "\n**Path to Green:** Hire counsel. (back to Green by 2026-02-21)\n"},
	} {
		if got := export.Markdown(publication([]domain.ReportBlock{block(tc.target)}, nil)); !strings.Contains(got, tc.want) {
			t.Errorf("Markdown of a block due back to Green by %s, published 2026-02-21, lacks %q:\n%s", tc.target.Format("2006-01-02"), tc.want, got)
		}
	}
}

// A date is never struck through beside itself (CONTEXT.md: Date Slip): a
// back-to-Green date moved back to one it slipped from strikes the most recent
// prior date that differs, keeping the count, and a delivery date leaves its
// own date out of the struck list.
func TestMarkdownStrikesNoDateEqualToItsOwn(t *testing.T) {
	t.Parallel()

	got := export.Markdown(publication([]domain.ReportBlock{{
		Goal: domain.Goal{
			Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: boss,
			Lifecycle: domain.LifecycleActive, DeliveryDate: date(2026, 11, 9),
		},
		Health:               domain.HealthRed,
		PriorDueDates:        []time.Time{date(2026, 11, 1), date(2026, 11, 9), date(2026, 11, 23)},
		PathToGreen:          "Hire counsel.",
		PathTargetDate:       date(2026, 6, 16),
		PriorPathTargetDates: []time.Time{date(2026, 6, 9), date(2026, 6, 16)},
	}}, nil))
	for _, want := range []string{
		" · due ~~2026-11-01~~ ~~2026-11-23~~ 2026-11-09\n",
		"**Path to Green:** Hire counsel. (back to Green by 2026-06-16 ~~2026-06-09~~ · 2 slips)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, got)
		}
	}
}

// The Goals that entered or left the Report since its previous publication
// follow On track, as on its page, one line each with its direction and why;
// a Report with none has no such section.
func TestMarkdownListsTheGoalsThatEnteredOrLeft(t *testing.T) {
	t.Parallel()

	pub := publication(nil, []domain.SelectedGoal{{Goal: domain.Goal{Title: "Cut churn", Owner: boss}, Health: domain.HealthGreen}})
	pub.Report.MembershipChanges = []domain.MembershipChange{
		{Goal: domain.Goal{Title: "Hire a PM"}, Direction: domain.MembershipAdded, Reason: domain.MembershipPickedByHand},
		{Goal: domain.Goal{Title: "Old *launch*"}, Direction: domain.MembershipLeft, Reason: domain.MembershipNoLongerMatches},
	}

	want := `## On track

- Green — Cut churn — boss

## Goals that entered or left this report

- Hire a PM — Added — picked by hand
- Old \*launch\* — Left — no longer matches the rules
`
	if got := export.Markdown(pub); !strings.HasSuffix(got, want) {
		t.Errorf("Markdown:\n%s\nwant it to end:\n%s", got, want)
	}

	pub.Report.MembershipChanges = nil
	if got := export.Markdown(pub); strings.Contains(got, "entered or left") {
		t.Errorf("Markdown of a Report with no membership changes lists some:\n%s", got)
	}
}

// A Green block shows no Path to Green, whatever its latest Check-in held, as
// its page shows one only on a Red or Yellow block.
func TestMarkdownWritesNoPathToGreenOnAGreenBlock(t *testing.T) {
	t.Parallel()

	got := export.Markdown(publication([]domain.ReportBlock{{
		Goal:           domain.Goal{Title: "Cut churn", WhyItMatters: "Keep customers.", Owner: boss, Lifecycle: domain.LifecycleActive},
		Health:         domain.HealthGreen,
		Badges:         []string{domain.BadgeNewDate},
		PathToGreen:    "Hire counsel.",
		PathTargetDate: date(2026, 2, 12),
	}}, nil))
	if strings.Contains(got, "Path to Green") || strings.Contains(got, "Hire counsel.") {
		t.Errorf("Markdown of a Green block writes its Path to Green:\n%s", got)
	}
}
