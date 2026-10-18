package domain_test

import (
	"slices"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// orgEmails is the emails of the people in id's Org, sorted, failing the
// test on error.
func orgEmails(t *testing.T, h *testsupport.Harness, id int64) []string {
	t.Helper()
	org, err := h.Service.Org(t.Context(), id)
	if err != nil {
		t.Fatalf("Org(%d): %v", id, err)
	}
	var out []string
	for _, a := range org {
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

// A person's Org is them and everyone below them at every depth, by Manager
// (CONTEXT.md: Org); someone beside them, or above, isn't in it.
func TestOrgIsThePersonAndEveryoneBelowThem(t *testing.T) {
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

	if got, want := orgEmails(t, h, cto.ID), []string{"cto@example.com", "dev@example.com", "lead@example.com"}; !slices.Equal(got, want) {
		t.Errorf("CTO's Org = %v, want %v", got, want)
	}
	if got := orgEmails(t, h, ceo.ID); len(got) != 6 {
		t.Errorf("CEO's Org = %v, want all six people", got)
	}
	if !hasAnyoneUnder(t, h, cto.ID) {
		t.Error("the CTO has people under them, but HasAnyoneUnder says not")
	}
}

// Someone with nobody under them has an Org of one: themselves.
func TestOrgOfSomeoneWithNobodyUnderThemIsThemAlone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	lead := h.SignIn("lead@example.com")
	dev := h.SignIn("dev@example.com")
	h.SetManager(dev, lead)

	if got, want := orgEmails(t, h, dev.ID), []string{"dev@example.com"}; !slices.Equal(got, want) {
		t.Errorf("Org = %v, want %v", got, want)
	}
	if hasAnyoneUnder(t, h, dev.ID) {
		t.Error("HasAnyoneUnder is true for someone with nobody under them")
	}
}

// The directory's cycles are stored as given (ADR 0008), so an Org stops at
// anyone it has already reached: A→B→A and a self-reference end the walk.
func TestOrgWalkEndsAtACycle(t *testing.T) {
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

	if got, want := orgEmails(t, h, a.ID), []string{"a@example.com", "b@example.com", "c@example.com"}; !slices.Equal(got, want) {
		t.Errorf("A's Org = %v, want %v", got, want)
	}
	if got, want := orgEmails(t, h, self.ID), []string{"self@example.com"}; !slices.Equal(got, want) {
		t.Errorf("a self-managed person's Org = %v, want %v", got, want)
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

// anyoneHasManager reports whether anyone has a Manager other than themselves,
// failing the test on error.
func anyoneHasManager(t *testing.T, h *testsupport.Harness) bool {
	t.Helper()
	has, err := h.Service.AnyoneHasManager(t.Context())
	if err != nil {
		t.Fatalf("AnyoneHasManager: %v", err)
	}
	return has
}

// The company has Managers once anyone has a Manager other than themselves; a
// self-reference, as a directory can give (ADR 0008), isn't one.
func TestAnyoneHasManagerIgnoresSelfReferences(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	vp := h.SignIn("vp@example.com")
	vera := h.SignIn("vera@example.com")
	mia := h.SignIn("mia@example.com")
	if anyoneHasManager(t, h) {
		t.Error("AnyoneHasManager is true with no Managers")
	}
	h.SetManager(vp, vp)
	if anyoneHasManager(t, h) {
		t.Error("AnyoneHasManager is true when the only Manager is a self-reference")
	}
	h.SetManager(mia, vera)
	if !anyoneHasManager(t, h) {
		t.Error("AnyoneHasManager is false with Vera managing Mia")
	}
}

// A Departed person keeps their Manager (ADR 0008), so still counts.
func TestAnyoneHasManagerCountsDepartedPeople(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	vera := h.SignIn("vera@example.com")
	mia := h.SignIn("mia@example.com")
	h.SetManager(mia, vera)
	if err := h.Service.MarkDeparted(t.Context(), admin.ID, mia.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	if !anyoneHasManager(t, h) {
		t.Error("AnyoneHasManager is false when the person with a Manager has departed")
	}
}
