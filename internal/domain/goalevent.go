package domain

import (
	"cmp"
	"slices"
	"time"
)

// GoalEvent is one entry in a Goal's History (CONTEXT.md: History): a
// Check-in with what it recorded, a Why it matters revision, a title change,
// an ownership change, a value change, a Parent suggestion, a Nudge, a link
// change, a Milestone added outside a Check-in, an Action Item about the Goal
// raised, reassigned or closed, or a Comment or reply about it. Each kind is
// its own type; its unexported method seals the set, so only the domain adds
// kinds.
type GoalEvent interface {
	// At is when it happened.
	At() time.Time
	// tie orders it among the events at the same instant.
	tie() goalEventTie
}

// CheckinEvent is a Check-in in the History, with the Date Slips, Metric
// readings and Highlights it recorded.
type CheckinEvent struct {
	Checkin    Checkin
	Slips      []DateSlip
	Readings   []CheckinReading
	Highlights []Highlight
}

// Repeats reports whether the Check-in repeats prev, the one it is compared
// with: prev says the same Health, status, Path to Green, target date, Ask for
// help and explanation, so ticking or unticking the ask never repeats, and
// this one records nothing more — no Date Slip, Milestone or Lifecycle
// change, Highlight, discarded Draft Highlight or Metric reading that changed.
// What prev records doesn't matter.
func (e CheckinEvent) Repeats(prev CheckinEvent) bool {
	c, p := e.Checkin, prev.Checkin
	if c.Health != p.Health || c.Status != p.Status || c.PathToGreen != p.PathToGreen ||
		!c.PathTargetDate.Equal(p.PathTargetDate) || c.AsksForHelp != p.AsksForHelp || c.Explanation != p.Explanation {
		return false
	}
	if len(e.Slips) > 0 || len(c.MilestoneChanges) > 0 || c.LifecycleChange.Changed() ||
		len(e.Highlights) > 0 || len(c.DiscardedDraftHighlights) > 0 {
		return false
	}
	return !slices.ContainsFunc(e.Readings, func(r CheckinReading) bool { return r.Changed })
}

// CheckinReading is a Metric's reading as a Check-in recorded it. Changed is
// whether it is the Metric's first reading or differs from the one before it.
type CheckinReading struct {
	Metric  Metric
	Value   float64
	Changed bool
}

// WhyItMattersRevisedEvent is a revision of the Goal's Why it matters.
type WhyItMattersRevisedEvent struct{ Revision WhyItMattersRevision }

// TitleChangeEvent is a change to the Goal's title.
type TitleChangeEvent struct{ Revision TitleRevision }

// OwnershipChangeEvent is a Handoff, with its outcome, or an Admin's Reassign.
type OwnershipChangeEvent struct{ Handoff Handoff }

// ValueChangeEvent is a change to one of the Goal's Dimension values or Fields.
type ValueChangeEvent struct{ Change ValueChange }

// ParentSuggestionEvent is a Parent suggestion made for the Goal, with its
// outcome.
type ParentSuggestionEvent struct{ Suggestion ParentSuggestion }

// NudgeEvent is a Nudge asking for a Check-in on the Goal.
type NudgeEvent struct{ Nudge Nudge }

// LinkChangeEvent is a change to one of the Goal's links.
type LinkChangeEvent struct{ Link LinkEvent }

// MilestoneAddedEvent is a Milestone added outside a Check-in.
type MilestoneAddedEvent struct{ Change MilestoneChange }

// ActionItemRaisedEvent is an Action Item about the Goal raised; RaisedFor is
// the owner it was raised for, before any reassignment.
type ActionItemRaisedEvent struct {
	Listing   ActionItemListing
	RaisedFor Account
}

// ActionItemReassignedEvent is an Action Item about the Goal handed to a new
// owner.
type ActionItemReassignedEvent struct{ Reassignment ActionItemReassignment }

// ActionItemClosedEvent is an Action Item about the Goal closed.
type ActionItemClosedEvent struct{ Listing ActionItemListing }

// CommentEvent is a Comment or reply about the Goal, made on the Goal itself
// or on a Report.
type CommentEvent struct{ Listing CommentListing }

func (e CheckinEvent) At() time.Time              { return e.Checkin.CreatedAt }
func (e WhyItMattersRevisedEvent) At() time.Time  { return e.Revision.CreatedAt }
func (e TitleChangeEvent) At() time.Time          { return e.Revision.CreatedAt }
func (e OwnershipChangeEvent) At() time.Time      { return e.Handoff.CreatedAt }
func (e ValueChangeEvent) At() time.Time          { return e.Change.CreatedAt }
func (e ParentSuggestionEvent) At() time.Time     { return e.Suggestion.CreatedAt }
func (e NudgeEvent) At() time.Time                { return e.Nudge.CreatedAt }
func (e LinkChangeEvent) At() time.Time           { return e.Link.CreatedAt }
func (e MilestoneAddedEvent) At() time.Time       { return e.Change.CreatedAt }
func (e ActionItemRaisedEvent) At() time.Time     { return e.Listing.Item.CreatedAt }
func (e ActionItemReassignedEvent) At() time.Time { return e.Reassignment.At }
func (e ActionItemClosedEvent) At() time.Time     { return e.Listing.Item.ClosedAt }
func (e CommentEvent) At() time.Time              { return e.Listing.Comment.CreatedAt }

// goalEventTie orders events at the same instant: by seq, then rank, then
// sub, each ascending from oldest to newest, so the larger shows first.
type goalEventTie struct {
	seq  int64 // the event's record ID, or for an Action Item's events its item's
	rank int   // an Action Item's event: 0 raised (and every other kind), 1 reassigned, 2 closed
	sub  int64 // a reassignment's record ID, ordering one item's reassignments; else 0
}

// compare is -1, 0 or +1 as k orders before, with or after o.
func (k goalEventTie) compare(o goalEventTie) int {
	if c := cmp.Compare(k.seq, o.seq); c != 0 {
		return c
	}
	if c := cmp.Compare(k.rank, o.rank); c != 0 {
		return c
	}
	return cmp.Compare(k.sub, o.sub)
}

func (e CheckinEvent) tie() goalEventTie             { return goalEventTie{seq: e.Checkin.ID} }
func (e WhyItMattersRevisedEvent) tie() goalEventTie { return goalEventTie{seq: e.Revision.ID} }
func (e TitleChangeEvent) tie() goalEventTie         { return goalEventTie{seq: e.Revision.ID} }
func (e OwnershipChangeEvent) tie() goalEventTie     { return goalEventTie{seq: e.Handoff.ID} }
func (e ValueChangeEvent) tie() goalEventTie         { return goalEventTie{seq: e.Change.ID} }
func (e ParentSuggestionEvent) tie() goalEventTie    { return goalEventTie{seq: e.Suggestion.ID} }
func (e NudgeEvent) tie() goalEventTie               { return goalEventTie{seq: e.Nudge.ID} }
func (e LinkChangeEvent) tie() goalEventTie          { return goalEventTie{seq: e.Link.ID} }
func (e MilestoneAddedEvent) tie() goalEventTie      { return goalEventTie{seq: e.Change.ID} }
func (e ActionItemRaisedEvent) tie() goalEventTie    { return goalEventTie{seq: e.Listing.Item.ID} }
func (e ActionItemReassignedEvent) tie() goalEventTie {
	return goalEventTie{seq: e.Reassignment.ItemID, rank: 1, sub: e.Reassignment.ID}
}
func (e ActionItemClosedEvent) tie() goalEventTie {
	return goalEventTie{seq: e.Listing.Item.ID, rank: 2}
}
func (e CommentEvent) tie() goalEventTie { return goalEventTie{seq: e.Listing.Comment.ID} }

// goalEventSources are what the Goal page's loader read that its History
// lists, each as the Service method it is read with returns it.
type goalEventSources struct {
	Checkins           []Checkin
	DateSlips          []DateSlip
	Highlights         []Highlight
	Metrics            []Metric
	Readings           map[int64][]MetricReading
	Revisions          []WhyItMattersRevision
	Titles             []TitleRevision
	Ownership          []Handoff
	Values             []ValueChange
	Suggestions        []ParentSuggestion
	Nudges             []Nudge
	Links              []LinkEvent
	MilestoneAdditions []MilestoneChange
	ActionItems        []ActionItemListing
	ActionReassigns    []ActionItemReassignment
	Comments           []CommentListing
}

// goalEventsOf is the Goal's History, newest first, from src.
func goalEventsOf(src goalEventSources) []GoalEvent {
	slipsBy := map[int64][]DateSlip{}
	for _, s := range src.DateSlips {
		slipsBy[s.CheckinID] = append(slipsBy[s.CheckinID], s)
	}
	// Each Metric in turn, so a Check-in's readings come in Metric order.
	readingsBy := map[int64][]CheckinReading{}
	for _, m := range src.Metrics {
		readings := src.Readings[m.ID]
		for i, r := range readings {
			readingsBy[r.CheckinID] = append(readingsBy[r.CheckinID], CheckinReading{
				Metric:  m,
				Value:   r.Value,
				Changed: i == 0 || r.Value != readings[i-1].Value,
			})
		}
	}
	highlightsBy := map[int64][]Highlight{}
	for _, hl := range src.Highlights {
		highlightsBy[hl.CheckinID] = append(highlightsBy[hl.CheckinID], hl)
	}

	var events []GoalEvent
	for _, c := range src.Checkins {
		events = append(events, CheckinEvent{Checkin: c, Slips: slipsBy[c.ID], Readings: readingsBy[c.ID], Highlights: highlightsBy[c.ID]})
	}
	for _, r := range src.Revisions {
		events = append(events, WhyItMattersRevisedEvent{Revision: r})
	}
	for _, r := range src.Titles {
		events = append(events, TitleChangeEvent{Revision: r})
	}
	for _, o := range src.Ownership {
		events = append(events, OwnershipChangeEvent{Handoff: o})
	}
	for _, c := range src.Values {
		events = append(events, ValueChangeEvent{Change: c})
	}
	for _, p := range src.Suggestions {
		events = append(events, ParentSuggestionEvent{Suggestion: p})
	}
	for _, n := range src.Nudges {
		events = append(events, NudgeEvent{Nudge: n})
	}
	for _, l := range src.Links {
		events = append(events, LinkChangeEvent{Link: l})
	}
	for _, m := range src.MilestoneAdditions {
		events = append(events, MilestoneAddedEvent{Change: m})
	}
	// An Action Item was raised for the owner its first reassignment took it
	// from, since a reassignment changes its owner in place.
	raisedFor := map[int64]Account{}
	for _, r := range src.ActionReassigns {
		if _, ok := raisedFor[r.ItemID]; !ok {
			raisedFor[r.ItemID] = r.From
		}
		events = append(events, ActionItemReassignedEvent{Reassignment: r})
	}
	for _, l := range src.ActionItems {
		owner, ok := raisedFor[l.Item.ID]
		if !ok {
			owner = l.Item.Owner
		}
		events = append(events, ActionItemRaisedEvent{Listing: l, RaisedFor: owner})
		if l.Item.Closed {
			events = append(events, ActionItemClosedEvent{Listing: l})
		}
	}
	for _, c := range src.Comments {
		events = append(events, CommentEvent{Listing: c})
	}
	// Newest first. Events at the same instant keep the order they were
	// recorded in, latest first, but an Action Item's events stay together:
	// newest item first, each closed above its reassignments, latest first,
	// above its raising.
	slices.SortStableFunc(events, func(a, b GoalEvent) int {
		if c := b.At().Compare(a.At()); c != 0 {
			return c
		}
		return b.tie().compare(a.tie())
	})
	return events
}
