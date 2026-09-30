-- name: CreateDimension :one
INSERT INTO dimensions (name, created_at)
VALUES (?, ?)
RETURNING *;

-- name: GetDimension :one
SELECT * FROM dimensions WHERE id = ? LIMIT 1;

-- name: ListDimensions :many
SELECT * FROM dimensions ORDER BY name, id;

-- name: CreateDimensionValue :one
INSERT INTO dimension_values (dimension_id, value, retired, created_at)
VALUES (?, ?, 0, ?)
RETURNING *;

-- name: GetDimensionValue :one
SELECT * FROM dimension_values WHERE id = ? LIMIT 1;

-- name: ListDimensionValues :many
-- Every value of a Dimension, retired ones included, so Admin management and
-- existing-Goal display both see the full list.
SELECT * FROM dimension_values WHERE dimension_id = ? ORDER BY value, id;

-- name: ListAllDimensionValues :many
-- Every value across every Dimension, for attaching values to their Dimensions
-- in one pass.
SELECT * FROM dimension_values ORDER BY dimension_id, value, id;

-- name: SetDimensionValueName :one
UPDATE dimension_values SET value = ? WHERE id = ?
RETURNING *;

-- name: SetDimensionValueRetired :one
UPDATE dimension_values SET retired = ? WHERE id = ?
RETURNING *;

-- name: AssignGoalValue :one
INSERT INTO goal_dimension_values (goal_id, dimension_value_id, created_at)
VALUES (?, ?, ?)
RETURNING *;

-- name: ClearGoalValuesInDimension :exec
-- Remove a Goal's assignment in one Dimension, so a fresh assignment replaces it
-- (a Goal carries at most one value per Dimension).
DELETE FROM goal_dimension_values
WHERE goal_id = ?
  AND dimension_value_id IN (SELECT id FROM dimension_values WHERE dimension_id = ?);

-- name: ListGoalValues :many
-- The values assigned to one Goal, each with its Dimension, retired ones
-- included so they stay readable on the Goal.
SELECT sqlc.embed(dimension_values), sqlc.embed(dimensions)
FROM goal_dimension_values
JOIN dimension_values ON dimension_values.id = goal_dimension_values.dimension_value_id
JOIN dimensions ON dimensions.id = dimension_values.dimension_id
WHERE goal_dimension_values.goal_id = ?
ORDER BY dimensions.name, dimension_values.value;

-- name: ListAllGoalValues :many
-- Every Goal's assigned values with their Dimension, for building the Goal list's
-- filter and grouping in one query.
SELECT goal_dimension_values.goal_id, sqlc.embed(dimension_values), sqlc.embed(dimensions)
FROM goal_dimension_values
JOIN dimension_values ON dimension_values.id = goal_dimension_values.dimension_value_id
JOIN dimensions ON dimensions.id = dimension_values.dimension_id
ORDER BY goal_dimension_values.goal_id, dimensions.name, dimension_values.value;
