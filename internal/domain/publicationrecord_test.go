package domain

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// recordTestFacts is a publication's facts holding every kind of fact, each
// with every field the page, Print and the Markdown export read of it set, and
// nothing else: what a record must give back as it went in.
func recordTestFacts() []ReportFact {
	ada := Account{ID: 2, Name: "Ada Okafor", Email: "ada@example.com"}
	sam := Account{ID: 3, Email: "sam@example.com"}
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	since := ReportSince{At: time.Date(2026, 2, 10, 16, 0, 0, 0, time.UTC), Words: ReportPreviousPublication}
	block := []ReportFact{
		{Kind: ReportFactBlockHead, Head: ReportBlockHead{
			GoalID: 4, Health: HealthRed, RunOffGreen: "3 wks", WasHealth: "was Green", Title: "Launch in EU",
			Owner: ada, Lifecycle: LifecycleActive, Due: day(4, 30), PriorDue: []time.Time{day(4, 1)}, Badges: []string{BadgeNewDate},
		}},
		{Kind: ReportFactChanged, Label: "Changed", Changes: []string{"Turned Red"}},
		{Kind: ReportFactScheduleConflict, Label: "Schedule conflict", ScheduleConflictGroups: []ReportScheduleConflictGroup{{
			Words: ReportConflictParentEarlier, DueWords: ReportConflictOtherDue,
			Conflicts: []ReportScheduleConflict{{GoalID: 7, Title: "Open in Iberia", Owner: sam, Due: day(4, 15)}},
		}}},
		{Kind: ReportFactContributesTo, Label: "Contributes to", Goals: []LinkedGoal{{GoalID: 5, Title: "Grow revenue", Health: HealthYellow, InReport: true}}},
		{Kind: ReportFactFields, Fields: []FieldValue{{Field: Field{Name: "Budget", Type: FieldNumber, Unit: "$"}, Value: "120"}}},
		{Kind: ReportFactWhyItMatters, Label: "Why it matters", Text: "Expand the market."},
		{Kind: ReportFactStatus, Label: "Status", Text: "Blocked on legal."},
		{Kind: ReportFactPathToGreen, Label: "Path to Green", PathToGreen: ReportPathToGreen{
			Plan: "Hire counsel.", By: day(3, 1), PriorDates: []time.Time{day(2, 1), day(2, 15)}, Slips: 2, Overdue: true,
			ByWords: ReportBackToGreenBy, OverdueWords: ReportPathOverdue,
		}},
		{Kind: ReportFactAskForHelp, Label: "Asks for help", Ask: ReportAsk{Addressee: &sam}},
		{Kind: ReportFactMilestones, Label: "Milestones", Milestones: []ReportMilestoneMark{
			{ReportMilestone: ReportMilestone{Milestone: Milestone{Name: "Beta", TargetDate: day(3, 1), Status: MilestonePlanned}, PriorDates: []time.Time{day(2, 20)}}, Mark: MilestoneMarkOverdue},
			{ReportMilestone: ReportMilestone{Milestone: Milestone{Name: "Pilot", TargetDate: day(4, 1), Status: MilestoneRemoved, RemovedReason: "Folded into Beta."}}, Mark: MilestoneMarkRemoved},
		}},
		{Kind: ReportFactMetrics, Label: "Metrics", Metrics: []ReportMetric{
			{Metric: Metric{Name: "NPS", Unit: "pts", Direction: MetricUp, Baseline: 20, Target: 50, TargetDate: day(6, 30)}, NoReadingWords: ReportNoReadingYet, AgainstWords: ReportAgainstATargetOf},
			{Metric: Metric{Name: "Churn", Unit: "%", Direction: MetricDown, Baseline: 9, Target: 4.5, TargetDate: day(6, 30)}, Current: 7.25, Read: true, NoReadingWords: ReportNoReadingYet, AgainstWords: ReportAgainstATargetOf},
		}},
		{Kind: ReportFactRolledUp, Label: "Rolled-up Health", RolledUp: ReportRolledUp{Health: HealthYellow, Present: true, Count: "1 of 3 Yellow"}},
		{Kind: ReportFactSupportedBy, Label: "Supported by", Goals: []LinkedGoal{{GoalID: 6, Title: "Alpha ledger"}}},
		{Kind: ReportFactExplanation, Label: "Why the Health differs", Text: "A child is recovering."},
	}
	asksNoOne := []ReportFact{
		{Kind: ReportFactBlockHead, Head: ReportBlockHead{GoalID: 8, Health: HealthYellow, Title: "Hire a PM", Owner: sam, Lifecycle: LifecycleActive}},
		{Kind: ReportFactWhyItMatters, Label: "Why it matters"},
		{Kind: ReportFactAskForHelp, Label: "Asks for help", Ask: ReportAsk{Words: ReportNoOneAboveTheOwner}},
	}
	return []ReportFact{
		{Kind: ReportFactByline, Label: "Published", Byline: ReportByline{
			PublishedBy: ada, PublishedAt: time.Date(2026, 3, 10, 12, 30, 0, 0, time.UTC), Since: since,
		}},
		{Kind: ReportFactSelection, Label: "Selection", Text: "Owner: Ada Okafor · Active"},
		{Kind: ReportFactHealthCounts, Label: "Health", HealthCounts: HealthCounts{Red: 1, Yellow: 1, Green: 1, CheckinOverdue: 1}},
		{Kind: ReportFactMovement, Label: "Since", Movement: ReportMovementLine{Since: since, Parts: []string{"1 turned Red"}}},
		{Kind: ReportFactActionItems, Label: "Open Action Items", ActionItems: []ActionItem{
			{ID: 11, Text: "Call legal", Owner: ada, ReassignedFrom: &sam, DueDate: day(3, 20), GoalTitle: "Launch in EU"},
		}},
		{Kind: ReportFactIntroduction, Text: "Where Growth stands."},
		{Kind: ReportFactNarrative, Narrative: []ReportNarrativeSection{{
			Heading: "Insights", Notes: []string{"Churn is seasonal.", "Pricing helps."},
			Highlights: []NarrativeHighlight{{Highlight: Highlight{Note: "Signed the first EU customer.", Owner: ada}, GoalID: 4, GoalTitle: "Launch in EU"}},
		}}},
		{Kind: ReportFactExceptions, Label: "Exceptions", Blocks: []ReportBlockFacts{{GoalID: 4, Facts: block}, {GoalID: 8, Facts: asksNoOne}}},
		{Kind: ReportFactOnTrack, Label: "On track", Lines: []SelectedGoal{
			{Goal: Goal{ID: 9, Title: "Cut churn", Owner: sam, DeliveryDate: day(5, 1)}, Health: HealthGreen, Fields: []FieldValue{{Field: Field{Name: "Region", Type: FieldShortText}, Value: "EMEA"}}},
		}},
		{Kind: ReportFactMembershipChanges, Label: "Goals that entered or left this report", MembershipChanges: []MembershipChange{
			{Goal: Goal{ID: 10, Title: "Hire a PM"}, Direction: MembershipAdded, Reason: MembershipPickedByHand},
		}},
	}
}

// A publication's facts, every kind of fact with every field its outputs
// read, freeze into a record and thaw back exactly as they went in, the
// Report Definition's name with them.
func TestPublicationRecordGivesBackItsFacts(t *testing.T) {
	t.Parallel()

	facts := recordTestFacts()
	var kinds []ReportFactKind
	var walk func([]ReportFact)
	walk = func(fs []ReportFact) {
		for _, f := range fs {
			kinds = append(kinds, f.Kind)
			for _, b := range f.Blocks {
				walk(b.Facts)
			}
		}
	}
	walk(facts)
	for _, kind := range AllFactKinds {
		if !slices.Contains(kinds, kind) {
			t.Errorf("the test's facts hold no %q fact", kind)
		}
	}

	data, err := freezePublication("Growth MBR", facts)
	if err != nil {
		t.Fatalf("freezePublication: %v", err)
	}
	name, got, err := thawPublication(data)
	if err != nil {
		t.Fatalf("thawPublication: %v", err)
	}
	if name != "Growth MBR" {
		t.Errorf("name = %q, want Growth MBR", name)
	}
	if !reflect.DeepEqual(got, facts) {
		t.Errorf("thawed facts =\n%+v\nwant\n%+v", got, facts)
	}
}

// A record holds its times in UTC, whatever zone they were frozen in.
func TestPublicationRecordHoldsTimesInUTC(t *testing.T) {
	t.Parallel()

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	at := time.Date(2026, 3, 11, 1, 0, 0, 0, tokyo)
	data, err := freezePublication("MBR", []ReportFact{{Kind: ReportFactByline, Label: "Published", Byline: ReportByline{PublishedAt: at}}})
	if err != nil {
		t.Fatalf("freezePublication: %v", err)
	}
	_, got, err := thawPublication(data)
	if err != nil {
		t.Fatalf("thawPublication: %v", err)
	}
	if published := got[0].Byline.PublishedAt; published != at.UTC() {
		t.Errorf("published at %v, want %v in UTC", published, at.UTC())
	}
}

// A version-1 record reads back in its own words, not in today's: rewording a
// fact later never changes how an earlier publication reads.
func TestPublicationRecordReadsInItsOwnWords(t *testing.T) {
	t.Parallel()

	data := `{"Version":1,"Name":"Growth MBR","Facts":[
		{"Kind":"exceptions","Label":"Exceptions","Blocks":[{"GoalID":4,"Facts":[
			{"Kind":"block head","Head":{"GoalID":4,"Health":"Red","Title":"Launch in EU","Owner":{"ID":2,"Name":"Ada Okafor","Email":"ada@example.com"},"Lifecycle":"Active","Due":"2026-04-30T00:00:00Z"}},
			{"Kind":"schedule conflict","Label":"Schedule conflict","ScheduleConflictGroups":[{"Words":"Parent due sooner","DueWords":"by","Conflicts":[
				{"GoalID":7,"Title":"Open in Iberia","Owner":{"ID":3,"Email":"sam@example.com"},"Due":"2026-04-15T00:00:00Z"}]}]},
			{"Kind":"why it matters","Label":"Why this matters","Text":"Expand the market."},
			{"Kind":"path to green","Label":"Path to Green","PathToGreen":{"Plan":"Hire counsel.","By":"2026-03-01T00:00:00Z","ByWords":"green again by","OverdueWords":"Late"}},
			{"Kind":"metrics","Label":"Metrics","Metrics":[{"Name":"NPS","Direction":"up","Baseline":20,"Target":50,"TargetDate":"2026-06-30T00:00:00Z","NoReadingWords":"not read yet","AgainstWords":"toward"}]}
		]}]}
	]}`
	name, facts, err := thawPublication([]byte(data))
	if err != nil {
		t.Fatalf("thawPublication: %v", err)
	}
	if name != "Growth MBR" || len(facts) != 1 || len(facts[0].Blocks) != 1 {
		t.Fatalf("thawed %q %+v, want Growth MBR with one block", name, facts)
	}
	block := facts[0].Blocks[0].Facts
	of := func(kind ReportFactKind) ReportFact {
		for _, f := range block {
			if f.Kind == kind {
				return f
			}
		}
		t.Fatalf("block has no %q fact", kind)
		return ReportFact{}
	}
	if g := of(ReportFactScheduleConflict).ScheduleConflictGroups; len(g) != 1 || g[0].Words != "Parent due sooner" || g[0].DueWords != "by" {
		t.Errorf("Schedule conflict groups %+v, want Parent due sooner, by", g)
	}
	if label := of(ReportFactWhyItMatters).Label; label != "Why this matters" {
		t.Errorf("Why it matters reads %q, want Why this matters", label)
	}
	if path := of(ReportFactPathToGreen).PathToGreen; path.ByWords != "green again by" || path.OverdueWords != "Late" {
		t.Errorf("Path to Green reads %q and %q, want green again by and Late", path.ByWords, path.OverdueWords)
	}
	if m := of(ReportFactMetrics).Metrics[0]; m.Reading(func(float64) string { return "n" }) != "not read yet" || m.AgainstWords != "toward" {
		t.Errorf("Metric reads %q against %q, want not read yet toward", m.Reading(func(float64) string { return "n" }), m.AgainstWords)
	}
}

// A record of any version but the current one is refused, naming the version:
// one with none is how a publication stored before records reads.
func TestPublicationRecordRefusesAnotherVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, data, want string
	}{
		{"no version", `{"Definition":{"Name":"MBR"},"Exceptions":[]}`, "publication record version 0, want 1"},
		{"version 2", `{"Version":2,"Name":"MBR","Facts":[]}`, "publication record version 2, want 1"},
	} {
		_, _, err := thawPublication([]byte(tc.data))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: thawPublication err = %v, want %q", tc.name, err, tc.want)
		}
	}
}
