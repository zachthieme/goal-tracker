-- name: CreateGoal :one
INSERT INTO goals (title, so_what, owner_id, lifecycle, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetGoal :one
SELECT sqlc.embed(goals), sqlc.embed(accounts)
FROM goals
JOIN accounts ON accounts.id = goals.owner_id
WHERE goals.id = ? LIMIT 1;

-- name: ListGoals :many
SELECT sqlc.embed(goals), sqlc.embed(accounts)
FROM goals
JOIN accounts ON accounts.id = goals.owner_id
ORDER BY goals.created_at DESC, goals.id DESC;
