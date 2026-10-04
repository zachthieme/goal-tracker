package testsupport_test

import (
	"context"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Each Harness has a database of its own: what one test writes, another
// never sees.
func TestHarnessesDoNotShareADatabase(t *testing.T) {
	t.Parallel()
	first := testsupport.New(t)
	second := testsupport.New(t)

	acc := first.SignIn("owner@example.com")

	if got, err := second.Service.Account(context.Background(), acc.ID); err == nil {
		t.Fatalf("second harness found Account %d (%q) signed in on the first", acc.ID, got.Email)
	}
}
