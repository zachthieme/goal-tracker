package domain

import (
	"bytes"
	"encoding/json"
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
		{Value: ReportBlockHeadFact{ReportBlockHead{
			GoalID: 4, Health: HealthRed, RunOffGreen: "3 wks", WasHealth: "was Green", Title: "Launch in EU",
			Owner: ada, Lifecycle: LifecycleActive, Due: day(4, 30), PriorDue: []time.Time{day(4, 1)}, Badges: []string{BadgeNewDate},
		}}},
		{Label: "Changed", Value: ReportChangedFact{[]string{"Turned Red"}}},
		{Label: "Schedule conflict", Value: ReportScheduleConflictFact{[]ReportScheduleConflictGroup{{
			Words: ReportConflictParentEarlier, DueWords: ReportConflictOtherDue,
			Conflicts: []ReportScheduleConflict{{GoalID: 7, Title: "Open in Iberia", Owner: sam, Due: day(4, 15)}},
		}}}},
		{Label: "Contributes to", Value: ReportContributesToFact{[]LinkedGoal{{GoalID: 5, Title: "Grow revenue", Health: HealthYellow, InReport: true}}}},
		{Value: ReportFieldsFact{[]FieldValue{{Field: Field{Name: "Budget", Type: FieldNumber, Unit: "$"}, Value: "120"}}}},
		{Label: "Why it matters", Value: ReportWhyItMattersFact{"Expand the market."}},
		{Label: "Status", Value: ReportStatusFact{"Blocked on legal."}},
		{Label: "Path to Green", Value: ReportPathToGreenFact{ReportPathToGreen{
			Plan: "Hire counsel.", By: day(3, 1), PriorDates: []time.Time{day(2, 1), day(2, 15)}, Slips: 2, Overdue: true,
			ByWords: ReportBackToGreenBy, OverdueWords: ReportPathOverdue,
		}}},
		{Label: "Asks for help", Value: ReportAskForHelpFact{ReportAsk{Addressee: &sam}}},
		{Label: "Milestones", Value: ReportMilestonesFact{[]ReportMilestoneMark{
			{ReportMilestone: ReportMilestone{Milestone: Milestone{Name: "Beta", TargetDate: day(3, 1), Status: MilestonePlanned}, PriorDates: []time.Time{day(2, 20)}}, Mark: MilestoneMarkOverdue},
			{ReportMilestone: ReportMilestone{Milestone: Milestone{Name: "Pilot", TargetDate: day(4, 1), Status: MilestoneRemoved, RemovedReason: "Folded into Beta."}}, Mark: MilestoneMarkRemoved},
		}}},
		{Label: "Metrics", Value: ReportMetricsFact{[]ReportMetric{
			{Metric: Metric{Name: "NPS", Unit: "pts", Direction: MetricUp, Baseline: 20, Target: 50, TargetDate: day(6, 30)}, NoReadingWords: ReportNoReadingYet, AgainstWords: ReportAgainstATargetOf},
			{Metric: Metric{Name: "Churn", Unit: "%", Direction: MetricDown, Baseline: 9, Target: 4.5, TargetDate: day(6, 30)}, Current: 7.25, Read: true, NoReadingWords: ReportNoReadingYet, AgainstWords: ReportAgainstATargetOf},
		}}},
		{Label: "Rolled-up Health", Value: ReportRolledUpFact{ReportRolledUp{Health: HealthYellow, Present: true, Count: "1 of 3 Yellow"}}},
		{Label: "Supported by", Value: ReportSupportedByFact{[]LinkedGoal{{GoalID: 6, Title: "Alpha ledger"}}}},
		{Label: "Why the Health differs", Value: ReportExplanationFact{"A child is recovering."}},
	}
	asksNoOne := []ReportFact{
		{Value: ReportBlockHeadFact{ReportBlockHead{GoalID: 8, Health: HealthYellow, Title: "Hire a PM", Owner: sam, Lifecycle: LifecycleActive}}},
		{Label: "Why it matters", Value: ReportWhyItMattersFact{}},
		{Label: "Asks for help", Value: ReportAskForHelpFact{ReportAsk{Words: ReportNoOneAboveTheOwner}}},
	}
	return []ReportFact{
		{Label: "Published", Value: ReportBylineFact{ReportByline{
			PublishedBy: ada, PublishedAt: time.Date(2026, 3, 10, 12, 30, 0, 0, time.UTC), Since: since,
		}}},
		{Label: "Selection", Value: ReportSelectionFact{"Owner: Ada Okafor · Active"}},
		{Label: "Health", Value: ReportHealthCountsFact{HealthCounts{Red: 1, Yellow: 1, Green: 1, CheckinOverdue: 1}}},
		{Label: "Since", Value: ReportMovementFact{ReportMovementLine{Since: since, Parts: []string{"1 turned Red"}}}},
		{Label: "Open Action Items", Value: ReportActionItemsFact{[]ActionItem{
			{ID: 11, Text: "Call legal", Owner: ada, ReassignedFrom: &sam, DueDate: day(3, 20), GoalTitle: "Launch in EU"},
		}}},
		{Value: ReportIntroductionFact{"Where Growth stands."}},
		{Value: ReportNarrativeFact{[]ReportNarrativeSection{{
			Heading: "Insights", Notes: []string{"Churn is seasonal.", "Pricing helps."},
			Highlights: []NarrativeHighlight{{Highlight: Highlight{Note: "Signed the first EU customer.", Owner: ada}, GoalID: 4, GoalTitle: "Launch in EU"}},
		}}}},
		{Label: "Exceptions", Value: ReportExceptionsFact{[]ReportBlockFacts{
			{GoalID: 4, Facts: block, Reasons: []ReportReason{ReportReasonRed, ReportReasonCheckinOverdue, ReportReasonTurnedRed}},
			{GoalID: 8, Facts: asksNoOne},
		}}},
		{Label: "On track", Value: ReportOnTrackFact{[]SelectedGoal{
			{Goal: Goal{ID: 9, Title: "Cut churn", Owner: sam, DeliveryDate: day(5, 1)}, Health: HealthGreen, Fields: []FieldValue{{Field: Field{Name: "Region", Type: FieldShortText}, Value: "EMEA"}}},
		}}},
		{Label: "Goals that entered or left this report", Value: ReportMembershipChangesFact{[]MembershipChange{
			{Goal: Goal{ID: 10, Title: "Hire a PM"}, Direction: MembershipAdded, Reason: MembershipPickedByHand},
		}}},
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
			kinds = append(kinds, f.Kind())
			if ex, ok := f.Value.(ReportExceptionsFact); ok {
				for _, b := range ex.Blocks {
					walk(b.Facts)
				}
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

// A publication's facts freeze into exactly the record they froze into before
// each kind of fact held its own value: the same fields under the same names,
// each payload left out when empty, so a record written before then and one
// written since read alike.
func TestPublicationRecordFreezesAsItAlwaysHas(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, defName string
		facts         []ReportFact
		want          string
	}{
		{"every kind of fact", "Growth MBR", recordTestFacts(), `
	{"Version":1,"Name":"Growth MBR","Facts":[
		{"Kind":"byline","Label":"Published","Byline":{"PublishedBy":{"ID":2,"Name":"Ada Okafor","Email":"ada@example.com"},"PublishedAt":"2026-03-10T12:30:00Z","Since":{"At":"2026-02-10T16:00:00Z","Words":"the previous publication"}}},
		{"Kind":"selection","Label":"Selection","Text":"Owner: Ada Okafor · Active"},
		{"Kind":"health counts","Label":"Health","HealthCounts":{"Red":1,"Yellow":1,"Green":1,"CheckinOverdue":1}},
		{"Kind":"movement","Label":"Since","Movement":{"Since":{"At":"2026-02-10T16:00:00Z","Words":"the previous publication"},"Parts":["1 turned Red"]}},
		{"Kind":"open action items","Label":"Open Action Items","ActionItems":[{"ID":11,"Text":"Call legal","Owner":{"ID":2,"Name":"Ada Okafor","Email":"ada@example.com"},"ReassignedFrom":{"ID":3,"Email":"sam@example.com"},"DueDate":"2026-03-20T00:00:00Z","GoalTitle":"Launch in EU"}]},
		{"Kind":"introduction","Text":"Where Growth stands."},
		{"Kind":"narrative","Narrative":[{"Heading":"Insights","Notes":["Churn is seasonal.","Pricing helps."],"Highlights":[{"Note":"Signed the first EU customer.","Owner":{"ID":2,"Name":"Ada Okafor","Email":"ada@example.com"},"GoalID":4,"GoalTitle":"Launch in EU"}]}]},
		{"Kind":"exceptions","Label":"Exceptions","Blocks":[
			{"GoalID":4,"Facts":[
				{"Kind":"block head","Head":{"GoalID":4,"Health":"Red","RunOffGreen":"3 wks","WasHealth":"was Green","Title":"Launch in EU","Owner":{"ID":2,"Name":"Ada Okafor","Email":"ada@example.com"},"Lifecycle":"Active","Due":"2026-04-30T00:00:00Z","PriorDue":["2026-04-01T00:00:00Z"],"Badges":["New Date"]}},
				{"Kind":"changed","Label":"Changed","Changes":["Turned Red"]},
				{"Kind":"schedule conflict","Label":"Schedule conflict","ScheduleConflictGroups":[{"Words":"Parent Goal due earlier","DueWords":"due","Conflicts":[{"GoalID":7,"Title":"Open in Iberia","Owner":{"ID":3,"Email":"sam@example.com"},"Due":"2026-04-15T00:00:00Z"}]}]},
				{"Kind":"contributes to","Label":"Contributes to","Goals":[{"GoalID":5,"Title":"Grow revenue","Health":"Yellow","InReport":true}]},
				{"Kind":"fields","Fields":[{"Name":"Budget","Type":"number","Unit":"$","Value":"120"}]},
				{"Kind":"why it matters","Label":"Why it matters","Text":"Expand the market."},
				{"Kind":"status","Label":"Status","Text":"Blocked on legal."},
				{"Kind":"path to green","Label":"Path to Green","PathToGreen":{"Plan":"Hire counsel.","By":"2026-03-01T00:00:00Z","PriorDates":["2026-02-01T00:00:00Z","2026-02-15T00:00:00Z"],"Slips":2,"Overdue":true,"ByWords":"back to Green by","OverdueWords":"Overdue"}},
				{"Kind":"asks for help","Label":"Asks for help","Ask":{"Addressee":{"ID":3,"Email":"sam@example.com"}}},
				{"Kind":"milestones","Label":"Milestones","Milestones":[{"Name":"Beta","TargetDate":"2026-03-01T00:00:00Z","Status":"Planned","PriorDates":["2026-02-20T00:00:00Z"],"Mark":"Overdue"},{"Name":"Pilot","TargetDate":"2026-04-01T00:00:00Z","Status":"Removed","RemovedReason":"Folded into Beta.","Mark":"Removed"}]},
				{"Kind":"metrics","Label":"Metrics","Metrics":[{"Name":"NPS","Unit":"pts","Direction":"up","Baseline":20,"Target":50,"TargetDate":"2026-06-30T00:00:00Z","NoReadingWords":"no reading yet","AgainstWords":"against a target of"},{"Name":"Churn","Unit":"%","Direction":"down","Baseline":9,"Target":4.5,"TargetDate":"2026-06-30T00:00:00Z","Current":7.25,"Read":true,"NoReadingWords":"no reading yet","AgainstWords":"against a target of"}]},
				{"Kind":"rolled-up health","Label":"Rolled-up Health","RolledUp":{"Health":"Yellow","Present":true,"Count":"1 of 3 Yellow"}},
				{"Kind":"supported by","Label":"Supported by","Goals":[{"GoalID":6,"Title":"Alpha ledger"}]},
				{"Kind":"why the health differs","Label":"Why the Health differs","Text":"A child is recovering."}
			],"Reasons":["red","check-in overdue","turned red"]},
			{"GoalID":8,"Facts":[
				{"Kind":"block head","Head":{"GoalID":8,"Health":"Yellow","Title":"Hire a PM","Owner":{"ID":3,"Email":"sam@example.com"},"Lifecycle":"Active","Due":"0001-01-01T00:00:00Z"}},
				{"Kind":"why it matters","Label":"Why it matters"},
				{"Kind":"asks for help","Label":"Asks for help","Ask":{"Words":"no one above the Owner"}}
			]}
		]},
		{"Kind":"on track","Label":"On track","Lines":[{"GoalID":9,"Title":"Cut churn","Owner":{"ID":3,"Email":"sam@example.com"},"Due":"2026-05-01T00:00:00Z","Health":"Green","Fields":[{"Name":"Region","Type":"short_text","Value":"EMEA"}]}]},
		{"Kind":"membership changes","Label":"Goals that entered or left this report","MembershipChanges":[{"GoalID":10,"Title":"Hire a PM","Direction":"Added","Reason":"picked by hand"}]}
	]}`},
		{"payloads left empty", "MBR", []ReportFact{
			{Label: "Published", Value: ReportBylineFact{}},
			{Label: "Health", Value: ReportHealthCountsFact{}},
			{Label: "Since", Value: ReportMovementFact{}},
			{Label: "Exceptions", Value: ReportExceptionsFact{[]ReportBlockFacts{{GoalID: 1, Facts: []ReportFact{
				{Value: ReportBlockHeadFact{}},
				{Label: "Why it matters", Value: ReportWhyItMattersFact{}},
				{Label: "Path to Green", Value: ReportPathToGreenFact{}},
				{Label: "Rolled-up Health", Value: ReportRolledUpFact{}},
			}}}}},
		}, `
	{"Version":1,"Name":"MBR","Facts":[
		{"Kind":"byline","Label":"Published"},
		{"Kind":"health counts","Label":"Health"},
		{"Kind":"movement","Label":"Since","Movement":{"Since":{"At":"0001-01-01T00:00:00Z"}}},
		{"Kind":"exceptions","Label":"Exceptions","Blocks":[
			{"GoalID":1,"Facts":[
				{"Kind":"block head","Head":{"GoalID":0,"Title":"","Owner":{"ID":0,"Email":""},"Lifecycle":"","Due":"0001-01-01T00:00:00Z"}},
				{"Kind":"why it matters","Label":"Why it matters"},
				{"Kind":"path to green","Label":"Path to Green","PathToGreen":{"Plan":"","By":"0001-01-01T00:00:00Z","ByWords":"","OverdueWords":""}},
				{"Kind":"rolled-up health","Label":"Rolled-up Health"}
			]}
		]}
	]}`},
	} {
		data, err := freezePublication(tc.defName, tc.facts)
		if err != nil {
			t.Fatalf("%s: freezePublication: %v", tc.name, err)
		}
		var want bytes.Buffer
		if err := json.Compact(&want, []byte(tc.want)); err != nil {
			t.Fatalf("%s: the test's record: %v", tc.name, err)
		}
		if !bytes.Equal(data, want.Bytes()) {
			t.Errorf("%s: froze\n%s\nwant\n%s", tc.name, data, want.Bytes())
		}
	}
}

// A record holding a fact of a kind no Report lists is refused, naming the
// kind, wherever the fact sits.
func TestPublicationRecordRefusesAnUnknownKindOfFact(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, data, want string
	}{
		{"a Report's fact", `{"Version":1,"Name":"MBR","Facts":[{"Kind":"bogus","Label":"Bogus"}]}`, `publication record fact kind "bogus"`},
		{"a block's fact", `{"Version":1,"Name":"MBR","Facts":[{"Kind":"exceptions","Label":"Exceptions","Blocks":[{"GoalID":4,"Facts":[
			{"Kind":"block head","Head":{"GoalID":4,"Title":"Launch in EU","Owner":{"ID":2,"Email":"ada@example.com"},"Lifecycle":"Active","Due":"2026-04-30T00:00:00Z"}},
			{"Kind":"forecast","Label":"Forecast","Text":"Sunny."}
		]}]}]}`, `publication record fact kind "forecast"`},
	} {
		_, _, err := thawPublication([]byte(tc.data))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: thawPublication err = %v, want %q", tc.name, err, tc.want)
		}
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
	data, err := freezePublication("MBR", []ReportFact{{Label: "Published", Value: ReportBylineFact{ReportByline{PublishedAt: at}}}})
	if err != nil {
		t.Fatalf("freezePublication: %v", err)
	}
	_, got, err := thawPublication(data)
	if err != nil {
		t.Fatalf("thawPublication: %v", err)
	}
	if published := got[0].Value.(ReportBylineFact).Byline.PublishedAt; published != at.UTC() {
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
	if name != "Growth MBR" || len(facts) != 1 {
		t.Fatalf("thawed %q %+v, want Growth MBR with one fact", name, facts)
	}
	exceptions, ok := facts[0].Value.(ReportExceptionsFact)
	if !ok || len(exceptions.Blocks) != 1 {
		t.Fatalf("thawed %+v, want the exceptions with one block", facts[0])
	}
	block := exceptions.Blocks[0].Facts
	of := func(kind ReportFactKind) ReportFact {
		for _, f := range block {
			if f.Kind() == kind {
				return f
			}
		}
		t.Fatalf("block has no %q fact", kind)
		return ReportFact{}
	}
	if g := of(ReportFactScheduleConflict).Value.(ReportScheduleConflictFact).Groups; len(g) != 1 || g[0].Words != "Parent due sooner" || g[0].DueWords != "by" {
		t.Errorf("Schedule conflict groups %+v, want Parent due sooner, by", g)
	}
	if label := of(ReportFactWhyItMatters).Label; label != "Why this matters" {
		t.Errorf("Why it matters reads %q, want Why this matters", label)
	}
	if path := of(ReportFactPathToGreen).Value.(ReportPathToGreenFact).Path; path.ByWords != "green again by" || path.OverdueWords != "Late" {
		t.Errorf("Path to Green reads %q and %q, want green again by and Late", path.ByWords, path.OverdueWords)
	}
	if m := of(ReportFactMetrics).Value.(ReportMetricsFact).Metrics[0]; m.Reading(func(float64) string { return "n" }) != "not read yet" || m.AgainstWords != "toward" {
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
