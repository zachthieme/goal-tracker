package web

import (
	"fmt"
	"net/url"
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
	historyAll          historyFilter = ""
	historyCheckins     historyFilter = "checkins"
	historySlips        historyFilter = "date-slips"
	historyWhyItMatters historyFilter = "why-it-matters"
	historyTitle        historyFilter = "title"
	historyOwnership    historyFilter = "ownership"
	historyLinks        historyFilter = "links"
	historyValues       historyFilter = "values"
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
	{historyWhyItMatters, "Why it matters"},
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

// historyEntry is one entry on a Goal's History timeline: one of the events
// the Goal page's loader listed (domain.GoalEvent).
//
// Folded, when set, makes the entry stand for a run of Check-ins that repeat
// the one before them, newest first; the entry is the newest of them.
type historyEntry struct {
	Event  domain.GoalEvent
	Folded []domain.CheckinEvent
}

// At is when the entry's event happened.
func (e historyEntry) At() time.Time {
	return e.Event.At()
}

// historyEventIs reports whether e's event is a T.
func historyEventIs[T domain.GoalEvent](e historyEntry) bool {
	_, ok := e.Event.(T)
	return ok
}

// kind names the entry's kind for its marker and the page's markup.
func (e historyEntry) kind() string {
	switch e.Event.(type) {
	case domain.CheckinEvent:
		return "checkin"
	case domain.WhyItMattersRevisedEvent:
		return "why-it-matters"
	case domain.TitleChangeEvent:
		return "title"
	case domain.OwnershipChangeEvent:
		return "ownership"
	case domain.ParentSuggestionEvent:
		return "parent-suggestion"
	case domain.NudgeEvent:
		return "nudge"
	case domain.LinkChangeEvent:
		return "link"
	case domain.MilestoneAddedEvent:
		return "milestone"
	case domain.ActionItemRaisedEvent, domain.ActionItemReassignedEvent, domain.ActionItemClosedEvent:
		return "action-item"
	case domain.CommentEvent:
		return "comment"
	}
	return "value"
}

// label is the entry's kind as its text label says it.
func (e historyEntry) label() string {
	switch ev := e.Event.(type) {
	case domain.CheckinEvent:
		return "Check-in"
	case domain.WhyItMattersRevisedEvent:
		return "Why it matters"
	case domain.TitleChangeEvent:
		return "Title"
	case domain.OwnershipChangeEvent:
		if ev.Handoff.Status == domain.HandoffReassigned {
			return "Reassign"
		}
		return "Handoff"
	case domain.ParentSuggestionEvent:
		return "Parent suggestion"
	case domain.NudgeEvent:
		return "Nudge"
	case domain.LinkChangeEvent:
		return "Link"
	case domain.MilestoneAddedEvent:
		return "Milestone"
	case domain.ActionItemClosedEvent:
		return "Action Item closed"
	case domain.ActionItemRaisedEvent:
		return "Action Item raised"
	case domain.ActionItemReassignedEvent:
		return "Action Item reassigned"
	case domain.CommentEvent:
		if ev.Listing.Comment.ThreadID != 0 {
			return "Reply"
		}
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
		return historyEventIs[domain.CheckinEvent](e) || historyEventIs[domain.NudgeEvent](e) ||
			historyEventIs[domain.MilestoneAddedEvent](e)
	case historySlips:
		c, ok := e.Event.(domain.CheckinEvent)
		return ok && len(c.Slips) > 0
	case historyWhyItMatters:
		return historyEventIs[domain.WhyItMattersRevisedEvent](e)
	case historyTitle:
		return historyEventIs[domain.TitleChangeEvent](e)
	case historyOwnership:
		return historyEventIs[domain.OwnershipChangeEvent](e)
	case historyLinks:
		return historyEventIs[domain.LinkChangeEvent](e) || historyEventIs[domain.ParentSuggestionEvent](e)
	case historyValues:
		return historyEventIs[domain.ValueChangeEvent](e)
	case historyActionItems:
		return historyEventIs[domain.ActionItemRaisedEvent](e) || historyEventIs[domain.ActionItemReassignedEvent](e) ||
			historyEventIs[domain.ActionItemClosedEvent](e)
	case historyComments:
		return historyEventIs[domain.CommentEvent](e)
	}
	return true
}

// markerHealth is the Health the entry's marker shows: its Check-in's, or ""
// for a Check-in with none and every other kind.
func (e historyEntry) markerHealth() string {
	if c, ok := e.Event.(domain.CheckinEvent); ok {
		return c.Checkin.Health
	}
	return ""
}

// readingText is a Check-in's Metric reading as the History shows it: its
// value with the Metric's unit.
func readingText(r domain.CheckinReading) string {
	return strings.TrimSpace(fmtNum(r.Value) + " " + r.Metric.Unit)
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

// newHistory is a Goal's History as the Goal page's loader listed it, newest
// first, showing everything and the first page.
func newHistory(v goalView, loc *time.Location, now time.Time) history {
	entries := make([]historyEntry, 0, len(v.GoalPage.History))
	for _, ev := range v.GoalPage.History {
		entries = append(entries, historyEntry{Event: ev})
	}
	return history{GoalID: v.Goal.ID, Entries: entries, Milestones: v.Milestones, Shown: historyFirstPage, Loc: loc, Now: now, Strip: v.HealthStrip}
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
		var run []domain.CheckinEvent
		for i+1 < len(listed) && listed[i].foldsOnto(listed[i+1]) {
			run = append(run, listed[i].Event.(domain.CheckinEvent))
			i++
		}
		if len(run) > 0 {
			out = append(out, historyEntry{Event: run[0], Folded: run})
		}
		out = append(out, listed[i])
	}
	return out
}

// foldsOnto reports whether the entry is a Check-in that repeats prev, the
// entry listed just before it, by the domain's rule (domain.CheckinEvent's
// Repeats).
func (e historyEntry) foldsOnto(prev historyEntry) bool {
	c, ok := e.Event.(domain.CheckinEvent)
	p, prevOK := prev.Event.(domain.CheckinEvent)
	return ok && prevOK && c.Repeats(p)
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
		heading := h.weekHeading(e.At())
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
	case historyWhyItMatters:
		return "No Why it matters revisions."
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
