package db

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	"email_outbox.account_id":                 "never filtered on; only read with its row, for the outbox's log lines",
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

// fkIndexHotQueries are the queries read on every Goal page and list, by the
// Goal or the person they are for, and the ones that find a day's Nudges or the
// open Action Items. Each must find its rows by index; queries meant to read a
// whole table stay off this list. No query lists Goals by Owner alone: the two
// "ForOwner" queries are the ones that find an Owner's Goals.
var fkIndexHotQueries = map[string]string{
	"ListOpenParentSuggestionsForOwner": listOpenParentSuggestionsForOwner,
	"ListPendingLinksForOwner":          listPendingLinksForOwner,
	"GetLatestCheckin":                  getLatestCheckin,
	"ListMetrics":                       listMetrics,
	"ListMilestones":                    listMilestones,
	"ListGoalComments":                  listGoalComments,
	"ListActionItemsForGoal":            listActionItemsForGoal,
	"ListParentGoals":                   listParentGoals,
	"ListChildGoals":                    listChildGoals,
	"ListNudgesOn":                      listNudgesOn,
	"ListOverdueActionItems":            listOverdueActionItems,
	"ListOpenActionItemsWithParties":    listOpenActionItemsWithParties,
}

// fkIndexNoScanTables are the tables that grow with every Goal: a hot query
// must never read one of them whole.
var fkIndexNoScanTables = []string{"goals", "checkins", "metrics", "comments", "links", "action_items", "nudges"}

// fkIndexScanDetail matches a plan step that reads a table, or a whole index
// of it, end to end: "SCAN goals", "SCAN parent", "SCAN g USING INDEX ...".
// The second group is the index it reads, if any.
var fkIndexScanDetail = regexp.MustCompile(`^SCAN (\S+)(?: USING (?:COVERING )?INDEX (\S+))?`)

// fkIndexPartialIndexes names every partial index in the schema. Reading one
// whole reads only the rows its WHERE keeps, such as the open Action Items, so
// a hot query may scan it.
func fkIndexPartialIndexes(t *testing.T, sqlDB *sql.DB) map[string]bool {
	t.Helper()
	rows, err := sqlDB.Query(`
		SELECT il.name
		FROM sqlite_schema AS m, pragma_index_list(m.name) AS il
		WHERE m.type = 'table' AND il.partial = 1`)
	if err != nil {
		t.Fatalf("list partial indexes: %v", err)
	}
	partial := map[string]bool{}
	for _, name := range fkIndexScanStrings(t, rows) {
		partial[name] = true
	}
	return partial
}

// fkIndexParams counts query's parameters as SQLite numbers them: "?N" is
// parameter N, and a bare "?" is one past the highest so far. A "?" in a
// string, quoted name or comment is not a parameter.
func fkIndexParams(query string) int {
	highest := 0
	for i := 0; i < len(query); i++ {
		switch {
		case query[i] == '\'' || query[i] == '"':
			end := strings.IndexByte(query[i+1:], query[i])
			if end < 0 {
				return highest
			}
			i += end + 1
		case strings.HasPrefix(query[i:], "--"):
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				return highest
			}
			i += end
		case strings.HasPrefix(query[i:], "/*"):
			end := strings.Index(query[i:], "*/")
			if end < 0 {
				return highest
			}
			i += end + 1
		case query[i] == '?':
			j := i + 1
			for j < len(query) && query[j] >= '0' && query[j] <= '9' {
				j++
			}
			n := highest + 1
			if j > i+1 {
				n, _ = strconv.Atoi(query[i+1 : j])
			}
			highest = max(highest, n)
			i = j - 1
		}
	}
	return highest
}

// fkIndexPlanStep is one step of a query plan: Parent is the ID of the step,
// such as a subquery, it belongs to, 0 for the query itself.
type fkIndexPlanStep struct {
	ID, Parent int
	Detail     string
}

// fkIndexPlanSteps returns the steps of query's plan, with NULL bound to each
// of its parameters.
func fkIndexPlanSteps(t *testing.T, sqlDB *sql.DB, query string) []fkIndexPlanStep {
	t.Helper()
	rows, err := sqlDB.Query("EXPLAIN QUERY PLAN "+query, make([]any, fkIndexParams(query))...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []fkIndexPlanStep
	for rows.Next() {
		var step fkIndexPlanStep
		var notUsed int
		if err := rows.Scan(&step.ID, &step.Parent, &notUsed, &step.Detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, step)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return plan
}

// fkIndexPlan returns the details of query's plan steps, with NULL bound to
// each of its parameters.
func fkIndexPlan(t *testing.T, sqlDB *sql.DB, query string) []string {
	t.Helper()
	var plan []string
	for _, step := range fkIndexPlanSteps(t, sqlDB, query) {
		plan = append(plan, step.Detail)
	}
	return plan
}

// fkIndexTableAliases maps each name query reads a table by, its own or an
// alias ("goals g", "goals AS parent"), to that table.
func fkIndexTableAliases(query string) map[string]string {
	aliases := map[string]string{}
	for _, table := range fkIndexNoScanTables {
		aliases[table] = table
		alias := regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+` + table + `\s+(?:AS\s+)?(\w+)`)
		for _, m := range alias.FindAllStringSubmatch(query, -1) {
			switch strings.ToUpper(m[1]) {
			case "ON", "WHERE", "JOIN", "LEFT", "INNER", "ORDER", "GROUP", "LIMIT", "USING":
			default:
				aliases[m[1]] = table
			}
		}
	}
	return aliases
}

// The queries every Goal page and list runs find their rows by index rather
// than reading a growing table whole.
func TestHotQueriesScanNoGrowingTable(t *testing.T) {
	t.Parallel()

	sqlDB := fkIndexMigratedDB(t)
	partial := fkIndexPartialIndexes(t, sqlDB)
	for name, query := range fkIndexHotQueries {
		aliases := fkIndexTableAliases(query)
		plan := fkIndexPlan(t, sqlDB, query)
		for _, step := range plan {
			m := fkIndexScanDetail.FindStringSubmatch(step)
			if m == nil || partial[m[2]] {
				continue
			}
			if table, ok := aliases[m[1]]; ok {
				t.Errorf("%s scans %s (%q); plan: %s", name, table, step, strings.Join(plan, "; "))
			}
		}
	}
}

// fkIndexLatestCheckinQueries are the queries that pick a Goal's latest
// Check-in: its Check-ins newest first, the first of them.
var fkIndexLatestCheckinQueries = map[string]string{
	"GetLatestCheckin":                 getLatestCheckin,
	"ListLatestCheckinsForGoals":       listLatestCheckinsForGoals,
	"ListActiveGoalsWithLatestCheckin": listActiveGoalsWithLatestCheckin,
	"ListOpenAsksForHelp":              listOpenAsksForHelp,
	"GetOpenAskForHelp":                getOpenAskForHelp,
}

// fkIndexGoalCheckinsDetail matches a plan step that finds a Goal's Check-ins
// by its goal_id: "SEARCH c USING INDEX ... (goal_id=?)".
var fkIndexGoalCheckinsDetail = regexp.MustCompile(`^(?:SEARCH|SCAN) (\S+) .*\bgoal_id\b`)

// The queries that pick a Goal's latest Check-in read its Check-ins already
// newest first from an index, rather than sorting them each time. A sort of
// anything else, such as a list of Goals, is not this test's business.
func TestLatestCheckinQueriesSortNoCheckins(t *testing.T) {
	t.Parallel()

	sqlDB := fkIndexMigratedDB(t)
	for name, query := range fkIndexLatestCheckinQueries {
		aliases := fkIndexTableAliases(query)
		plan := fkIndexPlanSteps(t, sqlDB, query)
		findsGoalCheckins := map[int]bool{}
		for _, step := range plan {
			m := fkIndexGoalCheckinsDetail.FindStringSubmatch(step.Detail)
			if m != nil && aliases[m[1]] == "checkins" {
				findsGoalCheckins[step.Parent] = true
			}
		}
		if len(findsGoalCheckins) == 0 {
			t.Errorf("%s finds no Goal's Check-ins by goal_id; plan: %s", name, fkIndexPlanText(plan))
		}
		for _, step := range plan {
			if step.Detail == "USE TEMP B-TREE FOR ORDER BY" && findsGoalCheckins[step.Parent] {
				t.Errorf("%s sorts a Goal's Check-ins in a temp B-tree; plan: %s", name, fkIndexPlanText(plan))
			}
		}
	}
}

// fkIndexPlanText joins plan's step details for a failure message.
func fkIndexPlanText(plan []fkIndexPlanStep) string {
	details := make([]string, 0, len(plan))
	for _, step := range plan {
		details = append(details, step.Detail)
	}
	return strings.Join(details, "; ")
}
