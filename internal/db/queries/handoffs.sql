-- name: CreateHandoff :one
INSERT INTO handoffs (goal_id, from_owner, to_owner, status, initiated_by, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetHandoff :one
SELECT * FROM handoffs WHERE id = ? LIMIT 1;

-- name: GetPendingHandoffForGoal :one
SELECT * FROM handoffs WHERE goal_id = ? AND status = 'pending' LIMIT 1;

-- name: SetHandoffStatus :exec
UPDATE handoffs SET status = ? WHERE id = ?;

-- name: DeleteHandoff :exec
DELETE FROM handoffs WHERE id = ?;

-- name: ListPendingHandoffsForNewOwner :many
-- Pending Handoffs awaiting a decision from the proposed new Owner, with the
-- Goal and both the current and proposed Owners resolved for display.
SELECT sqlc.embed(handoffs), sqlc.embed(goal), sqlc.embed(from_acct), sqlc.embed(to_acct)
FROM handoffs
JOIN goals goal ON goal.id = handoffs.goal_id
JOIN accounts from_acct ON from_acct.id = handoffs.from_owner
JOIN accounts to_acct ON to_acct.id = handoffs.to_owner
WHERE handoffs.to_owner = @to_owner AND handoffs.status = 'pending'
ORDER BY handoffs.created_at, handoffs.id;

-- name: SetGoalOwner :exec
-- Move a Goal to a new Owner: used when a Handoff is accepted and when an Admin
-- reassigns an Ownerless Goal.
UPDATE goals SET owner_id = ? WHERE id = ?;

-- name: SetAccountDeparted :exec
-- Record that a person has left the org, so their Goals become Ownerless.
UPDATE accounts SET departed = ? WHERE id = ?;

-- name: ListOwnerlessGoals :many
-- Every Goal whose Owner has departed the org (CONTEXT.md: Ownerless), newest
-- first, so an Admin can find and reassign them.
SELECT sqlc.embed(goals), sqlc.embed(accounts)
FROM goals
JOIN accounts ON accounts.id = goals.owner_id
WHERE accounts.departed = 1
ORDER BY goals.created_at DESC, goals.id DESC;
