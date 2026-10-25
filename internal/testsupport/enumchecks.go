package testsupport

import (
	"database/sql"
	"testing"
)

// EnumCheckTables are the tables migration 0064 rebuilt to add a CHECK on each
// of their enum and 0/1 flag columns.
var EnumCheckTables = []string{
	"accounts", "goals", "checkins", "milestones", "milestone_changes",
	"date_slips", "links", "link_events", "parent_suggestions", "handoffs",
	"metrics", "highlights", "draft_highlights", "report_definitions",
	"report_definition_goals", "report_rules", "narrative_picks",
	"narrative_texts", "undo_tokens", "dimensions", "dimension_values",
	"fields", "goal_value_changes",
}

// ArrangeEnumCheckRows adds one valid row with id 1 to each of
// EnumCheckTables (and a second Account and Goal, id 2, the first rows point
// at), straight into the database. Each column holds a value other than its
// default where it can, so a row copied across a rebuild without one shows.
func ArrangeEnumCheckRows(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.ExecContext(t.Context(), `
		INSERT INTO accounts (id, email, is_admin, created_at, departed, name, manager_id, timezone, oidc_subject) VALUES
			(1, 'sam@example.com', 1, '2026-01-02T00:00:00Z', 0, 'Sam', NULL, 'Europe/Paris', 'subject-sam'),
			(2, 'pat@example.com', 0, '2026-01-02T00:00:00Z', 1, 'Pat', 1, '', NULL);
		INSERT INTO goals (id, title, why_it_matters, owner_id, lifecycle, created_at, kind, delivery_date, cadence_days, top_level, activated_at) VALUES
			(1, 'Reduce outages', 'Outages cost trust.', 1, 'On Hold', '2026-01-02T00:00:00Z', 'Dated', '2026-12-31', 14, 1, '2026-01-03T00:00:00Z'),
			(2, 'Expand to the EU', 'Growth.', 2, 'Proposed', '2026-01-02T00:00:00Z', '', '', 7, 0, '');
		INSERT INTO checkins (id, goal_id, author_id, owner_id, health, status, path_to_green, path_target_date, created_at,
				explanation, lifecycle_from, lifecycle_to, lifecycle_reason, outcome, asks_for_help) VALUES
			(1, 1, 2, 1, 'Yellow', 'Slipping.', 'Add a region.', '2026-02-01', '2026-01-09T00:00:00Z',
				'A vendor slipped.', 'Active', 'On Hold', 'Waiting on the vendor.', 'Paused.', 1);
		INSERT INTO milestones (id, goal_id, name, target_date, created_at, status, removed_reason, added_while_active) VALUES
			(1, 1, 'Pilot', '2026-03-01', '2026-01-02T00:00:00Z', 'Removed', 'Folded into launch.', 1);
		INSERT INTO milestone_changes (id, checkin_id, milestone_id, kind, reason, created_at, name, author_id) VALUES
			(1, 1, 1, 'Removed', 'Folded into launch.', '2026-01-09T00:00:00Z', 'Pilot', 2);
		INSERT INTO date_slips (id, goal_id, checkin_id, milestone_id, old_date, new_date, reason, created_at, back_to_green) VALUES
			(1, 1, 1, 1, '2026-03-01', '2026-04-01', 'Vendor slip.', '2026-01-09T00:00:00Z', 1);
		INSERT INTO links (id, child_id, parent_id, status, note, requested_by, created_at) VALUES
			(1, 1, 2, 'accepted', 'Feeds growth.', 2, '2026-01-04T00:00:00Z');
		INSERT INTO link_events (id, child_id, parent_id, kind, actor_id, created_at) VALUES
			(1, 1, 2, 'rejection-undone', 2, '2026-01-04T00:00:00Z');
		INSERT INTO parent_suggestions (id, goal_id, parent_id, suggested_by, note, status, created_at, closed_at) VALUES
			(1, 1, 2, 2, 'Try this one.', 'declined', '2026-01-04T00:00:00Z', '2026-01-05T00:00:00Z');
		INSERT INTO handoffs (id, goal_id, from_owner, to_owner, status, initiated_by, created_at) VALUES
			(1, 1, 1, 2, 'rejected', 2, '2026-01-04T00:00:00Z');
		INSERT INTO metrics (id, goal_id, name, unit, direction, baseline, target, target_date, created_at) VALUES
			(1, 1, 'Incidents', 'count', 'down', 10.5, 2, '2026-12-31', '2026-01-02T00:00:00Z');
		INSERT INTO highlights (id, checkin_id, kind, note, created_at) VALUES
			(1, 1, 'Miss', 'Missed the SLA.', '2026-01-09T00:00:00Z');
		INSERT INTO draft_highlights (id, goal_id, kind, note, logged_by, created_at, discarded_checkin_id) VALUES
			(1, 1, 'Accomplishment', 'Cut MTTR.', 2, '2026-01-08T00:00:00Z', 1);
		INSERT INTO report_definitions (id, name, introduction, created_by, created_at, mode) VALUES
			(1, 'MBR', 'The month.', 2, '2026-01-02T00:00:00Z', 'picked');
		INSERT INTO report_definition_goals (id, report_definition_id, list, goal_id) VALUES
			(1, 1, 'exclude', 2);
		INSERT INTO dimensions (id, name, created_at, selection, list, retired, required) VALUES
			(1, 'Pillar', '2026-01-02T00:00:00Z', 'several', 'extendable', 1, 1);
		INSERT INTO report_rules (id, report_definition_id, attribute, dimension_id, op) VALUES
			(1, 1, 'dimension', 1, 'is any of');
		INSERT INTO narrative_picks (id, report_definition_id, highlight_id, section) VALUES
			(1, 1, 1, 'Insight');
		INSERT INTO narrative_texts (id, report_definition_id, section, text) VALUES
			(1, 1, 'Accomplishment', 'EU is open.');
		INSERT INTO undo_tokens (id, token, kind, subject_id, account_id, detail, issued_at, used_at) VALUES
			(1, 'tok-1', 'handoff-rejection', 1, 2, 'detail', '2026-01-04T00:00:00Z', '2026-01-04T00:01:00Z');
		INSERT INTO dimension_values (id, dimension_id, value, retired, created_at, position) VALUES
			(1, 1, 'Growth', 1, '2026-01-02T00:00:00Z', 3);
		INSERT INTO fields (id, name, type, unit, retired, created_at, required) VALUES
			(1, 'Budget', 'number', 'USD', 1, '2026-01-02T00:00:00Z', 1);
		INSERT INTO goal_value_changes (id, goal_id, actor_id, dimension_id, field_id, attribute, several, before_value, after_value, created_at) VALUES
			(1, 1, 2, 1, NULL, 'Pillar', 1, 'Efficiency', 'Growth', '2026-01-05T00:00:00Z')`); err != nil {
		t.Fatalf("arrange a row in each enum-checked table: %v", err)
	}
}
