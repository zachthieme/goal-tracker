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
