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
