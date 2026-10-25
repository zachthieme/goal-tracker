package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (ADR 0004)

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
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

// arrangeUpgradeAccountsAndGoals adds the Accounts (7 and 8) and Goals (1 and
// 9, owned by 7) that an upgrade test's rows point at, so they break no
// foreign key: Migrate refuses a database that holds an orphan.
func arrangeUpgradeAccountsAndGoals(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.Exec(`
		INSERT INTO accounts (id, email, created_at) VALUES
			(7, 'pat@example.com', '2026-01-02T00:00:00Z'),
			(8, 'sam@example.com', '2026-01-02T00:00:00Z');
		INSERT INTO goals (id, title, why_it_matters, owner_id, lifecycle, created_at) VALUES
			(1, 'Reduce outages', 'why', 7, 'Active', '2026-01-02T00:00:00Z'),
			(9, 'Expand to the EU', 'why', 7, 'Active', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("arrange Accounts and Goals: %v", err)
	}
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
		CREATE INDEX idx_highlights_checkin ON highlights (checkin_id);`); err != nil {
		t.Fatalf("undo 0031: %v", err)
	}
	arrangeUpgradeAccountsAndGoals(t, sqlDB)
	if _, err := sqlDB.Exec(`
		INSERT INTO checkins (id, goal_id, author_id, owner_id, health, status, created_at) VALUES
			(10, 1, 7, 7, 'Green', 'On track.', '2026-01-02T00:00:00Z'),
			(11, 1, 7, 7, 'Red', 'Missed.', '2026-01-09T00:00:00Z');
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

// Comments made on a Report before a Comment could be made on the Goal itself
// keep every column on upgrade, and the table no longer claims every Comment
// is on a publication (CONTEXT.md: Comment).
func TestMigrationKeepsReportCommentsAndFreesThePublication(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0049_goal_comments.sql")
	// Put back the table as 0016 made it: every Comment on a publication.
	if _, err := sqlDB.Exec(`
		DROP TABLE comments;
		CREATE TABLE comments (
			id             INTEGER PRIMARY KEY,
			publication_id INTEGER NOT NULL REFERENCES report_publications(id),
			goal_id        INTEGER NOT NULL REFERENCES goals(id),
			parent_id      INTEGER NOT NULL DEFAULT 0,
			author_id      INTEGER NOT NULL REFERENCES accounts(id),
			body           TEXT    NOT NULL,
			created_at     TEXT    NOT NULL
		);
		CREATE INDEX idx_comments_publication ON comments (publication_id);`); err != nil {
		t.Fatalf("undo 0049: %v", err)
	}
	arrangeUpgradeAccountsAndGoals(t, sqlDB)
	if _, err := sqlDB.Exec(`
		INSERT INTO report_definitions (id, name, introduction, created_by, created_at) VALUES
			(1, 'MBR', '', 7, '2026-01-02T00:00:00Z');
		INSERT INTO report_publications (id, report_definition_id, published_by, published_at, snapshot) VALUES
			(2, 1, 7, '2026-01-08T00:00:00Z', '{}');
		INSERT INTO comments (id, publication_id, goal_id, parent_id, author_id, body, created_at) VALUES
			(4, 2, 9, 0, 7, 'Second vendor quote?', '2026-01-09T00:00:00Z'),
			(6, 2, 9, 4, 8, 'Asked on Monday.', '2026-01-10T00:00:00Z')`); err != nil {
		t.Fatalf("arrange 0016 comments: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT id, publication_id, goal_id, parent_id, author_id, body, created_at
		FROM comments ORDER BY id`)
	if err != nil {
		t.Fatalf("read comments: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id, pubID, goalID, parentID, authorID int64
		var body, createdAt string
		if err := rows.Scan(&id, &pubID, &goalID, &parentID, &authorID, &body, &createdAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, fmt.Sprintf("%d pub %d goal %d parent %d author %d %s %s",
			id, pubID, goalID, parentID, authorID, body, createdAt))
	}
	want := []string{
		"4 pub 2 goal 9 parent 0 author 7 Second vendor quote? 2026-01-09T00:00:00Z",
		"6 pub 2 goal 9 parent 4 author 8 Asked on Monday. 2026-01-10T00:00:00Z",
	}
	if !slices.Equal(got, want) {
		t.Errorf("comments = %v, want %v", got, want)
	}

	var schema string
	if err := sqlDB.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'comments'`).Scan(&schema); err != nil {
		t.Fatalf("read comments schema: %v", err)
	}
	if strings.Contains(schema, "REFERENCES report_publications") {
		t.Errorf("comments still references report_publications: %s", schema)
	}
	for _, index := range []string{"idx_comments_publication", "idx_comments_goal"} {
		var n int
		if err := sqlDB.QueryRow(`SELECT count(*) FROM sqlite_master
			WHERE type = 'index' AND name = ? AND tbl_name = 'comments'`, index).Scan(&n); err != nil {
			t.Fatalf("look up %s: %v", index, err)
		}
		if n != 1 {
			t.Errorf("index %s missing on comments", index)
		}
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
	arrangeUpgradeAccountsAndGoals(t, sqlDB)
	if _, err := sqlDB.Exec(`
		INSERT INTO checkins (id, goal_id, author_id, owner_id, health, status, created_at) VALUES
			(10, 1, 7, 7, 'Green', 'On track.', '2026-01-09T00:00:00Z'),
			(11, 1, 7, 7, 'Yellow', 'Slipping.', '2026-01-16T00:00:00Z');
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

// An Action Item raised from a Comment before Action Items could name a Goal
// is about that Comment's Goal on upgrade; one raised directly stays about
// the Report as a whole (CONTEXT.md: Action Item).
func TestMigrationGivesExistingActionItemsTheirCommentsGoal(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0047_action_item_goals.sql")
	// Put back the table as 0046 left it: no Goal. SQLite won't drop an
	// indexed column, so the indexes go first.
	if _, err := sqlDB.Exec(`
		DROP INDEX idx_action_items_goal;
		DROP INDEX idx_action_items_owner;
		ALTER TABLE action_items DROP COLUMN goal_id`); err != nil {
		t.Fatalf("undo 0047: %v", err)
	}
	arrangeUpgradeAccountsAndGoals(t, sqlDB)
	if _, err := sqlDB.Exec(`
		INSERT INTO report_definitions (id, name, introduction, created_by, created_at) VALUES
			(1, 'MBR', '', 7, '2026-01-02T00:00:00Z');
		INSERT INTO report_publications (id, report_definition_id, published_by, published_at, snapshot) VALUES
			(2, 1, 7, '2026-01-08T00:00:00Z', '{}');
		INSERT INTO comments (id, publication_id, goal_id, parent_id, author_id, body, created_at) VALUES
			(4, 2, 9, 0, 7, 'Second vendor quote?', '2026-01-09T00:00:00Z');
		INSERT INTO action_items (id, report_definition_id, publication_id, comment_id, text, owner_id, due_date, created_by, created_at) VALUES
			(1, 1, 2, 4, 'Second vendor quote?', 7, '2026-02-01', 7, '2026-01-09T00:00:00Z'),
			(2, 1, 2, 0, 'Book the EU legal review.', 7, '2026-02-01', 7, '2026-01-09T00:00:00Z')`); err != nil {
		t.Fatalf("arrange 0046 action items: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT id, goal_id FROM action_items ORDER BY id`)
	if err != nil {
		t.Fatalf("read action items: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id, goalID int64
		if err := rows.Scan(&id, &goalID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, fmt.Sprintf("%d goal %d", id, goalID))
	}
	want := []string{"1 goal 9", "2 goal 0"}
	if !slices.Equal(got, want) {
		t.Errorf("action items = %v, want %v", got, want)
	}
}

// Milestone changes recorded before a Milestone could be added outside a
// Check-in keep every column on upgrade and take their author from their
// Check-in. Afterwards a change needs no Check-in.
func TestMigrationKeepsMilestoneChangesAndBackfillsTheirAuthor(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0042_milestone_changes_outside_checkins.sql")
	// Put back the table as 0034 and 0035 made it: every change in a Check-in,
	// with no author of its own.
	if _, err := sqlDB.Exec(`
		DROP TABLE milestone_changes;
		CREATE TABLE milestone_changes (
			id           INTEGER PRIMARY KEY,
			checkin_id   INTEGER NOT NULL REFERENCES checkins(id),
			milestone_id INTEGER NOT NULL REFERENCES milestones(id),
			kind         TEXT    NOT NULL,
			reason       TEXT    NOT NULL DEFAULT '',
			created_at   TEXT    NOT NULL,
			name         TEXT    NOT NULL DEFAULT ''
		);
		CREATE INDEX idx_milestone_changes_checkin ON milestone_changes (checkin_id);`); err != nil {
		t.Fatalf("undo 0042: %v", err)
	}
	arrangeUpgradeAccountsAndGoals(t, sqlDB)
	if _, err := sqlDB.Exec(`
		INSERT INTO milestones (id, goal_id, name, target_date, created_at) VALUES
			(1, 1, 'Beta', '2026-03-16', '2026-01-02T00:00:00Z'),
			(2, 1, 'GA', '2026-04-20', '2026-01-02T00:00:00Z');
		INSERT INTO checkins (id, goal_id, author_id, owner_id, health, status, created_at) VALUES
			(10, 1, 7, 7, 'Green', 'On track.', '2026-01-09T00:00:00Z'),
			(11, 1, 8, 7, 'Yellow', 'Slipping.', '2026-01-16T00:00:00Z');
		INSERT INTO milestone_changes (id, checkin_id, milestone_id, kind, reason, created_at, name) VALUES
			(3, 10, 2, 'Added', '', '2026-01-09T00:00:00Z', 'GA'),
			(5, 11, 1, 'Done', '', '2026-01-16T00:00:00Z', 'Beta'),
			(6, 11, 2, 'Removed', 'descoped', '2026-01-16T00:00:00Z', 'GA')`); err != nil {
		t.Fatalf("arrange 0035 milestone changes: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT id, checkin_id, milestone_id, kind, reason, created_at, name, author_id
		FROM milestone_changes ORDER BY id`)
	if err != nil {
		t.Fatalf("read milestone changes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var id, checkinID, milestoneID int64
		var kind, reason, createdAt, name string
		var authorID sql.NullInt64
		if err := rows.Scan(&id, &checkinID, &milestoneID, &kind, &reason, &createdAt, &name, &authorID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		author := "none"
		if authorID.Valid {
			author = strconv.FormatInt(authorID.Int64, 10)
		}
		got = append(got, fmt.Sprintf("%d %d %d %s %q %s %s by %s", id, checkinID, milestoneID, kind, reason, createdAt, name, author))
	}
	want := []string{
		`3 10 2 Added "" 2026-01-09T00:00:00Z GA by 7`,
		`5 11 1 Done "" 2026-01-16T00:00:00Z Beta by 8`,
		`6 11 2 Removed "descoped" 2026-01-16T00:00:00Z GA by 8`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("milestone changes = %v, want %v", got, want)
	}

	if _, err := sqlDB.Exec(`INSERT INTO milestone_changes (milestone_id, kind, name, author_id, created_at)
		VALUES (1, 'Added', 'Launch', 7, '2026-01-30T00:00:00Z')`); err != nil {
		t.Errorf("a Milestone change with no Check-in: %v", err)
	}
}

// Links, rejected requests and removals from before link changes were
// logged are back-filled into the log once on upgrade, with the times and
// people their rows recorded. A link stands for its request, or for being
// linked if it is Accepted; an Undo is put down to whoever rejected or removed
// the link, the only one who may undo it.
func TestMigrationBackfillsLinkEvents(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0038_link_events.sql")
	if _, err := sqlDB.Exec(`DROP TABLE link_events`); err != nil {
		t.Fatalf("undo 0038: %v", err)
	}
	h := &testsupport.Harness{T: t, DB: sqlDB, Clock: clock.NewFixed(testsupport.Epoch), Email: email.NewRecorder()}
	h.Service = domain.NewService(sqlDB, h.Clock, h.Email, nil)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	parent := h.CreateGoal(pat, "Reduce outages", "why")
	accepted := h.CreateGoal(sam, "Accepted", "why")
	pending := h.CreateGoal(sam, "Pending", "why")
	rejected := h.CreateGoal(sam, "Rejected", "why")
	unrejected := h.CreateGoal(sam, "Rejected, then undone", "why")
	removed := h.CreateGoal(sam, "Removed", "why")
	unremoved := h.CreateGoal(sam, "Removed, then undone", "why")
	if _, err := sqlDB.Exec(`
		INSERT INTO links (child_id, parent_id, status, note, requested_by, created_at) VALUES
			(?3, ?1, 'accepted', '', ?9, '2026-01-02T09:00:00Z'),
			(?4, ?1, 'pending', '', ?9, '2026-01-03T09:00:00Z'),
			(?6, ?1, 'pending', '', ?9, '2026-01-05T09:00:00Z'),
			(?8, ?1, 'accepted', '', ?9, '2026-01-07T09:00:00Z');
		INSERT INTO rejected_link_requests (child_id, parent_id, requested_by, request_created_at, rejected_by, rejected_at, restored_at) VALUES
			(?5, ?1, ?9, '2026-01-04T09:00:00Z', ?2, '2026-01-04T10:00:00Z', NULL),
			(?6, ?1, ?9, '2026-01-05T09:00:00Z', ?2, '2026-01-05T10:00:00Z', '2026-01-05T10:01:00Z');
		INSERT INTO link_removals (child_id, parent_id, requested_by, link_created_at, removed_by, removed_at, restored_at) VALUES
			(?7, ?1, ?9, '2026-01-06T09:00:00Z', ?2, '2026-01-06T10:00:00Z', NULL),
			(?8, ?1, ?9, '2026-01-07T09:00:00Z', ?9, '2026-01-07T10:00:00Z', '2026-01-07T10:01:00Z');`,
		parent.ID, pat.ID, accepted.ID, pending.ID, rejected.ID, unrejected.ID, removed.ID, unremoved.ID, sam.ID,
	); err != nil {
		t.Fatalf("arrange links: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, c := range []struct {
		child domain.Goal
		want  []string
	}{
		{accepted, []string{"linked by sam at 2026-01-02T09:00:00Z"}},
		{pending, []string{"requested by sam at 2026-01-03T09:00:00Z"}},
		{rejected, []string{
			"requested by sam at 2026-01-04T09:00:00Z",
			"rejected by pat at 2026-01-04T10:00:00Z",
		}},
		{unrejected, []string{
			"requested by sam at 2026-01-05T09:00:00Z",
			"rejected by pat at 2026-01-05T10:00:00Z",
			"rejection-undone by pat at 2026-01-05T10:01:00Z",
		}},
		{removed, []string{
			"linked by sam at 2026-01-06T09:00:00Z",
			"removed by pat at 2026-01-06T10:00:00Z",
		}},
		{unremoved, []string{
			"linked by sam at 2026-01-07T09:00:00Z",
			"removed by sam at 2026-01-07T10:00:00Z",
			"removal-undone by sam at 2026-01-07T10:01:00Z",
		}},
	} {
		events, err := h.Service.LinkEvents(context.Background(), c.child.ID)
		if err != nil {
			t.Fatalf("LinkEvents: %v", err)
		}
		var got []string
		for _, e := range events {
			if e.ParentID != parent.ID {
				t.Errorf("%s: event %+v isn't of its link to the parent", c.child.Title, e)
			}
			who := map[int64]string{pat.ID: "pat", sam.ID: "sam"}[e.Actor.ID]
			got = append(got, fmt.Sprintf("%s by %s at %s", e.Kind, who, e.CreatedAt.UTC().Format(time.RFC3339)))
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: link events = %v, want %v", c.child.Title, got, c.want)
		}
	}
}

// Report Definitions saved as roots, Depth and filters convert on upgrade to
// select the same Goals, never by following links again (ADR 0007): filters
// alone become Report rules, and roots become the Goals picked, frozen to the
// Goals the walk to Depth and the filters select now. Goals Done or Cancelled
// before the baseline drop out, as they do from every Report.
func TestMigrationConvertsReportDefinitionsToRulesAndPickedGoals(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0039_report_rules.sql")
	// Put back what 0039 replaces: 0008's roots, filters, depth and Owner
	// filter.
	if _, err := sqlDB.Exec(`
		DROP TABLE report_definition_goals;
		DROP TABLE report_rule_values;
		DROP TABLE report_rules;
		ALTER TABLE report_definitions DROP COLUMN mode;
		ALTER TABLE report_definitions ADD COLUMN depth INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE report_definitions ADD COLUMN owner_filter_id INTEGER NOT NULL DEFAULT 0;
		CREATE TABLE report_definition_roots (
			id                   INTEGER PRIMARY KEY,
			report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
			goal_id              INTEGER NOT NULL REFERENCES goals(id)
		);
		CREATE TABLE report_definition_filters (
			id                   INTEGER PRIMARY KEY,
			report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
			dimension_value_id   INTEGER NOT NULL REFERENCES dimension_values(id)
		);`); err != nil {
		t.Fatalf("undo 0039: %v", err)
	}
	h := &testsupport.Harness{T: t, DB: sqlDB, Clock: clock.NewFixed(testsupport.Epoch), Email: email.NewRecorder()}
	h.Service = domain.NewService(sqlDB, h.Clock, h.Email, []string{"boss@example.com"})
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	team := h.CreateDimension(boss, "Team", "Core", "Web")
	growth, trust, core, web := pillar.Values[0], pillar.Values[1], team.Values[0], team.Values[1]

	// root <- grower, truster, samsGrower, finished, pending (not accepted);
	// grower <- grandchild; loner is linked to nothing.
	root := h.ActiveGoal(boss, "Grow revenue", "why")
	grower := h.ActiveChildOf(boss, root, "Launch in EU", "why")
	truster := h.ActiveChildOf(boss, root, "Earn trust", "why")
	samsGrower := h.ActiveGoal(sam, "Grow EU revenue", "why")
	link := h.RequestLink(sam, samsGrower, root, "")
	if _, err := h.Service.AcceptLink(context.Background(), link.ID, boss.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	finished := h.ActiveChildOf(boss, root, "Old launch", "why")
	pending := h.ActiveGoal(sam, "Pending link", "why")
	h.RequestLink(sam, pending, root, "")
	grandchild := h.ActiveChildOf(boss, grower, "Localize checkout", "why")
	loner := h.ActiveGoal(boss, "Hire in EU", "why")
	for _, gv := range []struct {
		g domain.Goal
		v []domain.DimensionValue
	}{
		{root, []domain.DimensionValue{growth, core}},
		{grower, []domain.DimensionValue{growth, core}},
		{truster, []domain.DimensionValue{trust, core}},
		{samsGrower, []domain.DimensionValue{growth, core}},
		{finished, []domain.DimensionValue{growth, core}},
		{pending, []domain.DimensionValue{growth, core}},
		{grandchild, []domain.DimensionValue{growth}},
		{loner, []domain.DimensionValue{growth, web}},
	} {
		for _, v := range gv.v {
			h.AssignGoalValue(gv.g, v)
		}
	}
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: finished.ID, AuthorID: boss.ID, Status: "Shipped.", Lifecycle: domain.LifecycleDone, Outcome: "Live.",
	}); err != nil {
		t.Fatalf("mark Done: %v", err)
	}
	h.Clock.Advance(40 * 24 * time.Hour)

	if _, err := sqlDB.Exec(`
		INSERT INTO report_definitions (id, name, introduction, depth, owner_filter_id, created_by, created_at) VALUES
			(1, 'Filters only', '', 0, ?1, ?1, '2026-01-02T00:00:00Z'),
			(2, 'Depth 0', '', 0, ?1, ?1, '2026-01-02T00:00:00Z'),
			(3, 'Depth 1', '', 1, 0, ?1, '2026-01-02T00:00:00Z'),
			(4, 'Depth 0, all filtered out', '', 0, 0, ?1, '2026-01-02T00:00:00Z');
		INSERT INTO report_definition_filters (report_definition_id, dimension_value_id) VALUES
			(1, ?2), (1, ?3), (1, ?4),
			(2, ?2),
			(3, ?2), (3, ?3),
			(4, ?2);
		INSERT INTO report_definition_roots (report_definition_id, goal_id) VALUES
			(2, ?5), (2, ?6), (2, ?7),
			(3, ?5),
			(4, ?6);`,
		boss.ID, growth.ID, core.ID, web.ID, root.ID, truster.ID, samsGrower.ID,
	); err != nil {
		t.Fatalf("arrange report definitions: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	for _, c := range []struct {
		defID int64
		mode  string
		rules []domain.ReportRule
		want  []int64
	}{
		// Owner boss, carrying Growth, and Core or Web.
		{1, domain.ReportModeRules, []domain.ReportRule{
			{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{id(boss.ID)}},
			{Attribute: domain.RuleDimension, DimensionID: pillar.ID, Op: domain.RuleIsAnyOf, Values: []string{id(growth.ID)}},
			{Attribute: domain.RuleDimension, DimensionID: team.ID, Op: domain.RuleIsAnyOf, Values: []string{id(core.ID), id(web.ID)}},
		}, []int64{root.ID, grower.ID, loner.ID}},
		// The roots owned by boss carrying Growth.
		{2, domain.ReportModePicked, nil, []int64{root.ID}},
		// The root and its accepted children carrying Growth and Core.
		{3, domain.ReportModePicked, nil, []int64{root.ID, grower.ID, samsGrower.ID}},
		{4, domain.ReportModePicked, nil, nil},
	} {
		def, err := h.Service.GetReportDefinition(context.Background(), c.defID)
		if err != nil {
			t.Fatalf("GetReportDefinition %d: %v", c.defID, err)
		}
		if def.Mode != c.mode || !reflect.DeepEqual(def.Rules, c.rules) {
			t.Errorf("%s converted to %s with rules %+v, want %s with %+v", def.Name, def.Mode, def.Rules, c.mode, c.rules)
		}
		if got := draftGoals(t, h, def); !sameIDs(got, c.want) {
			t.Errorf("%s selects %v, want %v", def.Name, got, c.want)
		}
	}

	// The rules keep following the Goals' values.
	h.AssignGoalValue(grandchild, web)
	def, err := h.Service.GetReportDefinition(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetReportDefinition: %v", err)
	}
	if got, want := draftGoals(t, h, def), []int64{root.ID, grower.ID, grandchild.ID, loner.ID}; !sameIDs(got, want) {
		t.Errorf("once the grandchild is on Web, Filters only selects %v, want %v", got, want)
	}

	for _, gone := range []string{"report_definition_roots", "report_definition_filters"} {
		var n int
		if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, gone).Scan(&n); err != nil || n != 0 {
			t.Errorf("table %s is still there (%v)", gone, err)
		}
	}
	for _, gone := range []string{"depth", "owner_filter_id"} {
		var n int
		if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('report_definitions') WHERE name = ?`, gone).Scan(&n); err != nil || n != 0 {
			t.Errorf("column report_definitions.%s is still there (%v)", gone, err)
		}
	}
}

// draftGoals returns the ids of the Goals def's draft shows against its
// default baseline.
func draftGoals(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition) []int64 {
	t.Helper()
	r, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport %s: %v", def.Name, err)
	}
	var ids []int64
	for _, b := range r.Exceptions {
		ids = append(ids, b.Goal.ID)
	}
	for _, sg := range r.Lines {
		ids = append(ids, sg.Goal.ID)
	}
	return ids
}

// sameIDs reports whether got and want hold the same ids, each once.
func sameIDs(got, want []int64) bool {
	return slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want)))
}

// Draft notes written while a narrative section held at most one become their
// section's first note on upgrade, and the section can then take more
// (CONTEXT.md: Report).
func TestMigrationKeepsDraftNotesAndAllowsSeveralPerSection(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0040_several_narrative_notes.sql")
	// Put back the table as 0018 made it: one note per Definition and section.
	if _, err := sqlDB.Exec(`
		DROP TABLE narrative_texts;
		CREATE TABLE narrative_texts (
			report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
			section              TEXT    NOT NULL,
			text                 TEXT    NOT NULL,
			PRIMARY KEY (report_definition_id, section)
		);`); err != nil {
		t.Fatalf("undo 0040: %v", err)
	}
	h := &testsupport.Harness{T: t, DB: sqlDB, Clock: clock.NewFixed(testsupport.Epoch), Email: email.NewRecorder()}
	h.Service = domain.NewService(sqlDB, h.Clock, h.Email, []string{"boss@example.com"})
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "why")
	mbr := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	qbr := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "QBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	if _, err := sqlDB.Exec(`INSERT INTO narrative_texts (report_definition_id, section, text) VALUES
		(?1, 'Miss', 'Slipped a week.'),
		(?1, 'Insight', 'Pricing drives churn.'),
		(?2, 'Accomplishment', 'EU is open for business.')`, mbr.ID, qbr.ID); err != nil {
		t.Fatalf("arrange 0018 notes: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for def, want := range map[*domain.ReportDefinition]map[string][]string{
		&mbr: {domain.HighlightInsight: {"Pricing drives churn."}, domain.HighlightMiss: {"Slipped a week."}},
		&qbr: {domain.HighlightAccomplishment: {"EU is open for business."}},
	} {
		if got := draftNotes(t, h, *def); !reflect.DeepEqual(got, want) {
			t.Errorf("%s draft notes = %v, want %v", def.Name, got, want)
		}
	}

	if err := h.Service.CurateNarrative(context.Background(), boss.ID, mbr.ID, domain.CurateNarrativeInput{
		Notes: map[string][]string{domain.HighlightInsight: {"Pricing drives churn.", "Discounts don't save accounts."}},
	}); err != nil {
		t.Fatalf("a second note in the same section: %v", err)
	}
	want := map[string][]string{domain.HighlightInsight: {"Pricing drives churn.", "Discounts don't save accounts."}}
	if got := draftNotes(t, h, mbr); !reflect.DeepEqual(got, want) {
		t.Errorf("MBR draft notes after adding one = %v, want %v", got, want)
	}
}

// draftNotes returns the author's notes in the Report Definition's draft
// narrative, keyed by section.
func draftNotes(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition) map[string][]string {
	t.Helper()
	r, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	out := map[string][]string{}
	for _, sec := range r.Narrative {
		out[sec.Kind] = sec.Notes
	}
	return out
}

// A Goal's lead text and its revisions, stored under the old names, survive
// the rename to Why it matters (CONTEXT.md: Why it matters; ticket #381).
func TestMigrationKeepsAGoalsLeadTextAndRevisionsAsWhyItMatters(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0053_why_it_matters.sql")
	if _, err := sqlDB.Exec(`
		DROP INDEX idx_why_it_matters_revisions_goal;
		ALTER TABLE why_it_matters_revisions RENAME COLUMN why_it_matters TO so_what;
		ALTER TABLE why_it_matters_revisions RENAME TO so_what_revisions;
		CREATE INDEX idx_so_what_revisions_goal ON so_what_revisions (goal_id);
		ALTER TABLE goals RENAME COLUMN why_it_matters TO so_what;`); err != nil {
		t.Fatalf("undo 0053: %v", err)
	}
	if _, err := sqlDB.Exec(`
		INSERT INTO accounts (id, email, created_at) VALUES (1, 'priya@example.com', '2026-01-02T00:00:00Z');
		INSERT INTO goals (id, title, so_what, owner_id, lifecycle, created_at)
			VALUES (1, 'Launch in EU', 'EU shoppers can buy from us.', 1, 'Proposed', '2026-01-02T00:00:00Z');
		INSERT INTO so_what_revisions (goal_id, so_what, author_id, created_at) VALUES
			(1, 'EU shoppers cannot buy.', 1, '2026-01-02T00:00:00Z'),
			(1, 'EU shoppers can buy from us.', 1, '2026-01-03T00:00:00Z');`); err != nil {
		t.Fatalf("arrange rows under the old names: %v", err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var lead string
	if err := sqlDB.QueryRow(`SELECT why_it_matters FROM goals WHERE id = 1`).Scan(&lead); err != nil {
		t.Fatalf("read why_it_matters: %v", err)
	}
	if lead != "EU shoppers can buy from us." {
		t.Errorf("why_it_matters = %q, want the lead text it had", lead)
	}
	rows, err := sqlDB.Query(`SELECT why_it_matters FROM why_it_matters_revisions WHERE goal_id = 1 ORDER BY created_at`)
	if err != nil {
		t.Fatalf("read revisions: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var revisions []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatalf("scan revision: %v", err)
		}
		revisions = append(revisions, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("revisions: %v", err)
	}
	if want := []string{"EU shoppers cannot buy.", "EU shoppers can buy from us."}; !slices.Equal(revisions, want) {
		t.Errorf("revisions = %q, want %q", revisions, want)
	}
	var indexes int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_why_it_matters_revisions_goal'`).Scan(&indexes); err != nil {
		t.Fatalf("read index: %v", err)
	}
	if indexes != 1 {
		t.Error("why_it_matters_revisions has no goal_id index")
	}
}

// arrangeHandoffs adds Handoffs of Goal 1 from Account 7 to Account 8, one per
// status, to a database arranged by arrangeUpgradeAccountsAndGoals.
func arrangeHandoffs(t *testing.T, sqlDB *sql.DB, statuses ...string) error {
	t.Helper()
	for _, status := range statuses {
		if _, err := sqlDB.Exec(`INSERT INTO handoffs (goal_id, from_owner, to_owner, status, initiated_by, created_at)
			VALUES (1, 7, 8, ?, 7, '2026-01-02T00:00:00Z')`, status); err != nil {
			return err
		}
	}
	return nil
}

// A database holding two pending Handoffs of one Goal, as two started together
// could leave, won't migrate: the error names the handoffs table and both
// Handoffs are kept, so an operator can settle which stays by hand.
func TestMigrationFailsOnTwoPendingHandoffsOfOneGoal(t *testing.T) {
	t.Parallel()

	sqlDB := migratedExcept(t, "migrations/0059_one_pending_handoff.sql")
	// Back to the schema before 0059, which held no index on pending Handoffs.
	if _, err := sqlDB.Exec(`DROP INDEX idx_handoffs_one_pending`); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	arrangeUpgradeAccountsAndGoals(t, sqlDB)
	if err := arrangeHandoffs(t, sqlDB, "pending", "pending"); err != nil {
		t.Fatalf("arrange handoffs: %v", err)
	}

	err := db.Migrate(sqlDB)
	if err == nil {
		t.Fatal("Migrate succeeded on a database with two pending Handoffs of one Goal")
	}
	if !strings.Contains(err.Error(), "handoffs") {
		t.Errorf("Migrate error %q doesn't name the handoffs table", err)
	}
	var pending int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM handoffs WHERE status = 'pending'`).Scan(&pending); err != nil {
		t.Fatalf("count handoffs: %v", err)
	}
	if pending != 2 {
		t.Errorf("%d pending Handoffs after the failed migration, want both kept", pending)
	}
}

// Once migrated, a Goal has at most one pending Handoff, alongside any number
// with another outcome.
func TestOnlyOneHandoffOfAGoalIsPending(t *testing.T) {
	t.Parallel()

	sqlDB := openMigratedForFKCheck(t)
	arrangeUpgradeAccountsAndGoals(t, sqlDB)
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

// uniqueIndexCases are the duplicates migration 0060's unique indexes refuse,
// one per index. arrange adds the rows a duplicate points at, to a database
// arranged by arrangeUpgradeAccountsAndGoals; first is a row, duplicate
// repeats it, and distinct is a row beside it that repeats nothing. count
// counts the row and its duplicate.
var uniqueIndexCases = []struct {
	table, arrange, first, duplicate, distinct, count string
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
		count:     `SELECT COUNT(*) FROM metric_readings WHERE checkin_id = 1 AND metric_id = 1`,
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
		count:     `SELECT COUNT(*) FROM report_definition_fields WHERE report_definition_id = 1 AND field_id = 1`,
	},
	{
		table:     "dimension_values",
		arrange:   `INSERT INTO dimensions (id, name, created_at) VALUES (1, 'Pillar', '2026-01-02T00:00:00Z')`,
		first:     `INSERT INTO dimension_values (dimension_id, value, created_at) VALUES (1, 'Growth', '2026-01-02T00:00:00Z')`,
		duplicate: `INSERT INTO dimension_values (dimension_id, value, retired, created_at) VALUES (1, 'growth', 1, '2026-01-02T00:00:00Z')`,
		distinct:  `INSERT INTO dimension_values (dimension_id, value, created_at) VALUES (1, 'Efficiency', '2026-01-02T00:00:00Z')`,
		count:     `SELECT COUNT(*) FROM dimension_values WHERE dimension_id = 1 AND lower(value) = 'growth'`,
	},
	{
		table:     "dimensions",
		first:     `INSERT INTO dimensions (name, created_at) VALUES ('Pillar', '2026-01-02T00:00:00Z')`,
		duplicate: `INSERT INTO dimensions (name, retired, created_at) VALUES ('pillar', 1, '2026-01-02T00:00:00Z')`,
		distinct:  `INSERT INTO dimensions (name, created_at) VALUES ('Quarter', '2026-01-02T00:00:00Z')`,
		count:     `SELECT COUNT(*) FROM dimensions WHERE lower(name) = 'pillar'`,
	},
	{
		table:     "fields",
		first:     `INSERT INTO fields (name, type, created_at) VALUES ('Budget', 'number', '2026-01-02T00:00:00Z')`,
		duplicate: `INSERT INTO fields (name, type, retired, created_at) VALUES ('budget', 'short_text', 1, '2026-01-02T00:00:00Z')`,
		distinct:  `INSERT INTO fields (name, type, created_at) VALUES ('Sponsor', 'short_text', '2026-01-02T00:00:00Z')`,
		count:     `SELECT COUNT(*) FROM fields WHERE lower(name) = 'budget'`,
	},
}

// A database already holding a duplicate that migration 0060's unique indexes
// refuse won't migrate: the error names the duplicate's table and both rows
// are kept, so an operator settles which stays by hand.
func TestMigrationFailsOnADuplicateTheUniqueIndexesRefuse(t *testing.T) {
	t.Parallel()
	for _, tc := range uniqueIndexCases {
		t.Run(tc.table, func(t *testing.T) {
			t.Parallel()
			sqlDB := migratedExcept(t, "migrations/0060_unique_indexes.sql")
			// Back to the schema before 0060, which held none of its indexes.
			if _, err := sqlDB.Exec(`
				DROP INDEX idx_metric_readings_unique_metric;
				CREATE INDEX idx_metric_readings_checkin ON metric_readings (checkin_id);
				DROP INDEX idx_report_definition_fields_unique_field;
				CREATE INDEX idx_report_definition_fields_definition ON report_definition_fields (report_definition_id);
				DROP INDEX idx_dimension_values_unique_value;
				CREATE INDEX idx_dimension_values_dimension ON dimension_values (dimension_id);
				DROP INDEX idx_dimensions_unique_name;
				DROP INDEX idx_fields_unique_name`); err != nil {
				t.Fatalf("restore the indexes before 0060: %v", err)
			}
			arrangeUpgradeAccountsAndGoals(t, sqlDB)
			for _, stmt := range []string{tc.arrange, tc.first, tc.duplicate} {
				if _, err := sqlDB.Exec(stmt); err != nil {
					t.Fatalf("arrange %s: %v", tc.table, err)
				}
			}

			err := db.Migrate(sqlDB)
			if err == nil {
				t.Fatalf("Migrate succeeded on a database with a duplicate in %s", tc.table)
			}
			if !strings.Contains(err.Error(), tc.table) {
				t.Errorf("Migrate error %q doesn't name the %s table", err, tc.table)
			}
			var rows int
			if err := sqlDB.QueryRow(tc.count).Scan(&rows); err != nil {
				t.Fatalf("count %s: %v", tc.table, err)
			}
			if rows != 2 {
				t.Errorf("%d of the duplicate's rows in %s after the failed migration, want both kept", rows, tc.table)
			}
		})
	}
}

// Once migrated, each of migration 0060's tables refuses a duplicate, whatever
// its case where it holds a name or a value, and takes a row beside it that
// repeats nothing: another Metric in the same Check-in, another value in the
// same Dimension.
func TestTheUniqueIndexesRefuseADuplicate(t *testing.T) {
	t.Parallel()
	for _, tc := range uniqueIndexCases {
		t.Run(tc.table, func(t *testing.T) {
			t.Parallel()
			sqlDB := openMigratedForFKCheck(t)
			arrangeUpgradeAccountsAndGoals(t, sqlDB)
			for _, stmt := range []string{tc.arrange, tc.first} {
				if _, err := sqlDB.Exec(stmt); err != nil {
					t.Fatalf("arrange %s: %v", tc.table, err)
				}
			}

			if _, err := sqlDB.Exec(tc.duplicate); err == nil {
				t.Errorf("a duplicate was stored in %s", tc.table)
			}
			if _, err := sqlDB.Exec(tc.distinct); err != nil {
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
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return sqlDB
}

// insertOrphanedContributors adds n Contributors to a Goal that doesn't exist,
// on one connection with foreign keys off, as a database written before they
// were enforced could hold.
func insertOrphanedContributors(t *testing.T, sqlDB *sql.DB, n int) {
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
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO contributors (goal_id, account_id, created_at) VALUES (%d, 1, '2026-01-02T15:04:05Z')`, 1000+i))
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
	insertOrphanedContributors(t, sqlDB, 1)

	err := db.Migrate(sqlDB)
	if err == nil {
		t.Fatal("Migrate succeeded on a database with an orphaned Contributor")
	}
	for _, want := range []string{"contributors", "rowid 1", "goals"} {
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
	insertOrphanedContributors(t, sqlDB, 25)

	err := db.Migrate(sqlDB)
	if err == nil {
		t.Fatal("Migrate succeeded on a database with orphaned Contributors")
	}
	if got := strings.Count(err.Error(), "contributors rowid"); got != 20 {
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
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var on int
	if err := sqlDB.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if on != 1 {
		t.Errorf("PRAGMA foreign_keys = %d after Migrate, want 1", on)
	}
}

// sharedMigrationNumbers are the migrations allowed to share a number. Both
// 0057s were applied before anything stopped the repeat, and a migration's
// version is its whole filename, so renaming either would run it again.
var sharedMigrationNumbers = map[string][]string{
	"0057": {"0057_fk_indexes.sql", "0057_report_log.sql"},
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
		if len(names) < 2 || slices.Equal(names, sharedMigrationNumbers[number]) {
			continue
		}
		t.Errorf("migrations %s share the number %s; give the newest the next free number", strings.Join(names, ", "), number)
	}
}

// enumChecksMigration is the migration that rebuilt each enum and 0/1 flag
// column's table with a CHECK on it.
const enumChecksMigration = "migrations/0064_enum_checks.sql"

// execIgnoringChecks runs stmt on one connection with CHECK constraints off, as
// a database written before migration 0064 added them could hold its rows.
func execIgnoringChecks(t *testing.T, sqlDB *sql.DB, stmt string) {
	t.Helper()
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	for _, s := range []string{`PRAGMA ignore_check_constraints = ON`, stmt, `PRAGMA ignore_check_constraints = OFF`} {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// A database already holding a value outside an enum or flag column's CHECK
// won't migrate: the error names the table and column, and the value is kept,
// so an operator settles it by hand.
func TestMigrationFailsOnAValueTheEnumChecksRefuse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ constraint, arrange, read, want string }{
		{
			constraint: "goals_lifecycle_valid",
			arrange:    `UPDATE goals SET lifecycle = 'Archived' WHERE id = 1`,
			read:       `SELECT lifecycle FROM goals WHERE id = 1`,
			want:       "Archived",
		},
		{
			constraint: "accounts_is_admin_valid",
			arrange:    `UPDATE accounts SET is_admin = 2 WHERE id = 1`,
			read:       `SELECT is_admin FROM accounts WHERE id = 1`,
			want:       "2",
		},
	} {
		t.Run(tc.constraint, func(t *testing.T) {
			t.Parallel()
			sqlDB := migratedExcept(t, enumChecksMigration)
			testsupport.ArrangeEnumCheckRows(t, sqlDB)
			execIgnoringChecks(t, sqlDB, tc.arrange)

			err := db.Migrate(sqlDB)
			if err == nil {
				t.Fatalf("Migrate succeeded on a database breaking %s", tc.constraint)
			}
			if !strings.Contains(err.Error(), tc.constraint) {
				t.Errorf("Migrate error %q doesn't name %s", err, tc.constraint)
			}
			var got string
			if err := sqlDB.QueryRow(tc.read).Scan(&got); err != nil {
				t.Fatalf("read the row back: %v", err)
			}
			if got != tc.want {
				t.Errorf("%s = %q after the failed migration, want %q kept", tc.read, got, tc.want)
			}
		})
	}
}
