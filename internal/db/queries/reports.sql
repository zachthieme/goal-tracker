-- name: CreateReportDefinition :one
INSERT INTO report_definitions (name, introduction, depth, owner_filter_id, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetReportDefinition :one
SELECT * FROM report_definitions WHERE id = ? LIMIT 1;

-- name: ListReportDefinitions :many
SELECT * FROM report_definitions ORDER BY name, id;

-- name: AddReportDefinitionRoot :exec
INSERT INTO report_definition_roots (report_definition_id, goal_id)
VALUES (?, ?);

-- name: ListReportDefinitionRoots :many
-- The root Goal ids of a Report Definition, in the order they were saved.
SELECT goal_id FROM report_definition_roots
WHERE report_definition_id = ?
ORDER BY id;

-- name: AddReportDefinitionFilter :exec
INSERT INTO report_definition_filters (report_definition_id, dimension_value_id)
VALUES (?, ?);

-- name: ListReportDefinitionFilters :many
-- The Dimension-value filter ids of a Report Definition, in the order saved.
SELECT dimension_value_id FROM report_definition_filters
WHERE report_definition_id = ?
ORDER BY id;
