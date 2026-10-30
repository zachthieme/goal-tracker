package domain

import "time"

// ReportFactKind names one kind of fact a Report shows: one of its own parts
// (Report.Facts) or one part of an exception block (ReportBlock.Facts).
type ReportFactKind string

// The kinds of a Report's own facts, in the order PublicationFacts lists
// them.
const (
	ReportFactByline            ReportFactKind = "byline"
	ReportFactSelection         ReportFactKind = "selection"
	ReportFactHealthCounts      ReportFactKind = "health counts"
	ReportFactMovement          ReportFactKind = "movement"
	ReportFactActionItems       ReportFactKind = "open action items"
	ReportFactIntroduction      ReportFactKind = "introduction"
	ReportFactNarrative         ReportFactKind = "narrative"
	ReportFactExceptions        ReportFactKind = "exceptions"
	ReportFactOnTrack           ReportFactKind = "on track"
	ReportFactMembershipChanges ReportFactKind = "membership changes"
)

// The kinds of an exception block's facts, in the order ReportBlock.Facts
// lists them.
const (
	ReportFactBlockHead        ReportFactKind = "block head"
	ReportFactChanged          ReportFactKind = "changed"
	ReportFactScheduleConflict ReportFactKind = "schedule conflict"
	ReportFactContributesTo    ReportFactKind = "contributes to"
	ReportFactFields           ReportFactKind = "fields"
	ReportFactWhyItMatters     ReportFactKind = "why it matters"
	ReportFactStatus           ReportFactKind = "status"
	ReportFactPathToGreen      ReportFactKind = "path to green"
	ReportFactAskForHelp       ReportFactKind = "asks for help"
	ReportFactMilestones       ReportFactKind = "milestones"
	ReportFactMetrics          ReportFactKind = "metrics"
	ReportFactRolledUp         ReportFactKind = "rolled-up health"
	ReportFactSupportedBy      ReportFactKind = "supported by"
	ReportFactExplanation      ReportFactKind = "why the health differs"
)

// AllFactKinds is every kind of fact a Report or an exception block lists, so
// a test can check each output shows every one.
var AllFactKinds = []ReportFactKind{
	ReportFactByline, ReportFactSelection, ReportFactHealthCounts, ReportFactMovement,
	ReportFactActionItems, ReportFactIntroduction, ReportFactNarrative,
	ReportFactExceptions, ReportFactOnTrack, ReportFactMembershipChanges,
	ReportFactBlockHead, ReportFactChanged, ReportFactScheduleConflict, ReportFactContributesTo, ReportFactFields,
	ReportFactWhyItMatters, ReportFactStatus, ReportFactPathToGreen, ReportFactAskForHelp,
	ReportFactMilestones, ReportFactMetrics, ReportFactRolledUp, ReportFactSupportedBy,
	ReportFactExplanation,
}

// The fixed phrases a Report's facts read in, on its page and in its export
// alike.
const (
	// ReportPreviousPublication is what a Report reads changes against when
	// it reads them against its previous publication.
	ReportPreviousPublication = "the previous publication"
	// ReportNoReadingYet stands for a Metric's reading when it has none.
	ReportNoReadingYet = "no reading yet"
	// ReportAgainstATargetOf joins a Metric's reading to its target.
	ReportAgainstATargetOf = "against a target of"
	// ReportNoOneAboveTheOwner is whom an Ask for help asks when no one is
	// above the Owner.
	ReportNoOneAboveTheOwner = "no one above the Owner"
	// ReportBackToGreenBy leads a Path to Green's back-to-Green date.
	ReportBackToGreenBy = "back to Green by"
	// ReportPathOverdue marks a Path to Green whose back-to-Green date passed.
	ReportPathOverdue = "Overdue"
	// A Schedule conflict's group opens with its side's words
	// (ReportScheduleConflictGroups), and each Goal in it reads with its
	// Owner, then "due" before its delivery date.
	ReportConflictParentEarlier     = "Parent Goal due earlier"
	ReportConflictContributingLater = "Contributing Goal due later"
	ReportConflictOtherDue          = "due"
)

// ReportScheduleConflictGroup is one side of a block's Schedule conflicts: the
// words it opens with, the Goals at the other end, in the block's order, and
// the word each reads before its delivery date, "due".
type ReportScheduleConflictGroup struct {
	Words     string
	DueWords  string
	Conflicts []ReportScheduleConflict
}

// ReportScheduleConflictGroups splits a block's Schedule conflicts into at
// most two groups, each only when it has a conflict: the block's Goal as the
// child first, "Parent Goal due earlier · Launch invoicing for enterprise
// (Amara Nwosu), due Nov 30, 2026", then as the parent, "Contributing Goal
// due later · Branded PDF invoices (Noor Haddad), due Dec 14, 2026", each
// keeping the order the block gave it. Each output only formats the groups:
// Goals within one joined by " · ", the two joined by "; ", and never the
// block's own delivery date, which its head shows.
func ReportScheduleConflictGroups(conflicts []ReportScheduleConflict) []ReportScheduleConflictGroup {
	var groups []ReportScheduleConflictGroup
	for _, side := range []struct {
		asParent bool
		words    string
	}{{false, ReportConflictParentEarlier}, {true, ReportConflictContributingLater}} {
		var cs []ReportScheduleConflict
		for _, c := range conflicts {
			if c.AsParent == side.asParent {
				cs = append(cs, c)
			}
		}
		if len(cs) > 0 {
			groups = append(groups, ReportScheduleConflictGroup{Words: side.words, DueWords: ReportConflictOtherDue, Conflicts: cs})
		}
	}
	return groups
}

// ReportFact is one fact a Report shows, in the order it shows them: the label
// every output shows it under ("" for one shown without, as the introduction
// and the narrative are), and its value, of the type its kind names. Each
// output only formats it: whether a fact shows, where, and in what words is
// the list's (CONTEXT.md: Report). No list of facts holds one without a value.
type ReportFact struct {
	Label string
	Value ReportFactValue
}

// Kind is the kind of fact f is, its value's, or "" when it has none.
func (f ReportFact) Kind() ReportFactKind {
	if f.Value == nil {
		return ""
	}
	return f.Value.Kind()
}

// ReportFactValue is what one fact of a Report holds: one Report<Kind>Fact
// type per entry in AllFactKinds, and no other, so an output switching on
// its type reads each kind's own values.
type ReportFactValue interface {
	Kind() ReportFactKind
	reportFactValue()
}

// ReportBylineFact is who published the Report, when, and what it reads
// changes against.
type ReportBylineFact struct{ Byline ReportByline }

// ReportSelectionFact is how the Report selected its Goals, in plain words.
type ReportSelectionFact struct{ Text string }

// ReportHealthCountsFact counts the Report's Goals by Health
// (Report.HealthCounts).
type ReportHealthCountsFact struct{ Counts HealthCounts }

// ReportMovementFact is the line counting what moved since the baseline.
type ReportMovementFact struct{ Line ReportMovementLine }

// ReportActionItemsFact is the Report's open Action Items.
type ReportActionItemsFact struct{ Items []ActionItem }

// ReportIntroductionFact is the Report Definition's introduction.
type ReportIntroductionFact struct{ Text string }

// ReportNarrativeFact is the narrative's sections, each under its own heading.
type ReportNarrativeFact struct{ Sections []ReportNarrativeSection }

// ReportExceptionsFact is the exception blocks, each its own facts
// (ReportBlock.Facts).
type ReportExceptionsFact struct{ Blocks []ReportBlockFacts }

// ReportOnTrackFact is the one-line Goals, On track.
type ReportOnTrackFact struct{ Lines []SelectedGoal }

// ReportMembershipChangesFact is the Goals that entered or left the Report.
type ReportMembershipChangesFact struct{ Changes []MembershipChange }

// ReportBlockHeadFact is an exception block's head.
type ReportBlockHeadFact struct{ Head ReportBlockHead }

// ReportChangedFact says what changed on the block's Goal since the baseline.
type ReportChangedFact struct{ Changes []string }

// ReportScheduleConflictFact is the Schedule conflicts the block's Goal is in,
// by side (ReportScheduleConflictGroups).
type ReportScheduleConflictFact struct {
	Groups []ReportScheduleConflictGroup
}

// ReportContributesToFact is the Goals the block's Goal contributes to.
type ReportContributesToFact struct{ Goals []LinkedGoal }

// ReportFieldsFact is the block's values in the Fields the Report chose.
type ReportFieldsFact struct{ Values []FieldValue }

// ReportWhyItMattersFact is the block's Why it matters, even when empty.
type ReportWhyItMattersFact struct{ Text string }

// ReportStatusFact is the block's status.
type ReportStatusFact struct{ Text string }

// ReportPathToGreenFact is the block's plan back to Green.
type ReportPathToGreenFact struct{ Path ReportPathToGreen }

// ReportAskForHelpFact is the block's Ask for help.
type ReportAskForHelpFact struct{ Ask ReportAsk }

// ReportMilestonesFact is the block's Milestones, each with its mark.
type ReportMilestonesFact struct{ Marks []ReportMilestoneMark }

// ReportMetricsFact is the block's Metrics against their targets.
type ReportMetricsFact struct{ Metrics []ReportMetric }

// ReportRolledUpFact is the block's Rolled-up Health with its count line.
type ReportRolledUpFact struct{ RolledUp ReportRolledUp }

// ReportSupportedByFact is the Goals behind the block's Goal, Supported by.
type ReportSupportedByFact struct{ Goals []LinkedGoal }

// ReportExplanationFact is the Owner's explanation of why the block's Health
// differs from its Rolled-up Health.
type ReportExplanationFact struct{ Text string }

func (ReportBylineFact) Kind() ReportFactKind            { return ReportFactByline }
func (ReportSelectionFact) Kind() ReportFactKind         { return ReportFactSelection }
func (ReportHealthCountsFact) Kind() ReportFactKind      { return ReportFactHealthCounts }
func (ReportMovementFact) Kind() ReportFactKind          { return ReportFactMovement }
func (ReportActionItemsFact) Kind() ReportFactKind       { return ReportFactActionItems }
func (ReportIntroductionFact) Kind() ReportFactKind      { return ReportFactIntroduction }
func (ReportNarrativeFact) Kind() ReportFactKind         { return ReportFactNarrative }
func (ReportExceptionsFact) Kind() ReportFactKind        { return ReportFactExceptions }
func (ReportOnTrackFact) Kind() ReportFactKind           { return ReportFactOnTrack }
func (ReportMembershipChangesFact) Kind() ReportFactKind { return ReportFactMembershipChanges }
func (ReportBlockHeadFact) Kind() ReportFactKind         { return ReportFactBlockHead }
func (ReportChangedFact) Kind() ReportFactKind           { return ReportFactChanged }
func (ReportScheduleConflictFact) Kind() ReportFactKind  { return ReportFactScheduleConflict }
func (ReportContributesToFact) Kind() ReportFactKind     { return ReportFactContributesTo }
func (ReportFieldsFact) Kind() ReportFactKind            { return ReportFactFields }
func (ReportWhyItMattersFact) Kind() ReportFactKind      { return ReportFactWhyItMatters }
func (ReportStatusFact) Kind() ReportFactKind            { return ReportFactStatus }
func (ReportPathToGreenFact) Kind() ReportFactKind       { return ReportFactPathToGreen }
func (ReportAskForHelpFact) Kind() ReportFactKind        { return ReportFactAskForHelp }
func (ReportMilestonesFact) Kind() ReportFactKind        { return ReportFactMilestones }
func (ReportMetricsFact) Kind() ReportFactKind           { return ReportFactMetrics }
func (ReportRolledUpFact) Kind() ReportFactKind          { return ReportFactRolledUp }
func (ReportSupportedByFact) Kind() ReportFactKind       { return ReportFactSupportedBy }
func (ReportExplanationFact) Kind() ReportFactKind       { return ReportFactExplanation }

func (ReportBylineFact) reportFactValue()            {}
func (ReportSelectionFact) reportFactValue()         {}
func (ReportHealthCountsFact) reportFactValue()      {}
func (ReportMovementFact) reportFactValue()          {}
func (ReportActionItemsFact) reportFactValue()       {}
func (ReportIntroductionFact) reportFactValue()      {}
func (ReportNarrativeFact) reportFactValue()         {}
func (ReportExceptionsFact) reportFactValue()        {}
func (ReportOnTrackFact) reportFactValue()           {}
func (ReportMembershipChangesFact) reportFactValue() {}
func (ReportBlockHeadFact) reportFactValue()         {}
func (ReportChangedFact) reportFactValue()           {}
func (ReportScheduleConflictFact) reportFactValue()  {}
func (ReportContributesToFact) reportFactValue()     {}
func (ReportFieldsFact) reportFactValue()            {}
func (ReportWhyItMattersFact) reportFactValue()      {}
func (ReportStatusFact) reportFactValue()            {}
func (ReportPathToGreenFact) reportFactValue()       {}
func (ReportAskForHelpFact) reportFactValue()        {}
func (ReportMilestonesFact) reportFactValue()        {}
func (ReportMetricsFact) reportFactValue()           {}
func (ReportRolledUpFact) reportFactValue()          {}
func (ReportSupportedByFact) reportFactValue()       {}
func (ReportExplanationFact) reportFactValue()       {}

// ReportByline says who published a Report and when, and what it reads its
// changes against.
type ReportByline struct {
	PublishedBy Account
	PublishedAt time.Time
	Since       ReportSince
}

// ReportSince is what a Report reads its changes against: its previous
// publication, from the instant it was published, or its baseline date.
type ReportSince struct {
	// At is the instant the previous publication was published, or the
	// baseline date.
	At time.Time
	// Words name what it reads them against when it is the previous
	// publication, "the previous publication"; read against a date, they are
	// "", and each output writes the date as it writes dates.
	Words string
}

// ReportMovementLine is the line counting the Goals that moved since what the
// Report reads its changes against: its parts ("2 turned Red"), none when
// nothing moved.
type ReportMovementLine struct {
	Since ReportSince
	Parts []string
}

// Since is what r reads its changes against.
func (r Report) Since() ReportSince {
	if r.Previous.ID != 0 {
		return ReportSince{At: r.Previous.PublishedAt, Words: ReportPreviousPublication}
	}
	return ReportSince{At: r.Baseline}
}

// ReportNarrativeSection is one section of a Report's narrative as its facts
// show it: its heading ("Insights"), the author's notes, and the Highlights
// they picked into it.
type ReportNarrativeSection struct {
	Heading    string
	Notes      []string
	Highlights []NarrativeHighlight
}

// ReportBlockFacts is one exception block as its facts list it
// (ReportBlock.Facts), with the id of its Goal and the exception reasons the
// Report judged it one for (ReportBlock.Reasons).
type ReportBlockFacts struct {
	GoalID  int64
	Facts   []ReportFact
	Reasons []ReportReason
}

// PublicationFacts lists the facts of r published by by at the instant at, in
// order: its byline, how it selected its Goals in plain words (scope, left out
// when ""), then r's own (Report.Facts), its blocks read as of at. They are
// what a publication freezes.
func PublicationFacts(r Report, by Account, at time.Time, scope string) []ReportFact {
	facts := []ReportFact{{
		Label: "Published",
		Value: ReportBylineFact{ReportByline{PublishedBy: by, PublishedAt: at, Since: r.Since()}},
	}}
	if scope != "" {
		facts = append(facts, ReportFact{Label: "Selection", Value: ReportSelectionFact{scope}})
	}
	return append(facts, r.Facts(at)...)
}

// Facts lists a Report's own parts in order, each only when it has it: its
// Goals counted by Health when it selects any, the line counting what moved,
// its open Action Items, the introduction, the narrative, the exceptions, each
// block's facts read as of asOf (ReportBlock.Facts), the one-line Goals On
// track, and the Goals that entered or left it. asOf is the instant its Report
// is published, or now on a draft.
func (r Report) Facts(asOf time.Time) []ReportFact {
	var facts []ReportFact
	if len(r.Exceptions) > 0 || len(r.Lines) > 0 {
		facts = append(facts, ReportFact{Label: "Health", Value: ReportHealthCountsFact{r.HealthCounts()}})
	}
	if r.Movement != nil {
		facts = append(facts, ReportFact{Label: "Since", Value: ReportMovementFact{ReportMovementLine{Since: r.Since(), Parts: r.Movement.Parts()}}})
	}
	if len(r.ActionItems) > 0 {
		facts = append(facts, ReportFact{Label: "Open Action Items", Value: ReportActionItemsFact{r.ActionItems}})
	}
	if r.Definition.Introduction != "" {
		facts = append(facts, ReportFact{Value: ReportIntroductionFact{r.Definition.Introduction}})
	}
	if len(r.Narrative) > 0 {
		sections := make([]ReportNarrativeSection, 0, len(r.Narrative))
		for _, n := range r.Narrative {
			sections = append(sections, ReportNarrativeSection{Heading: n.Heading(), Notes: n.Notes, Highlights: n.Highlights})
		}
		facts = append(facts, ReportFact{Value: ReportNarrativeFact{sections}})
	}
	if len(r.Exceptions) > 0 {
		blocks := make([]ReportBlockFacts, 0, len(r.Exceptions))
		for _, b := range r.Exceptions {
			blocks = append(blocks, ReportBlockFacts{GoalID: b.Goal.ID, Facts: b.Facts(asOf), Reasons: b.Reasons})
		}
		facts = append(facts, ReportFact{Label: "Exceptions", Value: ReportExceptionsFact{blocks}})
	}
	if len(r.Lines) > 0 {
		facts = append(facts, ReportFact{Label: "On track", Value: ReportOnTrackFact{r.Lines}})
	}
	if len(r.MembershipChanges) > 0 {
		facts = append(facts, ReportFact{Label: "Goals that entered or left this report", Value: ReportMembershipChangesFact{r.MembershipChanges}})
	}
	return facts
}

// ReportBlockHead is the head of an exception block: its Health, with its Run
// off Green ("5 wks") and, when it changed since the baseline, what it was
// ("was Green"); then its Goal's title, Owner, Lifecycle, due date with the
// dates it slipped from, earliest first, and the block's badges.
type ReportBlockHead struct {
	GoalID      int64
	Health      string
	RunOffGreen string
	WasHealth   string
	Title       string
	Owner       Account
	Lifecycle   string
	Due         time.Time
	PriorDue    []time.Time
	Badges      []string
}

// ReportPathToGreen is a block's Path to Green: the plan, the date it is back
// to Green by with the dates that slipped from within the Run off Green,
// earliest first, how many times it slipped, and whether that date had passed
// as of the instant the block is read at (PathToGreenOverdueAt); ByWords lead
// the back-to-Green date, "back to Green by", and OverdueWords mark it
// overdue, "Overdue".
type ReportPathToGreen struct {
	Plan         string
	By           time.Time
	PriorDates   []time.Time
	Slips        int
	Overdue      bool
	ByWords      string
	OverdueWords string
}

// ReportAsk is a block's Ask for help: whom it asks, nil when no one is above
// the Owner, and then the Words saying so, "no one above the Owner".
type ReportAsk struct {
	Addressee *Account
	Words     string
}

// ReportMilestoneMark is a block's Milestone with the one mark it shows
// (MilestoneMarkOf), MilestoneMarkNone for none.
type ReportMilestoneMark struct {
	ReportMilestone
	Mark string
}

// Reading is a Metric's latest reading, written by num, or its NoReadingWords,
// "no reading yet", when it has none.
func (m ReportMetric) Reading(num func(float64) string) string {
	if !m.Read {
		return m.NoReadingWords
	}
	return num(m.Current)
}

// ReportRolledUp is a block's Rolled-up Health, when one is Present, with the
// line counting what sets it (RolledUpHealth.CountLine).
type ReportRolledUp struct {
	Health  string
	Present bool
	Count   string
}

// Facts lists an exception block's facts in order, each only when the block
// has it: the head, what changed, the Schedule conflicts it is in, the Goals it
// contributes to, the chosen Fields, Why it matters (even when empty), the
// status, the Path to Green on a Red or Yellow block, the Ask for help, the
// Milestones each marked as of asOf in the block's zone, the Metrics, the
// Rolled-up Health with its count line, the Goals Supported by, and the
// Owner's explanation when the block shows it (ShowsExplanation). asOf is the
// instant its Report was published, or now on a draft.
func (b ReportBlock) Facts(asOf time.Time) []ReportFact {
	g := b.Goal
	head := ReportBlockHead{
		GoalID: g.ID, Health: b.Health, RunOffGreen: RunOffGreenLabel(b.RunOffGreenWeeks),
		Title: g.Title, Owner: g.Owner, Lifecycle: g.Lifecycle,
		Due: g.DeliveryDate, PriorDue: b.PriorDueDates, Badges: b.Badges,
	}
	if b.PriorHealth != "" {
		head.WasHealth = "was " + b.PriorHealth
	}
	facts := []ReportFact{{Value: ReportBlockHeadFact{head}}}
	if len(b.Changes) > 0 {
		facts = append(facts, ReportFact{Label: "Changed", Value: ReportChangedFact{b.Changes}})
	}
	if len(b.ScheduleConflicts) > 0 {
		facts = append(facts, ReportFact{Label: "Schedule conflict", Value: ReportScheduleConflictFact{ReportScheduleConflictGroups(b.ScheduleConflicts)}})
	}
	if len(b.ContributesTo) > 0 {
		facts = append(facts, ReportFact{Label: "Contributes to", Value: ReportContributesToFact{b.ContributesTo}})
	}
	if len(b.Fields) > 0 {
		facts = append(facts, ReportFact{Value: ReportFieldsFact{b.Fields}})
	}
	facts = append(facts, ReportFact{Label: "Why it matters", Value: ReportWhyItMattersFact{g.WhyItMatters}})
	if b.Status != "" {
		facts = append(facts, ReportFact{Label: "Status", Value: ReportStatusFact{b.Status}})
	}
	if b.PathToGreen != "" && needsPathToGreen(b.Health) {
		facts = append(facts, ReportFact{Label: "Path to Green", Value: ReportPathToGreenFact{ReportPathToGreen{
			Plan: b.PathToGreen, By: b.PathTargetDate, PriorDates: b.PriorPathTargetDates,
			Slips: len(b.PriorPathTargetDates), Overdue: PathToGreenOverdueAt(b, asOf),
			ByWords: ReportBackToGreenBy, OverdueWords: ReportPathOverdue,
		}}})
	}
	if b.AsksForHelp {
		ask := ReportAsk{Addressee: b.AskAddressee}
		if ask.Addressee == nil {
			ask.Words = ReportNoOneAboveTheOwner
		}
		facts = append(facts, ReportFact{Label: "Asks for help", Value: ReportAskForHelpFact{ask}})
	}
	if len(b.Milestones) > 0 {
		marks := make([]ReportMilestoneMark, 0, len(b.Milestones))
		for _, m := range b.Milestones {
			marks = append(marks, ReportMilestoneMark{ReportMilestone: m, Mark: MilestoneMarkOf(m.Milestone, m.PriorDates, m.New, asOf.In(b.Location()))})
		}
		facts = append(facts, ReportFact{Label: "Milestones", Value: ReportMilestonesFact{marks}})
	}
	if len(b.Metrics) > 0 {
		metrics := make([]ReportMetric, 0, len(b.Metrics))
		for _, m := range b.Metrics {
			m.NoReadingWords, m.AgainstWords = ReportNoReadingYet, ReportAgainstATargetOf
			metrics = append(metrics, m)
		}
		facts = append(facts, ReportFact{Label: "Metrics", Value: ReportMetricsFact{metrics}})
	}
	if count := b.RolledUp.CountLine(); b.RolledUp.Present || count != "" {
		facts = append(facts, ReportFact{Label: "Rolled-up Health", Value: ReportRolledUpFact{ReportRolledUp{
			Health: b.RolledUp.Health, Present: b.RolledUp.Present, Count: count,
		}}})
	}
	if len(b.Behind) > 0 {
		facts = append(facts, ReportFact{Label: "Supported by", Value: ReportSupportedByFact{b.Behind}})
	}
	if b.ShowsExplanation() {
		facts = append(facts, ReportFact{Label: "Why the Health differs", Value: ReportExplanationFact{b.Explanation}})
	}
	return facts
}
