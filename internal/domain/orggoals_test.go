package domain_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// orgSignals reads id's OrgSignals, failing the test on error.
func orgSignals(t *testing.T, h *testsupport.Harness, id int64) domain.OrgSignals {
	t.Helper()
	sig, err := h.Service.OrgSignals(t.Context(), id)
	if err != nil {
		t.Fatalf("OrgSignals(%d): %v", id, err)
	}
	return sig
}

// signalsFor is the counts OrgSignals gives the person directly under the
// viewer with personID, failing the test if it gives them none.
func signalsFor(t *testing.T, sig domain.OrgSignals, personID int64) domain.SignalCounts {
	t.Helper()
	for _, p := range sig.Under {
		if p.Person.ID == personID {
			return p.Counts
		}
	}
	t.Fatalf("OrgSignals has no entry for Account %d; Under = %+v", personID, sig.Under)
	return domain.SignalCounts{}
}

// Each person directly under the viewer is counted from their whole Org's
// Goals, under every signal each Goal has, and the total adds the rows up.
func TestOrgSignalsCountEachOrgUnderTheViewer(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	viewer := h.SignIn("viewer@example.com")
	a := h.SignIn("a@example.com")
	b := h.SignIn("b@example.com")
	c := h.SignIn("c@example.com")
	h.SetManager(a, viewer)
	h.SetManager(b, viewer)
	h.SetManager(c, a)

	// A's Goal goes Red with a Path to Green due in three days, then nobody
	// checks in for ten: Red, Stale and Path to Green overdue at once.
	troubled := h.ActiveGoal(a, "Ship search", "People can't find things.")
	h.Checkin(a, troubled.ID, domain.HealthRed, "Blocked.", "Escalate.", testsupport.Epoch.AddDate(0, 0, 3))
	// B's Goal is never checked in on, so goes Stale.
	h.ActiveGoal(b, "Hire a designer", "No one owns design.")
	h.Clock.Advance(10 * 24 * time.Hour)

	far := h.Clock.Now().AddDate(0, 1, 0)
	slipping := h.ActiveGoal(c, "Cut costs", "Spend is up.")
	h.Checkin(c, slipping.ID, domain.HealthYellow, "Slipping.", "Renegotiate.", far)
	blocked := h.ActiveGoal(b, "Move offices", "The lease ends.")
	h.Checkin(b, blocked.ID, domain.HealthRed, "No site.", "Widen the search.", far)
	fine := h.ActiveGoal(b, "Run payroll", "People get paid.")
	h.Checkin(b, fine.ID, domain.HealthGreen, "Fine.", "", time.Time{})

	sig := orgSignals(t, h, viewer.ID)
	if got, want := signalsFor(t, sig, a.ID), (domain.SignalCounts{Red: 1, PathOverdue: 1, Stale: 1, Yellow: 1}); got != want {
		t.Errorf("A's counts = %+v, want %+v", got, want)
	}
	if got, want := signalsFor(t, sig, b.ID), (domain.SignalCounts{Red: 1, Stale: 1}); got != want {
		t.Errorf("B's counts = %+v, want %+v", got, want)
	}
	if got, want := sig.Total, (domain.SignalCounts{Red: 2, PathOverdue: 1, Stale: 2, Yellow: 1}); got != want {
		t.Errorf("Total = %+v, want %+v", got, want)
	}
	if len(sig.Under) != 2 {
		t.Errorf("Under has %d entries, want A and B only: %+v", len(sig.Under), sig.Under)
	}
}

// Only Active Goals Owned by someone under the viewer count: not the viewer's
// own, which Your goals covers, not one On Hold, and not one Owned outside
// the Org, even with a Delegate inside it (CONTEXT.md: Org).
func TestOrgSignalsCountOnlyActiveGoalsOwnedUnderTheViewer(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	viewer := h.SignIn("viewer@example.com")
	a := h.SignIn("a@example.com")
	outsider := h.SignIn("outsider@example.com")
	h.SetManager(a, viewer)

	far := h.Clock.Now().AddDate(0, 1, 0)
	own := h.ActiveGoal(viewer, "Set strategy", "No direction.")
	h.Checkin(viewer, own.ID, domain.HealthRed, "Stuck.", "Offsite.", far)
	outside := h.ActiveGoal(outsider, "Rebrand", "Old logo.")
	h.Checkin(outsider, outside.ID, domain.HealthRed, "Stuck.", "New agency.", far)
	h.AddDelegate(outsider, a, outside.ID)
	h.OnHoldGoal(a, "Open Berlin", "Expand.", "Waiting on budget.")
	h.Clock.Advance(10 * 24 * time.Hour)

	sig := orgSignals(t, h, viewer.ID)
	if sig.Total.Any() {
		t.Errorf("Total = %+v, want nothing flagged", sig.Total)
	}
	if got := signalsFor(t, sig, a.ID); got.Any() {
		t.Errorf("A's counts = %+v, want nothing flagged", got)
	}
}

// A Departed person directly under the viewer keeps their entry, and an
// Ownerless Goal stays in the Org its Departed Owner was in (ADR 0008).
func TestOrgSignalsKeepDepartedPeopleWhereTheyWere(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	viewer := h.SignIn("viewer@example.com")
	a := h.SignIn("a@example.com")
	b := h.SignIn("b@example.com")
	c := h.SignIn("c@example.com")
	h.SetManager(a, viewer)
	h.SetManager(b, viewer)
	h.SetManager(c, a)
	far := h.Clock.Now().AddDate(0, 1, 0)
	left := h.ActiveGoal(c, "Ship search", "People can't find things.")
	h.Checkin(c, left.ID, domain.HealthRed, "Blocked.", "Escalate.", far)
	for _, p := range []domain.Account{b, c} {
		if err := h.Service.MarkDeparted(t.Context(), admin.ID, p.ID); err != nil {
			t.Fatalf("MarkDeparted(%s): %v", p.Email, err)
		}
	}

	sig := orgSignals(t, h, viewer.ID)
	if got, want := signalsFor(t, sig, a.ID), (domain.SignalCounts{Red: 1}); got != want {
		t.Errorf("A's counts = %+v, want the Ownerless Goal's %+v", got, want)
	}
	for _, p := range sig.Under {
		if p.Person.ID == b.ID && !p.Person.Departed {
			t.Errorf("B's entry doesn't mark them Departed")
		}
	}
	signalsFor(t, sig, b.ID)
}

// The directory's cycles are stored as given (ADR 0008): with the viewer's
// Manager under them, the walk still ends, counts B's Goal once and the
// viewer's own nowhere. Someone who manages themselves gets no entry.
func TestOrgSignalsCountNoOneTwiceInAManagerCycle(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	viewer := h.SignIn("viewer@example.com")
	a := h.SignIn("a@example.com")
	b := h.SignIn("b@example.com")
	h.SetManager(a, viewer)
	h.SetManager(b, viewer)
	h.SetManager(viewer, a)
	far := h.Clock.Now().AddDate(0, 1, 0)
	own := h.ActiveGoal(viewer, "Set strategy", "No direction.")
	h.Checkin(viewer, own.ID, domain.HealthRed, "Stuck.", "Offsite.", far)
	red := h.ActiveGoal(b, "Ship search", "People can't find things.")
	h.Checkin(b, red.ID, domain.HealthRed, "Blocked.", "Escalate.", far)

	sig := orgSignals(t, h, viewer.ID)
	if got, want := sig.Total, (domain.SignalCounts{Red: 1}); got != want {
		t.Errorf("Total = %+v, want %+v", got, want)
	}
	if got, want := signalsFor(t, sig, b.ID), (domain.SignalCounts{Red: 1}); got != want {
		t.Errorf("B's counts = %+v, want %+v", got, want)
	}
	if got := signalsFor(t, sig, a.ID); got.Any() {
		t.Errorf("A's counts = %+v, want nothing: the viewer's Goal is theirs", got)
	}
	if len(sig.Under) != 2 {
		t.Errorf("Under = %+v, want A and B only", sig.Under)
	}

	self := h.SignIn("self@example.com")
	h.SetManager(self, self)
	selfGoal := h.ActiveGoal(self, "Write the memo", "Nobody knows the plan.")
	h.Checkin(self, selfGoal.ID, domain.HealthRed, "Late.", "Block a day.", far)
	if sig := orgSignals(t, h, self.ID); len(sig.Under) != 0 || sig.Total.Any() {
		t.Errorf("a self-managed person's OrgSignals = %+v, want no entries and nothing counted", sig)
	}
}

// Under puts the most Red first, then the most Path to Green overdue, the most
// Stale, the most Yellow, and then goes by Name, ignoring case; those with
// nothing flagged come last.
func TestOrgSignalsOrderTheWorstOffFirst(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	viewer := h.SignIn("viewer@example.com")
	people := map[string]domain.Account{}
	for _, name := range []string{"Clear", "Yellow", "Stale", "Path", "Beta red", "alpha red", "Two red"} {
		p := h.SignInNamed(strings.ToLower(strings.ReplaceAll(name, " ", ""))+"@example.com", name)
		h.SetManager(p, viewer)
		people[name] = p
	}
	checkin := func(name, health string, target time.Time) {
		g := h.ActiveGoal(people[name], name+" goal", "It matters.")
		if health == domain.HealthGreen {
			h.Checkin(people[name], g.ID, health, "Fine.", "", time.Time{})
			return
		}
		h.Checkin(people[name], g.ID, health, "Trouble.", "Fix it.", target)
	}
	// Stale's Goal is never checked in on; by day 8 it is Stale. Path's is
	// Yellow from day 4 with a Path to Green due on day 5, so by day 8 it is
	// overdue but not Stale.
	h.ActiveGoal(people["Stale"], "Stale goal", "It matters.")
	h.Clock.Set(testsupport.Epoch.AddDate(0, 0, 4))
	checkin("Path", domain.HealthYellow, testsupport.Epoch.AddDate(0, 0, 5))
	h.Clock.Set(testsupport.Epoch.AddDate(0, 0, 8))
	far := h.Clock.Now().AddDate(0, 1, 0)
	checkin("Yellow", domain.HealthYellow, far)
	checkin("Yellow", domain.HealthYellow, far)
	checkin("Beta red", domain.HealthRed, far)
	checkin("alpha red", domain.HealthRed, far)
	checkin("Two red", domain.HealthRed, far)
	checkin("Two red", domain.HealthRed, far)
	checkin("Clear", domain.HealthGreen, far)

	var got []string
	for _, p := range orgSignals(t, h, viewer.ID).Under {
		got = append(got, p.Person.Name)
	}
	want := []string{"Two red", "alpha red", "Beta red", "Path", "Stale", "Yellow", "Clear"}
	if !slices.Equal(got, want) {
		t.Errorf("Under = %v, want %v", got, want)
	}
}
