package testsupport

import "github.com/zachthieme/goal-tracker/internal/domain"

// SetManager records manager as person's Manager directly, failing the test on
// error. Only the directory sync sets a Manager in the app (ADR 0008); this is
// the scenario builder for arranging a Chain without one (CONTEXT.md: Manager,
// Chain).
func (h *Harness) SetManager(person, manager domain.Account) {
	h.T.Helper()
	if _, err := h.DB.Exec(`UPDATE accounts SET manager_id = ? WHERE id = ?`, manager.ID, person.ID); err != nil {
		h.T.Fatalf("SetManager(%s, %s): %v", person.Email, manager.Email, err)
	}
}
