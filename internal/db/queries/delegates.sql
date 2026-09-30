-- name: AddDelegate :one
INSERT INTO delegates (goal_id, account_id, created_at)
VALUES (?, ?, ?)
RETURNING *;

-- name: RemoveDelegate :exec
DELETE FROM delegates
WHERE goal_id = ? AND account_id = ?;

-- name: GetDelegate :one
SELECT * FROM delegates
WHERE goal_id = ? AND account_id = ? LIMIT 1;

-- name: ListDelegates :many
-- The Accounts an Owner has authorized to write Check-ins on a Goal, ordered by
-- email (CONTEXT.md: Delegate).
SELECT sqlc.embed(accounts)
FROM delegates
JOIN accounts ON accounts.id = delegates.account_id
WHERE delegates.goal_id = ?
ORDER BY accounts.email;

-- name: ListDelegatedGoals :many
-- Every Goal an Account is a Delegate for, with the Goal's Owner resolved, newest
-- Goal first - the Delegate's worklist of Goals they can check in on.
SELECT sqlc.embed(goals), sqlc.embed(accounts)
FROM delegates
JOIN goals ON goals.id = delegates.goal_id
JOIN accounts ON accounts.id = goals.owner_id
WHERE delegates.account_id = @account_id
ORDER BY goals.created_at DESC, goals.id DESC;
