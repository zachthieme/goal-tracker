package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Check-in may carry one optional Highlight, marked as an Insight,
// Accomplishment, or Miss; it is queryable by Goal and credits the Owner the
// Check-in was written for (CONTEXT.md: Highlight).
func TestSubmitCheckinRecordsOptionalHighlight(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:    goal.ID,
		AuthorID:  sam.ID,
		Health:    domain.HealthGreen,
		Status:    "Shipped the failover.",
		Highlight: &domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Zero downtime on the cutover."},
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

// A Highlight must be marked with a valid kind and carry a note; a bad kind or
// an empty note is rejected, and the whole Check-in is refused.
func TestHighlightRequiresValidKindAndNote(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	cases := map[string]domain.HighlightInput{
		"bad kind":   {Kind: "Brag", Note: "We shipped it."},
		"empty note": {Kind: domain.HighlightInsight, Note: "   "},
	}
	for name, hl := range cases {
		t.Run(name, func(t *testing.T) {
			hl := hl
			if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
				GoalID:    goal.ID,
				AuthorID:  sam.ID,
				Health:    domain.HealthGreen,
				Status:    "Status.",
				Highlight: &hl,
			}); !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded despite the invalid highlight: %d", len(history))
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
