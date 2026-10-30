package testsupport

import (
	"context"
	"slices"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// Directory is an in-memory org directory: the people it lists, in order. It
// is a domain.Directory beside directory.File and directory.Authentik, so a
// test arranges an Org through the same sync production uses (ADR 0008).
type Directory []domain.DirectoryPerson

// People lists the directory's entries; it never fails.
func (d Directory) People(context.Context) ([]domain.DirectoryPerson, error) {
	return d, nil
}

// SyncDirectory syncs a Directory of people, failing the test only if the sync
// returns an error. The Manager cycles it reports and the Timezones it refuses
// are returned for the test to assert on.
func (h *Harness) SyncDirectory(people ...domain.DirectoryPerson) domain.DirectorySyncResult {
	h.T.Helper()
	res, err := h.Service.SyncDirectory(h.T.Context(), Directory(people))
	if err != nil {
		h.T.Fatalf("SyncDirectory: %v", err)
	}
	return res
}

// CycleEmails is each of cycles as its people's emails, sorted, and the cycles
// in order, for a test asserting on the cycles a sync returns: the person a
// cycle's walk starts at follows the order Accounts are listed in.
func CycleEmails(cycles [][]domain.Account) [][]string {
	out := make([][]string, 0, len(cycles))
	for _, cycle := range cycles {
		emails := make([]string, 0, len(cycle))
		for _, member := range cycle {
			emails = append(emails, member.Email)
		}
		slices.Sort(emails)
		out = append(out, emails)
	}
	slices.SortFunc(out, slices.Compare)
	return out
}

// syncDirectoryPersonRefusingNothing syncs one person, as call names the
// builder doing it, and fails the test if the sync refuses their Timezone or
// reports any Manager cycle in the database, whoever's it is.
func (h *Harness) syncDirectoryPersonRefusingNothing(call string, p domain.DirectoryPerson) {
	h.T.Helper()
	res := h.SyncDirectory(p)
	for _, invalid := range res.InvalidTimezones {
		h.T.Fatalf("%s: the directory sync refused %s's Timezone %q: it isn't an IANA name", call, invalid.Email, invalid.Value)
	}
	for _, cycle := range res.Cycles {
		emails := make([]string, 0, len(cycle)+1)
		for _, member := range cycle {
			emails = append(emails, member.Email)
		}
		emails = append(emails, cycle[0].Email)
		h.T.Fatalf("%s: the database holds a Manager cycle, %s; a cycle stored anywhere fails every later SetManager and SetTimezone, so store it with h.SyncDirectory", call, strings.Join(emails, " → "))
	}
}

// account reads the Account with id as it is now, failing the test if there
// is none.
func (h *Harness) account(call string, id int64) domain.Account {
	h.T.Helper()
	acc, err := h.Service.Account(h.T.Context(), id)
	if err != nil {
		h.T.Fatalf("%s: %v", call, err)
	}
	return acc
}
