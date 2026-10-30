package domain_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// linkedBlock is the exception block a Report gives g, failing the test when
// it has none.
func linkedBlock(t *testing.T, r domain.Report, g domain.Goal) domain.ReportBlock {
	t.Helper()
	for _, b := range r.Exceptions {
		if b.Goal.ID == g.ID {
			return b
		}
	}
	t.Fatalf("no exception block for %q among %v", g.Title, blockIDs(r))
	return domain.ReportBlock{}
}

// linked is the LinkedGoal a block names g as.
func linked(g domain.Goal, health string, inReport bool) domain.LinkedGoal {
	return domain.LinkedGoal{GoalID: g.ID, Title: g.Title, Health: health, InReport: inReport}
}

// An exception block names the Goals in the same Report it contributes to,
// and behind its Rolled-up Health every child the Report holds and every Red
// or Yellow child it doesn't, Red first; a Green child outside the Report is
// only counted (#246, #247).
func TestReportBlockNamesTheGoalsItLinksTo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	s := h.LinkedGoalsReport(h.SignIn("boss@example.com"))
	r, err := h.Service.DraftReport(context.Background(), s.Definition, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}

	referral := linkedBlock(t, r, s.Referral)
	if want := []domain.LinkedGoal{linked(s.Growth, domain.HealthRed, true)}; !slices.Equal(referral.ContributesTo, want) {
		t.Errorf("Referral program ContributesTo = %+v, want %+v", referral.ContributesTo, want)
	}
	if want := []domain.LinkedGoal{
		linked(s.Alpha, domain.HealthRed, true),
		linked(s.Beta, domain.HealthRed, false),
		linked(s.Gamma, domain.HealthYellow, false),
		linked(s.Epsilon, domain.HealthGreen, true),
	}; !slices.Equal(referral.Behind, want) {
		t.Errorf("Referral program Behind = %+v, want %+v (Delta copy only counted)", referral.Behind, want)
	}
	if ru := referral.RolledUp; ru.RedChildren != 2 || ru.YellowChildren != 1 || ru.ActiveChildren != 5 {
		t.Errorf("Referral program RolledUp = %+v, want 2 Red and 1 Yellow of 5", ru)
	}

	alpha := linkedBlock(t, r, s.Alpha)
	if want := []domain.LinkedGoal{linked(s.Referral, domain.HealthRed, true)}; !slices.Equal(alpha.ContributesTo, want) {
		t.Errorf("Alpha ledger ContributesTo = %+v, want %+v", alpha.ContributesTo, want)
	}
	if alpha.Behind != nil {
		t.Errorf("Alpha ledger Behind = %+v, want none", alpha.Behind)
	}

	growth := linkedBlock(t, r, s.Growth)
	if want := []domain.LinkedGoal{linked(s.Referral, domain.HealthRed, true)}; !slices.Equal(growth.Behind, want) {
		t.Errorf("Growth Behind = %+v, want %+v", growth.Behind, want)
	}
	if growth.ContributesTo != nil {
		t.Errorf("Growth ContributesTo = %+v, want none", growth.ContributesTo)
	}

	// Showing a link never brings a Goal in (ADR 0007).
	if got, want := lineIDs(r), []int64{s.Epsilon.ID}; !slices.Equal(got, want) {
		t.Errorf("lines %v, want only Epsilon signup %v", got, want)
	}
	got := blockIDs(r)
	slices.Sort(got)
	if want := []int64{s.Growth.ID, s.Referral.ID, s.Alpha.ID}; !slices.Equal(got, want) {
		t.Errorf("exceptions %v, want exactly %v", got, want)
	}
}

// Within a Health, a block names its linked Goals by title ignoring case, then
// by id (#246): lowercase "alpha", made first, comes before "Alpha", and both
// before "Beta".
func TestReportBlockOrdersLinkedGoalsByTitleIgnoringCaseThenID(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	hub := h.ActiveGoal(boss, "Hub", "It matters.")
	beta := h.ActiveChildOf(boss, hub, "Beta", "It matters.")
	lowerAlpha := h.ActiveChildOf(boss, hub, "alpha", "It matters.")
	upperAlpha := h.ActiveChildOf(boss, hub, "Alpha", "It matters.")
	for _, g := range []domain.Goal{hub, beta, lowerAlpha, upperAlpha} {
		h.Checkin(boss, g.ID, domain.HealthRed, "Off track.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0))
	}

	r := draftReport(t, h, boss, time.Time{}, hub)

	if got, want := linkedBlock(t, r, hub).Behind, []domain.LinkedGoal{
		linked(lowerAlpha, domain.HealthRed, false),
		linked(upperAlpha, domain.HealthRed, false),
		linked(beta, domain.HealthRed, false),
	}; !slices.Equal(got, want) {
		t.Errorf("Hub Behind = %+v, want %+v", got, want)
	}
}

// A pending link, parent or child, is never named in "Contributes to" or
// "Behind" (#246): neither a Goal it would contribute to, nor one that would
// contribute to it, in the Report or a Red one outside it.
func TestReportBlockNamesNoPendingLink(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	kim := h.SignIn("kim@example.com")
	hub := h.ActiveGoal(boss, "Hub", "It matters.")
	above := h.ActiveGoal(kim, "Above", "It matters.")
	spoke := h.ActiveGoal(kim, "Spoke", "It matters.")
	outside := h.ActiveGoal(kim, "Outside", "It matters.")
	for _, g := range []domain.Goal{hub, above, spoke, outside} {
		h.Checkin(g.Owner, g.ID, domain.HealthRed, "Off track.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0))
	}
	h.RequestLink(boss, hub, above, "")
	h.RequestLink(kim, spoke, hub, "")
	h.RequestLink(kim, outside, hub, "")

	r := draftReport(t, h, boss, time.Time{}, hub, above, spoke)

	for _, g := range []domain.Goal{hub, above, spoke} {
		b := linkedBlock(t, r, g)
		if b.ContributesTo != nil {
			t.Errorf("%s ContributesTo = %+v, want none", g.Title, b.ContributesTo)
		}
		if b.Behind != nil {
			t.Errorf("%s Behind = %+v, want none", g.Title, b.Behind)
		}
	}
}

// A Goal due after a Goal it contributes to is in a Schedule conflict, and
// that makes it an exception even Green and unchanged since the baseline. Its
// block names the parent, its Owner and its delivery date, though the Report
// doesn't hold the parent, and naming it never brings it in (CONTEXT.md:
// Report; ADR 0007).
func TestReportShowsTheChildsScheduleConflictWithAParentOutsideIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	sam := h.SignInNamed("sam@example.com", "Sam Lee")
	pat := h.SignInNamed("pat@example.com", "Pat Parker")
	parentDue := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	childDue := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	parent := h.ActiveGoalDueLater(pat, "Open in Iberia", parentDue)
	child := h.ActiveGoalDueLater(sam, "Lisbon office", childDue)
	h.AcceptedLink(child, parent)
	settle(h)
	h.Checkin(sam, child.ID, domain.HealthGreen, "On track.", "", time.Time{})

	r := draftReport(t, h, sam, time.Time{}, child)

	if got, want := blockIDs(r), []int64{child.ID}; !slices.Equal(got, want) {
		t.Fatalf("exception blocks %v, want only the late child %v", got, want)
	}
	if got := lineIDs(r); len(got) != 0 {
		t.Errorf("one-line Goals %v, want none: naming the parent never brings it in", got)
	}
	b := r.Exceptions[0]
	if b.Health != domain.HealthGreen {
		t.Errorf("Health %q, want its latest Check-in's, Green", b.Health)
	}
	if len(b.ScheduleConflicts) != 1 {
		t.Fatalf("ScheduleConflicts %+v, want one", b.ScheduleConflicts)
	}
	c := b.ScheduleConflicts[0]
	if c.AsParent || c.GoalID != parent.ID || c.Title != "Open in Iberia" ||
		c.Owner.ID != pat.ID || c.Owner.Label() != "Pat Parker" || c.Owner.Email != "pat@example.com" ||
		!c.Due.Equal(parentDue) {
		t.Errorf("conflict %+v, want as the child, after Open in Iberia (Pat Parker), due %s", c, parentDue)
	}
}

// A Goal whose child is due after it is in a Schedule conflict as the parent:
// in a Report holding only the parent, it is an exception whose block names
// the late child, its Owner and its delivery date, while a child due on time
// is not named.
func TestReportShowsTheParentsScheduleConflictWithALateChildOutsideIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	sam := h.SignInNamed("sam@example.com", "Sam Lee")
	pat := h.SignInNamed("pat@example.com", "Pat Parker")
	parentDue := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	childDue := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	parent := h.ActiveGoalDueLater(pat, "Open in Iberia", parentDue)
	late := h.ActiveGoalDueLater(sam, "Lisbon office", childDue)
	h.AcceptedLink(late, parent)
	onTime := h.ActiveGoalDueLater(sam, "Madrid office", parentDue)
	h.AcceptedLink(onTime, parent)
	settle(h)
	h.Checkin(pat, parent.ID, domain.HealthGreen, "On track.", "", time.Time{})

	r := draftReport(t, h, pat, time.Time{}, parent)

	if got, want := blockIDs(r), []int64{parent.ID}; !slices.Equal(got, want) {
		t.Fatalf("exception blocks %v, want only the parent %v", got, want)
	}
	cs := r.Exceptions[0].ScheduleConflicts
	if len(cs) != 1 {
		t.Fatalf("ScheduleConflicts %+v, want only the late child's", cs)
	}
	if c := cs[0]; !c.AsParent || c.GoalID != late.ID || c.Title != "Lisbon office" ||
		c.Owner.ID != sam.ID || c.Owner.Label() != "Sam Lee" ||
		!c.Due.Equal(childDue) {
		t.Errorf("conflict %+v, want as the parent of Lisbon office (Sam Lee), due %s", c, childDue)
	}
}

// A block lists its Schedule conflicts as the child first, then as the parent,
// each by the other Goal's delivery date, then title (#641). The block reads
// parents and children newest first, so each side's Goals are created in the
// order the block should list them: they arrive reversed, and only the sort
// puts them right (#643).
func TestReportBlockOrdersScheduleConflictsAsChildFirstThenByDateThenTitle(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	day := func(month time.Month, d int) time.Time { return time.Date(2026, month, d, 0, 0, 0, 0, time.UTC) }
	hub := h.ActiveGoalDueLater(sam, "Lisbon office", day(time.November, 3))
	iberia := h.ActiveGoalDueLater(pat, "Open in Iberia", day(time.October, 10))
	europe := h.ActiveGoalDueLater(pat, "Europe expansion", day(time.October, 20))
	portugal := h.ActiveGoalDueLater(pat, "Portugal launch", day(time.October, 20))
	fitOut := h.ActiveGoalDueLater(kim, "Office fit-out", day(time.November, 10))
	hiring := h.ActiveGoalDueLater(kim, "Hire the team", day(time.December, 1))
	payroll := h.ActiveGoalDueLater(kim, "Local payroll", day(time.December, 1))
	for _, parent := range []domain.Goal{iberia, europe, portugal} {
		h.AcceptedLink(hub, parent)
	}
	for _, child := range []domain.Goal{fitOut, hiring, payroll} {
		h.AcceptedLink(child, hub)
	}
	settle(h)
	h.Checkin(sam, hub.ID, domain.HealthGreen, "On track.", "", time.Time{})

	r := draftReport(t, h, sam, time.Time{}, hub)

	type entry struct {
		asParent bool
		goalID   int64
		due      string
	}
	var got []entry
	for _, c := range linkedBlock(t, r, hub).ScheduleConflicts {
		got = append(got, entry{c.AsParent, c.GoalID, c.Due.Format(time.DateOnly)})
	}
	want := []entry{
		{false, iberia.ID, "2026-10-10"},
		{false, europe.ID, "2026-10-20"},
		{false, portugal.ID, "2026-10-20"},
		{true, fitOut.ID, "2026-11-10"},
		{true, hiring.ID, "2026-12-01"},
		{true, payroll.ID, "2026-12-01"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ScheduleConflicts %+v, want %+v", got, want)
	}
}

// A link raises no Schedule conflict, and so no exception, when the child is
// Done or Cancelled, either Goal has no delivery date, or the child is due on
// or before its parent: each Goal with no other reason stays one line.
func TestReportRaisesNoScheduleConflictWhereTheGraphFlagsNone(t *testing.T) {
	t.Parallel()

	parentDue := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		// arrange links a child to a parent and returns the Goals the Report
		// holds, every one Active.
		arrange func(h *testsupport.Harness, sam, pat domain.Account) []domain.Goal
	}{
		{"child Done", func(h *testsupport.Harness, sam, pat domain.Account) []domain.Goal {
			parent := h.ActiveGoalDueLater(pat, "Parent", parentDue)
			child := h.ActiveGoalDueLater(sam, "Child", parentDue.AddDate(0, 1, 0))
			h.AcceptedLink(child, parent)
			h.EndGoalInCheckin(sam, child.ID, domain.LifecycleDone)
			return []domain.Goal{parent}
		}},
		{"child Cancelled", func(h *testsupport.Harness, sam, pat domain.Account) []domain.Goal {
			parent := h.ActiveGoalDueLater(pat, "Parent", parentDue)
			child := h.ActiveGoalDueLater(sam, "Child", parentDue.AddDate(0, 1, 0))
			h.AcceptedLink(child, parent)
			h.EndGoalInCheckin(sam, child.ID, domain.LifecycleCancelled)
			return []domain.Goal{parent}
		}},
		{"parent with no delivery date", func(h *testsupport.Harness, sam, pat domain.Account) []domain.Goal {
			parent := activeOngoingGoal(h, pat, "Parent")
			child := h.ActiveGoalDueLater(sam, "Child", parentDue)
			h.AcceptedLink(child, parent)
			return []domain.Goal{parent, child}
		}},
		{"child with no delivery date", func(h *testsupport.Harness, sam, pat domain.Account) []domain.Goal {
			parent := h.ActiveGoalDueLater(pat, "Parent", parentDue)
			child := activeOngoingGoal(h, sam, "Child")
			h.AcceptedLink(child, parent)
			return []domain.Goal{parent, child}
		}},
		{"child due on its parent's date", func(h *testsupport.Harness, sam, pat domain.Account) []domain.Goal {
			parent := h.ActiveGoalDueLater(pat, "Parent", parentDue)
			child := h.ActiveGoalDueLater(sam, "Child", parentDue)
			h.AcceptedLink(child, parent)
			return []domain.Goal{parent, child}
		}},
		{"child due before its parent", func(h *testsupport.Harness, sam, pat domain.Account) []domain.Goal {
			parent := h.ActiveGoalDueLater(pat, "Parent", parentDue)
			child := h.ActiveGoalDueLater(sam, "Child", parentDue.AddDate(0, 0, -14))
			h.AcceptedLink(child, parent)
			return []domain.Goal{parent, child}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := testsupport.New(t, "boss@example.com")
			sam := h.SignIn("sam@example.com")
			pat := h.SignIn("pat@example.com")
			held := tc.arrange(h, sam, pat)
			settle(h)
			for _, g := range held {
				h.Checkin(g.Owner, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
			}

			r := draftReport(t, h, pat, time.Time{}, held...)

			if got := blockIDs(r); len(got) != 0 {
				t.Errorf("exception blocks %v, want none", got)
			}
			if got, want := lineIDs(r), idsOf(held); !sameSet(got, want) {
				t.Errorf("one-line Goals %v, want %v", got, want)
			}
		})
	}
}

// activeOngoingGoal creates an Active Ongoing Goal owned by owner, one with no
// delivery date, failing the test on error.
func activeOngoingGoal(h *testsupport.Harness, owner domain.Account, title string) domain.Goal {
	h.T.Helper()
	ctx := context.Background()
	g := h.CreateGoal(owner, title, title+" matters.")
	if _, err := h.Service.MarkGoalOngoing(ctx, owner.ID, g.ID); err != nil {
		h.T.Fatalf("MarkGoalOngoing: %v", err)
	}
	if _, err := h.Service.AddMetric(ctx, owner.ID, domain.AddMetricInput{
		GoalID: g.ID, Name: "NPS", Unit: "points", Direction: domain.MetricUp,
		Baseline: 10, Target: 40, TargetDate: h.Clock.Now().AddDate(1, 0, 0),
	}); err != nil {
		h.T.Fatalf("AddMetric: %v", err)
	}
	active, err := h.Service.ActivateGoal(ctx, owner.ID, g.ID)
	if err != nil {
		h.T.Fatalf("ActivateGoal: %v", err)
	}
	return active
}
