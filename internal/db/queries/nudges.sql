-- name: CreateNudge :one
INSERT INTO nudges (goal_id, sent_by, nudged_on, created_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListNudgesOn :many
-- The Nudges sent on a day of the org's calendar, each with who sent it.
SELECT sqlc.embed(nudges), sqlc.embed(accounts)
FROM nudges
JOIN accounts ON accounts.id = nudges.sent_by
WHERE nudges.nudged_on = ?
ORDER BY nudges.id;

-- name: ListNudgesForGoal :many
-- Every Nudge of a Goal with who sent it, oldest first.
SELECT sqlc.embed(nudges), sqlc.embed(accounts)
FROM nudges
JOIN accounts ON accounts.id = nudges.sent_by
WHERE nudges.goal_id = ?
ORDER BY nudges.created_at, nudges.id;
