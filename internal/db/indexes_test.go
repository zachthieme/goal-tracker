package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// fkIndexAllowList names the foreign-key columns ("table.column") left
// without an index of their own, each with why no query needs one.
var fkIndexAllowList = map[string]string{
	"action_item_reassignments.from_owner_id": "never filtered on; only read with its row",
	"action_item_reassignments.to_owner_id":   "never filtered on; only read with its row",
	"action_item_reassignments.reassigned_by": "never filtered on; only read with its row",
	"action_items.created_by":                 "never filtered on; only written with its row",
	"date_slips.checkin_id":                   "never filtered on; slips are read by Goal or Milestone",
	"goal_value_changes.dimension_id":         "never filtered on; only read with its row",
	"goal_value_changes.field_id":             "never filtered on; only read with its row",
	"link_removals.parent_id":                 "only filtered with child_id, through idx_link_removals_pair",
	"link_removals.requested_by":              "never filtered on; only read with its row",
	"link_removals.removed_by":                "never filtered on; only read with its row",
	"links.requested_by":                      "never filtered on; only read with its row",
	"milestone_changes.author_id":             "never filtered on; only read with its row",
	"narrative_picks.highlight_id":            "never filtered on; picks are read by Report Definition",
	"rejected_link_requests.parent_id":        "only filtered with child_id, through idx_rejected_link_requests_pair",
	"rejected_link_requests.requested_by":     "never filtered on; only read with its row",
	"rejected_link_requests.rejected_by":      "never filtered on; only read with its row",
	"report_definition_fields.field_id":       "never filtered on; read by Report Definition",
	"report_definition_goals.goal_id":         "never filtered on; read by Report Definition",
	"report_definitions.created_by":           "never filtered on; only read with its row",
	"report_publications.published_by":        "never filtered on; only read with its row",
	"undo_tokens.account_id":                  "never filtered on; tokens are read by token",
}

// fkIndexMigratedDB returns a fresh database with every migration applied.
func fkIndexMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := Open(filepath.Join(t.TempDir(), "indexes.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return sqlDB
}

// fkIndexColumns lists every foreign-key column in the schema as
// "table.column". A composite foreign key contributes its first column, the
// one an index has to lead with.
func fkIndexColumns(t *testing.T, sqlDB *sql.DB) []string {
	t.Helper()
	rows, err := sqlDB.Query(`
		SELECT DISTINCT m.name || '.' || fk."from"
		FROM sqlite_schema AS m, pragma_foreign_key_list(m.name) AS fk
		WHERE m.type = 'table' AND fk.seq = 0
		ORDER BY 1`)
	if err != nil {
		t.Fatalf("list foreign keys: %v", err)
	}
	return fkIndexScanStrings(t, rows)
}

// fkIndexLeadingColumns lists, as "table.column", the leading column of every
// index in the schema, including those SQLite makes for a PRIMARY KEY or
// UNIQUE constraint, and every INTEGER PRIMARY KEY column: that is the rowid,
// the key the table itself is stored by. A partial index is left out: it
// can't serve every query.
func fkIndexLeadingColumns(t *testing.T, sqlDB *sql.DB) map[string]bool {
	t.Helper()
	rows, err := sqlDB.Query(`
		SELECT m.name || '.' || ii.name
		FROM sqlite_schema AS m,
			pragma_index_list(m.name) AS il,
			pragma_index_info(il.name) AS ii
		WHERE m.type = 'table' AND il.partial = 0 AND ii.seqno = 0
		UNION
		SELECT m.name || '.' || ti.name
		FROM sqlite_schema AS m, pragma_table_info(m.name) AS ti
		WHERE m.type = 'table' AND ti.pk = 1 AND upper(ti.type) = 'INTEGER'
			AND (SELECT COUNT(*) FROM pragma_table_info(m.name) WHERE pk > 0) = 1`)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	leading := map[string]bool{}
	for _, column := range fkIndexScanStrings(t, rows) {
		leading[column] = true
	}
	return leading
}

func fkIndexScanStrings(t *testing.T, rows *sql.Rows) []string {
	t.Helper()
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// Every foreign-key column leads some index, so a query that filters or joins
// on it finds its rows without scanning the table, unless the allow-list says
// why it doesn't need to.
func TestEveryForeignKeyColumnLeadsAnIndex(t *testing.T) {
	t.Parallel()

	sqlDB := fkIndexMigratedDB(t)
	leading := fkIndexLeadingColumns(t, sqlDB)
	foreignKeys := map[string]bool{}
	for _, column := range fkIndexColumns(t, sqlDB) {
		foreignKeys[column] = true
		_, allowed := fkIndexAllowList[column]
		switch {
		case leading[column] && allowed:
			t.Errorf("%s leads an index; drop it from the allow-list", column)
		case !leading[column] && !allowed:
			t.Errorf("foreign-key column %s leads no index: index it, or allow-list it with a reason", column)
		}
	}
	for column, reason := range fkIndexAllowList {
		if !foreignKeys[column] {
			t.Errorf("allow-listed %s is not a foreign-key column", column)
		}
		if reason == "" {
			t.Errorf("allow-listed %s gives no reason", column)
		}
	}
}
