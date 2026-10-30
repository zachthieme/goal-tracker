package testsupport

import (
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// LinkedGoalsReport is a picked Report whose Goals contribute to each other
// (#246): Growth (Red) is contributed to by Referral program (Red), which is
// contributed to by Alpha ledger (Red), Beta payouts (Red), Gamma emails
// (Yellow), Delta copy (Green) and Epsilon signup (Green). The Definition
// picks Growth, Referral program, Alpha ledger and Epsilon signup; Epsilon is
// an unchanged Green Goal, so it lands as an on-track line.
type LinkedGoalsReport struct {
	Owner                                                domain.Account
	Growth, Referral, Alpha, Beta, Gamma, Delta, Epsilon domain.Goal
	Definition                                           domain.ReportDefinition
}

// LinkedGoalsReport arranges a LinkedGoalsReport owned by owner. Every Goal is
// created before the default baseline, and checked in after it, children
// before their parents, so each parent's Red matches its Rolled-up Health.
func (h *Harness) LinkedGoalsReport(owner domain.Account) LinkedGoalsReport {
	h.T.Helper()
	r := LinkedGoalsReport{Owner: owner}
	r.Growth = h.ActiveGoal(owner, "Growth", "Grow the business.")
	r.Referral = h.ActiveChildOf(owner, r.Growth, "Referral program", "Customers bring customers.")
	r.Alpha = h.ActiveChildOf(owner, r.Referral, "Alpha ledger", "Track the rewards.")
	r.Beta = h.ActiveChildOf(owner, r.Referral, "Beta payouts", "Pay the rewards.")
	r.Gamma = h.ActiveChildOf(owner, r.Referral, "Gamma emails", "Tell customers.")
	r.Delta = h.ActiveChildOf(owner, r.Referral, "Delta copy", "Say it well.")
	r.Epsilon = h.ActiveChildOf(owner, r.Referral, "Epsilon signup", "Let them join.")
	h.Clock.Advance(40 * 24 * time.Hour)
	for _, c := range []struct {
		goal   domain.Goal
		health string
	}{
		{r.Alpha, domain.HealthRed},
		{r.Beta, domain.HealthRed},
		{r.Gamma, domain.HealthYellow},
		{r.Delta, domain.HealthGreen},
		{r.Epsilon, domain.HealthGreen},
		{r.Referral, domain.HealthRed},
		{r.Growth, domain.HealthRed},
	} {
		h.checkinLinkedGoal(owner, c.goal, c.health)
	}
	r.Definition = h.SaveReportDefinition(owner, domain.SaveReportDefinitionInput{
		Name:   "Growth",
		Mode:   domain.ReportModePicked,
		Picked: []int64{r.Growth.ID, r.Referral.ID, r.Alpha.ID, r.Epsilon.ID},
	})
	return r
}

// checkinLinkedGoal checks goal in at health as author: Green with no path,
// or off track with a path back to Green due in a month.
func (h *Harness) checkinLinkedGoal(author domain.Account, goal domain.Goal, health string) {
	h.T.Helper()
	if health == domain.HealthGreen {
		h.Checkin(author, goal.ID, health, "On track.", "", time.Time{})
		return
	}
	h.Checkin(author, goal.ID, health, "Off track.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0))
}
