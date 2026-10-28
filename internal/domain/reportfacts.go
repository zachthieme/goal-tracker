package domain

import "time"

// ReportFactKind names one kind of fact a Report shows: one of its own parts
// (Report.Facts) or one part of an exception block (ReportBlock.Facts).
type ReportFactKind string

// The kinds of a Report's own facts, in the order Publication.Facts lists
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
// words it opens with and the Goals at the other end, in the block's order.
type ReportScheduleConflictGroup struct {
	Words     string
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
			groups = append(groups, ReportScheduleConflictGroup{Words: side.words, Conflicts: cs})
		}
	}
	return groups
}

// ReportFact is one fact a Report shows, in the order it shows them: its kind,
// the label every output shows it under ("" for one shown without, as the
// introduction and the narrative are), and its values, in the field its kind
// names. Each output only formats it: whether a fact shows, where, and in what
// words is the list's (CONTEXT.md: Report).
type ReportFact struct {
	Kind  ReportFactKind
	Label string

	// Text is a Selection's plain words, the introduction, or a block's Why
	// it matters, status or explanation of why its Health differs.
	Text string
	// Byline is who published the Report, when, and what it reads changes
	// against.
	Byline ReportByline
	// HealthCounts counts the Report's Goals by Health (Report.HealthCounts).
	HealthCounts HealthCounts
	// Movement is the line counting what moved since the baseline.
	Movement ReportMovementLine
	// ActionItems are the Report's open Action Items.
	ActionItems []ActionItem
	// Narrative is the narrative's sections, each under its own heading
	// (NarrativeSection.Heading).
	Narrative []NarrativeSection
	// Exceptions are the exception blocks, each listing its own facts
	// (ReportBlock.Facts).
	Exceptions []ReportBlock
	// Lines are the one-line Goals, On track.
	Lines []SelectedGoal
	// MembershipChanges are the Goals that entered or left the Report.
	MembershipChanges []MembershipChange

	// Head is an exception block's head.
	Head ReportBlockHead
	// Changes say what changed on the block's Goal since the baseline.
	Changes []string
	// ScheduleConflicts are the Schedule conflicts the block's Goal is in.
	ScheduleConflicts []ReportScheduleConflict
	// Goals are the Goals a block links to: those it contributes to, or
	// those behind it, Supported by.
	Goals []LinkedGoal
	// Fields are the block's values in the Fields the Report chose.
	Fields []FieldValue
	// PathToGreen is the block's plan back to Green.
	PathToGreen ReportPathToGreen
	// Ask is the block's Ask for help.
	Ask ReportAsk
	// Milestones are the block's Milestones, each with its mark.
	Milestones []ReportMilestoneMark
	// Metrics are the block's Metrics against their targets.
	Metrics []ReportMetric
	// RolledUp is the block's Rolled-up Health with its count line.
	RolledUp ReportRolledUp
}

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
	// Previous is set when it reads them against its previous publication.
	Previous bool
	// At is the instant the previous publication was published, or the
	// baseline date.
	At time.Time
}

// Words names what a Report reads its changes against when it is the previous
// publication, "the previous publication"; read against a date, it is "", and
// each output writes the date as it writes dates.
func (s ReportSince) Words() string {
	if s.Previous {
		return ReportPreviousPublication
	}
	return ""
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
		return ReportSince{Previous: true, At: r.Previous.PublishedAt}
	}
	return ReportSince{At: r.Baseline}
}

// Facts lists a publication's facts in order: its byline, how it selected its
// Goals as it read when published (none for a publication frozen before
// selections were), then its Report's (Report.Facts).
func (p Publication) Facts() []ReportFact {
	facts := []ReportFact{{
		Kind: ReportFactByline, Label: "Published",
		Byline: ReportByline{PublishedBy: p.PublishedBy, PublishedAt: p.PublishedAt, Since: p.Report.Since()},
	}}
	if p.Scope != "" {
		facts = append(facts, ReportFact{Kind: ReportFactSelection, Label: "Selection", Text: p.Scope})
	}
	return append(facts, p.Report.Facts()...)
}

// Facts lists a Report's own parts in order, each only when it has it: its
// Goals counted by Health when it selects any, the line counting what moved
// (none on a Report frozen before Reports counted it), its open Action Items,
// the introduction, the narrative, the exceptions, the one-line Goals On
// track, and the Goals that entered or left it.
func (r Report) Facts() []ReportFact {
	var facts []ReportFact
	if len(r.Exceptions) > 0 || len(r.Lines) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactHealthCounts, Label: "Health", HealthCounts: r.HealthCounts()})
	}
	if r.Movement != nil {
		facts = append(facts, ReportFact{Kind: ReportFactMovement, Label: "Since", Movement: ReportMovementLine{Since: r.Since(), Parts: r.Movement.Parts()}})
	}
	if len(r.ActionItems) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactActionItems, Label: "Open Action Items", ActionItems: r.ActionItems})
	}
	if r.Definition.Introduction != "" {
		facts = append(facts, ReportFact{Kind: ReportFactIntroduction, Text: r.Definition.Introduction})
	}
	if len(r.Narrative) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactNarrative, Narrative: r.Narrative})
	}
	if len(r.Exceptions) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactExceptions, Label: "Exceptions", Exceptions: r.Exceptions})
	}
	if len(r.Lines) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactOnTrack, Label: "On track", Lines: r.Lines})
	}
	if len(r.MembershipChanges) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactMembershipChanges, Label: "Goals that entered or left this report", MembershipChanges: r.MembershipChanges})
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
// as of the instant the block is read at (PathToGreenOverdueAt).
type ReportPathToGreen struct {
	Plan       string
	By         time.Time
	PriorDates []time.Time
	Slips      int
	Overdue    bool
}

// ByWords leads the back-to-Green date: "back to Green by".
func (ReportPathToGreen) ByWords() string { return ReportBackToGreenBy }

// OverdueWords marks an overdue Path to Green: "Overdue".
func (ReportPathToGreen) OverdueWords() string { return ReportPathOverdue }

// ReportAsk is a block's Ask for help: whom it asks, nil when no one is above
// the Owner.
type ReportAsk struct {
	Addressee *Account
}

// Words say whom an Ask for help asks when it names no one: "no one above the
// Owner"; it is "" when it names its Addressee.
func (a ReportAsk) Words() string {
	if a.Addressee == nil {
		return ReportNoOneAboveTheOwner
	}
	return ""
}

// ReportMilestoneMark is a block's Milestone with the one mark it shows
// (MilestoneMarkOf), MilestoneMarkNone for none.
type ReportMilestoneMark struct {
	ReportMilestone
	Mark string
}

// Reading is a Metric's latest reading, written by num, or "no reading yet".
func (m ReportMetric) Reading(num func(float64) string) string {
	if !m.Read {
		return ReportNoReadingYet
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
	facts := []ReportFact{{Kind: ReportFactBlockHead, Head: head}}
	if len(b.Changes) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactChanged, Label: "Changed", Changes: b.Changes})
	}
	if len(b.ScheduleConflicts) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactScheduleConflict, Label: "Schedule conflict", ScheduleConflicts: b.ScheduleConflicts})
	}
	if len(b.ContributesTo) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactContributesTo, Label: "Contributes to", Goals: b.ContributesTo})
	}
	if len(b.Fields) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactFields, Fields: b.Fields})
	}
	facts = append(facts, ReportFact{Kind: ReportFactWhyItMatters, Label: "Why it matters", Text: g.WhyItMatters})
	if b.Status != "" {
		facts = append(facts, ReportFact{Kind: ReportFactStatus, Label: "Status", Text: b.Status})
	}
	if b.PathToGreen != "" && needsPathToGreen(b.Health) {
		facts = append(facts, ReportFact{Kind: ReportFactPathToGreen, Label: "Path to Green", PathToGreen: ReportPathToGreen{
			Plan: b.PathToGreen, By: b.PathTargetDate, PriorDates: b.PriorPathTargetDates,
			Slips: len(b.PriorPathTargetDates), Overdue: PathToGreenOverdueAt(b, asOf),
		}})
	}
	if b.AsksForHelp {
		facts = append(facts, ReportFact{Kind: ReportFactAskForHelp, Label: "Asks for help", Ask: ReportAsk{Addressee: b.AskAddressee}})
	}
	if len(b.Milestones) > 0 {
		marks := make([]ReportMilestoneMark, 0, len(b.Milestones))
		for _, m := range b.Milestones {
			marks = append(marks, ReportMilestoneMark{ReportMilestone: m, Mark: MilestoneMarkOf(m.Milestone, m.PriorDates, m.New, asOf.In(b.Location()))})
		}
		facts = append(facts, ReportFact{Kind: ReportFactMilestones, Label: "Milestones", Milestones: marks})
	}
	if len(b.Metrics) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactMetrics, Label: "Metrics", Metrics: b.Metrics})
	}
	if count := b.RolledUp.CountLine(); b.RolledUp.Present || count != "" {
		facts = append(facts, ReportFact{Kind: ReportFactRolledUp, Label: "Rolled-up Health", RolledUp: ReportRolledUp{
			Health: b.RolledUp.Health, Present: b.RolledUp.Present, Count: count,
		}})
	}
	if len(b.Behind) > 0 {
		facts = append(facts, ReportFact{Kind: ReportFactSupportedBy, Label: "Supported by", Goals: b.Behind})
	}
	if b.ShowsExplanation() {
		facts = append(facts, ReportFact{Kind: ReportFactExplanation, Label: "Why the Health differs", Text: b.Explanation})
	}
	return facts
}
