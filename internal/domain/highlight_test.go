package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Check-in may carry an optional Highlight, marked as an Insight,
// Accomplishment, or Miss; it is queryable by Goal and credits the Owner the
// Check-in was written for (CONTEXT.md: Highlight).
func TestSubmitCheckinRecordsOptionalHighlight(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:     goal.ID,
		AuthorID:   sam.ID,
		Health:     domain.HealthGreen,
		Status:     "Shipped the failover.",
		Highlights: []domain.HighlightInput{{Kind: domain.HighlightAccomplishment, Note: "Zero downtime on the cutover."}},
	}); err != nil {
		t.Fatalf("SubmitCheckin with highlight: %v", err)
	}

	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	if len(highlights) != 1 {
		t.Fatalf("highlights = %d, want 1", len(highlights))
	}
	got := highlights[0]
	if got.Kind != domain.HighlightAccomplishment || got.Note != "Zero downtime on the cutover." {
		t.Errorf("highlight = %+v, want the Accomplishment just recorded", got)
	}
	if got.Owner.ID != sam.ID {
		t.Errorf("highlight credits Owner %d, want %d", got.Owner.ID, sam.ID)
	}
}

// A Check-in carries any number of Highlights, of any mix of kinds; each is
// recorded, and a Goal's Highlights from one Check-in list in the order they
// were entered (CONTEXT.md: Highlight).
func TestSubmitCheckinRecordsSeveralHighlightsInOrder(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Shipped the failover.",
		Highlights: []domain.HighlightInput{
			{Kind: domain.HighlightAccomplishment, Note: "Zero downtime on the cutover."},
			{Kind: domain.HighlightMiss, Note: "The runbook was a week late."},
			{Kind: domain.HighlightInsight, Note: "Drills find what reviews miss."},
		},
	}); err != nil {
		t.Fatalf("SubmitCheckin with highlights: %v", err)
	}

	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	want := []domain.HighlightInput{
		{Kind: domain.HighlightAccomplishment, Note: "Zero downtime on the cutover."},
		{Kind: domain.HighlightMiss, Note: "The runbook was a week late."},
		{Kind: domain.HighlightInsight, Note: "Drills find what reviews miss."},
	}
	if len(highlights) != len(want) {
		t.Fatalf("highlights = %d, want %d", len(highlights), len(want))
	}
	for i, w := range want {
		got := highlights[i]
		if got.Kind != w.Kind || got.Note != w.Note {
			t.Errorf("highlight %d = %s %q, want %s %q", i+1, got.Kind, got.Note, w.Kind, w.Note)
		}
		if got.Owner.ID != sam.ID {
			t.Errorf("highlight %d credits Owner %d, want %d", i+1, got.Owner.ID, sam.ID)
		}
	}
}

// A Highlight is optional: a Check-in without one records none.
func TestSubmitCheckinWithoutHighlightRecordsNone(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Nothing to flag.",
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	if len(highlights) != 0 {
		t.Errorf("highlights = %d, want 0 when none was flagged", len(highlights))
	}
}

// A Highlight with a note must be marked with a valid kind: a note with no
// kind, or a kind that isn't one, is refused with an error naming the row, and
// the whole Check-in is refused.
func TestHighlightWithNoteNeedsAKindNamingTheRow(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	cases := map[string]string{"no kind": "", "bad kind": "Brag"}
	for name, kind := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
				GoalID:   goal.ID,
				AuthorID: sam.ID,
				Health:   domain.HealthGreen,
				Status:   "Status.",
				Highlights: []domain.HighlightInput{
					{Kind: domain.HighlightInsight, Note: "Retries masked the root cause."},
					{Kind: kind, Note: "We shipped it."},
				},
			})
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if !strings.Contains(err.Error(), "Highlight 2") {
				t.Errorf("err = %q, want it to name Highlight 2", err)
			}
		})
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded despite the invalid highlight: %d", len(history))
	}
	if highlights, _ := h.Service.ListHighlightsByGoal(context.Background(), goal.ID); len(highlights) != 0 {
		t.Errorf("highlights = %d recorded despite the invalid one, want 0", len(highlights))
	}
}

// A Highlight row with a blank note is ignored, whatever kind it is marked as;
// the rest of the Check-in's Highlights are recorded.
func TestHighlightWithBlankNoteIsIgnored(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Status.",
		Highlights: []domain.HighlightInput{
			{Kind: domain.HighlightMiss, Note: "   "},
			{Kind: domain.HighlightInsight, Note: "Retries masked the root cause."},
			{},
		},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	if len(highlights) != 1 || highlights[0].Note != "Retries masked the root cause." {
		t.Errorf("highlights = %+v, want only the Insight with a note", highlights)
	}
}

// Kinds can repeat: two Highlights of the same kind on one Check-in are both
// recorded.
func TestCheckinRecordsTwoHighlightsOfTheSameKind(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	h.CheckinWithHighlights(sam, goal.ID,
		domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Cut MTTR in half."},
		domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Retired the old pager."},
	)

	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	if len(highlights) != 2 || highlights[0].Note != "Cut MTTR in half." || highlights[1].Note != "Retired the old pager." {
		t.Errorf("highlights = %+v, want both Accomplishments in order", highlights)
	}
}

// Highlights can be queried by Goal and by time range, which Report curation
// needs (CONTEXT.md: Report). The range is inclusive of its endpoints.
func TestHighlightsQueriedByGoalAndTimeRange(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	// Three Highlights a week apart.
	start := h.Clock.Now()
	h.CheckinWithHighlight(sam, goal.ID, domain.HighlightMiss, "Missed the SLA.")
	h.Clock.Advance(7 * 24 * time.Hour)
	mid := h.Clock.Now()
	h.CheckinWithHighlight(sam, goal.ID, domain.HighlightInsight, "Retries masked the root cause.")
	h.Clock.Advance(7 * 24 * time.Hour)
	last := h.Clock.Now()
	h.CheckinWithHighlight(sam, goal.ID, domain.HighlightAccomplishment, "Cut MTTR in half.")

	all, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("by Goal = %d, want 3", len(all))
	}

	// A window covering only the middle Highlight.
	window, err := h.Service.ListHighlightsByGoalInRange(context.Background(), goal.ID, mid, last.Add(-time.Second))
	if err != nil {
		t.Fatalf("ListHighlightsByGoalInRange: %v", err)
	}
	if len(window) != 1 || window[0].Note != "Retries masked the root cause." {
		t.Fatalf("in range = %+v, want only the middle Highlight", window)
	}

	// The full range returns all three.
	full, err := h.Service.ListHighlightsByGoalInRange(context.Background(), goal.ID, start, last)
	if err != nil {
		t.Fatalf("ListHighlightsByGoalInRange full: %v", err)
	}
	if len(full) != 3 {
		t.Errorf("full range = %d, want 3", len(full))
	}
}

// A "No change" Check-in repeats the previous Health and status but carries no
// Highlights, even when the Check-in it repeats carried several.
func TestNoChangeCheckinCarriesNoHighlights(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.CheckinWithHighlights(sam, goal.ID,
		domain.HighlightInput{Kind: domain.HighlightInsight, Note: "Retries masked the root cause."},
		domain.HighlightInput{Kind: domain.HighlightMiss, Note: "Missed the SLA."},
	)
	h.Clock.Advance(24 * time.Hour)

	if _, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID); err != nil {
		t.Fatalf("SubmitNoChangeCheckin: %v", err)
	}
	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	if len(highlights) != 2 {
		t.Errorf("highlights = %d, want the 2 from the first Check-in only", len(highlights))
	}
}
