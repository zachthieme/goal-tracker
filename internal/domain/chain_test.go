package domain_test

import (
	"slices"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// chainEmails is the emails of the people in id's Chain, sorted, failing the
// test on error.
func chainEmails(t *testing.T, h *testsupport.Harness, id int64) []string {
	t.Helper()
	chain, err := h.Service.Chain(t.Context(), id)
	if err != nil {
		t.Fatalf("Chain(%d): %v", id, err)
	}
	var out []string
	for _, a := range chain {
		out = append(out, a.Email)
	}
	slices.Sort(out)
	return out
}

// hasAnyoneUnder reports whether id has anyone under them, failing the test on
// error.
func hasAnyoneUnder(t *testing.T, h *testsupport.Harness, id int64) bool {
	t.Helper()
	under, err := h.Service.HasAnyoneUnder(t.Context(), id)
	if err != nil {
		t.Fatalf("HasAnyoneUnder(%d): %v", id, err)
	}
	return under
}

// A person's Chain is them and everyone below them at every depth, by Manager
// (CONTEXT.md: Chain); someone beside them, or above, isn't in it.
func TestChainIsThePersonAndEveryoneBelowThem(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ceo := h.SignIn("ceo@example.com")
	cto := h.SignIn("cto@example.com")
	cpo := h.SignIn("cpo@example.com")
	lead := h.SignIn("lead@example.com")
	dev := h.SignIn("dev@example.com")
	designer := h.SignIn("designer@example.com")
	h.SetManager(cto, ceo)
	h.SetManager(cpo, ceo)
	h.SetManager(lead, cto)
	h.SetManager(dev, lead)
	h.SetManager(designer, cpo)

	if got, want := chainEmails(t, h, cto.ID), []string{"cto@example.com", "dev@example.com", "lead@example.com"}; !slices.Equal(got, want) {
		t.Errorf("CTO's Chain = %v, want %v", got, want)
	}
	if got := chainEmails(t, h, ceo.ID); len(got) != 6 {
		t.Errorf("CEO's Chain = %v, want all six people", got)
	}
	if !hasAnyoneUnder(t, h, cto.ID) {
		t.Error("the CTO has people under them, but HasAnyoneUnder says not")
	}
}

// Someone with nobody under them has a Chain of one: themselves.
func TestChainOfSomeoneWithNobodyUnderThemIsThemAlone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	lead := h.SignIn("lead@example.com")
	dev := h.SignIn("dev@example.com")
	h.SetManager(dev, lead)

	if got, want := chainEmails(t, h, dev.ID), []string{"dev@example.com"}; !slices.Equal(got, want) {
		t.Errorf("Chain = %v, want %v", got, want)
	}
	if hasAnyoneUnder(t, h, dev.ID) {
		t.Error("HasAnyoneUnder is true for someone with nobody under them")
	}
}

// The directory's cycles are stored as given (ADR 0008), so a Chain stops at
// anyone it has already reached: A→B→A and a self-reference end the walk.
func TestChainWalkEndsAtACycle(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	a := h.SignIn("a@example.com")
	b := h.SignIn("b@example.com")
	c := h.SignIn("c@example.com")
	self := h.SignIn("self@example.com")
	h.SetManager(a, b)
	h.SetManager(b, a)
	h.SetManager(c, b)
	h.SetManager(self, self)

	if got, want := chainEmails(t, h, a.ID), []string{"a@example.com", "b@example.com", "c@example.com"}; !slices.Equal(got, want) {
		t.Errorf("A's Chain = %v, want %v", got, want)
	}
	if got, want := chainEmails(t, h, self.ID), []string{"self@example.com"}; !slices.Equal(got, want) {
		t.Errorf("a self-managed person's Chain = %v, want %v", got, want)
	}
	if hasAnyoneUnder(t, h, self.ID) {
		t.Error("a self-managed person with no one else under them has someone under them")
	}
}

// Manager is read back on the Account, and is none until something sets it.
func TestAccountCarriesItsManager(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	lead := h.SignIn("lead@example.com")
	dev := h.SignIn("dev@example.com")
	if dev.ManagerID != nil {
		t.Fatalf("a new Account has Manager %d, want none", *dev.ManagerID)
	}
	h.SetManager(dev, lead)

	got, err := h.Service.Account(t.Context(), dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ManagerID == nil || *got.ManagerID != lead.ID {
		t.Errorf("Manager = %v, want %d", got.ManagerID, lead.ID)
	}
}
