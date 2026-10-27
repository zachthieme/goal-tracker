package domain

import (
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"
)

// The Goal's History as goalEventsOf builds it from what the Goal page's
// loader read, with no database: each case names the events it lists, in
// order, as "kind id".

var eventAt = time.Date(2026, time.March, 2, 9, 0, 0, 0, time.UTC)

// eventNames names each event as "kind id", in order: an Action Item's
// events by the item's ID, a reassignment's as "reassigned item/id".
func eventNames(events []GoalEvent) []string {
	var out []string
	for _, e := range events {
		switch e := e.(type) {
		case CheckinEvent:
			out = append(out, fmt.Sprintf("checkin %d", e.Checkin.ID))
		case WhyItMattersRevisedEvent:
			out = append(out, fmt.Sprintf("why-it-matters %d", e.Revision.ID))
		case TitleChangeEvent:
			out = append(out, fmt.Sprintf("title %d", e.Revision.ID))
		case OwnershipChangeEvent:
			out = append(out, fmt.Sprintf("ownership %d", e.Handoff.ID))
		case ValueChangeEvent:
			out = append(out, fmt.Sprintf("value %d", e.Change.ID))
		case ParentSuggestionEvent:
			out = append(out, fmt.Sprintf("suggestion %d", e.Suggestion.ID))
		case NudgeEvent:
			out = append(out, fmt.Sprintf("nudge %d", e.Nudge.ID))
		case LinkChangeEvent:
			out = append(out, fmt.Sprintf("link %d", e.Link.ID))
		case MilestoneAddedEvent:
			out = append(out, fmt.Sprintf("milestone %d", e.Change.ID))
		case ActionItemRaisedEvent:
			out = append(out, fmt.Sprintf("raised %d", e.Listing.Item.ID))
		case ActionItemReassignedEvent:
			out = append(out, fmt.Sprintf("reassigned %d/%d", e.Reassignment.ItemID, e.Reassignment.ID))
		case ActionItemClosedEvent:
			out = append(out, fmt.Sprintf("closed %d", e.Listing.Item.ID))
		case CommentEvent:
			out = append(out, fmt.Sprintf("comment %d", e.Listing.Comment.ID))
		default:
			out = append(out, fmt.Sprintf("unknown %T", e))
		}
	}
	return out
}

func checkEventNames(t *testing.T, got []GoalEvent, want []string) {
	t.Helper()
	if names := eventNames(got); !slices.Equal(names, want) {
		t.Errorf("History = %q, want %q", names, want)
	}
}

func TestGoalEventsListNewestFirstAcrossKinds(t *testing.T) {
	t.Parallel()
	at := func(minutes int) time.Time { return eventAt.Add(time.Duration(minutes) * time.Minute) }

	got := goalEventsOf(goalEventSources{
		Checkins:  []Checkin{{ID: 1, CreatedAt: at(10)}, {ID: 2, CreatedAt: at(1)}},
		Revisions: []WhyItMattersRevision{{ID: 1, CreatedAt: at(2)}},
		Titles:    []TitleRevision{{ID: 1, CreatedAt: at(3)}},
		Ownership: []Handoff{{ID: 1, CreatedAt: at(4)}},
		Values:    []ValueChange{{ID: 1, CreatedAt: at(5)}},
		Suggestions: []ParentSuggestion{
			{ID: 1, CreatedAt: at(6)},
		},
		Nudges:             []Nudge{{ID: 1, CreatedAt: at(7)}},
		Links:              []LinkEvent{{ID: 1, CreatedAt: at(8)}},
		MilestoneAdditions: []MilestoneChange{{ID: 1, CreatedAt: at(9)}},
		ActionItems: []ActionItemListing{
			{Item: ActionItem{ID: 1, CreatedAt: at(11), Closed: true, ClosedAt: at(13)}},
		},
		ActionReassigns: []ActionItemReassignment{{ID: 1, ItemID: 1, At: at(12)}},
		Comments:        []CommentListing{{Comment: Comment{ID: 1, CreatedAt: at(14)}}},
	})

	checkEventNames(t, got, []string{
		"comment 1", "closed 1", "reassigned 1/1", "raised 1", "checkin 1",
		"milestone 1", "link 1", "nudge 1", "suggestion 1", "value 1",
		"ownership 1", "title 1", "why-it-matters 1", "checkin 2",
	})
}

func TestGoalEventsAtTheSameInstantListTheHigherIDFirst(t *testing.T) {
	t.Parallel()

	got := goalEventsOf(goalEventSources{
		Checkins: []Checkin{{ID: 4, CreatedAt: eventAt}, {ID: 5, CreatedAt: eventAt}},
		Nudges:   []Nudge{{ID: 7, CreatedAt: eventAt}, {ID: 8, CreatedAt: eventAt}},
	})

	checkEventNames(t, got, []string{"nudge 8", "nudge 7", "checkin 5", "checkin 4"})
}

// Records of different kinds at the same instant are ordered by their IDs,
// though each kind numbers its own; when the IDs are the same too, they keep
// the History's kind order: Check-ins, Why it matters revisions, title
// changes, ownership changes, value changes, Parent suggestions, Nudges, link
// changes, Milestone additions, Action Item reassignments, Action Items,
// Comments.
func TestGoalEventsOfDifferentKindsAtTheSameInstant(t *testing.T) {
	t.Parallel()

	t.Run("by their IDs", func(t *testing.T) {
		t.Parallel()
		got := goalEventsOf(goalEventSources{
			Checkins: []Checkin{{ID: 2, CreatedAt: eventAt}},
			Titles:   []TitleRevision{{ID: 1, CreatedAt: eventAt}},
			Comments: []CommentListing{{Comment: Comment{ID: 3, CreatedAt: eventAt}}},
		})
		checkEventNames(t, got, []string{"comment 3", "checkin 2", "title 1"})
	})

	t.Run("with the same ID, in kind order", func(t *testing.T) {
		t.Parallel()
		got := goalEventsOf(goalEventSources{
			Comments:           []CommentListing{{Comment: Comment{ID: 1, CreatedAt: eventAt}}},
			ActionItems:        []ActionItemListing{{Item: ActionItem{ID: 1, CreatedAt: eventAt}}},
			MilestoneAdditions: []MilestoneChange{{ID: 1, CreatedAt: eventAt}},
			Links:              []LinkEvent{{ID: 1, CreatedAt: eventAt}},
			Nudges:             []Nudge{{ID: 1, CreatedAt: eventAt}},
			Suggestions:        []ParentSuggestion{{ID: 1, CreatedAt: eventAt}},
			Values:             []ValueChange{{ID: 1, CreatedAt: eventAt}},
			Ownership:          []Handoff{{ID: 1, CreatedAt: eventAt}},
			Titles:             []TitleRevision{{ID: 1, CreatedAt: eventAt}},
			Revisions:          []WhyItMattersRevision{{ID: 1, CreatedAt: eventAt}},
			Checkins:           []Checkin{{ID: 1, CreatedAt: eventAt}},
		})
		checkEventNames(t, got, []string{
			"checkin 1", "why-it-matters 1", "title 1", "ownership 1", "value 1",
			"suggestion 1", "nudge 1", "link 1", "milestone 1", "raised 1", "comment 1",
		})
	})
}

// An Action Item's events stay together at the same instant: closed above its
// reassignments, latest first, above its raising, the newer item's first.
func TestGoalEventsKeepAnActionItemsEventsTogether(t *testing.T) {
	t.Parallel()

	t.Run("one item", func(t *testing.T) {
		t.Parallel()
		got := goalEventsOf(goalEventSources{
			ActionItems: []ActionItemListing{{Item: ActionItem{ID: 3, CreatedAt: eventAt, Closed: true, ClosedAt: eventAt}}},
			ActionReassigns: []ActionItemReassignment{
				{ID: 1, ItemID: 3, At: eventAt},
				{ID: 2, ItemID: 3, At: eventAt},
			},
		})
		checkEventNames(t, got, []string{"closed 3", "reassigned 3/2", "reassigned 3/1", "raised 3"})
	})

	t.Run("two items", func(t *testing.T) {
		t.Parallel()
		got := goalEventsOf(goalEventSources{
			ActionItems: []ActionItemListing{
				{Item: ActionItem{ID: 1, CreatedAt: eventAt, Closed: true, ClosedAt: eventAt}},
				{Item: ActionItem{ID: 2, CreatedAt: eventAt, Closed: true, ClosedAt: eventAt}},
			},
			ActionReassigns: []ActionItemReassignment{
				{ID: 1, ItemID: 2, At: eventAt},
				{ID: 2, ItemID: 1, At: eventAt},
			},
		})
		checkEventNames(t, got, []string{
			"closed 2", "reassigned 2/1", "raised 2",
			"closed 1", "reassigned 1/2", "raised 1",
		})
	})
}

// An Action Item was raised for the owner its first reassignment took it
// from, since a reassignment changes its owner in place; one never reassigned
// was raised for its owner.
func TestGoalEventsRaiseAnActionItemForItsFirstOwner(t *testing.T) {
	t.Parallel()
	ana := Account{ID: 1, Name: "Ana"}
	bo := Account{ID: 2, Name: "Bo"}
	cy := Account{ID: 3, Name: "Cy"}

	got := goalEventsOf(goalEventSources{
		ActionItems: []ActionItemListing{
			{Item: ActionItem{ID: 1, Owner: cy, CreatedAt: eventAt}},
			{Item: ActionItem{ID: 2, Owner: bo, CreatedAt: eventAt}},
		},
		ActionReassigns: []ActionItemReassignment{
			{ID: 1, ItemID: 1, From: ana, To: bo, At: eventAt.Add(time.Hour)},
			{ID: 2, ItemID: 1, From: bo, To: cy, At: eventAt.Add(2 * time.Hour)},
		},
	})

	raisedFor := map[int64]string{}
	for _, e := range got {
		if r, ok := e.(ActionItemRaisedEvent); ok {
			raisedFor[r.Listing.Item.ID] = r.RaisedFor.Name
		}
	}
	if want := map[int64]string{1: "Ana", 2: "Bo"}; !maps.Equal(raisedFor, want) {
		t.Errorf("raised for = %v, want %v", raisedFor, want)
	}
}

// readingLine is a Check-in's reading as "Metric value", with " changed" when
// it changed.
func readingLine(r CheckinReading) string {
	line := fmt.Sprintf("%s %g", r.Metric.Name, r.Value)
	if r.Changed {
		line += " changed"
	}
	return line
}

// A Check-in event carries the Date Slips, Metric readings and Highlights
// recorded in its Check-in and no other: its readings in the order of the
// Goal's Metrics, each Changed when it is the Metric's first or differs from
// the one before it.
func TestGoalEventsBundleWhatEachCheckinRecorded(t *testing.T) {
	t.Parallel()
	uptime := Metric{ID: 20, Name: "Uptime"}
	tickets := Metric{ID: 10, Name: "Tickets"}

	got := goalEventsOf(goalEventSources{
		Checkins: []Checkin{
			{ID: 3, CreatedAt: eventAt.Add(2 * time.Hour)},
			{ID: 2, CreatedAt: eventAt.Add(time.Hour)},
			{ID: 1, CreatedAt: eventAt},
		},
		DateSlips:  []DateSlip{{ID: 1, CheckinID: 2, Reason: "vendor late"}},
		Highlights: []Highlight{{ID: 1, CheckinID: 3, Note: "shipped"}, {ID: 2, CheckinID: 3, Note: "hired"}},
		Metrics:    []Metric{uptime, tickets},
		Readings: map[int64][]MetricReading{
			tickets.ID: {{CheckinID: 1, MetricID: tickets.ID, Value: 1}, {CheckinID: 3, MetricID: tickets.ID, Value: 2}},
			uptime.ID: {
				{CheckinID: 1, MetricID: uptime.ID, Value: 5},
				{CheckinID: 2, MetricID: uptime.ID, Value: 5},
				{CheckinID: 3, MetricID: uptime.ID, Value: 6},
			},
		},
	})

	type recorded struct{ slips, readings, highlights []string }
	want := map[int64]recorded{
		3: {readings: []string{"Uptime 6 changed", "Tickets 2 changed"}, highlights: []string{"shipped", "hired"}},
		2: {slips: []string{"vendor late"}, readings: []string{"Uptime 5"}},
		1: {readings: []string{"Uptime 5 changed", "Tickets 1 changed"}},
	}
	for _, e := range got {
		c := e.(CheckinEvent)
		var slips, readings, highlights []string
		for _, s := range c.Slips {
			slips = append(slips, s.Reason)
		}
		for _, r := range c.Readings {
			readings = append(readings, readingLine(r))
		}
		for _, hl := range c.Highlights {
			highlights = append(highlights, hl.Note)
		}
		w := want[c.Checkin.ID]
		if !slices.Equal(slips, w.slips) || !slices.Equal(readings, w.readings) || !slices.Equal(highlights, w.highlights) {
			t.Errorf("Check-in %d: slips %q, readings %q, highlights %q; want %q, %q, %q",
				c.Checkin.ID, slips, readings, highlights, w.slips, w.readings, w.highlights)
		}
	}
	checkEventNames(t, got, []string{"checkin 3", "checkin 2", "checkin 1"})
}

// A Check-in repeats the one it's compared with when it says the same and
// records nothing more of its own; anything it records, or anything it says
// differently, stops the repeat.
func TestCheckinEventRepeats(t *testing.T) {
	t.Parallel()
	target := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	prev := CheckinEvent{Checkin: Checkin{
		ID: 1, Health: HealthYellow, Status: "On it", PathToGreen: "Hire", PathTargetDate: target,
		AsksForHelp: true, Explanation: "Children lag",
	}}
	// same is a Check-in saying what prev says, changed by change.
	same := func(change func(*CheckinEvent)) CheckinEvent {
		e := CheckinEvent{Checkin: prev.Checkin}
		e.Checkin.ID = 2
		e.Readings = []CheckinReading{{Metric: Metric{ID: 1, Name: "Uptime"}, Value: 5}}
		if change != nil {
			change(&e)
		}
		return e
	}

	cases := []struct {
		name    string
		e       CheckinEvent
		repeats bool
	}{
		{"saying the same with an unchanged reading", same(nil), true},
		{"another Health", same(func(e *CheckinEvent) { e.Checkin.Health = HealthRed }), false},
		{"another status", same(func(e *CheckinEvent) { e.Checkin.Status = "Still on it" }), false},
		{"another Path to Green", same(func(e *CheckinEvent) { e.Checkin.PathToGreen = "Hire two" }), false},
		{"another Path to Green target date", same(func(e *CheckinEvent) { e.Checkin.PathTargetDate = target.AddDate(0, 0, 7) }), false},
		{"no longer asking for help", same(func(e *CheckinEvent) { e.Checkin.AsksForHelp = false }), false},
		{"another explanation", same(func(e *CheckinEvent) { e.Checkin.Explanation = "" }), false},
		{"a Date Slip", same(func(e *CheckinEvent) { e.Slips = []DateSlip{{ID: 1, CheckinID: 2}} }), false},
		{"a Milestone change", same(func(e *CheckinEvent) {
			e.Checkin.MilestoneChanges = []MilestoneChange{{ID: 1, Kind: MilestoneChangeDone}}
		}), false},
		{"a Lifecycle change", same(func(e *CheckinEvent) {
			e.Checkin.LifecycleChange = LifecycleChange{From: LifecycleActive, To: LifecycleOnHold}
		}), false},
		{"a Highlight", same(func(e *CheckinEvent) { e.Highlights = []Highlight{{ID: 1, CheckinID: 2}} }), false},
		{"a discarded Draft Highlight", same(func(e *CheckinEvent) {
			e.Checkin.DiscardedDraftHighlights = []DraftHighlight{{ID: 1}}
		}), false},
		{"a reading that changed", same(func(e *CheckinEvent) { e.Readings[0].Changed = true }), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.e.Repeats(prev); got != c.repeats {
				t.Errorf("Repeats = %v, want %v", got, c.repeats)
			}
		})
	}

	// What the Check-in compared with records doesn't stop a repeat.
	recording := prev
	recording.Slips = []DateSlip{{ID: 1, CheckinID: 1}}
	recording.Highlights = []Highlight{{ID: 1, CheckinID: 1}}
	if !same(nil).Repeats(recording) {
		t.Error("Repeats = false against a Check-in that recorded a Date Slip and a Highlight, want true")
	}
}
