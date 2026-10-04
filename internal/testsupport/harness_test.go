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
