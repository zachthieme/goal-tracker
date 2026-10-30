package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// A publication record is how a publication is stored: its Report
// Definition's name and its facts (PublicationFacts) as they read when it was
// published, every word and link included, so a later change to its Goals, or
// to how Reports word a fact, never changes how it reads (CONTEXT.md: Report
// Definition). It has types of its own, the record… types below, each holding
// only what the publication page, Print and the Markdown export read of a
// fact; no view type is marshalled, so a view type can change freely.
//
// publicationRecordVersion is the record's format, stored inside it. Adding a
// field the reader treats as absent-means-empty needs no new version: a record
// written before it reads as having none. Renaming a record field or a
// ReportFactKind string, or changing what a field means, does: bump the
// version and add, inside thawPublication, one step upgrading the previous
// version to this one. thawPublication refuses any version it can't read; a
// database holding publications from before records (#647) is reseeded, not
// upgraded.
//
// A new kind of fact is its Report<Kind>Fact, a field on recordFact for its
// payload, and its case in recordFactOf and thawFact; a new field of a payload
// is a field on the matching record… type and its line in the functions
// converting it. Nothing else in publish.go changes.
const publicationRecordVersion = 1

// publicationRecord is a publication as stored.
type publicationRecord struct {
	Version int
	Name    string
	Facts   []recordFact
}

// recordFact is one ReportFact as stored: its kind and label, then the
// payload its kind carries, each left out when empty.
type recordFact struct {
	Kind                   string
	Label                  string                        `json:",omitempty"`
	Text                   string                        `json:",omitempty"`
	Byline                 *recordByline                 `json:",omitempty"`
	HealthCounts           *recordHealthCounts           `json:",omitempty"`
	Movement               *recordMovement               `json:",omitempty"`
	ActionItems            []recordActionItem            `json:",omitempty"`
	Narrative              []recordNarrativeSection      `json:",omitempty"`
	Blocks                 []recordBlock                 `json:",omitempty"`
	Lines                  []recordLine                  `json:",omitempty"`
	MembershipChanges      []recordMembershipChange      `json:",omitempty"`
	Head                   *recordBlockHead              `json:",omitempty"`
	Changes                []string                      `json:",omitempty"`
	ScheduleConflictGroups []recordScheduleConflictGroup `json:",omitempty"`
	Goals                  []recordLinkedGoal            `json:",omitempty"`
	Fields                 []recordFieldValue            `json:",omitempty"`
	PathToGreen            *recordPathToGreen            `json:",omitempty"`
	Ask                    *recordAsk                    `json:",omitempty"`
	Milestones             []recordMilestone             `json:",omitempty"`
	Metrics                []recordMetric                `json:",omitempty"`
	RolledUp               *recordRolledUp               `json:",omitempty"`
}

// recordPerson is a person as a publication shows them.
type recordPerson struct {
	ID    int64
	Name  string `json:",omitempty"`
	Email string
}

type recordSince struct {
	At    time.Time
	Words string `json:",omitempty"`
}

type recordByline struct {
	PublishedBy recordPerson
	PublishedAt time.Time
	Since       recordSince
}

type recordHealthCounts struct {
	Red, Yellow, Green, CheckinOverdue int
}

type recordMovement struct {
	Since recordSince
	Parts []string `json:",omitempty"`
}

type recordActionItem struct {
	ID             int64
	Text           string
	Owner          recordPerson
	ReassignedFrom *recordPerson `json:",omitempty"`
	DueDate        time.Time
	GoalTitle      string `json:",omitempty"`
}

type recordNarrativeSection struct {
	Heading    string
	Notes      []string                   `json:",omitempty"`
	Highlights []recordNarrativeHighlight `json:",omitempty"`
}

type recordNarrativeHighlight struct {
	Note      string
	Owner     recordPerson
	GoalID    int64
	GoalTitle string
}

type recordBlock struct {
	GoalID  int64
	Facts   []recordFact
	Reasons []string `json:",omitempty"`
}

type recordLine struct {
	GoalID int64
	Title  string
	Owner  recordPerson
	Due    time.Time
	Health string             `json:",omitempty"`
	Fields []recordFieldValue `json:",omitempty"`
}

type recordMembershipChange struct {
	GoalID    int64
	Title     string
	Direction string
	Reason    string
}

type recordBlockHead struct {
	GoalID      int64
	Health      string `json:",omitempty"`
	RunOffGreen string `json:",omitempty"`
	WasHealth   string `json:",omitempty"`
	Title       string
	Owner       recordPerson
	Lifecycle   string
	Due         time.Time
	PriorDue    []time.Time `json:",omitempty"`
	Badges      []string    `json:",omitempty"`
}

type recordScheduleConflictGroup struct {
	Words     string
	DueWords  string
	Conflicts []recordScheduleConflict
}

type recordScheduleConflict struct {
	GoalID int64
	Title  string
	Owner  recordPerson
	Due    time.Time
}

type recordLinkedGoal struct {
	GoalID   int64
	Title    string
	Health   string `json:",omitempty"`
	InReport bool   `json:",omitempty"`
}

type recordFieldValue struct {
	Name  string
	Type  string
	Unit  string `json:",omitempty"`
	Value string
}

type recordPathToGreen struct {
	Plan         string
	By           time.Time
	PriorDates   []time.Time `json:",omitempty"`
	Slips        int         `json:",omitempty"`
	Overdue      bool        `json:",omitempty"`
	ByWords      string
	OverdueWords string
}

type recordAsk struct {
	Addressee *recordPerson `json:",omitempty"`
	Words     string        `json:",omitempty"`
}

type recordMilestone struct {
	Name          string
	TargetDate    time.Time
	Status        string
	RemovedReason string      `json:",omitempty"`
	PriorDates    []time.Time `json:",omitempty"`
	Mark          string      `json:",omitempty"`
}

type recordMetric struct {
	Name           string
	Unit           string `json:",omitempty"`
	Direction      string
	Baseline       float64
	Target         float64
	TargetDate     time.Time
	Current        float64 `json:",omitempty"`
	Read           bool    `json:",omitempty"`
	NoReadingWords string  `json:",omitempty"`
	AgainstWords   string
}

type recordRolledUp struct {
	Health  string `json:",omitempty"`
	Present bool   `json:",omitempty"`
	Count   string `json:",omitempty"`
}

// freezePublication stores a publication of the Report Definition named name,
// its facts as PublicationFacts lists them, as a record of the current
// version.
func freezePublication(name string, facts []ReportFact) ([]byte, error) {
	data, err := json.Marshal(publicationRecord{Version: publicationRecordVersion, Name: name, Facts: recordFactsOf(facts)})
	if err != nil {
		return nil, fmt.Errorf("freeze publication: %w", err)
	}
	return data, nil
}

// thawPublication reads a publication record back: the Report Definition's
// name and the facts as frozen, times in UTC. It refuses a record of any
// version but the current one, a record with none (version 0) included.
func thawPublication(data []byte) (name string, facts []ReportFact, err error) {
	var rec publicationRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return "", nil, fmt.Errorf("thaw publication: %w", err)
	}
	if rec.Version != publicationRecordVersion {
		return "", nil, fmt.Errorf("publication record version %d, want %d", rec.Version, publicationRecordVersion)
	}
	facts, err = thawFacts(rec.Facts)
	if err != nil {
		return "", nil, err
	}
	return rec.Name, facts, nil
}

func recordFactsOf(facts []ReportFact) []recordFact {
	return mapRecord(facts, recordFactOf)
}

// thawFacts is the facts recs store, nil for none, refusing a fact of a kind
// it doesn't know.
func thawFacts(recs []recordFact) ([]ReportFact, error) {
	if len(recs) == 0 {
		return nil, nil
	}
	facts := make([]ReportFact, 0, len(recs))
	for _, rec := range recs {
		f, err := thawFact(rec)
		if err != nil {
			return nil, err
		}
		facts = append(facts, f)
	}
	return facts, nil
}

// recordFactOf is f as stored: its kind and label, then the payload its value
// carries, each part left out when empty.
func recordFactOf(f ReportFact) recordFact {
	rec := recordFact{Kind: string(f.Kind()), Label: f.Label}
	switch v := f.Value.(type) {
	case ReportBylineFact:
		rec.Byline = recordIf(v.Byline != ReportByline{}, func() recordByline { return recordBylineOf(v.Byline) })
	case ReportSelectionFact:
		rec.Text = v.Text
	case ReportHealthCountsFact:
		rec.HealthCounts = recordIf(v.Counts != HealthCounts{}, func() recordHealthCounts { return recordHealthCountsOf(v.Counts) })
	case ReportMovementFact:
		rec.Movement = new(recordMovementOf(v.Line))
	case ReportActionItemsFact:
		rec.ActionItems = mapRecord(v.Items, recordActionItemOf)
	case ReportIntroductionFact:
		rec.Text = v.Text
	case ReportNarrativeFact:
		rec.Narrative = mapRecord(v.Sections, recordNarrativeSectionOf)
	case ReportExceptionsFact:
		rec.Blocks = mapRecord(v.Blocks, recordBlockOf)
	case ReportOnTrackFact:
		rec.Lines = mapRecord(v.Lines, recordLineOf)
	case ReportMembershipChangesFact:
		rec.MembershipChanges = mapRecord(v.Changes, recordMembershipChangeOf)
	case ReportBlockHeadFact:
		rec.Head = new(recordBlockHeadOf(v.Head))
	case ReportChangedFact:
		rec.Changes = v.Changes
	case ReportScheduleConflictFact:
		rec.ScheduleConflictGroups = mapRecord(v.Groups, recordScheduleConflictGroupOf)
	case ReportContributesToFact:
		rec.Goals = mapRecord(v.Goals, recordLinkedGoalOf)
	case ReportFieldsFact:
		rec.Fields = mapRecord(v.Values, recordFieldValueOf)
	case ReportWhyItMattersFact:
		rec.Text = v.Text
	case ReportStatusFact:
		rec.Text = v.Text
	case ReportPathToGreenFact:
		rec.PathToGreen = new(recordPathToGreenOf(v.Path))
	case ReportAskForHelpFact:
		rec.Ask = recordIf(v.Ask != ReportAsk{}, func() recordAsk { return recordAskOf(v.Ask) })
	case ReportMilestonesFact:
		rec.Milestones = mapRecord(v.Marks, recordMilestoneOf)
	case ReportMetricsFact:
		rec.Metrics = mapRecord(v.Metrics, recordMetricOf)
	case ReportRolledUpFact:
		rec.RolledUp = recordIf(v.RolledUp != ReportRolledUp{}, func() recordRolledUp { return recordRolledUp(v.RolledUp) })
	case ReportSupportedByFact:
		rec.Goals = mapRecord(v.Goals, recordLinkedGoalOf)
	case ReportExplanationFact:
		rec.Text = v.Text
	}
	return rec
}

// thawFact is the ReportFact rec stores, its value the Report<Kind>Fact its
// kind names, refusing a kind it doesn't know.
func thawFact(rec recordFact) (ReportFact, error) {
	f := ReportFact{Label: rec.Label}
	switch kind := ReportFactKind(rec.Kind); kind {
	case ReportFactByline:
		f.Value = ReportBylineFact{thawIf(rec.Byline, thawByline)}
	case ReportFactSelection:
		f.Value = ReportSelectionFact{rec.Text}
	case ReportFactHealthCounts:
		f.Value = ReportHealthCountsFact{thawIf(rec.HealthCounts, thawHealthCounts)}
	case ReportFactMovement:
		f.Value = ReportMovementFact{thawIf(rec.Movement, thawMovement)}
	case ReportFactActionItems:
		f.Value = ReportActionItemsFact{mapRecord(rec.ActionItems, thawActionItem)}
	case ReportFactIntroduction:
		f.Value = ReportIntroductionFact{rec.Text}
	case ReportFactNarrative:
		f.Value = ReportNarrativeFact{mapRecord(rec.Narrative, thawNarrativeSection)}
	case ReportFactExceptions:
		blocks := make([]ReportBlockFacts, 0, len(rec.Blocks))
		for _, b := range rec.Blocks {
			block, err := thawBlock(b)
			if err != nil {
				return ReportFact{}, err
			}
			blocks = append(blocks, block)
		}
		f.Value = ReportExceptionsFact{thawSlice(blocks)}
	case ReportFactOnTrack:
		f.Value = ReportOnTrackFact{mapRecord(rec.Lines, thawLine)}
	case ReportFactMembershipChanges:
		f.Value = ReportMembershipChangesFact{mapRecord(rec.MembershipChanges, thawMembershipChange)}
	case ReportFactBlockHead:
		f.Value = ReportBlockHeadFact{thawIf(rec.Head, thawBlockHead)}
	case ReportFactChanged:
		f.Value = ReportChangedFact{thawSlice(rec.Changes)}
	case ReportFactScheduleConflict:
		f.Value = ReportScheduleConflictFact{mapRecord(rec.ScheduleConflictGroups, thawScheduleConflictGroup)}
	case ReportFactContributesTo:
		f.Value = ReportContributesToFact{mapRecord(rec.Goals, thawLinkedGoal)}
	case ReportFactFields:
		f.Value = ReportFieldsFact{mapRecord(rec.Fields, thawFieldValue)}
	case ReportFactWhyItMatters:
		f.Value = ReportWhyItMattersFact{rec.Text}
	case ReportFactStatus:
		f.Value = ReportStatusFact{rec.Text}
	case ReportFactPathToGreen:
		f.Value = ReportPathToGreenFact{thawIf(rec.PathToGreen, thawPathToGreen)}
	case ReportFactAskForHelp:
		f.Value = ReportAskForHelpFact{thawIf(rec.Ask, thawAsk)}
	case ReportFactMilestones:
		f.Value = ReportMilestonesFact{mapRecord(rec.Milestones, thawMilestoneMark)}
	case ReportFactMetrics:
		f.Value = ReportMetricsFact{mapRecord(rec.Metrics, thawMetric)}
	case ReportFactRolledUp:
		f.Value = ReportRolledUpFact{thawIf(rec.RolledUp, func(r recordRolledUp) ReportRolledUp { return ReportRolledUp(r) })}
	case ReportFactSupportedBy:
		f.Value = ReportSupportedByFact{mapRecord(rec.Goals, thawLinkedGoal)}
	case ReportFactExplanation:
		f.Value = ReportExplanationFact{rec.Text}
	default:
		return ReportFact{}, fmt.Errorf("publication record fact kind %q", kind)
	}
	return f, nil
}

// mapRecord converts each of in by conv, and is nil for none, so a fact
// without a payload reads back as it went in.
func mapRecord[T, U any](in []T, conv func(T) U) []U {
	if len(in) == 0 {
		return nil
	}
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, conv(v))
	}
	return out
}

// recordIf is the payload rec builds when the fact has it, and nil otherwise.
func recordIf[T any](has bool, rec func() T) *T {
	if !has {
		return nil
	}
	v := rec()
	return &v
}

// thawIf is the payload rec stores, read by conv, or its zero value when rec
// holds none.
func thawIf[T, U any](rec *T, conv func(T) U) U {
	if rec == nil {
		var zero U
		return zero
	}
	return conv(*rec)
}

func thawSlice[T any](in []T) []T {
	if len(in) == 0 {
		return nil
	}
	return in
}

// recordUTCTimes is times in UTC, nil for none.
func recordUTCTimes(times []time.Time) []time.Time {
	return mapRecord(times, time.Time.UTC)
}

func recordPersonOf(a Account) recordPerson {
	return recordPerson{ID: a.ID, Name: a.Name, Email: a.Email}
}

func thawPerson(p recordPerson) Account {
	return Account{ID: p.ID, Name: p.Name, Email: p.Email}
}

func recordPersonPtrOf(a *Account) *recordPerson {
	return recordIf(a != nil, func() recordPerson { return recordPersonOf(*a) })
}

func thawPersonPtr(p *recordPerson) *Account {
	if p == nil {
		return nil
	}
	a := thawPerson(*p)
	return &a
}

func recordSinceOf(s ReportSince) recordSince {
	return recordSince{At: s.At.UTC(), Words: s.Words}
}

func thawSince(s recordSince) ReportSince {
	return ReportSince{At: s.At.UTC(), Words: s.Words}
}

func recordBylineOf(b ReportByline) recordByline {
	return recordByline{PublishedBy: recordPersonOf(b.PublishedBy), PublishedAt: b.PublishedAt.UTC(), Since: recordSinceOf(b.Since)}
}

func thawByline(b recordByline) ReportByline {
	return ReportByline{PublishedBy: thawPerson(b.PublishedBy), PublishedAt: b.PublishedAt.UTC(), Since: thawSince(b.Since)}
}

func recordHealthCountsOf(c HealthCounts) recordHealthCounts {
	return recordHealthCounts{Red: c.Red, Yellow: c.Yellow, Green: c.Green, CheckinOverdue: c.CheckinOverdue}
}

func thawHealthCounts(c recordHealthCounts) HealthCounts {
	return HealthCounts{Red: c.Red, Yellow: c.Yellow, Green: c.Green, CheckinOverdue: c.CheckinOverdue}
}

func recordMovementOf(m ReportMovementLine) recordMovement {
	return recordMovement{Since: recordSinceOf(m.Since), Parts: m.Parts}
}

func thawMovement(m recordMovement) ReportMovementLine {
	return ReportMovementLine{Since: thawSince(m.Since), Parts: thawSlice(m.Parts)}
}

func recordActionItemOf(a ActionItem) recordActionItem {
	return recordActionItem{
		ID: a.ID, Text: a.Text, Owner: recordPersonOf(a.Owner), ReassignedFrom: recordPersonPtrOf(a.ReassignedFrom),
		DueDate: a.DueDate.UTC(), GoalTitle: a.GoalTitle,
	}
}

func thawActionItem(a recordActionItem) ActionItem {
	return ActionItem{
		ID: a.ID, Text: a.Text, Owner: thawPerson(a.Owner), ReassignedFrom: thawPersonPtr(a.ReassignedFrom),
		DueDate: a.DueDate.UTC(), GoalTitle: a.GoalTitle,
	}
}

func recordNarrativeSectionOf(n ReportNarrativeSection) recordNarrativeSection {
	return recordNarrativeSection{Heading: n.Heading, Notes: n.Notes, Highlights: mapRecord(n.Highlights, func(h NarrativeHighlight) recordNarrativeHighlight {
		return recordNarrativeHighlight{Note: h.Highlight.Note, Owner: recordPersonOf(h.Highlight.Owner), GoalID: h.GoalID, GoalTitle: h.GoalTitle}
	})}
}

func thawNarrativeSection(n recordNarrativeSection) ReportNarrativeSection {
	return ReportNarrativeSection{Heading: n.Heading, Notes: thawSlice(n.Notes), Highlights: mapRecord(n.Highlights, func(h recordNarrativeHighlight) NarrativeHighlight {
		return NarrativeHighlight{Highlight: Highlight{Note: h.Note, Owner: thawPerson(h.Owner)}, GoalID: h.GoalID, GoalTitle: h.GoalTitle}
	})}
}

func recordBlockOf(b ReportBlockFacts) recordBlock {
	return recordBlock{GoalID: b.GoalID, Facts: recordFactsOf(b.Facts), Reasons: mapRecord(b.Reasons, func(r ReportReason) string { return string(r) })}
}

func thawBlock(b recordBlock) (ReportBlockFacts, error) {
	facts, err := thawFacts(b.Facts)
	if err != nil {
		return ReportBlockFacts{}, err
	}
	return ReportBlockFacts{GoalID: b.GoalID, Facts: facts, Reasons: mapRecord(b.Reasons, func(r string) ReportReason { return ReportReason(r) })}, nil
}

func recordLineOf(sg SelectedGoal) recordLine {
	return recordLine{
		GoalID: sg.Goal.ID, Title: sg.Goal.Title, Owner: recordPersonOf(sg.Goal.Owner), Due: sg.Goal.DeliveryDate.UTC(),
		Health: sg.Health, Fields: mapRecord(sg.Fields, recordFieldValueOf),
	}
}

func thawLine(l recordLine) SelectedGoal {
	return SelectedGoal{
		Goal:   Goal{ID: l.GoalID, Title: l.Title, Owner: thawPerson(l.Owner), DeliveryDate: l.Due.UTC()},
		Health: l.Health, Fields: mapRecord(l.Fields, thawFieldValue),
	}
}

func recordMembershipChangeOf(c MembershipChange) recordMembershipChange {
	return recordMembershipChange{GoalID: c.Goal.ID, Title: c.Goal.Title, Direction: c.Direction, Reason: c.Reason}
}

func thawMembershipChange(c recordMembershipChange) MembershipChange {
	return MembershipChange{Goal: Goal{ID: c.GoalID, Title: c.Title}, Direction: c.Direction, Reason: c.Reason}
}

func recordBlockHeadOf(h ReportBlockHead) recordBlockHead {
	return recordBlockHead{
		GoalID: h.GoalID, Health: h.Health, RunOffGreen: h.RunOffGreen, WasHealth: h.WasHealth,
		Title: h.Title, Owner: recordPersonOf(h.Owner), Lifecycle: h.Lifecycle,
		Due: h.Due.UTC(), PriorDue: recordUTCTimes(h.PriorDue), Badges: h.Badges,
	}
}

func thawBlockHead(h recordBlockHead) ReportBlockHead {
	return ReportBlockHead{
		GoalID: h.GoalID, Health: h.Health, RunOffGreen: h.RunOffGreen, WasHealth: h.WasHealth,
		Title: h.Title, Owner: thawPerson(h.Owner), Lifecycle: h.Lifecycle,
		Due: h.Due.UTC(), PriorDue: recordUTCTimes(h.PriorDue), Badges: thawSlice(h.Badges),
	}
}

func recordScheduleConflictGroupOf(g ReportScheduleConflictGroup) recordScheduleConflictGroup {
	return recordScheduleConflictGroup{Words: g.Words, DueWords: g.DueWords, Conflicts: mapRecord(g.Conflicts, func(c ReportScheduleConflict) recordScheduleConflict {
		return recordScheduleConflict{GoalID: c.GoalID, Title: c.Title, Owner: recordPersonOf(c.Owner), Due: c.Due.UTC()}
	})}
}

func thawScheduleConflictGroup(g recordScheduleConflictGroup) ReportScheduleConflictGroup {
	return ReportScheduleConflictGroup{Words: g.Words, DueWords: g.DueWords, Conflicts: mapRecord(g.Conflicts, func(c recordScheduleConflict) ReportScheduleConflict {
		return ReportScheduleConflict{GoalID: c.GoalID, Title: c.Title, Owner: thawPerson(c.Owner), Due: c.Due.UTC()}
	})}
}

func recordLinkedGoalOf(g LinkedGoal) recordLinkedGoal {
	return recordLinkedGoal(g)
}

func thawLinkedGoal(g recordLinkedGoal) LinkedGoal {
	return LinkedGoal(g)
}

func recordFieldValueOf(v FieldValue) recordFieldValue {
	return recordFieldValue{Name: v.Field.Name, Type: v.Field.Type, Unit: v.Field.Unit, Value: v.Value}
}

func thawFieldValue(v recordFieldValue) FieldValue {
	return FieldValue{Field: Field{Name: v.Name, Type: v.Type, Unit: v.Unit}, Value: v.Value}
}

func recordPathToGreenOf(p ReportPathToGreen) recordPathToGreen {
	return recordPathToGreen{
		Plan: p.Plan, By: p.By.UTC(), PriorDates: recordUTCTimes(p.PriorDates), Slips: p.Slips, Overdue: p.Overdue,
		ByWords: p.ByWords, OverdueWords: p.OverdueWords,
	}
}

func thawPathToGreen(p recordPathToGreen) ReportPathToGreen {
	return ReportPathToGreen{
		Plan: p.Plan, By: p.By.UTC(), PriorDates: recordUTCTimes(p.PriorDates), Slips: p.Slips, Overdue: p.Overdue,
		ByWords: p.ByWords, OverdueWords: p.OverdueWords,
	}
}

func recordAskOf(a ReportAsk) recordAsk {
	return recordAsk{Addressee: recordPersonPtrOf(a.Addressee), Words: a.Words}
}

func thawAsk(a recordAsk) ReportAsk {
	return ReportAsk{Addressee: thawPersonPtr(a.Addressee), Words: a.Words}
}

func recordMilestoneOf(m ReportMilestoneMark) recordMilestone {
	return recordMilestone{
		Name: m.Milestone.Name, TargetDate: m.Milestone.TargetDate.UTC(), Status: m.Milestone.Status,
		RemovedReason: m.Milestone.RemovedReason, PriorDates: recordUTCTimes(m.PriorDates), Mark: m.Mark,
	}
}

func thawMilestoneMark(m recordMilestone) ReportMilestoneMark {
	return ReportMilestoneMark{
		ReportMilestone: ReportMilestone{
			Milestone:  Milestone{Name: m.Name, TargetDate: m.TargetDate.UTC(), Status: m.Status, RemovedReason: m.RemovedReason},
			PriorDates: recordUTCTimes(m.PriorDates),
		},
		Mark: m.Mark,
	}
}

func recordMetricOf(m ReportMetric) recordMetric {
	return recordMetric{
		Name: m.Metric.Name, Unit: m.Metric.Unit, Direction: m.Metric.Direction, Baseline: m.Metric.Baseline,
		Target: m.Metric.Target, TargetDate: m.Metric.TargetDate.UTC(), Current: m.Current, Read: m.Read,
		NoReadingWords: m.NoReadingWords, AgainstWords: m.AgainstWords,
	}
}

func thawMetric(m recordMetric) ReportMetric {
	return ReportMetric{
		Metric: Metric{
			Name: m.Name, Unit: m.Unit, Direction: m.Direction, Baseline: m.Baseline,
			Target: m.Target, TargetDate: m.TargetDate.UTC(),
		},
		Current: m.Current, Read: m.Read, NoReadingWords: m.NoReadingWords, AgainstWords: m.AgainstWords,
	}
}
