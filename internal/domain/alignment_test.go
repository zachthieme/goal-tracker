package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Admin marks a Goal as one of the org's root outcomes, and can unmark it
// again (CONTEXT.md: Top-level Goal).
func TestAdminMarksAndUnmarksTopLevelGoal(t *testing.T) {
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

func titles(goals []domain.Goal) []string {
	out := make([]string, 0, len(goals))
	for _, g := range goals {
		out = append(out, g.Title)
	}
	return out
}
