package testsupport

import (
	"fmt"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// SetTimezone records tz, an IANA name or "" for none, as person's Timezone,
// by syncing person alone through the in-memory Directory, as the directory
// sync sets every Timezone (ADR 0008; CONTEXT.md: Timezone). Their Name and
// Manager are left as they are. It fails the test on error, on a refused
// Timezone, and on any Manager cycle in the database: store a cycle with
// h.SyncDirectory.
func (h *Harness) SetTimezone(person domain.Account, tz string) {
	h.T.Helper()
	call := fmt.Sprintf("SetTimezone(%s, %q)", person.Email, tz)
	current := h.account(call, person.ID)
	var managerEmail string
	if current.ManagerID != nil {
		managerEmail = h.account(call, *current.ManagerID).Email
	}
	h.syncDirectoryPersonRefusingNothing(call, domain.DirectoryPerson{
		Email:        current.Email,
		ManagerEmail: managerEmail,
		Timezone:     tz,
	})
}
