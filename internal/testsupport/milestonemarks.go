package testsupport

import (
	"context"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// MilestoneMarksGoal arranges an Active Goal owned by owner, titled "Launch in
// EU", whose Milestones carry every mark, and returns it with the clock at
// 2026-02-11, 40 days after Epoch. Its Owner-set Health is Yellow, as is its
// one Active child's, so its Rolled-up Health is Yellow too. As of 2026-02-11
// its Milestones are:
//
//   - Security review, due 2026-01-20 and still Planned: Red.
//   - Pilot, due 2026-02-01, marked Done.
//   - Vendor sign-off, slipped three times, 2026-03-01 to 2026-03-10 to
//     2026-03-20 to 2026-04-20: Yellow.
//   - Beta, slipped once, 2026-04-02 to 2026-04-12: Yellow.
//   - Docs, due 2026-03-10, added by the latest Check-in: New.
//   - Runbook, due 2026-05-01, added by the first Check-in, on Epoch: no mark
//     on the Goal page, nor in a Report read against a baseline after Epoch.
//   - GA launch, due 2026-06-15, added at Epoch: no mark.
//   - Launch party, due 2026-05-20, Removed because "Budget cut.".
//
// It is a scenario builder for the surfaces that list Milestones.
func (h *Harness) MilestoneMarksGoal(owner domain.Account) domain.Goal {
	h.T.Helper()
	ctx := context.Background()
	day := func(month time.Month, d int) time.Time { return time.Date(2026, month, d, 0, 0, 0, 0, time.UTC) }

	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	child := h.ActiveChildOf(owner, g, "EU payments", "Take payments in the EU.")
	for _, m := range []domain.AddMilestoneInput{
		{GoalID: g.ID, Name: "Security review", TargetDate: day(1, 20)},
		{GoalID: g.ID, Name: "Pilot", TargetDate: day(2, 1)},
		{GoalID: g.ID, Name: "Vendor sign-off", TargetDate: day(3, 1)},
		{GoalID: g.ID, Name: "Launch party", TargetDate: day(5, 20)},
		{GoalID: g.ID, Name: "GA launch", TargetDate: day(6, 15)},
	} {
		if _, err := h.Service.AddMilestone(ctx, m); err != nil {
			h.T.Fatalf("AddMilestone %q: %v", m.Name, err)
		}
	}
	milestones, err := h.Service.ListMilestones(ctx, g.ID)
	if err != nil {
		h.T.Fatalf("ListMilestones: %v", err)
	}
	id := map[string]int64{}
	for _, m := range milestones {
		id[m.Name] = m.ID
	}

	yellow := func(goalID int64, changes []domain.MilestoneChangeInput, added []domain.NewMilestoneInput) {
		h.T.Helper()
		if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
			GoalID:         goalID,
			AuthorID:       owner.ID,
			Health:         domain.HealthYellow,
			Status:         "Milestones are moving.",
			PathToGreen:    "Lock the vendor.",
			PathTargetDate: day(6, 1),
			Milestones:     changes,
			NewMilestones:  added,
		}); err != nil {
			h.T.Fatalf("SubmitCheckin: %v", err)
		}
	}
	slip := func(name string, to time.Time) domain.MilestoneChangeInput {
		return domain.MilestoneChangeInput{MilestoneID: id[name], TargetDate: to, DateReason: "Vendor slipped."}
	}

	yellow(g.ID, []domain.MilestoneChangeInput{slip("Vendor sign-off", day(3, 10))},
		[]domain.NewMilestoneInput{{Name: "Runbook", TargetDate: day(5, 1)}})
	h.Clock.Advance(40 * 24 * time.Hour)
	yellow(child.ID, nil, nil)
	yellow(g.ID, []domain.MilestoneChangeInput{
		slip("Vendor sign-off", day(3, 20)),
		slip("Beta", day(4, 12)),
		{MilestoneID: id["Pilot"], Status: domain.MilestoneDone},
		{MilestoneID: id["Launch party"], Status: domain.MilestoneRemoved, RemovedReason: "Budget cut."},
	}, nil)
	h.Clock.Advance(time.Hour)
	yellow(g.ID, []domain.MilestoneChangeInput{slip("Vendor sign-off", day(4, 20))},
		[]domain.NewMilestoneInput{{Name: "Docs", TargetDate: day(3, 10)}})
	return g
}
