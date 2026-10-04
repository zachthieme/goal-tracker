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
	historyOwnership historyFilter = "ownership"
	historyValues    historyFilter = "values"
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
	{historyOwnership, "Ownership"},
	{historyValues, "Values"},
}

// historyPage is how many entries the timeline shows at first, and how many
// more each "Show earlier" adds.
const historyPage = 20

// historyEntry is one event in a Goal's History: a Check-in with the Date
// Slips and Metric readings recorded in it, a So What revision, an ownership
// change (a Handoff or an Admin Reassign), a change to a Dimension value or
// Field, a Parent suggestion with its outcome, or a Nudge. Exactly one of
// Checkin, Revision, Handoff, Value, Suggestion and Nudge is set.
type historyEntry struct {
	At         time.Time
	Checkin    *domain.Checkin
	Slips      []domain.DateSlip
	Readings   []historyReading
	Revision   *domain.SoWhatRevision
	Handoff    *domain.Handoff
	Value      *domain.ValueChange
	Suggestion *domain.ParentSuggestion
	Nudge      *domain.Nudge
}

// historyReading is a Metric's reading as a Check-in recorded it.
type historyReading struct {
	Metric string
	Value  string
}

// kind names the entry's kind for its marker and the page's markup.
func (e historyEntry) kind() string {
	switch {
	case e.Checkin != nil:
		return "checkin"
	case e.Revision != nil:
		return "so-what"
	case e.Handoff != nil:
		return "ownership"
	case e.Suggestion != nil:
		return "parent-suggestion"
	case e.Nudge != nil:
		return "nudge"
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
	case e.Handoff != nil && e.Handoff.Status == domain.HandoffReassigned:
		return "Reassign"
	case e.Handoff != nil:
		return "Handoff"
	case e.Suggestion != nil:
		return "Parent suggestion"
	case e.Nudge != nil:
		return "Nudge"
	}
	return "Value"
}

// in reports whether the entry is listed under filter. Date Slips lists the
// Check-ins that carry one; Check-ins lists the Nudges asking for one too; a
// Parent suggestion is listed under All only.
func (e historyEntry) in(filter historyFilter) bool {
	switch filter {
	case historyCheckins:
		return e.Checkin != nil || e.Nudge != nil
	case historySlips:
		return len(e.Slips) > 0
	case historySoWhat:
		return e.Revision != nil
	case historyOwnership:
		return e.Handoff != nil
	case historyValues:
		return e.Value != nil
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
// revisions, ownership changes, value changes, Parent suggestions and Nudges
// into one timeline, newest first, showing everything and the first page.
func newHistory(v goalView, loc *time.Location, now time.Time) history {
	slipsBy := map[int64][]domain.DateSlip{}
	for _, s := range v.DateSlips {
		slipsBy[s.CheckinID] = append(slipsBy[s.CheckinID], s)
	}
	readingsBy := map[int64][]historyReading{}
	for _, t := range v.Trends {
		for _, r := range t.Readings {
			readingsBy[r.CheckinID] = append(readingsBy[r.CheckinID], historyReading{
				Metric: t.Metric.Name,
				Value:  strings.TrimSpace(fmtNum(r.Value) + " " + t.Metric.Unit),
			})
		}
	}

	var entries []historyEntry
	for _, c := range v.Checkins {
		entries = append(entries, historyEntry{At: c.CreatedAt, Checkin: &c, Slips: slipsBy[c.ID], Readings: readingsBy[c.ID]})
	}
	for _, r := range v.Revisions {
		entries = append(entries, historyEntry{At: r.CreatedAt, Revision: &r})
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
	// Newest first; entries made at the same moment keep the order they were
	// recorded in, latest first.
	slices.SortStableFunc(entries, func(a, b historyEntry) int {
		if c := b.At.Compare(a.At); c != 0 {
			return c
		}
		return cmp.Compare(b.seq(), a.seq())
	})
	return history{GoalID: v.Goal.ID, Entries: entries, Milestones: v.Milestones, Shown: historyPage, Loc: loc, Now: now, Strip: v.HealthStrip}
}

// seq is the entry's record ID, which orders entries of one kind made at the
// same moment.
func (e historyEntry) seq() int64 {
	switch {
	case e.Checkin != nil:
		return e.Checkin.ID
	case e.Revision != nil:
		return e.Revision.ID
	case e.Handoff != nil:
		return e.Handoff.ID
	case e.Suggestion != nil:
		return e.Suggestion.ID
	case e.Nudge != nil:
		return e.Nudge.ID
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
	h.Shown = historyPage
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

// more reports whether entries earlier than those shown are left.
func (h history) more() bool {
	return len(h.listed()) > h.Shown
}

// historyWeek is the entries of one week, Monday to Sunday in the org's
// calendar, under its heading.
type historyWeek struct {
	Heading string
	Entries []historyEntry
}

// weeks are the shown entries grouped by the week they fell in.
func (h history) weeks() []historyWeek {
	listed := h.listed()
	if len(listed) > h.Shown {
		listed = listed[:h.Shown]
	}
	var out []historyWeek
	for _, e := range listed {
		heading := h.weekHeading(e.At)
		if len(out) == 0 || out[len(out)-1].Heading != heading {
			out = append(out, historyWeek{Heading: heading})
		}
		out[len(out)-1].Entries = append(out[len(out)-1].Entries, e)
	}
	return out
}

// weekHeading names the week t falls in by its Monday, "Week of 28 Sep", with
// the year when it isn't this year.
func (h history) weekHeading(t time.Time) string {
	local := t.In(h.Loc)
	y, m, d := local.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, h.Loc)
	monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	return "Week of " + h.dayLabel(monday)
}

// dayLabel names a calendar date, "28 Sep", with the year when it isn't this
// year.
func (h history) dayLabel(d time.Time) string {
	if d.Year() != h.Now.In(h.Loc).Year() {
		return d.Format("2 Jan 2006")
	}
	return d.Format("2 Jan")
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
// "Week of 28 Sep", as History's headings do, and any other's by its first and
// last days.
func (h history) periodLabel(p domain.HealthPeriod) string {
	if h.Strip.CadenceDays == 7 {
		return "Week of " + h.dayLabel(p.First)
	}
	return h.dayLabel(p.First) + " – " + h.dayLabel(p.Last)
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

// slipWhat names what a Date Slip moved: the delivery date or a Milestone.
func (h history) slipWhat(s domain.DateSlip) string {
	if s.MilestoneID == 0 {
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
// Milestone GA (2026-11-09)", "Marked Beta Done" or "Removed Rollout to 50%:
// descoped".
func milestoneChangeText(m domain.MilestoneChange) string {
	switch m.Kind {
	case domain.MilestoneChangeAdded:
		return fmt.Sprintf("Added Milestone %s (%s)", m.Name, fmtDate(m.AddedDate))
	case domain.MilestoneChangeDone:
		return fmt.Sprintf("Marked %s Done", m.Name)
	}
	return fmt.Sprintf("Removed %s: %s", m.Name, m.Reason)
}

// when is t as an entry shows it, in the org's timezone: "Fri 2 Jan 15:04".
func (h history) when(t time.Time) string {
	return t.In(h.Loc).Format("Mon 2 Jan 15:04")
}

// suggestionOutcome says how a Parent suggestion ended and when, or that it's
// still open.
func (h history) suggestionOutcome(p domain.ParentSuggestion) string {
	switch p.Status {
	case domain.SuggestionOpen:
		return "Still open"
	case domain.SuggestionAccepted:
		return "Accepted " + h.when(p.ClosedAt)
	case domain.SuggestionDeclined:
		return "Declined " + h.when(p.ClosedAt)
	case domain.SuggestionWithdrawn:
		return "Withdrawn " + h.when(p.ClosedAt)
	}
	return "No longer applies, since " + h.when(p.ClosedAt)
}

// url is the Goal page listing filter with shown entries, scrolled to the
// History block. The first page and All are left out of the address.
func (h history) url(filter historyFilter, shown int) templ.SafeURL {
	q := url.Values{}
	if filter != historyAll {
		q.Set("history", string(filter))
	}
	if shown > historyPage {
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
	return h.url(h.Filter, h.Shown+historyPage)
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
	case historyValues:
		return "No value changes yet."
	}
	return "Nothing has happened on this Goal yet."
}
