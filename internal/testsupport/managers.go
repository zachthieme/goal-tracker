package testsupport

import (
	"fmt"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// SetManager records manager as person's Manager, or none for a zero manager,
// by syncing person alone through the in-memory Directory, as the directory
// sync sets every Manager (ADR 0008; CONTEXT.md: Manager, Org). Their Name and
// Timezone are left as they are. It fails the test on error, on a refused
// Timezone, and on any Manager cycle in the database: store a cycle with
// h.SyncDirectory.
func (h *Harness) SetManager(person, manager domain.Account) {
	h.T.Helper()
	call := fmt.Sprintf("SetManager(%s, %s)", person.Email, manager.Email)
	current := h.account(call, person.ID)
	var managerEmail string
	if manager.ID != 0 {
		managerEmail = h.account(call, manager.ID).Email
	}
	h.syncDirectoryPersonRefusingNothing(call, domain.DirectoryPerson{
		Email:        current.Email,
		ManagerEmail: managerEmail,
		Timezone:     current.Timezone,
	})
}
