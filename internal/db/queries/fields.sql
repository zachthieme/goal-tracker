-- name: CreateField :one
INSERT INTO fields (name, type, unit, created_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListFields :many
SELECT * FROM fields ORDER BY name, id;

-- name: GetField :one
SELECT * FROM fields WHERE id = ? LIMIT 1;

-- name: SetGoalFieldValue :exec
-- Give a Goal a value in a Field, replacing the one it had.
INSERT INTO goal_field_values (goal_id, field_id, value, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (goal_id, field_id) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;

-- name: ClearGoalFieldValue :exec
DELETE FROM goal_field_values WHERE goal_id = ? AND field_id = ?;

-- name: ListGoalFieldValues :many
-- The values one Goal has, each with its Field, Retired ones included so they
-- stay readable on the Goal.
SELECT goal_field_values.value, sqlc.embed(fields)
FROM goal_field_values
JOIN fields ON fields.id = goal_field_values.field_id
WHERE goal_field_values.goal_id = ?
ORDER BY fields.name, fields.id;

-- name: SetFieldRetired :one
-- Retire a Field (1) or restore it (0); nothing about it is deleted.
UPDATE fields SET retired = ? WHERE id = ?
RETURNING *;
