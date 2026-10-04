-- name: CreateReportDefinition :one
INSERT INTO report_definitions (name, introduction, mode, created_by, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetReportDefinition :one
SELECT * FROM report_definitions WHERE id = ? LIMIT 1;

-- name: ListReportDefinitions :many
SELECT * FROM report_definitions ORDER BY name, id;

-- name: UpdateReportDefinition :exec
-- Edit a Report Definition's name, introduction and mode; its rules, listed
-- Goals and Fields are cleared and saved again.
UPDATE report_definitions SET name = ?, introduction = ?, mode = ?
WHERE id = ?;

-- name: ClearReportRuleValues :exec
DELETE FROM report_rule_values
WHERE report_rule_id IN (SELECT id FROM report_rules WHERE report_definition_id = ?);

-- name: ClearReportRules :exec
DELETE FROM report_rules WHERE report_definition_id = ?;

-- name: ClearReportDefinitionGoals :exec
DELETE FROM report_definition_goals WHERE report_definition_id = ?;

-- name: ClearReportDefinitionFields :exec
DELETE FROM report_definition_fields WHERE report_definition_id = ?;

-- name: AddReportRule :one
INSERT INTO report_rules (report_definition_id, attribute, dimension_id, op)
VALUES (?, ?, ?, ?)
RETURNING id;

-- name: AddReportRuleValue :exec
INSERT INTO report_rule_values (report_rule_id, value)
VALUES (?, ?);

-- name: ListReportRules :many
-- A Report Definition's rules, in the order they were saved.
SELECT * FROM report_rules
WHERE report_definition_id = ?
ORDER BY id;

-- name: ListReportRuleValues :many
-- The values of a Report Definition's rules, each rule's in the order saved.
SELECT report_rule_values.report_rule_id, report_rule_values.value
FROM report_rule_values
JOIN report_rules ON report_rules.id = report_rule_values.report_rule_id
WHERE report_rules.report_definition_id = ?
ORDER BY report_rule_values.id;

-- name: AddReportDefinitionGoal :exec
-- List a Goal on a Report Definition: 'picked', 'include' (Also include) or
-- 'exclude' (Leave out).
INSERT INTO report_definition_goals (report_definition_id, list, goal_id)
VALUES (?, ?, ?);

-- name: ListReportDefinitionGoals :many
-- The Goals a Report Definition lists by hand, each list in the order saved.
SELECT list, goal_id FROM report_definition_goals
WHERE report_definition_id = ?
ORDER BY id;

-- name: AddReportDefinitionField :exec
INSERT INTO report_definition_fields (report_definition_id, field_id)
VALUES (?, ?);

-- name: ListReportDefinitionFields :many
-- The ids of the Fields a Report Definition shows beside each Goal, in the
-- order saved.
SELECT field_id FROM report_definition_fields
WHERE report_definition_id = ?
ORDER BY id;

-- name: RemoveMergedRuleValueWhereTargetListed :exec
-- Merging a Dimension value: drop it from the Dimension rules that already
-- list the target, so no rule lists the target twice.
DELETE FROM report_rule_values
WHERE report_rule_values.value = sqlc.arg(merged_value)
  AND report_rule_id IN (SELECT id FROM report_rules WHERE attribute = 'dimension')
  AND report_rule_id IN (
    SELECT target.report_rule_id FROM report_rule_values AS target
    WHERE target.value = sqlc.arg(target_value)
  );

-- name: MoveRuleValuesToTarget :exec
-- Merging a Dimension value: the Dimension rules listing it list the target
-- instead, whatever their operator.
UPDATE report_rule_values SET value = sqlc.arg(target_value)
WHERE value = sqlc.arg(merged_value)
  AND report_rule_id IN (SELECT id FROM report_rules WHERE attribute = 'dimension');

-- name: CreateReportPublication :one
INSERT INTO report_publications (report_definition_id, published_by, published_at, snapshot)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetReportPublication :one
SELECT * FROM report_publications WHERE id = ? LIMIT 1;

-- name: ListReportPublications :many
-- A Report Definition's publications, newest first.
SELECT * FROM report_publications
WHERE report_definition_id = ?
ORDER BY id DESC;

-- name: LatestReportPublication :one
-- A Report Definition's latest publication, without its snapshot: what the
-- next publication reads its changes against.
SELECT id, published_at FROM report_publications
WHERE report_definition_id = ?
ORDER BY id DESC
LIMIT 1;

-- name: ClearNarrativePicks :exec
-- Drop a Report Definition's draft narrative picks, to replace them or once
-- they are frozen into a publication.
DELETE FROM narrative_picks WHERE report_definition_id = ?;

-- name: AddNarrativePick :exec
INSERT INTO narrative_picks (report_definition_id, highlight_id, section)
VALUES (?, ?, ?);

-- name: ListNarrativePicks :many
-- The Highlights picked into a Report Definition's draft narrative.
SELECT * FROM narrative_picks
WHERE report_definition_id = ?
ORDER BY id;

-- name: ClearNarrativeTexts :exec
-- Drop the author's notes from a Report Definition's draft narrative.
DELETE FROM narrative_texts WHERE report_definition_id = ?;

-- name: AddNarrativeText :exec
-- Add one of the author's notes to a section of a Report Definition's draft
-- narrative, after the section's earlier notes.
INSERT INTO narrative_texts (report_definition_id, section, text)
VALUES (?, ?, ?);

-- name: ListNarrativeTexts :many
-- The author's notes in a Report Definition's draft narrative, in the order
-- they were entered.
SELECT * FROM narrative_texts
WHERE report_definition_id = ?
ORDER BY id;
