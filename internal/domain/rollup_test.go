package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A parent's Rolled-up Health is the worst Owner-set Health among its Active
// children (ADR-0003). With a Yellow child and a Red child, the worst is Red.
func TestRolledUpHealthIsWorstAmongActiveChildren(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Org outcome", "It matters.")
	yellowChild := h.ActiveChildOf(sam, parent, "Yellow work", "Yellow so what.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red so what.")
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
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parentA := h.ActiveGoal(sam, "Parent A", "A matters.")
	parentB := h.ActiveGoal(sam, "Parent B", "B matters.")

	// A Red child shared by both parents.
	shared := h.ActiveChildOf(sam, parentA, "Shared work", "Shared so what.")
	h.RequestLink(sam, shared, parentB, "")
	h.Checkin(sam, shared.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	// A Green child under B only, which must not pull B's roll-up off Red.
	greenChild := h.ActiveChildOf(sam, parentB, "Green work", "Green so what.")
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
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")

	// A Proposed child (never activated), linked to the parent.
	proposed := h.CreateGoal(sam, "Proposed work", "Proposed so what.")
	h.RequestLink(sam, proposed, parent, "")
	// An Active child with no Check-in yet.
	h.ActiveChildOf(sam, parent, "Fresh work", "Fresh so what.")

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
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red so what.")
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
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red so what.")
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
