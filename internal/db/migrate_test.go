package db_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
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
	t.Parallel()

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
	t.Parallel()

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

// A Dimension's values stored before the Admin set their order keep the
// alphabetical order they were listed in, as their starting order.
func TestMigrationStartsValueOrderAlphabetically(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0023_dimension_value_order.sql")
	if _, err := sqlDB.Exec(`ALTER TABLE dimension_values DROP COLUMN position`); err != nil {
		t.Fatalf("undo 0023: %v", err)
	}
	if _, err := sqlDB.Exec(`
		INSERT INTO dimensions (id, name, created_at) VALUES (1, 'Pillar', '2026-01-02T00:00:00Z'), (2, 'Quarter', '2026-01-02T00:00:00Z');
		INSERT INTO dimension_values (dimension_id, value, created_at) VALUES
			(1, 'Reliability', '2026-01-02T00:00:00Z'),
			(2, 'Q2', '2026-01-02T00:00:00Z'),
			(1, 'Growth', '2026-01-02T00:00:00Z'),
			(1, 'Efficiency', '2026-01-02T00:00:00Z'),
			(2, 'Q1', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("insert values: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT value FROM dimension_values ORDER BY dimension_id, position`)
	if err != nil {
		t.Fatalf("read values: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	want := []string{"Efficiency", "Growth", "Reliability", "Q1", "Q2"}
	if !slices.Equal(got, want) {
		t.Errorf("values in order = %v, want %v", got, want)
	}
}

// Highlights recorded while a Check-in could carry only one keep their id,
// Check-in, kind, note and timestamp on upgrade, and their Check-in can then
// take more (CONTEXT.md: Highlight).
func TestMigrationKeepsHighlightsAndAllowsSeveralPerCheckin(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0031_several_highlights.sql")
	// Put back the table as 0010 made it: one Highlight per Check-in.
	if _, err := sqlDB.Exec(`
		DROP TABLE highlights;
		CREATE TABLE highlights (
			id         INTEGER PRIMARY KEY,
			checkin_id INTEGER NOT NULL UNIQUE REFERENCES checkins(id),
			kind       TEXT    NOT NULL,
			note       TEXT    NOT NULL,
			created_at TEXT    NOT NULL
		);
		CREATE INDEX idx_highlights_checkin ON highlights (checkin_id);
		INSERT INTO highlights (id, checkin_id, kind, note, created_at) VALUES
			(4, 10, 'Insight', 'Retries masked the root cause.', '2026-01-02T00:00:00Z'),
			(7, 11, 'Miss', 'Missed the SLA.', '2026-01-09T00:00:00Z')`); err != nil {
		t.Fatalf("arrange 0010 highlights: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT id, checkin_id, kind, note, created_at FROM highlights ORDER BY id`)
	if err != nil {
		t.Fatalf("read highlights: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id, checkinID int64
		var kind, note, createdAt string
		if err := rows.Scan(&id, &checkinID, &kind, &note, &createdAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, fmt.Sprintf("%d %d %s %s %s", id, checkinID, kind, note, createdAt))
	}
	want := []string{
		"4 10 Insight Retries masked the root cause. 2026-01-02T00:00:00Z",
		"7 11 Miss Missed the SLA. 2026-01-09T00:00:00Z",
	}
	if !slices.Equal(got, want) {
		t.Errorf("highlights = %v, want %v", got, want)
	}

	if _, err := sqlDB.Exec(`INSERT INTO highlights (checkin_id, kind, note, created_at)
		VALUES (10, 'Accomplishment', 'Cut MTTR in half.', '2026-01-02T00:00:00Z')`); err != nil {
		t.Errorf("a second Highlight on the same Check-in: %v", err)
	}
}

// Milestone changes recorded before they kept the Milestone's name take the
// name it has on upgrade: nothing recorded an earlier one.
func TestMigrationNamesExistingMilestoneChanges(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0035_milestone_change_names.sql")
	// Put back the table as 0034 made it: no name.
	if _, err := sqlDB.Exec(`ALTER TABLE milestone_changes DROP COLUMN name`); err != nil {
		t.Fatalf("undo 0035: %v", err)
	}
	if _, err := sqlDB.Exec(`
		INSERT INTO milestones (id, goal_id, name, target_date, created_at) VALUES
			(1, 1, 'Beta', '2026-03-16', '2026-01-02T00:00:00Z'),
			(2, 1, 'GA', '2026-04-20', '2026-01-02T00:00:00Z');
		INSERT INTO milestone_changes (id, checkin_id, milestone_id, kind, reason, created_at) VALUES
			(3, 10, 2, 'Added', '', '2026-01-09T00:00:00Z'),
			(5, 11, 1, 'Done', '', '2026-01-16T00:00:00Z'),
			(6, 11, 2, 'Removed', 'descoped', '2026-01-16T00:00:00Z')`); err != nil {
		t.Fatalf("arrange 0034 milestone changes: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT id, kind, name FROM milestone_changes ORDER BY id`)
	if err != nil {
		t.Fatalf("read milestone changes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id int64
		var kind, name string
		if err := rows.Scan(&id, &kind, &name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, fmt.Sprintf("%d %s %s", id, kind, name))
	}
	want := []string{"3 Added GA", "5 Done Beta", "6 Removed GA"}
	if !slices.Equal(got, want) {
		t.Errorf("milestone changes = %v, want %v", got, want)
	}
}
