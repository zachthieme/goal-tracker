-- name: CreateCheckin :one
INSERT INTO checkins (goal_id, author_id, owner_id, health, status, path_to_green, path_target_date, explanation, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLatestCheckin :one
-- The most recent Check-in on a Goal, which carries the Goal's current Health,
-- status, and Path to Green (CONTEXT.md: current values come from the latest
-- Check-in).
SELECT * FROM checkins WHERE goal_id = ? ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: ListCheckins :many
-- A Goal's Check-in history, newest first, with each Check-in's author and the
-- Owner it was written for resolved for display.
SELECT sqlc.embed(checkins), sqlc.embed(author), sqlc.embed(owner)
FROM checkins
JOIN accounts author ON author.id = checkins.author_id
JOIN accounts owner ON owner.id = checkins.owner_id
WHERE checkins.goal_id = @goal_id
ORDER BY checkins.created_at DESC, checkins.id DESC;
