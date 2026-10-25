package testsupport

import "github.com/zachthieme/goal-tracker/internal/domain"

// SetTimezone records tz, an IANA name or "" for none, as person's Timezone
// directly, failing the test on error. Only the directory sync sets a Timezone
// in the app; this is the scenario builder for arranging one without it
// (CONTEXT.md: Timezone).
func (h *Harness) SetTimezone(person domain.Account, tz string) {
	h.T.Helper()
	if _, err := h.DB.ExecContext(h.T.Context(), `UPDATE accounts SET timezone = ? WHERE id = ?`, tz, person.ID); err != nil {
		h.T.Fatalf("SetTimezone(%s, %q): %v", person.Email, tz, err)
	}
}
