package domain

import (
	"context"
	"errors"
	"slices"
	"time"
)

// Report is the draft Report a Report Definition produces, read against a
// baseline (CONTEXT.md: Report). Exceptions get the full MBR block; every other
// selected Goal takes one line.
type Report struct {
	Definition ReportDefinition
	// Baseline is the date changes are read against: the reader's choice, or
	// by default the date of the Definition's previous publication, or 30 days
	// ago when it has none. It is a calendar date in the org's timezone.
	Baseline time.Time
	// Previous is the previous publication changes are read against, from the
	// instant it was published; its ID is 0 when they are read against a date.
	Previous PreviousPublication
	// ActionItems are the Definition's open Action Items, soonest due first,
	// shown at the top of the Report until each is closed.
	ActionItems []ActionItem
	Exceptions  []ReportBlock
	Lines       []SelectedGoal
	// Added counts the selected Goals in only through the Definition's Also
	// include, and LeftOut its Leave out Goals the rules would otherwise have
	// selected, for its scope. They aren't frozen with the snapshot; only the
	// scope they are said in is.
	Added   int `json:"-"`
	LeftOut int `json:"-"`
	// Highlights are every Highlight in scope since the baseline, for the
	// author to curate while preparing a publication. They are not frozen
	// with the snapshot; only the narrative the author curates from them is.
	Highlights []NarrativeHighlight `json:"-"`
	// Narrative is the Report's Insights, Accomplishments, and Misses, curated
	// by its author from Highlights, with their own text. It is frozen with
	// the snapshot.
	Narrative []NarrativeSection
	// MembershipChanges are the Goals that entered or left the Report since
	// its Definition's latest publication, whatever the baseline. They are
	// frozen with the snapshot.
	MembershipChanges []MembershipChange `json:",omitempty"`
	// Movement counts what moved since the baseline, for the line a Report
	// opens with. It is frozen with the snapshot; one frozen before Reports
	// counted it has none.
	Movement *ReportMovement `json:",omitempty"`
}

// covers reports whether the Report includes the Goal, as an exception block
// or one line.
func (r Report) covers(goalID int64) bool {
	for _, b := range r.Exceptions {
		if b.Goal.ID == goalID {
			return true
		}
	}
	for _, sg := range r.Lines {
		if sg.Goal.ID == goalID {
			return true
		}
	}
	return false
}

// ReportBlock is one exception Goal's full MBR block.
type ReportBlock struct {
	Goal   Goal
	Health string
	// PriorHealth is the Goal's Health at the baseline, set only when its
	// Health has changed since, shown beside it ("Yellow · was Green").
	PriorHealth string `json:",omitempty"`
	// RunOffGreenWeeks is the Goal's Run off Green in weeks when the Report
	// was drafted, shown beside its Health ("Red · 5 wks"); see RunOffGreen.
	// It is 0 when the Goal isn't Yellow or Red. It is frozen with the
	// snapshot, so a later Check-in doesn't change a publication's count; a
	// snapshot frozen before Reports showed it has 0.
	RunOffGreenWeeks int `json:",omitempty"`
	// Changes say in words what changed on the Goal since the baseline, in
	// the order reportChanges gives them ("Turned Red", "Delivery date
	// slipped 3 weeks"); a Goal whose trouble is only standing has none.
	Changes []string `json:",omitempty"`
	// Badges flag what the reader must not miss, in the order BadgeNew,
	// BadgeNewDate, BadgeCheckinOverdue, BadgeOwnerless, BadgeOnHold.
	Badges []string
	// PriorDueDates are the delivery dates the Goal held before each of its
	// Date Slips, earliest first, shown struck through ahead of its current
	// date (~~10/15~~ 11/03).
	PriorDueDates []time.Time
	// Status, PathToGreen, PathTargetDate, and Explanation are the latest
	// Check-in's: its short status, the plan back to Green with its target
	// date, and why the Owner's Health differs from the Rolled-up Health.
	Status         string
	PathToGreen    string
	PathTargetDate time.Time
	// PriorPathTargetDates are the dates the back-to-Green date slipped from
	// within the Goal's current Run off Green, earliest first (see
	// BackToGreenPriorDates), struck through beside PathTargetDate as a
	// Milestone's are. It is frozen with the snapshot, so a later slip doesn't
	// change a publication's; a snapshot frozen before Reports showed them has
	// none.
	PriorPathTargetDates []time.Time `json:",omitempty"`
	Explanation          string
	Milestones           []ReportMilestone
	Metrics              []ReportMetric
	RolledUp             RolledUpHealth
	// ContributesTo are the Goals this one contributes to that the Report
	// holds. Behind are the Goals that contribute to it that the Report holds,
	// and the Red and Yellow Active ones it doesn't, which set its Rolled-up
	// Health (#246). Both are Red first, then Yellow, then the rest, and a
	// snapshot frozen before Reports named them has neither.
	ContributesTo []LinkedGoal `json:",omitempty"`
	Behind        []LinkedGoal `json:",omitempty"`
	// Fields are the Goal's values in the Fields the definition chose, shown
	// as labelled values beside the Goal, never in the narrative (ADR 0005).
	Fields []FieldValue `json:",omitempty"`
	// AsksForHelp marks an Ask for help open when the Report was drafted: the
	// Goal Active, its latest Check-in Yellow or Red and asking (CONTEXT.md:
	// Ask for help). AskAddressee is whom it asked then, nil when no one was
	// above the Owner. Both are frozen with the publication, so a later
	// Check-in or directory change doesn't alter them; a snapshot frozen
	// before Reports showed asks has neither.
	AsksForHelp  bool     `json:",omitempty"`
	AskAddressee *Account `json:",omitempty"`
	// Timezone is the IANA name of the zone the Goal was judged in when the
	// Report was drafted (see Service.GoalTimezone): its Owner's, or the
	// company's. It is frozen with the snapshot, so a publication marks its
	// Milestones and Path to Green overdue by the date it was published there,
	// whatever the Owner's Timezone is later; a block frozen before it has "",
	// read as UTC (see Location).
	Timezone string `json:",omitempty"`
}

// Location is the zone b's Timezone names, the one its Goal's dates are
// judged in, and UTC for a block with none or a name that doesn't load.
func (b ReportBlock) Location() *time.Location {
	if loc, ok := loadTimezone(b.Timezone); ok {
		return loc
	}
	return time.UTC
}

// ShowsExplanation reports whether b shows the Owner's explanation of why its
// Health differs from the Rolled-up Health: only when it has one and a
// Rolled-up Health shows beside it, set by its children or counting them
// Check-in overdue. Every surface asks it, so they show it alike.
func (b ReportBlock) ShowsExplanation() bool {
	return b.Explanation != "" && (b.RolledUp.Present || b.RolledUp.CheckinOverdueChildren > 0)
}

// PathToGreenOverdueAt reports whether b's Path to Green had missed its
// back-to-Green date as of asOf — the instant its Report was published, or now
// on a draft — judged in b's Location as Freshness judges a Goal's
// (pathToGreenPastTarget): never on a Green, non-Active or undated block. It
// needs only what a frozen Report snapshot holds, so every surface marks it the
// same way, older publications included.
func PathToGreenOverdueAt(b ReportBlock, asOf time.Time) bool {
	return pathToGreenPastTarget(b.Goal.Lifecycle, b.Health, b.PathTargetDate, asOf, b.Location())
}

// ReportMilestone is a Milestone as an exception block shows it: marked New
// when added since the baseline, Done or Removed by its Status, and with the
// dates it held before its Date Slips to strike through.
type ReportMilestone struct {
	Milestone  Milestone
	New        bool
	PriorDates []time.Time
}

// ReportMetric is a Metric against its target: Current is its latest reading,
// and Read is false when it has none yet.
type ReportMetric struct {
	Metric  Metric
	Current float64
	Read    bool
}

// Badges a ReportBlock can carry. New marks a Goal created since the baseline
// and New Date one whose delivery date slipped since it; the rest mirror the
// Goal's current state (CONTEXT.md: Check-in overdue, Ownerless, Lifecycle).
const (
	BadgeNew            = "New"
	BadgeNewDate        = "New Date"
	BadgeCheckinOverdue = "Check-in overdue"
	BadgeOwnerless      = "Ownerless"
	BadgeOnHold         = LifecycleOnHold
)

// defaultBaselineDays is how far back a Report reads changes when the reader
// picks no baseline.
const defaultBaselineDays = 30

// DraftReport drafts the Report for def against baseline. A zero baseline
// takes the default: the Definition's previous publication, so the next one
// marks what changed since it, or 30 days before today when there is none.
func (s *Service) DraftReport(ctx context.Context, def ReportDefinition, baseline time.Time) (Report, error) {
	return s.draftReport(ctx, def, baseline, func(r *Report) error { return s.draftNarrative(ctx, r) })
}

// draftReport drafts the Report for def against baseline, as DraftReport
// does, its narrative built by narrate once its in-scope Highlights are known:
// the saved narrative for a draft, or the author's unsaved one for a preview.
func (s *Service) draftReport(ctx context.Context, def ReportDefinition, baseline time.Time, narrate func(*Report) error) (Report, error) {
	r := Report{Definition: def}
	since, err := s.readAgainst(ctx, &r, baseline)
	if err != nil {
		return Report{}, err
	}
	sel, err := s.SelectGoals(ctx, def, since)
	if err != nil {
		return Report{}, err
	}
	selected := sel.Goals
	r.Added, r.LeftOut = sel.added(), len(sel.LeftOut)
	if r.ActionItems, err = s.OpenActionItems(ctx, def.ID); err != nil {
		return Report{}, err
	}
	// Every read below is of all the selected Goals, or all the exceptions,
	// at once, so drafting runs the same queries however many Goals (#411).
	goals := make([]Goal, 0, len(selected))
	for _, sg := range selected {
		goals = append(goals, sg.Goal)
	}
	histories, err := s.goalHistories(ctx, goals)
	if err != nil {
		return Report{}, err
	}
	fields, err := s.reportFieldsByGoal(ctx, def, goals)
	if err != nil {
		return Report{}, err
	}
	highlights, err := s.highlightsByGoal(ctx, goalIDsOf(goals))
	if err != nil {
		return Report{}, err
	}
	last := make(map[int64]lastCheckin, len(goals))
	for _, g := range goals {
		last[g.ID] = histories[g.ID].last()
	}
	freshness, err := s.freshnessGivenLast(ctx, goals, last)
	if err != nil {
		return Report{}, err
	}
	type exceptionGoal struct {
		h      goalHistory
		fresh  Freshness
		fields []FieldValue
	}
	var pending []exceptionGoal
	for _, sg := range selected {
		sg.Fields = fields[sg.Goal.ID]
		h := histories[sg.Goal.ID]
		r.Highlights = append(r.Highlights, scopedHighlights(sg.Goal, highlights[sg.Goal.ID], since)...)
		fresh := freshness[sg.Goal.ID]
		if !h.exception(since, fresh) {
			r.Lines = append(r.Lines, sg)
			continue
		}
		pending = append(pending, exceptionGoal{h: h, fresh: fresh, fields: sg.Fields})
	}
	exceptionIDs := make([]int64, 0, len(pending))
	for _, p := range pending {
		exceptionIDs = append(exceptionIDs, p.h.goal.ID)
	}
	reads, err := s.readBlocks(ctx, exceptionIDs)
	if err != nil {
		return Report{}, err
	}
	var exceptions []reportException
	for _, p := range pending {
		b, err := s.reportBlock(p.h, since, p.fresh, reads)
		if err != nil {
			return Report{}, err
		}
		b.Fields = p.fields
		b.Changes = p.h.reportChanges(b, since, r.Baseline, p.fresh)
		exceptions = append(exceptions, reportException{
			block:          b,
			pathOverdue:    p.fresh.PathToGreenOverdue,
			slipped:        p.h.deliveryMoveDays(since) > 0,
			movedLifecycle: p.h.movedLifecycle(b, since),
		})
	}
	sortReportExceptions(exceptions)
	for _, x := range exceptions {
		r.Exceptions = append(r.Exceptions, x.block)
	}
	linkReportBlocks(&r, reads)
	if err := narrate(&r); err != nil {
		return Report{}, err
	}
	if r.MembershipChanges, err = s.membershipChanges(ctx, r, since); err != nil {
		return Report{}, err
	}
	r.Movement = reportMovement(r, exceptions, since)
	return r, nil
}

// SelectGoalsAgainst returns what def selects on a draft read against
// baseline, as DraftReport reads it: a date, or zero for def's default — its
// previous publication, or 30 days before today when there is none.
func (s *Service) SelectGoalsAgainst(ctx context.Context, def ReportDefinition, baseline time.Time) (ReportSelection, error) {
	since, err := s.readAgainst(ctx, &Report{Definition: def}, baseline)
	if err != nil {
		return ReportSelection{}, err
	}
	return s.SelectGoals(ctx, def, since)
}

// readAgainst sets the baseline r reads changes against — the previous
// publication of its Definition when baseline is zero, or 30 days before today
// when there is none, or else the date baseline — and returns since, which
// reports whether an instant counts as a change: after the previous
// publication, which already showed anything recorded by then, or on or after
// the baseline date in the org's calendar.
func (s *Service) readAgainst(ctx context.Context, r *Report, baseline time.Time) (func(time.Time) bool, error) {
	if baseline.IsZero() {
		var err error
		if r.Previous, err = s.previousPublication(ctx, r.Definition.ID); err != nil {
			return nil, err
		}
	}
	switch {
	case r.Previous.ID != 0:
		r.Baseline = orgDate(r.Previous.PublishedAt, s.loc)
	case baseline.IsZero():
		r.Baseline = orgDate(s.clock.Now(), s.loc).AddDate(0, 0, -defaultBaselineDays)
	default:
		y, m, d := baseline.Date()
		r.Baseline = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return s.since(*r), nil
}

// since reports whether an instant counts as a change on r, as readAgainst
// set it to read: after its previous publication, or else on or after its
// baseline date in the org's calendar.
func (s *Service) since(r Report) func(time.Time) bool {
	if r.Previous.ID != 0 {
		at := r.Previous.PublishedAt
		return func(t time.Time) bool { return t.After(at) }
	}
	date := r.Baseline
	return func(t time.Time) bool { return !orgDate(t, s.loc).Before(date) }
}

// reportFieldsByGoal are each of goals' values in the Fields def chose, by
// Field name, keyed by Goal ID. A chosen Field a Goal has no value in is left
// out, not shown blank; one retired since it was chosen still shows, as
// Retired values stay readable (CONTEXT.md: Retired).
func (s *Service) reportFieldsByGoal(ctx context.Context, def ReportDefinition, goals []Goal) (map[int64][]FieldValue, error) {
	if len(def.FieldIDs) == 0 {
		return map[int64][]FieldValue{}, nil
	}
	values, err := s.fieldValuesByGoal(ctx, goalIDsOf(goals))
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]FieldValue, len(values))
	for id, vs := range values {
		for _, v := range vs {
			if slices.Contains(def.FieldIDs, v.Field.ID) {
				out[id] = append(out[id], v)
			}
		}
	}
	return out, nil
}

// goalIDsOf are the IDs of goals, in order.
func goalIDsOf(goals []Goal) []int64 {
	ids := make([]int64, 0, len(goals))
	for _, g := range goals {
		ids = append(ids, g.ID)
	}
	return ids
}

// goalHistory is what a Report reads about one Goal to judge it against the
// baseline: its Check-ins, newest first, and its Date Slips, earliest first.
type goalHistory struct {
	goal     Goal
	checkins []Checkin
	slips    []DateSlip
}

// goalHistories reads the history of each of goals, keyed by Goal ID, in the
// same two queries however many Goals there are.
func (s *Service) goalHistories(ctx context.Context, goals []Goal) (map[int64]goalHistory, error) {
	ids := goalIDsOf(goals)
	checkins, err := s.checkinsByGoal(ctx, ids)
	if err != nil {
		return nil, err
	}
	slips, err := s.DateSlipsByGoal(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]goalHistory, len(goals))
	for _, g := range goals {
		out[g.ID] = goalHistory{goal: g, checkins: checkins[g.ID], slips: slips[g.ID]}
	}
	return out, nil
}

// latest is the Goal's latest Check-in, the zero Checkin when it has none.
func (h goalHistory) latest() Checkin {
	if len(h.checkins) == 0 {
		return Checkin{}
	}
	return h.checkins[0]
}

// last is the latest Check-in as freshness reads it.
func (h goalHistory) last() lastCheckin {
	c := h.latest()
	return lastCheckin{at: c.CreatedAt, health: c.Health, pathTargetDate: c.PathTargetDate}
}

// exception reports whether the Goal earns the full block: it is Red or Yellow,
// Check-in overdue, or Ownerless, or since the baseline it was created, slipped
// a date, changed Lifecycle, or changed Health (CONTEXT.md: Report).
func (h goalHistory) exception(since func(time.Time) bool, fresh Freshness) bool {
	return needsPathToGreen(h.latest().Health) ||
		fresh.CheckinOverdue ||
		h.goal.Ownerless ||
		h.changed(since)
}

// changed reports whether, since the baseline, the Goal was created, slipped
// a date, changed Lifecycle, or changed Health.
func (h goalHistory) changed(since func(time.Time) bool) bool {
	return since(h.goal.CreatedAt) ||
		slices.ContainsFunc(h.slips, func(d DateSlip) bool { return since(d.CreatedAt) }) ||
		h.lifecycleChanged(since) ||
		h.priorHealth(since) != ""
}

// priorHealth is the Goal's Health at the baseline when its latest Check-in's
// Health differs from it, and empty otherwise. Its Health at the baseline is
// that of its latest Check-in that since does not count; a Goal with no
// Check-in by then had none, and neither Health may be empty. Only the end
// state counts: Green to Yellow and back since the baseline is no change.
func (h goalHistory) priorHealth(since func(time.Time) bool) string {
	now := h.latest().Health
	i := slices.IndexFunc(h.checkins, func(c Checkin) bool { return !since(c.CreatedAt) })
	if i < 0 || now == "" {
		return ""
	}
	if then := h.checkins[i].Health; then != "" && then != now {
		return then
	}
	return ""
}

// lifecycleChanged reports whether the Goal changed Lifecycle since the
// baseline: activated through the activation gate, or moved in a Check-in.
func (h goalHistory) lifecycleChanged(since func(time.Time) bool) bool {
	if !h.goal.ActivatedAt.IsZero() && since(h.goal.ActivatedAt) {
		return true
	}
	return slices.ContainsFunc(h.checkins, func(c Checkin) bool {
		return c.LifecycleChange.Changed() && since(c.CreatedAt)
	})
}

// badges lists the block's badges in their fixed order.
func (h goalHistory) badges(since func(time.Time) bool, fresh Freshness) []string {
	var out []string
	if since(h.goal.CreatedAt) {
		out = append(out, BadgeNew)
	}
	if slices.ContainsFunc(h.slips, func(d DateSlip) bool { return d.OnDeliveryDate() && since(d.CreatedAt) }) {
		out = append(out, BadgeNewDate)
	}
	if fresh.CheckinOverdue {
		out = append(out, BadgeCheckinOverdue)
	}
	if h.goal.Ownerless {
		out = append(out, BadgeOwnerless)
	}
	if h.goal.Lifecycle == LifecycleOnHold {
		out = append(out, BadgeOnHold)
	}
	return out
}

// blockReads are what exception blocks show beyond their Goals' histories,
// read for every block of a draft at once by readBlocks, each keyed by Goal
// ID unless it says otherwise.
type blockReads struct {
	milestones map[int64][]Milestone
	metrics    map[int64][]Metric
	// readings are the Metrics' readings, earliest first, by Metric ID.
	readings map[int64][]MetricReading
	parents  map[int64][]Goal
	children map[int64][]Goal
	// childLatest are the latest Check-ins of the blocks' Active children.
	childLatest map[int64]Checkin
	// account looks up an Account for an Ask's addressee, reading each once.
	account func(id int64) (Account, bool, error)
}

// readBlocks reads what the exception blocks of goalIDs show beyond their
// histories, in the same few queries however many there are, but for an Ask's
// addressee, read one Manager at a time and each once.
func (s *Service) readBlocks(ctx context.Context, goalIDs []int64) (blockReads, error) {
	var reads blockReads
	var err error
	if reads.milestones, err = s.milestonesByGoal(ctx, goalIDs); err != nil {
		return blockReads{}, err
	}
	if reads.metrics, err = s.metricsByGoal(ctx, goalIDs); err != nil {
		return blockReads{}, err
	}
	if reads.readings, err = s.metricReadingsByMetric(ctx, goalIDs); err != nil {
		return blockReads{}, err
	}
	if reads.parents, err = s.parentsByGoal(ctx, goalIDs); err != nil {
		return blockReads{}, err
	}
	if reads.children, err = s.childrenByGoal(ctx, goalIDs); err != nil {
		return blockReads{}, err
	}
	var children []Goal
	for _, cs := range reads.children {
		children = append(children, cs...)
	}
	if reads.childLatest, err = s.LatestCheckinsByGoal(ctx, dedupeIDs(activeGoalIDs(children))); err != nil {
		return blockReads{}, err
	}
	accounts := map[int64]Account{}
	reads.account = func(id int64) (Account, bool, error) {
		if a, ok := accounts[id]; ok {
			return a, true, nil
		}
		a, err := s.Account(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return Account{}, false, nil
		} else if err != nil {
			return Account{}, false, err
		}
		accounts[id] = a
		return a, true, nil
	}
	return reads, nil
}

// reportBlock builds the full MBR block for an exception Goal from what
// readBlocks read of it.
func (s *Service) reportBlock(h goalHistory, since func(time.Time) bool, fresh Freshness, reads blockReads) (ReportBlock, error) {
	latest := h.latest()
	b := ReportBlock{
		Goal:             h.goal,
		Health:           latest.Health,
		PriorHealth:      h.priorHealth(since),
		RunOffGreenWeeks: s.runOffGreen(h.goal, h.checkins).Weeks,
		Badges:           h.badges(since, fresh),
		PriorDueDates:    h.priorDates(0),
		Status:           latest.Status,
		PathToGreen:      latest.PathToGreen,
		PathTargetDate:   latest.PathTargetDate,
		Explanation:      latest.Explanation,
		AsksForHelp:      h.goal.Lifecycle == LifecycleActive && needsPathToGreen(latest.Health) && latest.AsksForHelp,
		Timezone:         s.GoalTimezone(h.goal).String(),
	}
	b.PriorPathTargetDates = BackToGreenPriorDates(h.slips, s.runOffGreen(h.goal, h.checkins))
	if b.AsksForHelp {
		var err error
		if b.AskAddressee, err = askAddressee(h.goal.Owner, reads.account); err != nil {
			return ReportBlock{}, err
		}
	}
	for _, m := range reads.milestones[h.goal.ID] {
		b.Milestones = append(b.Milestones, ReportMilestone{
			Milestone:  m,
			New:        since(m.CreatedAt),
			PriorDates: h.priorDates(m.ID),
		})
	}
	for _, m := range reads.metrics[h.goal.ID] {
		rm := ReportMetric{Metric: m}
		if readings := reads.readings[m.ID]; len(readings) > 0 {
			rm.Current, rm.Read = readings[len(readings)-1].Value, true
		}
		b.Metrics = append(b.Metrics, rm)
	}
	b.RolledUp = s.rollUp(reads.children[h.goal.ID], reads.childLatest)
	return b, nil
}

// priorDates returns the dates the delivery date (milestoneID 0) or a
// Milestone held before each of its Date Slips, earliest first. A slip of the
// back-to-Green date is neither.
func (h goalHistory) priorDates(milestoneID int64) []time.Time {
	var out []time.Time
	for _, d := range h.slips {
		if d.MilestoneID == milestoneID && !d.BackToGreen {
			out = append(out, d.OldDate)
		}
	}
	return out
}
