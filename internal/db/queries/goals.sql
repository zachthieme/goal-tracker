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

-- name: SetGoalKind :one
UPDATE goals SET kind = ?, delivery_date = ? WHERE id = ?
RETURNING *;

-- name: SetGoalCadence :one
UPDATE goals SET cadence_days = ? WHERE id = ?
RETURNING *;

-- name: SetGoalSoWhat :one
UPDATE goals SET so_what = ? WHERE id = ?
RETURNING *;

-- name: SetGoalLifecycle :one
UPDATE goals SET lifecycle = ? WHERE id = ?
RETURNING *;

-- name: CreateMilestone :one
INSERT INTO milestones (goal_id, name, target_date, created_at, added_while_active)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMilestone :one
SELECT * FROM milestones WHERE id = ? LIMIT 1;

-- name: UpdateMilestone :one
UPDATE milestones SET name = ?, target_date = ? WHERE id = ?
RETURNING *;

-- name: ListMilestones :many
SELECT * FROM milestones WHERE goal_id = ? ORDER BY target_date, id;

-- name: CreateMetric :one
INSERT INTO metrics (goal_id, name, unit, direction, baseline, target, target_date, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMetric :one
SELECT * FROM metrics WHERE id = ? LIMIT 1;

-- name: UpdateMetric :one
UPDATE metrics SET name = ?, unit = ?, direction = ?, baseline = ?, target = ?, target_date = ?
WHERE id = ?
RETURNING *;

-- name: ListMetrics :many
SELECT * FROM metrics WHERE goal_id = ? ORDER BY target_date, id;

-- name: AddContributor :one
INSERT INTO contributors (goal_id, account_id, created_at)
VALUES (?, ?, ?)
RETURNING *;

-- name: ListContributors :many
SELECT sqlc.embed(accounts)
FROM contributors
JOIN accounts ON accounts.id = contributors.account_id
WHERE contributors.goal_id = ?
ORDER BY accounts.email;

-- name: GetContributor :one
SELECT * FROM contributors
WHERE goal_id = ? AND account_id = ? LIMIT 1;

-- name: CreateSoWhatRevision :one
INSERT INTO so_what_revisions (goal_id, so_what, author_id, created_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListSoWhatRevisions :many
SELECT sqlc.embed(so_what_revisions), sqlc.embed(accounts)
FROM so_what_revisions
JOIN accounts ON accounts.id = so_what_revisions.author_id
WHERE so_what_revisions.goal_id = ?
ORDER BY so_what_revisions.created_at DESC, so_what_revisions.id DESC;

-- name: SetGoalTopLevel :exec
UPDATE goals SET top_level = ? WHERE id = ?;

-- name: ListUnalignedGoals :many
-- Active Goals that contribute to no other Goal through an accepted link and
-- aren't Top-level (CONTEXT.md: Unaligned), newest first.
SELECT sqlc.embed(goals), sqlc.embed(accounts)
FROM goals
JOIN accounts ON accounts.id = goals.owner_id
WHERE goals.lifecycle = 'Active'
  AND goals.top_level = 0
  AND NOT EXISTS (
    SELECT 1 FROM links
    WHERE links.child_id = goals.id AND links.status = 'accepted'
  )
ORDER BY goals.created_at DESC, goals.id DESC;

-- name: ListAcceptedLinkGoals :many
-- Every accepted "contributes to" link with its child and parent Goals and their
-- Owners resolved, for reading risks off the graph (ticket #11).
SELECT sqlc.embed(child), sqlc.embed(child_owner), sqlc.embed(parent), sqlc.embed(parent_owner)
FROM links
JOIN goals child ON child.id = links.child_id
JOIN accounts child_owner ON child_owner.id = child.owner_id
JOIN goals parent ON parent.id = links.parent_id
JOIN accounts parent_owner ON parent_owner.id = parent.owner_id
WHERE links.status = 'accepted'
ORDER BY links.created_at, links.id;
