package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
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

// Milestone changes recorded before a Milestone could be added outside a
// Check-in keep every column on upgrade and take their author from their
// Check-in; a change whose Check-in is missing keeps no author. Afterwards a
// change needs no Check-in.
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
		CREATE INDEX idx_milestone_changes_checkin ON milestone_changes (checkin_id);
		INSERT INTO checkins (id, goal_id, author_id, owner_id, health, status, created_at) VALUES
			(10, 1, 7, 7, 'Green', 'On track.', '2026-01-09T00:00:00Z'),
			(11, 1, 8, 7, 'Yellow', 'Slipping.', '2026-01-16T00:00:00Z');
		INSERT INTO milestone_changes (id, checkin_id, milestone_id, kind, reason, created_at, name) VALUES
			(3, 10, 2, 'Added', '', '2026-01-09T00:00:00Z', 'GA'),
			(5, 11, 1, 'Done', '', '2026-01-16T00:00:00Z', 'Beta'),
			(6, 11, 2, 'Removed', 'descoped', '2026-01-16T00:00:00Z', 'GA'),
			(9, 99, 4, 'Done', '', '2026-01-23T00:00:00Z', 'Docs')`); err != nil {
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
		`9 99 4 Done "" 2026-01-23T00:00:00Z Docs by none`,
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

	if err := h.Service.CurateNarrative(context.Background(), mbr.ID, domain.CurateNarrativeInput{
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
