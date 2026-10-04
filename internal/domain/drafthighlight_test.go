package domain_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Owner and a Delegate each log a Draft Highlight on a Goal between
// Check-ins, with or without a kind; each records who logged it and when, and
// the Goal's pending Draft Highlights list them oldest first (CONTEXT.md:
// Draft Highlight).
func TestLogDraftHighlightByOwnerAndDelegate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)

	first, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{
		GoalID: goal.ID, AuthorID: sam.ID, Kind: domain.HighlightAccomplishment, Note: "  Cut paging noise by half.  ",
	})
	if err != nil {
		t.Fatalf("LogDraftHighlight by the Owner: %v", err)
	}
	if first.Kind != domain.HighlightAccomplishment || first.Note != "Cut paging noise by half." {
		t.Errorf("logged %q: %q, want Accomplishment: the trimmed note", first.Kind, first.Note)
	}
	if first.LoggedBy.ID != sam.ID || !first.CreatedAt.Equal(testsupport.Epoch) {
		t.Errorf("logged by %d at %v, want %d at %v", first.LoggedBy.ID, first.CreatedAt, sam.ID, testsupport.Epoch)
	}

	h.Clock.Advance(time.Hour)
	second, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{
		GoalID: goal.ID, AuthorID: dee.ID, Note: "Vendor may raise prices.",
	})
	if err != nil {
		t.Fatalf("LogDraftHighlight by a Delegate: %v", err)
	}
	if second.Kind != "" || second.LoggedBy.ID != dee.ID {
		t.Errorf("logged kind %q by %d, want no kind by the Delegate %d", second.Kind, second.LoggedBy.ID, dee.ID)
	}

	pending, err := h.Service.PendingDraftHighlights(ctx, sam.ID, goal.ID)
	if err != nil {
		t.Fatalf("PendingDraftHighlights: %v", err)
	}
	if got := draftNotes(pending); !slices.Equal(got, []string{"Cut paging noise by half.", "Vendor may raise prices."}) {
		t.Errorf("pending = %q, want both, oldest first", got)
	}
}

// draftNotes are the Draft Highlights' notes, in order.
func draftNotes(drafts []domain.DraftHighlight) []string {
	var out []string
	for _, d := range drafts {
		out = append(out, d.Note)
	}
	return out
}

// A Draft Highlight needs a note, and its kind, if any, must be an Insight,
// Accomplishment or Miss.
func TestLogDraftHighlightRefusesBlankNoteAndUnknownKind(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	for _, in := range []domain.LogDraftHighlightInput{
		{GoalID: goal.ID, AuthorID: sam.ID, Kind: domain.HighlightInsight, Note: "   "},
		{GoalID: goal.ID, AuthorID: sam.ID, Kind: "Win", Note: "Shipped."},
	} {
		if _, err := h.Service.LogDraftHighlight(ctx, in); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("LogDraftHighlight(%q, %q): err = %v, want ErrValidation", in.Kind, in.Note, err)
		}
	}
	if pending, _ := h.Service.PendingDraftHighlights(ctx, sam.ID, goal.ID); len(pending) != 0 {
		t.Errorf("pending = %d, want none logged", len(pending))
	}
}

// Nobody but the Goal's Owner and Delegates may log, see or delete its Draft
// Highlights: not a Contributor, not an Admin, not a Delegate who has left the
// org.
func TestDraftHighlightsRefuseAnyoneElse(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	ada := h.SignIn("ada@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)
	draft := h.LogDraftHighlight(dee, goal.ID, domain.HighlightInsight, "Retries mask the root cause.")
	if err := h.Service.MarkDeparted(ctx, ada.ID, dee.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	for _, who := range []domain.Account{ada, pat, dee} {
		if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{GoalID: goal.ID, AuthorID: who.ID, Note: "Mine."}); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s logging: err = %v, want ErrNotAuthorized", who.Email, err)
		}
		if _, err := h.Service.PendingDraftHighlights(ctx, who.ID, goal.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s seeing: err = %v, want ErrNotAuthorized", who.Email, err)
		}
		if err := h.Service.DeleteDraftHighlight(ctx, who.ID, draft.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s deleting: err = %v, want ErrNotAuthorized", who.Email, err)
		}
	}
	if pending, _ := h.Service.PendingDraftHighlights(ctx, sam.ID, goal.ID); !slices.Equal(draftNotes(pending), []string{"Retries mask the root cause."}) {
		t.Errorf("pending = %q, want only the Delegate's, untouched", draftNotes(pending))
	}
}

// A Draft Highlight may be logged on a Proposed, Active or On Hold Goal, but
// not on one that is Done or Cancelled: it takes no more Check-ins.
func TestLogDraftHighlightOnlyWhileTheGoalTakesCheckins(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	proposed := h.CreateGoal(sam, "Proposed", "Why.")
	onHold := h.OnHoldGoal(sam, "On Hold", "Why.", "Waiting on budget.")
	done := h.ActiveGoal(sam, "Done", "Why.")
	h.EndGoal(sam, done.ID, domain.LifecycleDone)
	cancelled := h.ActiveGoal(sam, "Cancelled", "Why.")
	h.EndGoal(sam, cancelled.ID, domain.LifecycleCancelled)

	for _, g := range []domain.Goal{proposed, onHold} {
		if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{GoalID: g.ID, AuthorID: sam.ID, Note: "Noted."}); err != nil {
			t.Errorf("logging on %s: %v", g.Title, err)
		}
	}
	for _, g := range []domain.Goal{done, cancelled} {
		if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{GoalID: g.ID, AuthorID: sam.ID, Note: "Noted."}); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("logging on %s: err = %v, want ErrValidation", g.Title, err)
		}
	}
}

// A pending Draft Highlight deleted by the Owner or a Delegate is gone: no
// longer pending, and the next Check-in neither keeps nor discards it.
func TestDeleteDraftHighlightLeavesNoTrace(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)
	draft := h.LogDraftHighlight(sam, goal.ID, domain.HighlightMiss, "Runbook was late.")

	if err := h.Service.DeleteDraftHighlight(ctx, dee.ID, draft.ID); err != nil {
		t.Fatalf("DeleteDraftHighlight by a Delegate: %v", err)
	}
	if pending, _ := h.Service.PendingDraftHighlights(ctx, sam.ID, goal.ID); len(pending) != 0 {
		t.Errorf("pending = %q, want none after the delete", draftNotes(pending))
	}
	if err := h.Service.DeleteDraftHighlight(ctx, sam.ID, draft.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleting it again: err = %v, want ErrNotFound", err)
	}

	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	checkins, err := h.Service.ListCheckins(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	for _, c := range checkins {
		if len(c.DiscardedDraftHighlights) != 0 {
			t.Errorf("Check-in %d discarded %q, want nothing", c.ID, draftNotes(c.DiscardedDraftHighlights))
		}
	}
}
