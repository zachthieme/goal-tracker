package web

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// historyFilter is a History filter chip: which kind of entry the timeline
// lists, from the Goal page's ?history= address. historyAll lists everything.
type historyFilter string

const (
	historyAll       historyFilter = ""
	historyCheckins  historyFilter = "checkins"
	historySlips     historyFilter = "date-slips"
	historySoWhat    historyFilter = "so-what"
	historyTitle     historyFilter = "title"
	historyOwnership historyFilter = "ownership"
	historyLinks     historyFilter = "links"
	historyValues    historyFilter = "values"
	// historyActionItems lists the Action Items about the Goal: each one
	// raised, reassigned and closed.
	historyActionItems historyFilter = "action-items"
	// historyComments lists every Comment and reply about the Goal, made on
	// the Goal itself or on a Report.
	historyComments historyFilter = "comments"
)

// historyFilters are the chips in the order they show, each with its label.
var historyFilters = []struct {
	filter historyFilter
	label  string
}{
	{historyAll, "All"},
	{historyCheckins, "Check-ins"},
	{historySlips, "Date Slips"},
	{historySoWhat, "So What"},
	{historyTitle, "Title"},
	{historyOwnership, "Ownership"},
	{historyLinks, "Links"},
	{historyValues, "Values"},
	{historyActionItems, "Action Items"},
	{historyComments, "Comments"},
}

// historyFirstPage is how many entries the timeline shows at first, and
// historyPageStep how many more each "Show earlier" adds.
const (
	historyFirstPage = 5
	historyPageStep  = 20
)

// historyEntry is one event in a Goal's History: a Check-in with the Date
// Slips and Metric readings recorded in it, a So What revision, a change to
// its title, an ownership change (a Handoff or an Admin Reassign), a change to
// a Dimension value or Field, a Parent suggestion with its outcome, a Nudge, or
// a change to one of its links, a Milestone added outside a Check-in, an
// Action Item about it raised, reassigned or closed, or a Comment or reply
// about it, on the Goal itself or on a Report.
// Exactly one of Checkin, Revision, Title, Handoff, Value, Suggestion, Nudge,
// Link, Milestone, ActionItem, ActionReassign and Comment is set. A Check-in's
// Highlights are the ones it carries.
//
// Folded, when set, makes the entry stand for a run of Check-ins that repeat
// the one before them, newest first; the entry is the newest of them.
type historyEntry struct {
	At         time.Time
	Checkin    *domain.Checkin
	Slips      []domain.DateSlip
	Readings   []historyReading
	Highlights []domain.Highlight
	Folded     []historyEntry
	Revision   *domain.SoWhatRevision
	Title      *domain.TitleRevision
	Handoff    *domain.Handoff
	Value      *domain.ValueChange
	Suggestion *domain.ParentSuggestion
	Nudge      *domain.Nudge
	Link       *domain.LinkEvent
	Milestone  *domain.MilestoneChange
	// ActionItem is an Action Item raised, or closed when ActionItemEvent
	// says so; ActionRaisedFor is who it was raised for, its owner before any
	// reassignment. ActionReassign is one handed to a new owner.
	ActionItem      *domain.ActionItemListing
	ActionItemEvent historyActionItemEvent
	ActionRaisedFor domain.Account
	ActionReassign  *domain.ActionItemReassignment
	Comment         *domain.CommentListing
}

// historyActionItemEvent is what an ActionItem entry records of its Action
// Item: that it was raised, or closed.
type historyActionItemEvent int

const (
	historyActionItemRaised historyActionItemEvent = iota
	historyActionItemClosed
)

// historyReading is a Metric's reading as a Check-in recorded it. Changed is
// whether it differs from the Metric's reading before it, or is its first.
type historyReading struct {
	Metric  string
	Value   string
	Changed bool
}

// kind names the entry's kind for its marker and the page's markup.
func (e historyEntry) kind() string {
	switch {
	case e.Checkin != nil:
		return "checkin"
	case e.Revision != nil:
		return "so-what"
	case e.Title != nil:
		return "title"
	case e.Handoff != nil:
		return "ownership"
	case e.Suggestion != nil:
		return "parent-suggestion"
	case e.Nudge != nil:
		return "nudge"
	case e.Link != nil:
		return "link"
	case e.Milestone != nil:
		return "milestone"
	case e.ActionItem != nil, e.ActionReassign != nil:
		return "action-item"
	case e.Comment != nil:
		return "comment"
	}
	return "value"
}

// label is the entry's kind as its text label says it.
func (e historyEntry) label() string {
	switch {
	case e.Checkin != nil:
		return "Check-in"
	case e.Revision != nil:
		return "So What"
	case e.Title != nil:
		return "Title"
	case e.Handoff != nil && e.Handoff.Status == domain.HandoffReassigned:
		return "Reassign"
	case e.Handoff != nil:
		return "Handoff"
	case e.Suggestion != nil:
		return "Parent suggestion"
	case e.Nudge != nil:
		return "Nudge"
	case e.Link != nil:
		return "Link"
	case e.Milestone != nil:
		return "Milestone"
	case e.ActionItem != nil && e.ActionItemEvent == historyActionItemClosed:
		return "Action Item closed"
	case e.ActionItem != nil:
		return "Action Item raised"
	case e.ActionReassign != nil:
		return "Action Item reassigned"
	case e.Comment != nil && e.Comment.Comment.ThreadID != 0:
		return "Reply"
	case e.Comment != nil:
		return "Comment"
	}
	return "Value"
}

// in reports whether the entry is listed under filter. Date Slips lists the
// Check-ins that carry one; Check-ins lists the Nudges asking for one too, and
// the Milestones added between them; Links lists the Goal's link changes and
// its Parent suggestions.
func (e historyEntry) in(filter historyFilter) bool {
	switch filter {
	case historyCheckins:
		return e.Checkin != nil || e.Nudge != nil || e.Milestone != nil
	case historySlips:
		return len(e.Slips) > 0
	case historySoWhat:
		return e.Revision != nil
	case historyTitle:
		return e.Title != nil
	case historyOwnership:
		return e.Handoff != nil
	case historyLinks:
		return e.Link != nil || e.Suggestion != nil
	case historyValues:
		return e.Value != nil
	case historyActionItems:
		return e.ActionItem != nil || e.ActionReassign != nil
	case historyComments:
		return e.Comment != nil
	}
	return true
}

// history is a Goal's History as one timeline, newest first: every entry,
// the filter and how many entries the page shows, and the org's calendar the
// weeks are counted in. Milestones name the Milestones a Date Slip moved.
type history struct {
	GoalID     int64
	Entries    []historyEntry
	Milestones []domain.Milestone
	Filter     historyFilter
	Shown      int
	Loc        *time.Location
	Now        time.Time
	// Strip is the Goal's Health over its last Check-in periods, shown above
	// the timeline.
	Strip domain.HealthStrip
}

// newHistory gathers a Goal's Check-ins, Date Slips, Metric readings, So What
// revisions, title changes, ownership changes, value changes, Parent suggestions, Nudges,
// link changes, the Milestones added outside a Check-in, its Action Items'
// raising, reassignments and closing, and every Comment and reply about it into
// one timeline, newest first, showing everything and the first page.
func newHistory(v goalView, loc *time.Location, now time.Time) history {
	slipsBy := map[int64][]domain.DateSlip{}
	for _, s := range v.DateSlips {
		slipsBy[s.CheckinID] = append(slipsBy[s.CheckinID], s)
	}
	readingsBy := map[int64][]historyReading{}
	for _, t := range v.Trends {
		for i, r := range t.Readings {
			readingsBy[r.CheckinID] = append(readingsBy[r.CheckinID], historyReading{
				Metric:  t.Metric.Name,
				Value:   strings.TrimSpace(fmtNum(r.Value) + " " + t.Metric.Unit),
				Changed: i == 0 || r.Value != t.Readings[i-1].Value,
			})
		}
	}
	highlightsBy := map[int64][]domain.Highlight{}
	for _, hl := range v.Highlights {
		highlightsBy[hl.CheckinID] = append(highlightsBy[hl.CheckinID], hl)
	}

	var entries []historyEntry
	for _, c := range v.Checkins {
		entries = append(entries, historyEntry{At: c.CreatedAt, Checkin: &c, Slips: slipsBy[c.ID], Readings: readingsBy[c.ID], Highlights: highlightsBy[c.ID]})
	}
	for _, r := range v.Revisions {
		entries = append(entries, historyEntry{At: r.CreatedAt, Revision: &r})
	}
	for _, r := range v.TitleRevisions {
		entries = append(entries, historyEntry{At: r.CreatedAt, Title: &r})
	}
	for _, o := range v.Ownership {
		entries = append(entries, historyEntry{At: o.CreatedAt, Handoff: &o})
	}
	for _, c := range v.ValueHistory {
		entries = append(entries, historyEntry{At: c.CreatedAt, Value: &c})
	}
	for _, p := range v.Suggestions {
		entries = append(entries, historyEntry{At: p.CreatedAt, Suggestion: &p})
	}
	for _, n := range v.Nudges {
		entries = append(entries, historyEntry{At: n.CreatedAt, Nudge: &n})
	}
	for _, l := range v.LinkEvents {
		entries = append(entries, historyEntry{At: l.CreatedAt, Link: &l})
	}
	for _, m := range v.MilestoneAdditions {
		entries = append(entries, historyEntry{At: m.CreatedAt, Milestone: &m})
	}
	// An Action Item was raised for the owner its first reassignment took it
	// from, since a reassignment changes its owner in place.
	raisedFor := map[int64]domain.Account{}
	for _, r := range v.ActionReassigns {
		if _, ok := raisedFor[r.ItemID]; !ok {
			raisedFor[r.ItemID] = r.From
		}
		entries = append(entries, historyEntry{At: r.At, ActionReassign: &r})
	}
	for _, l := range v.ActionItems {
		owner, ok := raisedFor[l.Item.ID]
		if !ok {
			owner = l.Item.Owner
		}
		entries = append(entries, historyEntry{At: l.Item.CreatedAt, ActionItem: &l, ActionRaisedFor: owner})
		if l.Item.Closed {
			entries = append(entries, historyEntry{At: l.Item.ClosedAt, ActionItem: &l, ActionItemEvent: historyActionItemClosed})
		}
	}
	for _, c := range v.Comments {
		entries = append(entries, historyEntry{At: c.Comment.CreatedAt, Comment: &c})
	}
	// Newest first. Entries made at the same moment keep the order they were
	// recorded in, latest first, but an Action Item's events stay together:
	// newest item first, each closed above its reassignments, latest first,
	// above its raising.
	slices.SortStableFunc(entries, func(a, b historyEntry) int {
		if c := b.At.Compare(a.At); c != 0 {
			return c
		}
		return b.tieKey().compare(a.tieKey())
	})
	return history{GoalID: v.Goal.ID, Entries: entries, Milestones: v.Milestones, Shown: historyFirstPage, Loc: loc, Now: now, Strip: v.HealthStrip}
}

// historyTieKey orders entries made at the same moment: by seq, then rank,
// then sub, each ascending from oldest to newest.
type historyTieKey struct {
	seq  int64 // the entry's record ID, or for an Action Item's events its item's
	rank int   // an Action Item's event: 0 raised (and every other kind), 1 reassigned, 2 closed
	sub  int64 // a reassignment's record ID, ordering one item's reassignments; else 0
}

// compare is -1, 0 or +1 as k orders before, with or after o.
func (k historyTieKey) compare(o historyTieKey) int {
	if c := cmp.Compare(k.seq, o.seq); c != 0 {
		return c
	}
	if c := cmp.Compare(k.rank, o.rank); c != 0 {
		return c
	}
	return cmp.Compare(k.sub, o.sub)
}

// tieKey is the key that orders the entry among those made at the same moment.
func (e historyEntry) tieKey() historyTieKey {
	k := historyTieKey{seq: e.seq()}
	switch {
	case e.ActionReassign != nil:
		k.rank, k.sub = 1, e.ActionReassign.ID
	case e.ActionItem != nil && e.ActionItemEvent == historyActionItemClosed:
		k.rank = 2
	}
	return k
}

// seq is the entry's record ID, which orders entries of one kind made at the
// same moment; for an Action Item's events it is the item's ID.
func (e historyEntry) seq() int64 {
	switch {
	case e.Checkin != nil:
		return e.Checkin.ID
	case e.Revision != nil:
		return e.Revision.ID
	case e.Title != nil:
		return e.Title.ID
	case e.Handoff != nil:
		return e.Handoff.ID
	case e.Suggestion != nil:
		return e.Suggestion.ID
	case e.Nudge != nil:
		return e.Nudge.ID
	case e.Link != nil:
		return e.Link.ID
	case e.Milestone != nil:
		return e.Milestone.ID
	case e.ActionItem != nil:
		return e.ActionItem.Item.ID
	case e.ActionReassign != nil:
		return e.ActionReassign.ItemID
	case e.Comment != nil:
		return e.Comment.Comment.ID
	}
	return e.Value.ID
}

// narrowed is h with the filter and page count its address asks for: an
// unknown filter shows everything, and a missing or bad count the first page.
func (h history) narrowed(q url.Values) history {
	h.Filter = historyAll
	for _, f := range historyFilters {
		if string(f.filter) == q.Get("history") {
			h.Filter = f.filter
		}
	}
	h.Shown = historyFirstPage
	if n, err := strconv.Atoi(q.Get("shown")); err == nil && n > 0 {
		h.Shown = n
	}
	return h
}

// historyChip is a filter chip with the number of entries it lists.
type historyChip struct {
	Filter historyFilter
	Label  string
	Count  int
}

// chips are the filter chips, each with its count.
func (h history) chips() []historyChip {
	out := make([]historyChip, 0, len(historyFilters))
	for _, f := range historyFilters {
		n := 0
		for _, e := range h.Entries {
			if e.in(f.filter) {
				n++
			}
		}
		out = append(out, historyChip{Filter: f.filter, Label: f.label, Count: n})
	}
	return out
}

// listed is the entries the filter lists, newest first.
func (h history) listed() []historyEntry {
	var out []historyEntry
	for _, e := range h.Entries {
		if e.in(h.Filter) {
			out = append(out, e)
		}
	}
	return out
}

// slots are the listed entries with each run of Check-ins that repeat the one
// before them folded into one entry, at the newest one's place, newest first.
// Each slot is one of the entries the page shows.
func (h history) slots() []historyEntry {
	listed := h.listed()
	var out []historyEntry
	for i := 0; i < len(listed); i++ {
		var run []historyEntry
		for i+1 < len(listed) && listed[i].repeats(listed[i+1]) {
			run = append(run, listed[i])
			i++
		}
		if len(run) > 0 {
			folded := run[0]
			folded.Folded = run
			out = append(out, folded)
		}
		out = append(out, listed[i])
	}
	return out
}

// repeats reports whether the entry is a Check-in that repeats prev, the entry
// just before it: prev is a Check-in with the same Health, status, Path to
// Green, target date, Ask for help and explanation, so ticking or unticking
// the ask never folds, and this one records nothing more — no
// Date Slip, Milestone or Lifecycle change, Highlight, discarded Draft
// Highlight or Metric reading that differs from the one before it.
func (e historyEntry) repeats(prev historyEntry) bool {
	c, p := e.Checkin, prev.Checkin
	if c == nil || p == nil {
		return false
	}
	if c.Health != p.Health || c.Status != p.Status || c.PathToGreen != p.PathToGreen ||
		!c.PathTargetDate.Equal(p.PathTargetDate) || c.AsksForHelp != p.AsksForHelp || c.Explanation != p.Explanation {
		return false
	}
	if len(e.Slips) > 0 || len(c.MilestoneChanges) > 0 || c.LifecycleChange.Changed() ||
		len(e.Highlights) > 0 || len(c.DiscardedDraftHighlights) > 0 {
		return false
	}
	return !slices.ContainsFunc(e.Readings, func(r historyReading) bool { return r.Changed })
}

// foldedLabel is what a folded entry reads: "No change", or "No change ×3"
// for several.
func (e historyEntry) foldedLabel() string {
	if len(e.Folded) == 1 {
		return "No change"
	}
	return fmt.Sprintf("No change ×%d", len(e.Folded))
}

// more reports whether slots earlier than those shown are left.
func (h history) more() bool {
	return len(h.slots()) > h.Shown
}

// historyWeek is the entries of one week, Monday to Sunday in the org's
// calendar, under its heading.
type historyWeek struct {
	Heading string
	Entries []historyEntry
}

// weeks are the shown slots grouped by the week they fell in.
func (h history) weeks() []historyWeek {
	slots := h.slots()
	if len(slots) > h.Shown {
		slots = slots[:h.Shown]
	}
	var out []historyWeek
	for _, e := range slots {
		heading := h.weekHeading(e.At)
		if len(out) == 0 || out[len(out)-1].Heading != heading {
			out = append(out, historyWeek{Heading: heading})
		}
		out[len(out)-1].Entries = append(out[len(out)-1].Entries, e)
	}
	return out
}

// weekHeading names the week t falls in by its Monday, "Week of Sep 28", with
// the year when it isn't this year.
func (h history) weekHeading(t time.Time) string {
	local := t.In(h.Loc)
	y, m, d := local.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, h.Loc)
	monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	return "Week of " + h.dates().date(monday)
}

// dates is the clock History shows its dates and times against.
func (h history) dates() displayDates {
	return displayDates{Now: h.Now, Loc: h.Loc}
}

// stripCell is one cell of the Health strip: its state (the Health's class,
// "no-checkin" for a period the Goal went without a Check-in it owed,
// "not-yet-due" for one in progress with none yet, or "blank" for one it owed
// none in) and its text equivalent.
type stripCell struct {
	State string
	Text  string
}

// health reports whether the cell shows a Health.
func (c stripCell) health() bool {
	return c.State != "no-checkin" && c.State != "not-yet-due" && c.State != "blank"
}

// stripCells are the Health strip's cells, oldest first.
func (h history) stripCells() []stripCell {
	out := make([]stripCell, 0, len(h.Strip.Periods))
	for _, p := range h.Strip.Periods {
		c := stripCell{State: healthClass(p.Health), Text: h.periodLabel(p) + ": " + p.Health}
		switch {
		case p.NoCheckin:
			c.State, c.Text = "no-checkin", h.periodLabel(p)+": no Check-in"
		case p.NotYetDue:
			c.State, c.Text = "not-yet-due", h.periodLabel(p)+": not yet due"
		case p.Health == "" && p.Lifecycle == domain.LifecycleProposed:
			c.State, c.Text = "blank", h.periodLabel(p)+": not yet Active"
		case p.Health == "":
			c.State, c.Text = "blank", h.periodLabel(p)+": "+p.Lifecycle
		}
		out = append(out, c)
	}
	return out
}

// periodLabel names a period on the strip: a weekly Goal's by its Monday,
// "Week of Sep 28", as History's headings do, and any other's by its first and
// last days, "Sep 28 – Oct 11".
func (h history) periodLabel(p domain.HealthPeriod) string {
	if h.Strip.CadenceDays == 7 {
		return "Week of " + h.dates().date(p.First)
	}
	return h.dates().date(p.First) + " – " + h.dates().date(p.Last)
}

// stripSpan names the stretch the strip covers: "Last 11 weeks", or "Last 11
// periods of 14 days".
func (h history) stripSpan() string {
	if h.Strip.CadenceDays == 7 {
		return fmt.Sprintf("Last %d weeks", len(h.Strip.Periods))
	}
	return fmt.Sprintf("Last %d periods of %d days", len(h.Strip.Periods), h.Strip.CadenceDays)
}

// stripSummary sums the strip up in one line: "Last 11 weeks: 7 Green, 1
// Yellow, 2 with no Check-in, 1 not yet due."
func (h history) stripSummary() string {
	counts := map[string]int{}
	for _, p := range h.Strip.Periods {
		switch {
		case p.NoCheckin:
			counts["no-checkin"]++
		case p.NotYetDue:
			counts["not-yet-due"]++
		case p.Health == "":
			counts["blank"]++
		default:
			counts[p.Health]++
		}
	}
	var parts []string
	for _, k := range []struct{ key, text string }{
		{domain.HealthGreen, "Green"},
		{domain.HealthYellow, "Yellow"},
		{domain.HealthRed, "Red"},
		{"no-checkin", "with no Check-in"},
		{"not-yet-due", "not yet due"},
		{"blank", "not Active"},
	} {
		if n := counts[k.key]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, k.text))
		}
	}
	return h.stripSpan() + ": " + strings.Join(parts, ", ") + "."
}

// slipWhat names what a Date Slip moved: the delivery date, a Milestone or the
// Path to Green's back-to-Green date.
func (h history) slipWhat(s domain.DateSlip) string {
	switch {
	case s.BackToGreen:
		return "Back to Green date"
	case s.OnDeliveryDate():
		return "Delivery date"
	}
	for _, m := range h.Milestones {
		if m.ID == s.MilestoneID {
			return "Milestone " + m.Name
		}
	}
	return fmt.Sprintf("Milestone %d", s.MilestoneID)
}

// milestoneChangeText says what a Check-in did to a Milestone: "Added
// Milestone GA (Nov 9)", "Marked Beta Done" or "Removed Rollout to 50%:
// descoped".
func milestoneChangeText(m domain.MilestoneChange, dates displayDates) string {
	switch m.Kind {
	case domain.MilestoneChangeAdded:
		return fmt.Sprintf("Added Milestone %s (%s)", m.Name, dates.date(m.AddedDate))
	case domain.MilestoneChangeDone:
		return fmt.Sprintf("Marked %s Done", m.Name)
	}
	return fmt.Sprintf("Removed %s: %s", m.Name, m.Reason)
}

// linkChange says what a link change did, phrased for the Goal whose History
// it's on, as the words before and after the other Goal's title: "No longer
// contributes to" Reduce outages on the child, Migrate displays "no longer
// contributes to this" on the parent. An Undo says "Undid:" before the change
// it undid.
func linkChange(e domain.LinkEvent) (before, after string) {
	kind := e.Kind
	undid := ""
	switch kind {
	case domain.LinkEventRejectionUndone:
		kind, undid = domain.LinkEventRejected, "Undid: "
	case domain.LinkEventRemovalUndone:
		kind, undid = domain.LinkEventRemoved, "Undid: "
	}
	if e.OnChild() {
		switch kind {
		case domain.LinkEventRequested:
			before = "Requested to contribute to "
		case domain.LinkEventAccepted:
			after = " accepted this as a contributor"
		case domain.LinkEventRejected:
			after = " declined the request to contribute to it"
		case domain.LinkEventRemoved:
			before = "No longer contributes to "
		default:
			before = "Contributes to "
		}
	} else {
		switch kind {
		case domain.LinkEventRequested:
			after = " asked to contribute to this"
		case domain.LinkEventAccepted:
			before, after = "Accepted ", " as a contributor"
		case domain.LinkEventRejected:
			before, after = "Declined ", "'s request to contribute to this"
		case domain.LinkEventRemoved:
			after = " no longer contributes to this"
		default:
			after = " contributes to this"
		}
	}
	return undid + before, after
}

// suggestionOutcome says how a Parent suggestion ended and when, or that it's
// still open.
func (h history) suggestionOutcome(p domain.ParentSuggestion) string {
	switch p.Status {
	case domain.SuggestionOpen:
		return "Still open"
	case domain.SuggestionAccepted:
		return "Accepted " + h.dates().timestamp(p.ClosedAt)
	case domain.SuggestionDeclined:
		return "Declined " + h.dates().timestamp(p.ClosedAt)
	case domain.SuggestionWithdrawn:
		return "Withdrawn " + h.dates().timestamp(p.ClosedAt)
	}
	return "No longer applies, since " + h.dates().timestamp(p.ClosedAt)
}

// url is the Goal page listing filter with shown entries, scrolled to the
// History block. The first page and All are left out of the address.
func (h history) url(filter historyFilter, shown int) templ.SafeURL {
	q := url.Values{}
	if filter != historyAll {
		q.Set("history", string(filter))
	}
	if shown > historyFirstPage {
		q.Set("shown", strconv.Itoa(shown))
	}
	u := fmt.Sprintf("/goals/%d", h.GoalID)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return templ.SafeURL(u + "#history")
}

// earlierURL shows the next page of entries under the same filter.
func (h history) earlierURL() templ.SafeURL {
	return h.url(h.Filter, h.Shown+historyPageStep)
}

// chipAttrs marks the chip for the filter shown as the current one.
func (h history) chipAttrs(c historyChip) templ.Attributes {
	if c.Filter == h.Filter {
		return templ.Attributes{"aria-current": "page"}
	}
	return templ.Attributes{}
}

// emptyText is what the timeline says when the filter lists nothing.
func (h history) emptyText() string {
	switch h.Filter {
	case historyCheckins:
		return "No Check-ins yet."
	case historySlips:
		return "No Date Slips."
	case historySoWhat:
		return "No So What revisions."
	case historyOwnership:
		return "No ownership changes yet."
	case historyLinks:
		return "No link changes or Parent suggestions yet."
	case historyValues:
		return "No value changes yet."
	case historyActionItems:
		return "No Action Items yet."
	case historyComments:
		return "No Comments yet."
	}
	return "Nothing has happened on this Goal yet."
}
