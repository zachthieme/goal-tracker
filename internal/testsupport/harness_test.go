package testsupport_test

import (
	"context"
	"strings"
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

// A test whose name puts '#' or '?' in its temp directory still gets the
// migrated database: an unnamed subtest is called "#00", and an unescaped '#'
// would cut the database's path short and open an empty one.
func TestHarnessDatabaseIsMigratedWhateverTheTestsName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "what?"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := testsupport.New(t)

			if _, err := h.Service.SignIn(context.Background(), "owner@example.com"); err != nil {
				t.Fatalf("SignIn in %q: %v", t.Name(), err)
			}
		})
	}
}

// A harness database enforces foreign keys as the app's does: a Contributor
// can't be added to a Goal that doesn't exist.
func TestHarnessDatabaseEnforcesForeignKeys(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	acc := h.SignIn("owner@example.com")

	_, err := h.DB.Exec(`INSERT INTO contributors (goal_id, account_id, created_at) VALUES (999, ?, '2026-01-02T15:04:05Z')`, acc.ID)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Fatalf("insert Contributor for missing Goal: err = %v, want a foreign-key error", err)
	}
}
