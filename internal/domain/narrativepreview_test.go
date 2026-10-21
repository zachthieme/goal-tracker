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

// Previewing a narrative the author hasn't saved reads the draft with it in
// place of the saved one, and saves nothing: the next draft still reads the
// saved narrative, and the Report log records no change (ticket #444).
func TestPreviewNarrativeShowsThePostedNarrativeAndSavesNothing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightMiss, "Lost the second customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	signed := highlightID(t, r, "Signed the first EU customer.")
	lost := highlightID(t, r, "Lost the second customer.")
	if err := h.Service.CurateNarrative(ctx, boss.ID, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: lost, Section: domain.HighlightMiss}},
		Notes: map[string][]string{domain.HighlightMiss: {"Slipped a week."}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}
	logged, err := h.Service.ReportLog(ctx, def.ID)
	if err != nil {
		t.Fatalf("ReportLog: %v", err)
	}

	preview, err := h.Service.PreviewNarrative(ctx, boss.ID, def, time.Time{}, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: signed, Section: domain.HighlightInsight}},
		Notes: map[string][]string{domain.HighlightInsight: {"  Pricing drives churn.  ", ""}},
	})
	if err != nil {
		t.Fatalf("PreviewNarrative: %v", err)
	}
	if len(preview.Narrative) != 1 || preview.Narrative[0].Kind != domain.HighlightInsight ||
		!slices.Equal(preview.Narrative[0].Notes, []string{"Pricing drives churn."}) ||
		!slices.Equal(highlightNotes(preview.Narrative[0].Highlights), []string{"Signed the first EU customer."}) {
		t.Errorf("previewed narrative %+v, want only Insights with the posted note and the EU customer", preview.Narrative)
	}
	for _, nh := range preview.Highlights {
		want := map[int64]string{signed: domain.HighlightInsight}[nh.Highlight.ID]
		if nh.Section != want {
			t.Errorf("previewed Highlight %q marked in section %q, want %q", nh.Highlight.Note, nh.Section, want)
		}
	}

	saved, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(saved.Narrative) != 1 || saved.Narrative[0].Kind != domain.HighlightMiss ||
		!slices.Equal(saved.Narrative[0].Notes, []string{"Slipped a week."}) {
		t.Errorf("saved narrative after a preview %+v, want the Misses saved before", saved.Narrative)
	}
	if after, err := h.Service.ReportLog(ctx, def.ID); err != nil || len(after) != len(logged) {
		t.Errorf("Report log after a preview has %d entries (err %v), want the %d before", len(after), err, len(logged))
	}
}

// A preview refuses what saving would: a narrative CurateNarrative refuses,
// and anyone who may not curate the Report (ticket #444).
func TestPreviewNarrativeRefusesWhatCurateNarrativeRefuses(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	stranger := h.SignIn("stranger@example.com")
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
		"notes for a section that isn't one": {Notes: map[string][]string{"Wins": {"We won."}}},
	} {
		if _, err := h.Service.PreviewNarrative(ctx, boss.ID, def, time.Time{}, in); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: err %v, want ErrValidation", name, err)
		}
	}
	if _, err := h.Service.PreviewNarrative(ctx, stranger.ID, def, time.Time{}, domain.CurateNarrativeInput{
		Notes: map[string][]string{domain.HighlightInsight: {"Pricing drives churn."}},
	}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("a stranger's preview: err %v, want ErrNotAuthorized", err)
	}
}

// A narrative the author is composing is unsaved when saving it would change
// the saved one: blank note boxes, spacing around a note, and the order picks
// come in don't count (ticket #444).
func TestNarrativeUnsavedWhenSavingWouldChangeIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightAccomplishment, "Signed the first EU customer.")
	h.CheckinWithHighlight(boss, g.ID, domain.HighlightMiss, "Lost the second customer.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	signed := highlightID(t, r, "Signed the first EU customer.")
	lost := highlightID(t, r, "Lost the second customer.")
	if err := h.Service.CurateNarrative(ctx, boss.ID, def.ID, domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{
			{HighlightID: signed, Section: domain.HighlightAccomplishment},
			{HighlightID: lost, Section: domain.HighlightMiss},
		},
		Notes: map[string][]string{domain.HighlightMiss: {"Slipped a week."}},
	}); err != nil {
		t.Fatalf("CurateNarrative: %v", err)
	}

	for name, tc := range map[string]struct {
		in   domain.CurateNarrativeInput
		want bool
	}{
		"as saved, with a blank box and spacing": {domain.CurateNarrativeInput{
			Picks: []domain.NarrativePick{
				{HighlightID: lost, Section: domain.HighlightMiss},
				{HighlightID: signed, Section: domain.HighlightAccomplishment},
			},
			Notes: map[string][]string{domain.HighlightMiss: {" Slipped a week. ", ""}, domain.HighlightInsight: {""}},
		}, false},
		"a note changed": {domain.CurateNarrativeInput{
			Picks: []domain.NarrativePick{
				{HighlightID: signed, Section: domain.HighlightAccomplishment},
				{HighlightID: lost, Section: domain.HighlightMiss},
			},
			Notes: map[string][]string{domain.HighlightMiss: {"Slipped two weeks."}},
		}, true},
		"a Highlight moved": {domain.CurateNarrativeInput{
			Picks: []domain.NarrativePick{
				{HighlightID: signed, Section: domain.HighlightInsight},
				{HighlightID: lost, Section: domain.HighlightMiss},
			},
			Notes: map[string][]string{domain.HighlightMiss: {"Slipped a week."}},
		}, true},
		"a Highlight dropped": {domain.CurateNarrativeInput{
			Picks: []domain.NarrativePick{{HighlightID: lost, Section: domain.HighlightMiss}},
			Notes: map[string][]string{domain.HighlightMiss: {"Slipped a week."}},
		}, true},
	} {
		got, err := h.Service.NarrativeUnsaved(ctx, def.ID, tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%s: unsaved %v (err %v), want %v", name, got, err, tc.want)
		}
	}
}
