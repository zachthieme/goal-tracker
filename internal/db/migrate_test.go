package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (ADR 0004)

	"github.com/zachthieme/goal-tracker/internal/db"
)

// migratedExcept returns a database migrated up to, but not including,
// version, so a test can arrange the rows that migration finds on upgrade.
func migratedExcept(t *testing.T, version string) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	res, err := sqlDB.Exec(`DELETE FROM schema_migrations WHERE version = ?`, version)
	if err != nil {
		t.Fatalf("forget %s: %v", version, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("no migration %s to forget", version)
	}
	return sqlDB
}

// An email stored before emails were matched whatever their case is folded
// on upgrade, so it is found by the lookup (CONTEXT.md: Account).
func TestMigrationLowercasesAndTrimsExistingEmails(t *testing.T) {
	sqlDB := migratedExcept(t, "migrations/0020_lowercase_account_emails.sql")
	if _, err := sqlDB.Exec(
		`INSERT INTO accounts (email, created_at) VALUES (' Freya.Nilsen@Example.com ', '2026-01-02T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var email string
	if err := sqlDB.QueryRow(`SELECT email FROM accounts`).Scan(&email); err != nil {
		t.Fatalf("read email: %v", err)
	}
	if email != "freya.nilsen@example.com" {
		t.Errorf("email = %q, want freya.nilsen@example.com", email)
	}
}

// The migration merges nothing: two Accounts that differ only by case fail it,
// and it changes no row, so an operator can resolve the duplicate by hand.
func TestMigrationFailsOnAccountsDifferingOnlyByCase(t *testing.T) {
	sqlDB := migratedExcept(t, "migrations/0020_lowercase_account_emails.sql")
	if _, err := sqlDB.Exec(`INSERT INTO accounts (email, created_at) VALUES
		('sam@example.com', '2026-01-02T00:00:00Z'),
		('Sam@Example.com', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	if err := db.Migrate(sqlDB); err == nil {
		t.Fatal("migrate: want an error for emails that collide once lowercased")
	}

	var mixed int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM accounts WHERE email = 'Sam@Example.com'`).Scan(&mixed); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if mixed != 1 {
		t.Error("the failed migration changed the accounts")
	}
}
