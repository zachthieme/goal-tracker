package domain_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Each exception block carries the reasons the Report judged its Goal an
// exception for, in their fixed order, and publishing it freezes them with the
// block: a Goal Ownerless; one Red and Check-in overdue; one that turned
// Yellow; and one created since the baseline (CONTEXT.md: Report).
func TestReportBlocksCarryTheirExceptionReasonsIntoAPublication(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ownerless := h.ActiveGoal(sam, "Ownerless", "why")
	staysRed := h.ActiveGoal(boss, "Stays Red", "why")
	turnedYellow := h.ActiveGoal(boss, "Turned Yellow", "why")
	unchanged := h.ActiveGoal(boss, "Unchanged", "why")
	h.Checkin(boss, staysRed.ID, domain.HealthRed, "Blocked.", "Hire counsel.", h.Clock.Now().AddDate(0, 6, 0))
	for _, g := range []domain.Goal{ownerless, turnedYellow, unchanged} {
		h.Checkin(g.Owner, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	}
	settle(h)
	h.Checkin(sam, ownerless.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(boss, turnedYellow.ID, domain.HealthYellow, "Off track.", "Add staff.", h.Clock.Now().AddDate(0, 6, 0))
	h.Checkin(boss, unchanged.ID, domain.HealthGreen, "On track.", "", time.Time{})
	if err := h.Service.MarkDeparted(ctx, boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	created := h.ActiveGoal(boss, "Created", "why")
	h.Checkin(boss, created.ID, domain.HealthGreen, "On track.", "", time.Time{})
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{ownerless.ID, staysRed.ID, turnedYellow.ID, unchanged.ID, created.ID},
	})

	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}

	want := map[int64][]domain.ReportReason{
		ownerless.ID:    {domain.ReportReasonOwnerless},
		staysRed.ID:     {domain.ReportReasonRed, domain.ReportReasonCheckinOverdue},
		turnedYellow.ID: {domain.ReportReasonYellow, domain.ReportReasonTurnedYellow},
		created.ID:      {domain.ReportReasonNew},
	}
	if got := blockIDs(r); !sameSet(got, []int64{ownerless.ID, staysRed.ID, turnedYellow.ID, created.ID}) {
		t.Fatalf("exception blocks %v, want all but the unchanged Goal %d", got, unchanged.ID)
	}
	for _, b := range r.Exceptions {
		if !slices.Equal(b.Reasons, want[b.Goal.ID]) {
			t.Errorf("%q reasons %q, want %q", b.Goal.Title, b.Reasons, want[b.Goal.ID])
		}
	}

	pub := h.PublishReport(boss, def)
	frozen := h.PublicationFact(pub, domain.ReportFactExceptions).Value.(domain.ReportExceptionsFact).Blocks
	if len(frozen) != len(r.Exceptions) {
		t.Fatalf("publication froze %d blocks, want the draft's %d", len(frozen), len(r.Exceptions))
	}
	for i, b := range frozen {
		if d := r.Exceptions[i]; b.GoalID != d.Goal.ID || !slices.Equal(b.Reasons, d.Reasons) {
			t.Errorf("frozen block %d is Goal %d with reasons %q, want the draft's Goal %d with %q", i, b.GoalID, b.Reasons, d.Goal.ID, d.Reasons)
		}
	}
}
