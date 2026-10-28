package domain_test

import (
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// factOutline is each fact's kind and label, in order, as a test reads a fact
// list: "kind: label".
func factOutline(facts []domain.ReportFact) []string {
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		out = append(out, string(f.Kind)+": "+f.Label)
	}
	return out
}

// factOfKind is the one fact of kind in facts, failing the test unless there
// is exactly one.
func factOfKind(t *testing.T, facts []domain.ReportFact, kind domain.ReportFactKind) domain.ReportFact {
	t.Helper()
	var found []domain.ReportFact
	for _, f := range facts {
		if f.Kind == kind {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		t.Fatalf("facts hold %d of kind %q, want 1: %v", len(found), kind, factOutline(facts))
	}
	return found[0]
}

var (
	factsBoss  = domain.Account{ID: 1, Email: "boss@example.com", Name: "Bo Ss"}
	factsAda   = domain.Account{ID: 2, Email: "ada@example.com", Name: "Ada Okafor"}
	factsAsOf  = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	factsLater = time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
)

// factsPublication is a publication holding one of each of its Report's own
// parts, read against its previous publication.
func factsPublication() domain.Publication {
	return domain.Publication{
		ID:          9,
		PublishedBy: factsBoss,
		PublishedAt: factsAsOf,
		Scope:       "Team: Growth",
		Report: domain.Report{
			Definition:  domain.ReportDefinition{Name: "Growth MBR", Introduction: "Where Growth stands."},
			Baseline:    time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC),
			Previous:    domain.PreviousPublication{ID: 8, PublishedAt: time.Date(2026, 2, 10, 16, 0, 0, 0, time.UTC)},
			ActionItems: []domain.ActionItem{{Text: "Call legal", Owner: factsAda, DueDate: factsLater}},
			Narrative:   []domain.NarrativeSection{{Kind: domain.HighlightInsight, Notes: []string{"Churn is seasonal."}}},
			Exceptions:  []domain.ReportBlock{{Goal: domain.Goal{Title: "Launch in EU", Owner: factsBoss}, Health: domain.HealthRed}},
			Lines:       []domain.SelectedGoal{{Goal: domain.Goal{Title: "Cut churn", Owner: factsAda}, Health: domain.HealthGreen}},
			MembershipChanges: []domain.MembershipChange{
				{Goal: domain.Goal{Title: "Hire a PM"}, Direction: domain.MembershipAdded, Reason: domain.MembershipPickedByHand},
			},
			Movement: &domain.ReportMovement{TurnedRed: 1},
		},
	}
}

// A publication lists its byline and Selection, then its Report's parts in the
// order both the page and the Markdown export show them, each with the label
// both show; the introduction and the narrative carry none, the narrative's
// sections having their own headings.
func TestPublicationFactsListTheReportsPartsInOrder(t *testing.T) {
	t.Parallel()

	got := factOutline(factsPublication().Facts())
	want := []string{
		"byline: Published",
		"selection: Selection",
		"health counts: Health",
		"movement: Since",
		"open action items: Open Action Items",
		"introduction: ",
		"narrative: ",
		"exceptions: Exceptions",
		"on track: On track",
		"membership changes: Goals that entered or left this report",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Publication.Facts() =\n%q\nwant\n%q", got, want)
	}
}

// A publication read against its previous publication says so in its byline
// and its movement line; one read against a date names none.
func TestPublicationFactsSayWhatChangesAreReadSince(t *testing.T) {
	t.Parallel()

	pub := factsPublication()
	facts := pub.Facts()
	byline := factOfKind(t, facts, domain.ReportFactByline).Byline
	if byline.PublishedBy != factsBoss || !byline.PublishedAt.Equal(factsAsOf) {
		t.Errorf("byline = %+v, want published by boss at %v", byline, factsAsOf)
	}
	if got := byline.Since.Words(); got != "the previous publication" {
		t.Errorf("byline since words = %q, want the previous publication", got)
	}
	if got, want := byline.Since.At, pub.Report.Previous.PublishedAt; !got.Equal(want) {
		t.Errorf("byline since at = %v, want the previous publication's %v", got, want)
	}
	movement := factOfKind(t, facts, domain.ReportFactMovement).Movement
	if movement.Since.Words() != "the previous publication" || !slices.Equal(movement.Parts, []string{"1 turned Red"}) {
		t.Errorf("movement = %+v, want 1 turned Red since the previous publication", movement)
	}

	pub.Report.Previous = domain.PreviousPublication{}
	facts = pub.Facts()
	since := factOfKind(t, facts, domain.ReportFactByline).Byline.Since
	if since.Words() != "" || !since.At.Equal(pub.Report.Baseline) {
		t.Errorf("byline since = %+v (words %q), want the baseline date and no words", since, since.Words())
	}
	if got := factOfKind(t, facts, domain.ReportFactMovement).Movement.Since; got != since {
		t.Errorf("movement since = %+v, want the byline's %+v", got, since)
	}
}

// A Report that selects no Goals lists no Health counts, no Exceptions and no
// On track, and a part it lacks is left out: no Selection, movement line,
// Action Items, introduction, narrative or membership changes.
func TestReportFactsLeaveOutWhatTheReportLacks(t *testing.T) {
	t.Parallel()

	pub := domain.Publication{PublishedBy: factsBoss, PublishedAt: factsAsOf, Report: domain.Report{Baseline: factsAsOf}}
	if got, want := factOutline(pub.Facts()), []string{"byline: Published"}; !slices.Equal(got, want) {
		t.Errorf("Publication.Facts() of an empty Report = %q, want %q", got, want)
	}

	r := domain.Report{Lines: []domain.SelectedGoal{{Goal: domain.Goal{Title: "Cut churn"}, Health: domain.HealthGreen}}}
	if got, want := factOutline(r.Facts()), []string{"health counts: Health", "on track: On track"}; !slices.Equal(got, want) {
		t.Errorf("Report.Facts() of one one-line Goal = %q, want %q", got, want)
	}
	if got := factOfKind(t, r.Facts(), domain.ReportFactHealthCounts).HealthCounts; got.Green != 1 {
		t.Errorf("health counts = %+v, want 1 Green", got)
	}
}

// factsBlock is a Red exception block holding every fact a block can: its
// Path to Green's back-to-Green date passed before factsAsOf, it asks for help
// with no one above its Owner, one of its Metrics has no reading yet, and it
// shows the Owner's explanation beside its Rolled-up Health.
func factsBlock() domain.ReportBlock {
	return domain.ReportBlock{
		Goal: domain.Goal{
			ID: 4, Title: "Launch in EU", WhyItMatters: "Expand the market.", Owner: factsBoss,
			Lifecycle: domain.LifecycleActive, DeliveryDate: factsLater,
		},
		Health:               domain.HealthRed,
		PriorHealth:          domain.HealthGreen,
		RunOffGreenWeeks:     3,
		Changes:              []string{"Turned Red"},
		Badges:               []string{domain.BadgeNewDate},
		PriorDueDates:        []time.Time{time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		Status:               "Blocked on legal.",
		PathToGreen:          "Hire counsel.",
		PathTargetDate:       time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		PriorPathTargetDates: []time.Time{time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)},
		Explanation:          "A child is recovering.",
		Milestones: []domain.ReportMilestone{
			{Milestone: domain.Milestone{Name: "Beta", TargetDate: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Status: domain.MilestonePlanned}},
			{Milestone: domain.Milestone{Name: "Pilot", TargetDate: factsLater, Status: domain.MilestonePlanned}, New: true},
		},
		Metrics: []domain.ReportMetric{
			{Metric: domain.Metric{Name: "NPS", Target: 50}},
			{Metric: domain.Metric{Name: "Countries live", Target: 5}, Current: 2, Read: true},
		},
		RolledUp:      domain.RolledUpHealth{Health: domain.HealthYellow, Present: true, ActiveChildren: 3, YellowChildren: 1},
		ScheduleConflicts: []domain.ReportScheduleConflict{
			{GoalID: 7, Title: "Open in Iberia", Owner: factsBoss, Due: time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC), OwnDue: factsLater},
		},
		ContributesTo: []domain.LinkedGoal{{GoalID: 5, Title: "Grow revenue", Health: domain.HealthYellow, InReport: true}},
		Behind:        []domain.LinkedGoal{{GoalID: 6, Title: "Alpha ledger", Health: domain.HealthYellow}},
		Fields:        []domain.FieldValue{{Field: domain.Field{Name: "Budget"}, Value: "120"}},
		AsksForHelp:   true,
	}
}

// An exception block lists its facts in the order both outputs show them, each
// with the label both show; its head and its Fields carry none.
func TestReportBlockFactsListTheBlockInOrder(t *testing.T) {
	t.Parallel()

	got := factOutline(factsBlock().Facts(factsAsOf))
	want := []string{
		"block head: ",
		"changed: Changed",
		"schedule conflict: Schedule conflict",
		"contributes to: Contributes to",
		"fields: ",
		"why it matters: Why it matters",
		"status: Status",
		"path to green: Path to Green",
		"asks for help: Asks for help",
		"milestones: Milestones",
		"metrics: Metrics",
		"rolled-up health: Rolled-up Health",
		"supported by: Supported by",
		"why the health differs: Why the Health differs",
	}
	if !slices.Equal(got, want) {
		t.Errorf("ReportBlock.Facts() =\n%q\nwant\n%q", got, want)
	}
}

// A block's Schedule conflict fact carries each conflict as the block holds
// it, as the child first, then as the parent; a block in none has no such
// fact.
func TestReportBlockFactsNameEachScheduleConflict(t *testing.T) {
	t.Parallel()

	b := factsBlock()
	asParent := domain.ReportScheduleConflict{AsParent: true, GoalID: 8, Title: "Lisbon office", Owner: factsBoss, Due: factsLater.AddDate(0, 1, 0), OwnDue: factsLater}
	b.ScheduleConflicts = append(b.ScheduleConflicts, asParent)
	got := factOfKind(t, b.Facts(factsAsOf), domain.ReportFactScheduleConflict).ScheduleConflicts
	if !slices.EqualFunc(got, b.ScheduleConflicts, func(x, y domain.ReportScheduleConflict) bool {
		return x.AsParent == y.AsParent && x.GoalID == y.GoalID && x.Title == y.Title && x.Owner == y.Owner &&
			x.Due.Equal(y.Due) && x.OwnDue.Equal(y.OwnDue)
	}) {
		t.Errorf("Schedule conflict fact = %+v, want %+v", got, b.ScheduleConflicts)
	}

	b.ScheduleConflicts = nil
	for _, f := range b.Facts(factsAsOf) {
		if f.Kind == domain.ReportFactScheduleConflict {
			t.Errorf("a block in no Schedule conflict lists %+v", f)
		}
	}
}

// A block's head reads its Health with its Run off Green and what it was,
// "was Green", then its title, Owner, Lifecycle, due date with its earlier
// dates, and badges.
func TestReportBlockFactsHeadReadsTheHealthFirst(t *testing.T) {
	t.Parallel()

	b := factsBlock()
	head := factOfKind(t, b.Facts(factsAsOf), domain.ReportFactBlockHead).Head
	want := domain.ReportBlockHead{
		GoalID: 4, Health: domain.HealthRed, RunOffGreen: "3 wks", WasHealth: "was Green",
		Title: "Launch in EU", Owner: factsBoss, Lifecycle: domain.LifecycleActive,
		Due: factsLater, PriorDue: b.PriorDueDates, Badges: b.Badges,
	}
	if !headsEqual(head, want) {
		t.Errorf("head = %+v, want %+v", head, want)
	}

	b.PriorHealth, b.RunOffGreenWeeks = "", 0
	if head := factOfKind(t, b.Facts(factsAsOf), domain.ReportFactBlockHead).Head; head.WasHealth != "" || head.RunOffGreen != "" {
		t.Errorf("head of an unchanged block = %+v, want no earlier Health and no Run off Green", head)
	}
}

func headsEqual(a, b domain.ReportBlockHead) bool {
	return a.GoalID == b.GoalID && a.Health == b.Health && a.RunOffGreen == b.RunOffGreen &&
		a.WasHealth == b.WasHealth && a.Title == b.Title && a.Owner == b.Owner &&
		a.Lifecycle == b.Lifecycle && a.Due.Equal(b.Due) && slices.Equal(a.PriorDue, b.PriorDue) &&
		slices.Equal(a.Badges, b.Badges)
}

// A Path to Green whose back-to-Green date passed before asOf is Overdue, and
// reads "back to Green by" its date with the dates it slipped from and how
// many times; one whose date hasn't passed isn't Overdue.
func TestReportBlockFactsMarkAnOverduePathToGreen(t *testing.T) {
	t.Parallel()

	b := factsBlock()
	path := factOfKind(t, b.Facts(factsAsOf), domain.ReportFactPathToGreen).PathToGreen
	if path.Plan != "Hire counsel." || !path.By.Equal(b.PathTargetDate) || !path.Overdue ||
		!slices.Equal(path.PriorDates, b.PriorPathTargetDates) || path.Slips != 2 {
		t.Errorf("path = %+v, want Hire counsel. by %v, slipped twice, Overdue", path, b.PathTargetDate)
	}
	if path.ByWords() != "back to Green by" || path.OverdueWords() != "Overdue" {
		t.Errorf("path reads %q and %q, want back to Green by and Overdue", path.ByWords(), path.OverdueWords())
	}
	if path := factOfKind(t, b.Facts(b.PathTargetDate), domain.ReportFactPathToGreen).PathToGreen; path.Overdue {
		t.Errorf("path as of its own date = %+v, want not Overdue", path)
	}
}

// A Green block shows no Path to Green, whatever its Check-in held, as the
// page shows it only in its Red or Yellow box; a block shows Why it matters
// even when empty, and no explanation without a Rolled-up Health beside it.
func TestReportBlockFactsOfAGreenBlock(t *testing.T) {
	t.Parallel()

	b := domain.ReportBlock{
		Goal:        domain.Goal{Title: "Cut churn", Owner: factsBoss, Lifecycle: domain.LifecycleActive},
		Health:      domain.HealthGreen,
		PathToGreen: "Keep going.",
		Explanation: "Children lag.",
	}
	got := factOutline(b.Facts(factsAsOf))
	want := []string{"block head: ", "why it matters: Why it matters"}
	if !slices.Equal(got, want) {
		t.Errorf("Facts() of a Green block = %q, want %q", got, want)
	}
}

// An Ask for help names whom it asks, or says no one is above the Owner.
func TestReportBlockFactsSayWhomItAsksForHelp(t *testing.T) {
	t.Parallel()

	b := factsBlock()
	ask := factOfKind(t, b.Facts(factsAsOf), domain.ReportFactAskForHelp).Ask
	if ask.Addressee != nil || ask.Words() != "no one above the Owner" {
		t.Errorf("ask = %+v reading %q, want no one above the Owner", ask, ask.Words())
	}
	b.AskAddressee = &factsAda
	ask = factOfKind(t, b.Facts(factsAsOf), domain.ReportFactAskForHelp).Ask
	if ask.Addressee == nil || *ask.Addressee != factsAda || ask.Words() != "" {
		t.Errorf("ask = %+v reading %q, want of Ada and no words", ask, ask.Words())
	}
}

// Each Milestone carries its mark as of asOf in the block's zone, and each
// Metric its reading against its target, "no reading yet" when it has none.
func TestReportBlockFactsMarkMilestonesAndReadMetrics(t *testing.T) {
	t.Parallel()

	b := factsBlock()
	facts := b.Facts(factsAsOf)
	var marks []string
	for _, m := range factOfKind(t, facts, domain.ReportFactMilestones).Milestones {
		marks = append(marks, m.Mark)
	}
	if want := []string{domain.MilestoneMarkOverdue, domain.MilestoneMarkNew}; !slices.Equal(marks, want) {
		t.Errorf("marks = %q, want %q", marks, want)
	}
	asOf := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if m := factOfKind(t, b.Facts(asOf), domain.ReportFactMilestones).Milestones[0]; m.Mark != domain.MilestoneMarkNone {
		t.Errorf("Beta's mark on its own date = %q, want none", m.Mark)
	}
	b.Timezone = "Pacific/Kiritimati" // UTC+14: Mar 1 noon UTC is Mar 2 there
	if m := factOfKind(t, b.Facts(asOf), domain.ReportFactMilestones).Milestones[0]; m.Mark != domain.MilestoneMarkOverdue {
		t.Errorf("Beta's mark the day after its date in its zone = %q, want Overdue", m.Mark)
	}

	var readings []string
	for _, m := range factOfKind(t, facts, domain.ReportFactMetrics).Metrics {
		readings = append(readings, m.Reading(func(f float64) string { return "n" }))
	}
	if want := []string{"no reading yet", "n"}; !slices.Equal(readings, want) {
		t.Errorf("readings = %q, want %q", readings, want)
	}
	if got := domain.ReportAgainstATargetOf; got != "against a target of" {
		t.Errorf("a Metric reads %q its target, want against a target of", got)
	}
}

// The Rolled-up Health carries its count line, and the explanation shows only
// beside one (ReportBlock.ShowsExplanation).
func TestReportBlockFactsCarryTheRolledUpHealthAndExplanation(t *testing.T) {
	t.Parallel()

	b := factsBlock()
	facts := b.Facts(factsAsOf)
	rolled := factOfKind(t, facts, domain.ReportFactRolledUp).RolledUp
	if want := (domain.ReportRolledUp{Health: domain.HealthYellow, Present: true, Count: "1 of 3 Yellow"}); rolled != want {
		t.Errorf("rolled up = %+v, want %+v", rolled, want)
	}
	if got := factOfKind(t, facts, domain.ReportFactExplanation).Text; got != "A child is recovering." {
		t.Errorf("explanation = %q", got)
	}

	b.RolledUp = domain.RolledUpHealth{}
	for _, f := range b.Facts(factsAsOf) {
		if f.Kind == domain.ReportFactExplanation || f.Kind == domain.ReportFactRolledUp {
			t.Errorf("block with no Rolled-up Health lists %q", f.Kind)
		}
	}
}
