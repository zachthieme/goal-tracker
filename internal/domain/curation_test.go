package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// highlightNotes returns the notes of a Report's Highlights, in order.
func highlightNotes(hs []domain.NarrativeHighlight) []string {
	out := make([]string, 0, len(hs))
	for _, nh := range hs {
		out = append(out, nh.Highlight.Note)
	}
	return out
}

// While preparing a publication the author sees every Highlight in scope since
// the baseline: those on the Goals the Report Definition selects, written since
// the baseline, each crediting the Goal's Owner (CONTEXT.md: Highlight).
func TestDraftListsHighlightsInScopeSinceTheBaseline(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	bob := h.SignIn("bob@example.com")
	eu := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	churn := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	outside := h.ActiveGoal(boss, "Rewrite billing", "Billing is slow.")
	h.AddDelegate(alice, bob, eu.ID)
	h.CheckinWithHighlight(alice, eu.ID, domain.HighlightInsight, "Too old to count.")
	settle(h)
	h.CheckinWithHighlight(bob, eu.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.Clock.Advance(day)
	h.CheckinWithHighlight(boss, churn.ID, domain.HighlightMiss, "Lost two big accounts.")
	h.CheckinWithHighlight(boss, outside.ID, domain.HighlightInsight, "Not in this Report.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{eu.ID, churn.ID}})

	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	got := highlightNotes(r.Highlights)
	want := []string{"Signed the first EU customer.", "Lost two big accounts."}
	if !sameStrings(got, want) {
		t.Fatalf("draft Highlights %q, want %q", got, want)
	}
	for _, nh := range r.Highlights {
		switch nh.Highlight.Note {
		case "Signed the first EU customer.":
			// Bob wrote it as Alice's Delegate; Alice, the Owner, is credited.
			if nh.Highlight.Owner.ID != alice.ID || nh.GoalID != eu.ID || nh.GoalTitle != "Launch in EU" ||
				nh.Highlight.Kind != domain.HighlightAccomplishment {
				t.Errorf("EU Highlight credits %s on Goal %d %q as %q, want Alice on Launch in EU as an Accomplishment",
					nh.Highlight.Owner.Email, nh.GoalID, nh.GoalTitle, nh.Highlight.Kind)
			}
		case "Lost two big accounts.":
			if nh.Highlight.Owner.ID != boss.ID || nh.GoalID != churn.ID {
				t.Errorf("churn Highlight credits %s on Goal %d, want boss on Cut churn", nh.Highlight.Owner.Email, nh.GoalID)
			}
		}
		if nh.Section != "" {
			t.Errorf("Highlight %q is in section %q before the author picked it", nh.Highlight.Note, nh.Section)
		}
	}
}

// sameStrings reports whether got and want hold the same strings, in any order.
func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	count := map[string]int{}
	for _, s := range got {
		count[s]++
	}
	for _, s := range want {
		count[s]--
		if count[s] < 0 {
			return false
		}
	}
	return true
}
