package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// settle moves the clock 40 days past the harness Epoch, so Goals arranged at
// Epoch were created and activated before the default baseline (30 days ago).
func settle(h *testsupport.Harness) {
	h.Clock.Advance(40 * day)
}

// A Red Goal gets the full MBR block; an unchanged Green Goal gets one line
// (CONTEXT.md: Report).
func TestReportRedIsExceptionAndUnchangedGreenIsOneLine(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	settle(h)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})

	r := draftReport(t, h, boss, time.Time{}, red, green)

	if got, want := blockIDs(r), []int64{red.ID}; !sameSet(got, want) {
		t.Errorf("exception blocks %v, want %v", got, want)
	}
	if got, want := lineIDs(r), []int64{green.ID}; !sameSet(got, want) {
		t.Errorf("one-line Goals %v, want %v", got, want)
	}
	if got := r.Exceptions[0].Health; got != domain.HealthRed {
		t.Errorf("block Health %q, want Red", got)
	}
}

// draftReport saves a Report Definition over goals (depth 0, so exactly those
// Goals) and drafts its Report against baseline, failing the test on error. A
// zero baseline takes the default.
func draftReport(t *testing.T, h *testsupport.Harness, actor domain.Account, baseline time.Time, goals ...domain.Goal) domain.Report {
	t.Helper()
	roots := make([]int64, 0, len(goals))
	for _, g := range goals {
		roots = append(roots, g.ID)
	}
	def := h.SaveReportDefinition(actor, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: roots})
	r, err := h.Service.DraftReport(context.Background(), def, baseline)
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	return r
}

// blockIDs lists the Goals a Report gives a full exception block.
func blockIDs(r domain.Report) []int64 {
	ids := make([]int64, 0, len(r.Exceptions))
	for _, b := range r.Exceptions {
		ids = append(ids, b.Goal.ID)
	}
	return ids
}

// lineIDs lists the Goals a Report gives one line each.
func lineIDs(r domain.Report) []int64 {
	return selectedIDs(r.Lines)
}
