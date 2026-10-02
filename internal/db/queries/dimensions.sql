-- name: CreateDimension :one
INSERT INTO dimensions (name, created_at)
VALUES (?, ?)
RETURNING *;

-- name: GetDimension :one
SELECT * FROM dimensions WHERE id = ? LIMIT 1;

-- name: ListDimensions :many
SELECT * FROM dimensions ORDER BY name, id;

-- name: SetDimensionSelection :one
-- Whether a Goal takes one value ('one') or several ('several') in this
-- Dimension.
UPDATE dimensions SET selection = ? WHERE id = ?
RETURNING *;

-- name: SetDimensionList :one
-- Whether the Dimension's list is Fixed ('fixed') or Extendable ('extendable').
UPDATE dimensions SET list = ? WHERE id = ?
RETURNING *;

-- name: ListGoalsWithSeveralValuesInDimension :many
-- The Goals carrying more than one of a Dimension's values, which keep it from
-- being switched to one value.
SELECT sqlc.embed(goals), sqlc.embed(accounts)
FROM goals
JOIN accounts ON accounts.id = goals.owner_id
WHERE goals.id IN (
    SELECT goal_dimension_values.goal_id
    FROM goal_dimension_values
    JOIN dimension_values ON dimension_values.id = goal_dimension_values.dimension_value_id
    WHERE dimension_values.dimension_id = ?
    GROUP BY goal_dimension_values.goal_id
    HAVING COUNT(*) > 1
)
ORDER BY goals.title, goals.id;

-- name: CreateDimensionValue :one
-- A new value takes the position after the Dimension's last, so it lands at the
-- end of the list.
INSERT INTO dimension_values (dimension_id, value, retired, created_at, position)
VALUES (
    sqlc.arg(dimension_id), sqlc.arg(value), 0, sqlc.arg(created_at),
    (SELECT COALESCE(MAX(position) + 1, 0) FROM dimension_values WHERE dimension_id = sqlc.arg(dimension_id))
)
RETURNING *;

-- name: GetDimensionValue :one
SELECT * FROM dimension_values WHERE id = ? LIMIT 1;

-- name: ListDimensionValues :many
-- Every value of a Dimension in the Admin's order, retired ones included, so
-- Admin management and existing-Goal display both see the full list.
SELECT * FROM dimension_values WHERE dimension_id = ? ORDER BY position, id;

-- name: ListAllDimensionValues :many
-- Every value across every Dimension, for attaching values to their Dimensions
-- in one pass, each Dimension's in the Admin's order.
SELECT * FROM dimension_values ORDER BY dimension_id, position, id;

-- name: SetDimensionValueName :one
UPDATE dimension_values SET value = ? WHERE id = ?
RETURNING *;

-- name: SetDimensionValueRetired :one
UPDATE dimension_values SET retired = ? WHERE id = ?
RETURNING *;

-- name: SetDimensionValuePosition :exec
-- Where a value sits in its Dimension's list, lowest first.
UPDATE dimension_values SET position = ? WHERE id = ?;

-- name: AssignGoalValueIfAbsent :exec
-- Give a Goal a value; a value it already carries is left as it is.
INSERT INTO goal_dimension_values (goal_id, dimension_value_id, created_at)
VALUES (?, ?, ?)
ON CONFLICT (goal_id, dimension_value_id) DO NOTHING;

-- name: ClearGoalValuesInDimension :exec
-- Remove a Goal's values in one Dimension, so a fresh assignment replaces them.
DELETE FROM goal_dimension_values
WHERE goal_id = ?
  AND dimension_value_id IN (SELECT id FROM dimension_values WHERE dimension_id = ?);

-- name: RemoveGoalValue :exec
DELETE FROM goal_dimension_values WHERE goal_id = ? AND dimension_value_id = ?;

-- name: ListGoalValues :many
-- The values assigned to one Goal, each with its Dimension, retired ones
-- included so they stay readable on the Goal.
SELECT sqlc.embed(dimension_values), sqlc.embed(dimensions)
FROM goal_dimension_values
JOIN dimension_values ON dimension_values.id = goal_dimension_values.dimension_value_id
JOIN dimensions ON dimensions.id = dimension_values.dimension_id
WHERE goal_dimension_values.goal_id = ?
ORDER BY dimensions.name, dimension_values.position, dimension_values.id;

-- name: ListAllGoalValues :many
-- Every Goal's assigned values with their Dimension, for building the Goal list's
-- filter and grouping in one query.
SELECT goal_dimension_values.goal_id, sqlc.embed(dimension_values), sqlc.embed(dimensions)
FROM goal_dimension_values
JOIN dimension_values ON dimension_values.id = goal_dimension_values.dimension_value_id
JOIN dimensions ON dimensions.id = dimension_values.dimension_id
ORDER BY goal_dimension_values.goal_id, dimensions.name, dimension_values.position, dimension_values.id;

-- name: RemoveMergedGoalValueWhereTargetCarried :exec
-- Merging a value: drop it from the Goals already carrying the target, so
-- moving the rest leaves no Goal carrying the target twice.
DELETE FROM goal_dimension_values
WHERE goal_dimension_values.dimension_value_id = sqlc.arg(merged_id)
  AND goal_id IN (
    SELECT target.goal_id FROM goal_dimension_values AS target WHERE target.dimension_value_id = sqlc.arg(target_id)
  );

-- name: MoveGoalValuesToTarget :exec
-- Merging a value: the Goals carrying it carry the target instead.
UPDATE goal_dimension_values SET dimension_value_id = sqlc.arg(target_id)
WHERE dimension_value_id = sqlc.arg(merged_id);

-- name: DeleteDimensionValue :exec
-- Only a merge deletes a value, once nothing refers to it any more.
DELETE FROM dimension_values WHERE id = ?;
