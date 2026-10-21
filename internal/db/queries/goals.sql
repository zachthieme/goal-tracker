-- name: CreateGoal :one
INSERT INTO goals (title, why_it_matters, owner_id, lifecycle, created_at)
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

-- name: ListGoalsByID :many
-- The given Goals, each with its Owner, as GetGoal reads one. A Goal that
-- doesn't exist has no row.
SELECT sqlc.embed(goals), sqlc.embed(accounts)
FROM goals
JOIN accounts ON accounts.id = goals.owner_id
WHERE goals.id IN (sqlc.slice(goal_ids));

-- name: SetGoalKind :one
UPDATE goals SET kind = ?, delivery_date = ? WHERE id = ?
RETURNING *;

-- name: SetGoalCadence :one
UPDATE goals SET cadence_days = ? WHERE id = ?
RETURNING *;

-- name: SetGoalWhyItMatters :one
UPDATE goals SET why_it_matters = ? WHERE id = ?
RETURNING *;

-- name: SetGoalTitle :one
UPDATE goals SET title = ? WHERE id = ?
RETURNING *;

-- name: SetGoalLifecycle :one
UPDATE goals SET lifecycle = ? WHERE id = ?
RETURNING *;

-- name: ActivateGoal :exec
-- Move a Proposed Goal to Active, recording when (CONTEXT.md: Check-in overdue
-- is judged from activation until the first Check-in).
UPDATE goals SET lifecycle = 'Active', activated_at = ? WHERE id = ?;

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

-- name: CreateWhyItMattersRevision :one
INSERT INTO why_it_matters_revisions (goal_id, why_it_matters, author_id, created_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListWhyItMattersRevisions :many
SELECT sqlc.embed(why_it_matters_revisions), sqlc.embed(accounts)
FROM why_it_matters_revisions
JOIN accounts ON accounts.id = why_it_matters_revisions.author_id
WHERE why_it_matters_revisions.goal_id = ?
ORDER BY why_it_matters_revisions.created_at DESC, why_it_matters_revisions.id DESC;

-- name: CreateGoalTitleRevision :one
INSERT INTO goal_title_revisions (goal_id, old_title, new_title, author_id, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListGoalTitleRevisions :many
SELECT sqlc.embed(goal_title_revisions), sqlc.embed(accounts)
FROM goal_title_revisions
JOIN accounts ON accounts.id = goal_title_revisions.author_id
WHERE goal_title_revisions.goal_id = ?
ORDER BY goal_title_revisions.created_at DESC, goal_title_revisions.id DESC;

-- name: SetGoalTopLevel :exec
UPDATE goals SET top_level = ? WHERE id = ?;

-- name: ListUnlinkedGoals :many
-- Active Goals that contribute to no other Goal through an accepted link and
-- aren't Top-level (CONTEXT.md: Unlinked), newest first.
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

-- name: ListDimensionsCarriedByGoals :many
-- The Dimensions each given Goal has a value in, Retired values included, for
-- judging which required ones it lacks (CONTEXT.md: Incomplete).
SELECT DISTINCT goal_dimension_values.goal_id, dimension_values.dimension_id
FROM goal_dimension_values
JOIN dimension_values ON dimension_values.id = goal_dimension_values.dimension_value_id
WHERE goal_dimension_values.goal_id IN (sqlc.slice(goal_ids));

-- name: ListFieldsSetOnGoals :many
-- The Fields each given Goal has a value in, Retired ones included, for judging
-- which required ones it lacks (CONTEXT.md: Incomplete).
SELECT goal_id, field_id FROM goal_field_values
WHERE goal_id IN (sqlc.slice(goal_ids));
