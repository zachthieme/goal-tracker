package domain_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A parent's Rolled-up Health is the worst Owner-set Health among its Active
// children (ADR-0003). With a Yellow child and a Red child, the worst is Red.
func TestRolledUpHealthIsWorstAmongActiveChildren(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Org outcome", "It matters.")
	yellowChild := h.ActiveChildOf(sam, parent, "Yellow work", "Yellow why it matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red why it matters.")
	h.Checkin(sam, yellowChild.ID, domain.HealthYellow, "Wobbling.", "Recover.", pathDate)
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	got, err := h.Service.RolledUpHealth(context.Background(), parent.ID)
	if err != nil {
		t.Fatalf("RolledUpHealth: %v", err)
	}
	if !got.Present {
		t.Fatalf("Present = false, want a Rolled-up Health from the Active children")
	}
	if got.Health != domain.HealthRed {
		t.Errorf("Rolled-up Health = %q, want %q (the worst among children)", got.Health, domain.HealthRed)
	}
}

// A child that contributes to several parents rolls up into each of them
// independently (CONTEXT.md: a Goal may have many parents). Each parent's
// Rolled-up Health reflects only its own children.
func TestRolledUpHealthAcrossMultipleParents(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parentA := h.ActiveGoal(sam, "Parent A", "A matters.")
	parentB := h.ActiveGoal(sam, "Parent B", "B matters.")

	// A Red child shared by both parents.
	shared := h.ActiveChildOf(sam, parentA, "Shared work", "Shared why it matters.")
	h.RequestLink(sam, shared, parentB, "")
	h.Checkin(sam, shared.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	// A Green child under B only, which must not pull B's roll-up off Red.
	greenChild := h.ActiveChildOf(sam, parentB, "Green work", "Green why it matters.")
	h.Checkin(sam, greenChild.ID, domain.HealthGreen, "On track.", "", pathDate)

	for _, p := range []domain.Goal{parentA, parentB} {
		got, err := h.Service.RolledUpHealth(context.Background(), p.ID)
		if err != nil {
			t.Fatalf("RolledUpHealth(%s): %v", p.Title, err)
		}
		if got.Health != domain.HealthRed {
			t.Errorf("%s Rolled-up Health = %q, want %q", p.Title, got.Health, domain.HealthRed)
		}
	}
}

// Only Active children with an Owner-set Health count. A Proposed child, and an
// Active child with no Check-in yet, have no Health, so a parent with only those
// has no Rolled-up Health (ADR-0003).
func TestRolledUpHealthAbsentWithoutActiveHealthyChildren(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")

	// A Proposed child (never activated), linked to the parent.
	proposed := h.CreateGoal(sam, "Proposed work", "Proposed why it matters.")
	h.RequestLink(sam, proposed, parent, "")
	// An Active child with no Check-in yet.
	h.ActiveChildOf(sam, parent, "Fresh work", "Fresh why it matters.")

	got, err := h.Service.RolledUpHealth(context.Background(), parent.ID)
	if err != nil {
		t.Fatalf("RolledUpHealth: %v", err)
	}
	if got.Present {
		t.Errorf("Present = true (Health %q), want no Rolled-up Health from children without an Owner-set Health", got.Health)
	}
}

// When an Owner's Health differs from the Rolled-up Health, the Check-in must
// carry an explanation (ADR-0003: the Owner has to explain the difference). A
// parent with a Red child, checked in Green, needs one.
func TestCheckinDifferingFromRollupRequiresExplanation(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red why it matters.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	// Green differs from the Red roll-up, and no explanation is given.
	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   parent.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "I think we're fine.",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (Health differs from the Rolled-up Health)", err)
	}

	// With an explanation, the same Check-in is accepted and records it.
	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:      parent.ID,
		AuthorID:    sam.ID,
		Health:      domain.HealthGreen,
		Status:      "I think we're fine.",
		Explanation: "The Red child is a stretch item that doesn't gate delivery.",
	})
	if err != nil {
		t.Fatalf("SubmitCheckin with an explanation: %v", err)
	}
	if c.Explanation != "The Red child is a stretch item that doesn't gate delivery." {
		t.Errorf("Explanation = %q, want the submitted explanation", c.Explanation)
	}
}

// When an Owner's Health matches the Rolled-up Health, no explanation is needed.
func TestCheckinMatchingRollupNeedsNoExplanation(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red why it matters.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         parent.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthRed,
		Status:         "Matching the worst child.",
		PathToGreen:    "Escalate too.",
		PathTargetDate: pathDate,
	})
	if err != nil {
		t.Fatalf("SubmitCheckin matching the roll-up: %v", err)
	}
	if c.Explanation != "" {
		t.Errorf("Explanation = %q, want none when Health matches the roll-up", c.Explanation)
	}
}

// With no Active child that has a Health, there is no Rolled-up Health to differ
// from, so a Check-in needs no explanation.
func TestCheckinNeedsNoExplanationWithoutRollup(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Leaf", "It matters.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "On track.",
	}); err != nil {
		t.Fatalf("SubmitCheckin on a Goal with no roll-up: %v", err)
	}
}

// A one-click "no change" Check-in cannot silently violate the explanation rule:
// when repeating the previous Health would differ from a now-changed Rolled-up
// Health and the previous Check-in carried no explanation, it is refused so the
// Owner uses the full form to explain (ADR-0003).
func TestNoChangeCheckinRefusedWhenItWouldDifferFromRollup(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")

	// The parent checks in Green while it has no children with a Health.
	h.Checkin(sam, parent.ID, domain.HealthGreen, "On track.", "", pathDate)

	// Now a Red child appears, making the Rolled-up Health Red.
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red why it matters.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	_, err := h.Service.SubmitNoChangeCheckin(context.Background(), parent.ID, sam.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (no-change would differ from the Rolled-up Health)", err)
	}
	if got, want := domain.Reason(err), "this Health differs from the Rolled-up Health (Red); explain why — submit a Check-in to explain"; got != want {
		t.Errorf("Reason(%v) = %q, want %q", err, got, want)
	}
}

// A Rolled-up Health counts the Active children checked in Red and Yellow
// beside the Check-in overdue ones; a child with no Check-in counts only among
// the Active (#246).
func TestRolledUpHealthCountsRedAndYellowChildren(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	for i, health := range []string{domain.HealthRed, domain.HealthRed, domain.HealthYellow, domain.HealthGreen} {
		child := h.ActiveChildOf(sam, parent, fmt.Sprintf("Child %d", i), "Why it matters.")
		h.Checkin(sam, child.ID, health, "Status.", "Recover.", pathDate)
	}
	h.ActiveChildOf(sam, parent, "Fresh work", "Fresh why it matters.")

	got, err := h.Service.RolledUpHealth(context.Background(), parent.ID)
	if err != nil {
		t.Fatalf("RolledUpHealth: %v", err)
	}
	if got.RedChildren != 2 || got.YellowChildren != 1 || got.ActiveChildren != 5 {
		t.Errorf("RolledUpHealth = %+v, want 2 Red and 1 Yellow of 5 Active children", got)
	}
}

// The count line counts Red, Yellow and Check-in overdue children, each only
// when there are some, and the first carries how many Active children there are
// (#246).
func TestRolledUpHealthCountLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		rollup domain.RolledUpHealth
		want   string
	}{
		{domain.RolledUpHealth{RedChildren: 2, YellowChildren: 1, CheckinOverdueChildren: 1, ActiveChildren: 6}, "2 of 6 Red · 1 Yellow · 1 overdue"},
		{domain.RolledUpHealth{YellowChildren: 1, ActiveChildren: 4}, "1 of 4 Yellow"},
		{domain.RolledUpHealth{CheckinOverdueChildren: 1, ActiveChildren: 3}, "1 of 3 overdue"},
		{domain.RolledUpHealth{Health: domain.HealthGreen, Present: true, ActiveChildren: 3}, ""},
	} {
		if got := tc.rollup.CountLine(); got != tc.want {
			t.Errorf("%+v.CountLine() = %q, want %q", tc.rollup, got, tc.want)
		}
	}
}
