package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// arrangeAccountsAndGoals adds the Accounts (7 and 8) and Goals (1 and 9,
// owned by 7) that a test's rows point at, so they break no foreign key:
// Migrate refuses a database that holds an orphan.
func arrangeAccountsAndGoals(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.ExecContext(t.Context(), `
		INSERT INTO accounts (id, email, created_at) VALUES
			(7, 'pat@example.com', '2026-01-02T00:00:00Z'),
			(8, 'sam@example.com', '2026-01-02T00:00:00Z');
		INSERT INTO goals (id, title, why_it_matters, owner_id, lifecycle, created_at) VALUES
			(1, 'Reduce outages', 'why', 7, 'Active', '2026-01-02T00:00:00Z'),
			(9, 'Expand to the EU', 'why', 7, 'Active', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("arrange Accounts and Goals: %v", err)
	}
}

// arrangeHandoffs adds Handoffs of Goal 1 from Account 7 to Account 8, one per
// status, to a database arranged by arrangeAccountsAndGoals.
func arrangeHandoffs(t *testing.T, sqlDB *sql.DB, statuses ...string) error {
	t.Helper()
	for _, status := range statuses {
		if _, err := sqlDB.ExecContext(t.Context(), `INSERT INTO handoffs (goal_id, from_owner, to_owner, status, initiated_by, created_at)
			VALUES (1, 7, 8, ?, 7, '2026-01-02T00:00:00Z')`, status); err != nil {
			return err
		}
	}
	return nil
}

// Once migrated, a Goal has at most one pending Handoff, alongside any number
// with another outcome.
func TestOnlyOneHandoffOfAGoalIsPending(t *testing.T) {
	t.Parallel()

	sqlDB := openMigratedForFKCheck(t)
	arrangeAccountsAndGoals(t, sqlDB)
	if err := arrangeHandoffs(t, sqlDB, "pending"); err != nil {
		t.Fatalf("arrange the first pending Handoff: %v", err)
	}
	if err := arrangeHandoffs(t, sqlDB, "pending"); err == nil {
		t.Error("a second pending Handoff of the Goal was stored")
	}
	if err := arrangeHandoffs(t, sqlDB, "accepted", "rejected", "cancelled", "reassigned", "rejected"); err != nil {
		t.Errorf("a Handoff with another outcome was refused: %v", err)
	}
}

// uniqueIndexCases are the duplicates the baseline's unique indexes refuse,
// one per index. arrange adds the rows a duplicate points at, to a database
// arranged by arrangeAccountsAndGoals; first is a row, duplicate repeats it,
// and distinct is a row beside it that repeats nothing.
var uniqueIndexCases = []struct {
	table, arrange, first, duplicate, distinct string
}{
	{
		table: "metric_readings",
		arrange: `INSERT INTO metrics (id, goal_id, name, unit, direction, baseline, target, target_date, created_at) VALUES
				(1, 1, 'Uptime', '%', 'up', 90, 99, '2026-12-31', '2026-01-02T00:00:00Z'),
				(2, 1, 'Incidents', 'count', 'down', 10, 2, '2026-12-31', '2026-01-02T00:00:00Z');
			INSERT INTO checkins (id, goal_id, author_id, owner_id, health, status, created_at)
				VALUES (1, 1, 7, 7, 'Green', 'On track.', '2026-01-03T00:00:00Z')`,
		first:     `INSERT INTO metric_readings (checkin_id, metric_id, value, created_at) VALUES (1, 1, 95, '2026-01-03T00:00:00Z')`,
		duplicate: `INSERT INTO metric_readings (checkin_id, metric_id, value, created_at) VALUES (1, 1, 96, '2026-01-03T00:00:00Z')`,
		distinct:  `INSERT INTO metric_readings (checkin_id, metric_id, value, created_at) VALUES (1, 2, 4, '2026-01-03T00:00:00Z')`,
	},
	{
		table: "report_definition_fields",
		arrange: `INSERT INTO report_definitions (id, name, introduction, created_by, created_at)
				VALUES (1, 'Monthly review', '', 7, '2026-01-02T00:00:00Z');
			INSERT INTO fields (id, name, type, created_at) VALUES
				(1, 'Budget', 'number', '2026-01-02T00:00:00Z'),
				(2, 'Sponsor', 'short_text', '2026-01-02T00:00:00Z')`,
		first:     `INSERT INTO report_definition_fields (report_definition_id, field_id) VALUES (1, 1)`,
		duplicate: `INSERT INTO report_definition_fields (report_definition_id, field_id) VALUES (1, 1)`,
		distinct:  `INSERT INTO report_definition_fields (report_definition_id, field_id) VALUES (1, 2)`,
	},
	{
		table:     "dimension_values",
		arrange:   `INSERT INTO dimensions (id, name, created_at) VALUES (1, 'Pillar', '2026-01-02T00:00:00Z')`,
		first:     `INSERT INTO dimension_values (dimension_id, value, created_at) VALUES (1, 'Growth', '2026-01-02T00:00:00Z')`,
		duplicate: `INSERT INTO dimension_values (dimension_id, value, retired, created_at) VALUES (1, 'growth', 1, '2026-01-02T00:00:00Z')`,
		distinct:  `INSERT INTO dimension_values (dimension_id, value, created_at) VALUES (1, 'Efficiency', '2026-01-02T00:00:00Z')`,
	},
	{
		table:     "dimensions",
		first:     `INSERT INTO dimensions (name, created_at) VALUES ('Pillar', '2026-01-02T00:00:00Z')`,
		duplicate: `INSERT INTO dimensions (name, retired, created_at) VALUES ('pillar', 1, '2026-01-02T00:00:00Z')`,
		distinct:  `INSERT INTO dimensions (name, created_at) VALUES ('Quarter', '2026-01-02T00:00:00Z')`,
	},
	{
		table:     "fields",
		first:     `INSERT INTO fields (name, type, created_at) VALUES ('Budget', 'number', '2026-01-02T00:00:00Z')`,
		duplicate: `INSERT INTO fields (name, type, retired, created_at) VALUES ('budget', 'short_text', 1, '2026-01-02T00:00:00Z')`,
		distinct:  `INSERT INTO fields (name, type, created_at) VALUES ('Sponsor', 'short_text', '2026-01-02T00:00:00Z')`,
	},
}

// Once migrated, each table with one of the baseline's unique indexes refuses
// a duplicate, whatever its case where it holds a name or a value, and takes a
// row beside it that repeats nothing: another Metric in the same Check-in,
// another value in the same Dimension.
func TestTheUniqueIndexesRefuseADuplicate(t *testing.T) {
	t.Parallel()
	for _, tc := range uniqueIndexCases {
		t.Run(tc.table, func(t *testing.T) {
			t.Parallel()
			sqlDB := openMigratedForFKCheck(t)
			arrangeAccountsAndGoals(t, sqlDB)
			for _, stmt := range []string{tc.arrange, tc.first} {
				if _, err := sqlDB.ExecContext(t.Context(), stmt); err != nil {
					t.Fatalf("arrange %s: %v", tc.table, err)
				}
			}

			if _, err := sqlDB.ExecContext(t.Context(), tc.duplicate); err == nil {
				t.Errorf("a duplicate was stored in %s", tc.table)
			}
			if _, err := sqlDB.ExecContext(t.Context(), tc.distinct); err != nil {
				t.Errorf("a row repeating nothing was refused by %s: %v", tc.table, err)
			}
		})
	}
}

// openMigratedForFKCheck opens a database through db.Open, as the app does, and
// migrates it.
func openMigratedForFKCheck(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(t.Context(), sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return sqlDB
}

// insertOrphanedDelegates adds n Delegates to a Goal that doesn't exist,
// on one connection with foreign keys off, as a database written before they
// were enforced could hold.
func insertOrphanedDelegates(t *testing.T, sqlDB *sql.DB, n int) {
	t.Helper()
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	stmts := []string{
		`PRAGMA foreign_keys = OFF`,
		`INSERT INTO accounts (id, email, created_at) VALUES (1, 'owner@example.com', '2026-01-02T15:04:05Z')`,
	}
	for i := range n {
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO delegates (goal_id, account_id, created_at) VALUES (%d, 1, '2026-01-02T15:04:05Z')`, 1000+i))
	}
	stmts = append(stmts, `PRAGMA foreign_keys = ON`)
	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

// A database that already holds a row breaking a foreign key won't migrate, so
// the app refuses to start on it; the error names the row and its missing
// parent.
func TestMigrateRefusesADatabaseWithAnOrphanedRow(t *testing.T) {
	t.Parallel()
	sqlDB := openMigratedForFKCheck(t)
	insertOrphanedDelegates(t, sqlDB, 1)

	err := db.Migrate(t.Context(), sqlDB)
	if err == nil {
		t.Fatal("Migrate succeeded on a database with an orphaned Delegate")
	}
	for _, want := range []string{"delegates", "rowid 1", "goals"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Migrate error %q doesn't name %q", err, want)
		}
	}
}

// A failed foreign-key check names the first 20 rows that break one and counts
// the rest, so the error stays readable however many there are.
func TestMigrateNamesAtMostTwentyOrphanedRows(t *testing.T) {
	t.Parallel()
	sqlDB := openMigratedForFKCheck(t)
	insertOrphanedDelegates(t, sqlDB, 25)

	err := db.Migrate(t.Context(), sqlDB)
	if err == nil {
		t.Fatal("Migrate succeeded on a database with orphaned Delegates")
	}
	if got := strings.Count(err.Error(), "delegates rowid"); got != 20 {
		t.Errorf("Migrate error names %d rows, want 20: %v", got, err)
	}
	if !strings.Contains(err.Error(), "and 5 more") {
		t.Errorf("Migrate error %q doesn't count the 5 rows it leaves out", err)
	}
}

// Migrate turns foreign keys off on the connection it migrates on, and back on
// before the pool hands that connection out again.
func TestMigrateLeavesForeignKeysOnForTheConnectionItRanOn(t *testing.T) {
	t.Parallel()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	// One connection, so the one Migrate ran on is the one read next.
	sqlDB.SetMaxOpenConns(1)
	if err := db.Migrate(t.Context(), sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var on int
	if err := sqlDB.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if on != 1 {
		t.Errorf("PRAGMA foreign_keys = %d after Migrate, want 1", on)
	}
}

// Each migration takes a number of its own, so a new one can't quietly reuse
// one a migration written alongside it already took.
func TestMigrationsDoNotShareANumber(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	byNumber := map[string][]string{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		number, _, _ := strings.Cut(name, "_")
		byNumber[number] = append(byNumber[number], name)
	}
	for number, names := range byNumber {
		if len(names) < 2 {
			continue
		}
		t.Errorf("migrations %s share the number %s; give the newest the next free number", strings.Join(names, ", "), number)
	}
}

// migrateRefusalSnapshot reads what a refused Migrate must leave as it found
// it: every applied version, and every schema object with its SQL.
func migrateRefusalSnapshot(t *testing.T, sqlDB *sql.DB) (versions, schema []string) {
	t.Helper()
	for _, q := range []struct {
		dst   *[]string
		query string
	}{
		{&versions, `SELECT version FROM schema_migrations ORDER BY version`},
		{&schema, `SELECT type || ' ' || name || ' ' || tbl_name || ' ' || coalesce(sql, '') FROM sqlite_schema ORDER BY name`},
	} {
		rows, err := sqlDB.QueryContext(t.Context(), q.query)
		if err != nil {
			t.Fatalf("%s: %v", q.query, err)
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("scan: %v", err)
			}
			*q.dst = append(*q.dst, s)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("%s: %v", q.query, err)
		}
		_ = rows.Close()
	}
	return versions, schema
}

// A database that applied a migration this build doesn't hold, as one built
// by the chain the baseline replaced did, is refused before anything runs on
// it, with how to recreate it, and left as it was.
func TestMigrateRefusesADatabaseFromBeforeTheBaseline(t *testing.T) {
	t.Parallel()
	sqlDB := openMigratedForFKCheck(t)
	// Forget the baseline and claim an unknown version instead, so a Migrate
	// that only skipped what it knows would apply the baseline over it.
	if _, err := sqlDB.ExecContext(t.Context(), `
		DELETE FROM schema_migrations;
		INSERT INTO schema_migrations (version, applied_at) VALUES ('migrations/from-the-old-chain.sql', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("arrange an unknown version: %v", err)
	}
	versionsBefore, schemaBefore := migrateRefusalSnapshot(t, sqlDB)

	err := db.Migrate(t.Context(), sqlDB)
	if err == nil {
		t.Fatal("Migrate succeeded on a database that applied an unknown migration")
	}
	for _, want := range []string{"predates the baseline", "make seed", "migrations/from-the-old-chain.sql"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Migrate error %q doesn't say %q", err, want)
		}
	}
	versionsAfter, schemaAfter := migrateRefusalSnapshot(t, sqlDB)
	if !reflect.DeepEqual(versionsAfter, versionsBefore) {
		t.Errorf("schema_migrations after the refusal = %q, want %q", versionsAfter, versionsBefore)
	}
	if !reflect.DeepEqual(schemaAfter, schemaBefore) {
		t.Errorf("sqlite_schema changed on the refusal:\nbefore %q\nafter  %q", schemaBefore, schemaAfter)
	}
}
