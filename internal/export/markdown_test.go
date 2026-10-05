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
// read its changes against, and the introduction; every Goal that is not an
// exception takes one line with its title, Owner, Health, and due date.
func TestMarkdownHeaderAndOneLineGoals(t *testing.T) {
	t.Parallel()

	pub := publication(nil, []domain.SelectedGoal{
		{Goal: domain.Goal{Title: "Cut churn", Owner: boss, DeliveryDate: date(2026, 6, 30)}, Health: domain.HealthGreen},
		{Goal: domain.Goal{Title: "Hire a PM", Owner: boss}},
	})

	want := `# EU MBR

Published 2026-02-21 09:30 UTC by boss (boss@example.com). Changes since 2026-01-12.

Quarterly business review.

## Other Goals

- Cut churn — boss — Green — due 2026-06-30
- Hire a PM — boss — —
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
// delivery date with the dates it slipped from struck through, Health, So What,
// status, Path to Green, Milestones — a removed one struck through — Metrics
// against target, and the Rolled-up Health with the Owner's explanation.
func TestMarkdownExceptionBlockKeepsStrikethroughsAndBadges(t *testing.T) {
	t.Parallel()

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

boss · Active · Health: **Red** · due ~~2026-03-01~~ ~~2026-04-01~~ 2026-05-01

**So What:** Expand the market.

**Status:** Blocked on legal.

**Path to Green:** Hire counsel. (back to Green by 2026-03-15)

**Milestones:**

- **[Red]** 2026-02-15 ~~2026-02-01~~ Beta
- **[Done]** 2026-01-30 Pilot
- **[Removed]** 2026-03-01 ~~Launch party~~ — Budget cut.

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

// Each Milestone reads mark, date, name: the mark as of the day the Report was
// published, then the current date with the most recent date it slipped from
// struck through, and how many times it slipped once that is more than one. A
// Milestone on track has no mark.
func TestMarkdownListsMilestonesAsMarkDateName(t *testing.T) {
	t.Parallel()

	pub := publication([]domain.ReportBlock{{
		Goal:   domain.Goal{Title: "Launch in EU", SoWhat: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive},
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

- **[Yellow]** 2026-04-20 ~~2026-04-03~~ (3) Vendor sign-off
- **[Yellow]** 2026-04-01 ~~2026-03-20~~ Security review
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
	before := `{"Definition":{"ID":1,"Name":"MBR"},"Exceptions":[{"Goal":{"ID":1,"Title":"Launch in EU","SoWhat":"Expand the market.",` +
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
	want := "- **[Red]** 2025-12-20 ~~2025-12-01~~ Beta\n- **[New]** 2026-03-01 GA\n"
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
		"- 1\\. Hire — boss — Green\n",
		"- \\- Retain \\`all\\` — boss — Green\n",
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

Quarterly business review.

## Other Goals

- Cut churn — Ada Okafor (ada.okafor@example.com) — Green
- Hire a PM — Ada Okafor — Green
- Close the books — boss — Green
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
		Goal:   domain.Goal{Title: "Launch in EU", SoWhat: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive},
		Health: domain.HealthRed,
		Fields: []domain.FieldValue{{Field: budget, Value: "120"}, {Field: notes, Value: "Counsel hired.\nLegal *unblocked*."}},
	}}, []domain.SelectedGoal{
		{Goal: domain.Goal{Title: "Cut churn", Owner: boss}, Health: domain.HealthGreen, Fields: []domain.FieldValue{{Field: budget, Value: "40"}}},
		{Goal: domain.Goal{Title: "Hire a PM", Owner: boss}, Health: domain.HealthGreen},
	})

	want := `### Launch in EU

boss · Active · Health: **Red**

**Budget:** 120 $ · **Notes:** Counsel hired. Legal \*unblocked\*.

**So What:** Expand the market.

## Other Goals

- Cut churn — boss — Green — Budget: 40 $
- Hire a PM — boss — Green
`
	got := export.Markdown(pub)
	if !strings.HasSuffix(got, want) {
		t.Errorf("Markdown:\n%s\nwant it to end with:\n%s", got, want)
	}
	if strings.Contains(got, "160") {
		t.Errorf("Markdown totals Budget across Goals:\n%s", got)
	}
}

// A Goal whose Health changed since the baseline shows what it was after its
// Health; one whose Health didn't change shows no earlier Health.
func TestMarkdownShowsTheEarlierHealthOfAChangedGoal(t *testing.T) {
	t.Parallel()

	pub := publication([]domain.ReportBlock{
		{
			Goal:        domain.Goal{Title: "Launch in EU", SoWhat: "Expand the market.", Owner: boss, Lifecycle: domain.LifecycleActive, DeliveryDate: date(2026, 5, 1)},
			Health:      domain.HealthYellow,
			PriorHealth: domain.HealthGreen,
		},
		{
			Goal:   domain.Goal{Title: "Cut churn", SoWhat: "Keep customers.", Owner: boss, Lifecycle: domain.LifecycleActive},
			Health: domain.HealthRed,
		},
	}, nil)

	got := export.Markdown(pub)
	for _, want := range []string{
		"\n\nboss · Active · Health: **Yellow** · was Green · due 2026-05-01\n",
		"\n\nboss · Active · Health: **Red**\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, got)
		}
	}
}
