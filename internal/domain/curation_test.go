package domain_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
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
	t.Parallel()

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
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID, churn.ID}})

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

// section returns the Report narrative's section of the given kind, and
// whether the narrative has it.
func section(r domain.Report, kind string) (domain.NarrativeSection, bool) {
	for _, s := range r.Narrative {
		if s.Kind == kind {
			return s, true
		}
	}
	return domain.NarrativeSection{}, false
}

// highlightID returns the id of the Report's in-scope Highlight with the given
// note, failing the test when there is none.
func highlightID(t *testing.T, r domain.Report, note string) int64 {
	t.Helper()
	for _, nh := range r.Highlights {
		if nh.Highlight.Note == note {
			return nh.Highlight.ID
		}
	}
	t.Fatalf("no Highlight %q in the Report's scope", note)
	return 0
}

// The author picks which Highlights go into Insights, Accomplishments, and
// Misses — whatever each was flagged as — and adds their own text to each
// section. Highlights left out stay out of the narrative.
func TestAuthorCuratesTheNarrative(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	eu := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	churn := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.CheckinWithHighlight(alice, eu.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.CheckinWithHighlight(boss, churn.ID, domain.HighlightMiss, "Lost two big accounts.")
	h.CheckinWithHighlight(boss, churn.ID, domain.HighlightInsight, "Churn follows price rises.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID, churn.ID}})
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	signed := highlightID(t, r, "Signed the first EU customer.")
	lost := highlightID(t, r, "Lost two big accounts.")

	if err := h.Service.CurateNarrative(ctx, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{
			{HighlightID: signed, Section: domain.HighlightAccomplishment},
			// The author reads the lost accounts as an Insight, not a Miss.
			{HighlightID: lost, Section: domain.HighlightInsight},
		},
		Text: map[string]string{
			domain.HighlightInsight:        "  Pricing drives churn.  ",
			domain.HighlightAccomplishment: "EU is open for business.",
		},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}

	r, err = h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	var kinds []string
	for _, s := range r.Narrative {
		kinds = append(kinds, s.Kind)
	}
	if want := []string{domain.HighlightInsight, domain.HighlightAccomplishment}; !slices.Equal(kinds, want) {
		t.Fatalf("narrative sections %q, want %q (an empty Misses section is left out)", kinds, want)
	}
	insights, _ := section(r, domain.HighlightInsight)
	if insights.Text != "Pricing drives churn." || !slices.Equal(highlightNotes(insights.Highlights), []string{"Lost two big accounts."}) {
		t.Errorf("Insights read %q with %q, want the author's text and the lost accounts", insights.Text, highlightNotes(insights.Highlights))
	}
	wins, _ := section(r, domain.HighlightAccomplishment)
	if wins.Text != "EU is open for business." || !slices.Equal(highlightNotes(wins.Highlights), []string{"Signed the first EU customer."}) {
		t.Errorf("Accomplishments read %q with %q, want the author's text and the EU customer", wins.Text, highlightNotes(wins.Highlights))
	}
	if got := wins.Highlights[0].Highlight.Owner; got.ID != alice.ID {
		t.Errorf("included Highlight credits %s, want the Goal's Owner %s", got.Email, alice.Email)
	}
	for _, nh := range r.Highlights {
		want := map[int64]string{signed: domain.HighlightAccomplishment, lost: domain.HighlightInsight}[nh.Highlight.ID]
		if nh.Section != want {
			t.Errorf("Highlight %q marked in section %q, want %q", nh.Highlight.Note, nh.Section, want)
		}
	}

	// Curating again replaces the narrative: the author takes the EU customer
	// back out and clears the Insights text.
	if err := h.Service.CurateNarrative(ctx, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: lost, Section: domain.HighlightMiss}},
	}); err != nil {
		t.Fatalf("CurateNarrative again: %v", err)
	}
	r, err = h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(r.Narrative) != 1 || r.Narrative[0].Kind != domain.HighlightMiss || r.Narrative[0].Text != "" ||
		!slices.Equal(highlightNotes(r.Narrative[0].Highlights), []string{"Lost two big accounts."}) {
		t.Errorf("narrative after re-curating %+v, want only Misses with the lost accounts", r.Narrative)
	}
}

// Only Highlights on Goals the Report Definition selects can go into its
// narrative, each into one of Insights, Accomplishments, or Misses.
func TestCurateNarrativeRejectsHighlightsOutsideTheReport(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	eu := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	outside := h.ActiveGoal(boss, "Rewrite billing", "Billing is slow.")
	h.CheckinWithHighlight(boss, eu.ID, domain.HighlightInsight, "EU buyers want invoices.")
	h.CheckinWithHighlight(boss, outside.ID, domain.HighlightInsight, "Not in this Report.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID}})
	all := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "All", Mode: domain.ReportModePicked, Picked: []int64{eu.ID, outside.ID}})
	r, err := h.Service.DraftReport(ctx, all, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	inside, other := highlightID(t, r, "EU buyers want invoices."), highlightID(t, r, "Not in this Report.")

	for name, in := range map[string]domain.CurateNarrativeInput{
		"a Highlight on a Goal the Report doesn't select": {Picks: []domain.NarrativePick{{HighlightID: other, Section: domain.HighlightInsight}}},
		"a section that isn't one":                        {Picks: []domain.NarrativePick{{HighlightID: inside, Section: "Win"}}},
		"a Highlight picked twice": {Picks: []domain.NarrativePick{
			{HighlightID: inside, Section: domain.HighlightInsight},
			{HighlightID: inside, Section: domain.HighlightMiss},
		}},
		"text for a section that isn't one": {Text: map[string]string{"Wins": "We won."}},
	} {
		if err := h.Service.CurateNarrative(ctx, def.ID, in); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: err %v, want ErrValidation", name, err)
		}
	}
	if err := h.Service.CurateNarrative(ctx, 9999, domain.CurateNarrativeInput{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown Report Definition: err %v, want ErrNotFound", err)
	}
}

// Publishing freezes the narrative with the snapshot: curating afterwards never
// changes the publication, and the next publication's narrative starts empty.
// The Highlights the author left out are not part of the snapshot.
func TestNarrativeIsFrozenWithTheSnapshot(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	eu := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(alice, eu.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.CheckinWithHighlight(alice, eu.ID, domain.HighlightInsight, "EU buyers want invoices.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID}})
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	signed := highlightID(t, r, "Signed the first EU customer.")
	if err := h.Service.CurateNarrative(ctx, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: signed, Section: domain.HighlightAccomplishment}},
		Text:  map[string]string{domain.HighlightAccomplishment: "EU is open for business."},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}

	pub := publish(t, h, boss, def, time.Time{})
	wins, ok := section(pub.Report, domain.HighlightAccomplishment)
	if !ok || len(pub.Report.Narrative) != 1 || wins.Text != "EU is open for business." ||
		!slices.Equal(highlightNotes(wins.Highlights), []string{"Signed the first EU customer."}) {
		t.Fatalf("published narrative %+v, want the curated Accomplishments", pub.Report.Narrative)
	}
	if got := wins.Highlights[0].Highlight.Owner.Email; got != alice.Email {
		t.Errorf("published Highlight credits %s, want the Goal's Owner %s", got, alice.Email)
	}
	if len(pub.Report.Highlights) != 0 {
		t.Errorf("snapshot keeps %d uncurated Highlights, want none", len(pub.Report.Highlights))
	}

	// The next publication's narrative starts empty.
	h.Clock.Advance(day)
	h.CheckinWithHighlight(alice, eu.ID, domain.HighlightMiss, "Lost the second customer.")
	next, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(next.Narrative) != 0 {
		t.Errorf("next draft's narrative %+v, want it to start empty", next.Narrative)
	}
	if got := highlightNotes(next.Highlights); !slices.Equal(got, []string{"Lost the second customer."}) {
		t.Errorf("next draft's Highlights %q, want only those since the publication", got)
	}
	if err := h.Service.CurateNarrative(ctx, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: highlightID(t, next, "Lost the second customer."), Section: domain.HighlightMiss}},
		Text:  map[string]string{domain.HighlightAccomplishment: "Rewritten."},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	got, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if !reflect.DeepEqual(got.Report.Narrative, pub.Report.Narrative) {
		t.Errorf("published narrative changed after re-curating\n got %+v\nwant %+v", got.Report.Narrative, pub.Report.Narrative)
	}
}

// Each Highlight of a Check-in is its own entry in the author's pick list, so
// the author can pull one into the narrative and leave the other; the
// publication shows only the one pulled, crediting the Goal's Owner
// (CONTEXT.md: Highlight).
func TestAuthorPullsOneHighlightOfACheckinAndLeavesAnother(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	eu := h.ActiveGoal(alice, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlights(alice, eu.ID,
		domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Signed the first EU customer."},
		domain.HighlightInput{Kind: domain.HighlightMiss, Note: "Lost the second customer."},
	)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID}})
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if got := highlightNotes(r.Highlights); !slices.Equal(got, []string{"Signed the first EU customer.", "Lost the second customer."}) {
		t.Fatalf("pick list %q, want each Highlight of the Check-in separately", got)
	}
	if err := h.Service.CurateNarrative(ctx, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: highlightID(t, r, "Lost the second customer."), Section: domain.HighlightMiss}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}

	pub := publish(t, h, boss, def, time.Time{})
	if len(pub.Report.Narrative) != 1 {
		t.Fatalf("published narrative %+v, want only the Misses", pub.Report.Narrative)
	}
	misses, ok := section(pub.Report, domain.HighlightMiss)
	if !ok || !slices.Equal(highlightNotes(misses.Highlights), []string{"Lost the second customer."}) {
		t.Fatalf("published Misses %+v, want only the Highlight pulled", misses)
	}
	if got := misses.Highlights[0].Highlight.Owner.Email; got != alice.Email {
		t.Errorf("published Highlight credits %s, want the Goal's Owner %s", got, alice.Email)
	}
}
