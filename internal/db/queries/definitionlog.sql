-- name: RecordDefinitionChange :exec
-- Keep one change to a Dimension's or Field's definition in the Definition log.
INSERT INTO definition_changes (actor_id, dimension_id, field_id, summary, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: ListDefinitionChanges :many
-- A page of the whole Definition log, newest first, with the Account that made
-- each change resolved for display: up to page_size changes made before the
-- one with id before.
SELECT sqlc.embed(definition_changes), sqlc.embed(accounts)
FROM definition_changes
JOIN accounts ON accounts.id = definition_changes.actor_id
WHERE definition_changes.id < @before
ORDER BY definition_changes.id DESC
LIMIT @page_size;

-- name: ListDefinitionChangesForDimension :many
-- The Definition log narrowed to one Dimension and its values, newest first.
SELECT sqlc.embed(definition_changes), sqlc.embed(accounts)
FROM definition_changes
JOIN accounts ON accounts.id = definition_changes.actor_id
WHERE definition_changes.dimension_id = ?
ORDER BY definition_changes.id DESC;

-- name: ListDefinitionChangesForField :many
-- The Definition log narrowed to one Field, newest first.
SELECT sqlc.embed(definition_changes), sqlc.embed(accounts)
FROM definition_changes
JOIN accounts ON accounts.id = definition_changes.actor_id
WHERE definition_changes.field_id = ?
ORDER BY definition_changes.id DESC;
