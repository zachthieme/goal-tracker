-- name: RecordValueChange :exec
-- Keep one change to a Goal's Dimension values or Fields in its Value history.
INSERT INTO goal_value_changes
    (goal_id, actor_id, dimension_id, field_id, attribute, several, before_value, after_value, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListValueChangesForGoal :many
-- A Goal's Value history, oldest first, with the Account that made each change
-- resolved for display.
SELECT sqlc.embed(goal_value_changes), sqlc.embed(accounts)
FROM goal_value_changes
JOIN accounts ON accounts.id = goal_value_changes.actor_id
WHERE goal_value_changes.goal_id = ?
ORDER BY goal_value_changes.id;
