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
		!c.Due.Equal(parentDue) || !c.OwnDue.Equal(childDue) {
		t.Errorf("conflict %+v, want as the child, after Open in Iberia (Pat Parker), due %s, its own date %s", c, parentDue, childDue)
	}
}
