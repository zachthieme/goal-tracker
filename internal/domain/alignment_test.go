package domain_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Admin marks a Goal as one of the org's root outcomes, and can unmark it
// again (CONTEXT.md: Top-level Goal).
func TestAdminMarksAndUnmarksTopLevelGoal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")

	marked, err := h.Service.MarkTopLevel(ctx, ada.ID, g.ID)
	if err != nil {
		t.Fatalf("MarkTopLevel: %v", err)
	}
	if !marked.TopLevel {
		t.Errorf("TopLevel = false after marking, want true")
	}
	if viewed, _ := h.Service.ViewGoal(ctx, g.ID); !viewed.TopLevel {
		t.Errorf("viewed Goal TopLevel = false after marking, want true")
	}

	unmarked, err := h.Service.UnmarkTopLevel(ctx, ada.ID, g.ID)
	if err != nil {
		t.Fatalf("UnmarkTopLevel: %v", err)
	}
	if unmarked.TopLevel {
		t.Errorf("TopLevel = true after unmarking, want false")
	}
}

// Only an Admin decides which Goals are the org's root outcomes; the Goal's own
// Owner may not mark or unmark it.
func TestOnlyAdminMarksTopLevelGoal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")

	if _, err := h.Service.MarkTopLevel(ctx, sam.ID, g.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Owner MarkTopLevel err = %v, want ErrNotAuthorized", err)
	}
	h.MarkTopLevel(ada, g)
	if _, err := h.Service.UnmarkTopLevel(ctx, sam.ID, g.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Owner UnmarkTopLevel err = %v, want ErrNotAuthorized", err)
	}
	if viewed, _ := h.Service.ViewGoal(ctx, g.ID); !viewed.TopLevel {
		t.Errorf("TopLevel = false after a refused unmark, want it still marked")
	}
}

// Marking a Goal that does not exist is reported as not found.
func TestMarkTopLevelUnknownGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")

	if _, err := h.Service.MarkTopLevel(context.Background(), ada.ID, 999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("MarkTopLevel(unknown) err = %v, want ErrNotFound", err)
	}
}

// The Unaligned list holds the Active Goals that contribute to no other Goal and
// aren't Top-level (CONTEXT.md: Unaligned). A Goal whose only parent link is
// still pending is Unaligned: the link exists only once accepted.
func TestUnalignedListsActiveGoalsWithNoAcceptedParent(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")

	root := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Org outcome", "It matters."))
	h.ActiveChildOf(sam, root, "Aligned work", "Feeds the outcome.")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")
	pending := h.ActiveGoal(pat, "Awaiting acceptance", "Asked to join.")
	h.RequestLink(pat, pending, root, "") // sam hasn't accepted yet
	h.CreateGoal(sam, "Still proposed", "Not Active yet.")
	h.OnHoldGoal(sam, "Paused", "Not Active now.", "Waiting on budget.")

	got := h.Unaligned()

	want := map[int64]string{loner.ID: loner.Title, pending.ID: pending.Title}
	if len(got) != len(want) {
		t.Fatalf("Unaligned = %v, want exactly %v", titles(got), want)
	}
	for _, g := range got {
		if _, ok := want[g.ID]; !ok {
			t.Errorf("Unaligned includes %q, which should not be listed", g.Title)
		}
	}
}

// Unmarking a Top-level Goal that contributes to nothing makes it Unaligned
// again; marking it takes it back off the list.
func TestTopLevelMarkTakesGoalOffUnalignedList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")

	if got := h.Unaligned(); len(got) != 1 || got[0].ID != g.ID {
		t.Fatalf("Unaligned before marking = %v, want [%q]", titles(got), g.Title)
	}
	h.MarkTopLevel(ada, g)
	if got := h.Unaligned(); len(got) != 0 {
		t.Errorf("Unaligned after marking Top-level = %v, want none", titles(got))
	}
	if _, err := h.Service.UnmarkTopLevel(ctx, ada.ID, g.ID); err != nil {
		t.Fatalf("UnmarkTopLevel: %v", err)
	}
	if got := h.Unaligned(); len(got) != 1 || got[0].ID != g.ID {
		t.Errorf("Unaligned after unmarking = %v, want [%q]", titles(got), g.Title)
	}
}

// A child whose delivery date is later than its parent's is a schedule conflict
// nobody reported: the org-wide signals list the pair, and both the child and
// the parent are flagged on their own. An earlier or equal date is fine, as is
// an Ongoing child with no delivery date.
func TestChildDeliveringAfterParentFlagsBoth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	parent := h.ActiveGoalDue(sam, "Launch", june)
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0))
	h.RequestLink(sam, late, parent, "")
	early := h.ActiveGoalDue(sam, "Early piece", june.AddDate(0, -1, 0))
	h.RequestLink(sam, early, parent, "")
	onTime := h.ActiveGoalDue(sam, "On-time piece", june)
	h.RequestLink(sam, onTime, parent, "")
	ongoing := h.CreateGoal(sam, "Ongoing piece", "Always on.")
	if _, err := h.Service.MarkGoalOngoing(context.Background(), ongoing.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	h.RequestLink(sam, ongoing, parent, "")

	want := []string{"Late piece -> Launch"}
	if got := conflictPairs(h.GraphSignals().ScheduleConflicts); !equalStrings(got, want) {
		t.Errorf("org-wide ScheduleConflicts = %v, want %v", got, want)
	}
	if got := conflictPairs(h.GoalSignals(late).ScheduleConflicts); !equalStrings(got, want) {
		t.Errorf("child's ScheduleConflicts = %v, want %v", got, want)
	}
	if got := conflictPairs(h.GoalSignals(parent).ScheduleConflicts); !equalStrings(got, want) {
		t.Errorf("parent's ScheduleConflicts = %v, want %v", got, want)
	}
	if got := h.GoalSignals(early).ScheduleConflicts; len(got) != 0 {
		t.Errorf("early child's ScheduleConflicts = %v, want none", conflictPairs(got))
	}
}

// A schedule conflict appears when a Date Slip moves the child past its parent,
// since it is read from the graph's current dates rather than reported.
func TestDateSlipPastParentRaisesScheduleConflict(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	parent := h.ActiveGoalDue(sam, "Launch", june)
	child := h.ActiveGoalDue(sam, "Piece", june.AddDate(0, -1, 0))
	h.RequestLink(sam, child, parent, "")
	if got := h.GraphSignals().ScheduleConflicts; len(got) != 0 {
		t.Fatalf("ScheduleConflicts before the slip = %v, want none", conflictPairs(got))
	}

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             child.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthYellow,
		Status:             "Slipping.",
		PathToGreen:        "Add a second engineer.",
		PathTargetDate:     june,
		DeliveryDate:       june.AddDate(0, 2, 0),
		DeliveryDateReason: "Vendor delay.",
	}); err != nil {
		t.Fatalf("SubmitCheckin with a Date Slip: %v", err)
	}

	want := []string{"Piece -> Launch"}
	if got := conflictPairs(h.GraphSignals().ScheduleConflicts); !equalStrings(got, want) {
		t.Errorf("ScheduleConflicts after the slip = %v, want %v", got, want)
	}
}

// A Goal that is Cancelled no longer puts its partner's date at risk, so a
// Cancelled late child raises no schedule conflict.
func TestCancelledChildRaisesNoScheduleConflict(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	parent := h.ActiveGoalDue(sam, "Launch", june)
	child := h.ActiveGoalDue(sam, "Piece", june.AddDate(0, 1, 0))
	h.RequestLink(sam, child, parent, "")

	changeLifecycle(t, h, sam, child, domain.LifecycleCancelled)

	if got := h.GraphSignals().ScheduleConflicts; len(got) != 0 {
		t.Errorf("ScheduleConflicts = %v, want none for a Cancelled child", conflictPairs(got))
	}
}

// When a parent goes On Hold or is Cancelled, the Goals contributing to it are
// flagged: their work may no longer be needed. Children of an Active parent
// aren't, nor is a child that is itself Cancelled.
func TestChildrenFlaggedWhenParentOnHoldOrCancelled(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")

	paused := h.ActiveGoal(sam, "Paused outcome", "It mattered.")
	pausedChild := h.ActiveChildOf(sam, paused, "Under paused", "Feeds paused.")
	cancelledKid := h.ActiveChildOf(sam, paused, "Already cancelled", "Stopped too.")
	dropped := h.ActiveGoal(sam, "Dropped outcome", "It mattered once.")
	droppedChild := h.ActiveChildOf(sam, dropped, "Under dropped", "Feeds dropped.")
	running := h.ActiveGoal(sam, "Running outcome", "Still matters.")
	runningChild := h.ActiveChildOf(sam, running, "Under running", "Feeds running.")

	if got := h.GraphSignals().HaltedParents; len(got) != 0 {
		t.Fatalf("HaltedParents with every parent Active = %v, want none", haltedPairs(got))
	}

	changeLifecycle(t, h, sam, cancelledKid, domain.LifecycleCancelled)
	changeLifecycle(t, h, sam, paused, domain.LifecycleOnHold)
	changeLifecycle(t, h, sam, dropped, domain.LifecycleCancelled)

	want := []string{"Under paused -> Paused outcome", "Under dropped -> Dropped outcome"}
	if got := haltedPairs(h.GraphSignals().HaltedParents); !equalStrings(got, want) {
		t.Errorf("org-wide HaltedParents = %v, want %v", got, want)
	}
	if got := haltedPairs(h.GoalSignals(pausedChild).HaltedParents); !equalStrings(got, want[:1]) {
		t.Errorf("paused parent's child HaltedParents = %v, want %v", got, want[:1])
	}
	if got := haltedPairs(h.GoalSignals(droppedChild).HaltedParents); !equalStrings(got, want[1:]) {
		t.Errorf("cancelled parent's child HaltedParents = %v, want %v", got, want[1:])
	}
	if got := h.GoalSignals(runningChild).HaltedParents; len(got) != 0 {
		t.Errorf("Active parent's child HaltedParents = %v, want none", haltedPairs(got))
	}
	if got := h.GoalSignals(paused).HaltedParents; len(got) != 0 {
		t.Errorf("the On Hold parent itself HaltedParents = %v, want none (only children are flagged)", haltedPairs(got))
	}
}

// changeLifecycle moves g to the Lifecycle `to` in a Check-in by owner, giving a
// reason, failing the test on error.
func changeLifecycle(t *testing.T, h *testsupport.Harness, owner domain.Account, g domain.Goal, to string) {
	t.Helper()
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          g.ID,
		AuthorID:        owner.ID,
		Status:          "Changing course.",
		Lifecycle:       to,
		LifecycleReason: "Priorities moved.",
	}); err != nil {
		t.Fatalf("SubmitCheckin to %s: %v", to, err)
	}
}

func haltedPairs(hs []domain.HaltedParent) []string {
	out := make([]string, 0, len(hs))
	for _, hp := range hs {
		out = append(out, fmt.Sprintf("%s -> %s", hp.Child.Title, hp.Parent.Title))
	}
	return out
}

func conflictPairs(cs []domain.ScheduleConflict) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, fmt.Sprintf("%s -> %s", c.Child.Title, c.Parent.Title))
	}
	return out
}

func titles(goals []domain.Goal) []string {
	out := make([]string, 0, len(goals))
	for _, g := range goals {
		out = append(out, g.Title)
	}
	return out
}
