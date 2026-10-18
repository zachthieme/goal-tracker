package domain_test

import (
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A new Account's sessions are generation 0, and each EndSessions moves it on
// by one, so every session cookie issued before it stops working.
func TestEndSessionsMovesTheSessionGenerationOn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	alex := h.SignIn("alex@example.com")

	for want := range int64(3) {
		got, err := h.Service.SessionGeneration(t.Context(), sam.ID)
		if err != nil {
			t.Fatalf("SessionGeneration: %v", err)
		}
		if got != want {
			t.Fatalf("after %d EndSessions, generation %d, want %d", want, got, want)
		}
		if err := h.Service.EndSessions(t.Context(), sam.ID); err != nil {
			t.Fatalf("EndSessions: %v", err)
		}
	}

	if got, err := h.Service.SessionGeneration(t.Context(), alex.ID); err != nil || got != 0 {
		t.Errorf("ending sam's sessions moved alex's generation to %d (err %v), want 0", got, err)
	}
}
