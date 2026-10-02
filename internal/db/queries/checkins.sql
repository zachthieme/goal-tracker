-- name: CreateCheckin :one
INSERT INTO checkins (goal_id, author_id, owner_id, health, status, path_to_green, path_target_date, explanation,
                      lifecycle_from, lifecycle_to, lifecycle_reason, outcome, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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

-- name: CreateMetricReading :one
-- Record a Metric's current value against the Check-in that read it (CONTEXT.md:
-- Metric - its current value is recorded at each Check-in).
INSERT INTO metric_readings (checkin_id, metric_id, value, created_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListMetricReadings :many
-- A Metric's readings over time, earliest first, for its trend against target.
SELECT * FROM metric_readings WHERE metric_id = ? ORDER BY created_at, id;

-- name: CreateHighlight :one
-- Record one of a Check-in's Highlights (CONTEXT.md: Highlight). A Check-in may
-- carry several; they are inserted in the order entered.
INSERT INTO highlights (checkin_id, kind, note, created_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListHighlightsByGoal :many
-- A Goal's Highlights, newest Check-in first and each Check-in's in the order
-- entered, each crediting the Owner the Check-in was written for. Report curation queries Highlights by Goal (CONTEXT.md:
-- Highlight; the Goal's Owner is credited).
SELECT sqlc.embed(highlights), sqlc.embed(owner)
FROM highlights
JOIN checkins ON checkins.id = highlights.checkin_id
JOIN accounts owner ON owner.id = checkins.owner_id
WHERE checkins.goal_id = @goal_id
ORDER BY checkins.created_at DESC, checkins.id DESC, highlights.id;

-- name: ListHighlightsByGoalInRange :many
-- A Goal's Highlights whose Check-in falls within [from, to] inclusive, newest
-- Check-in first and each Check-in's in the order entered. Report curation queries Highlights by Goal and by time range.
SELECT sqlc.embed(highlights), sqlc.embed(owner)
FROM highlights
JOIN checkins ON checkins.id = highlights.checkin_id
JOIN accounts owner ON owner.id = checkins.owner_id
WHERE checkins.goal_id = @goal_id
  AND checkins.created_at >= @from
  AND checkins.created_at <= @to
ORDER BY checkins.created_at DESC, checkins.id DESC, highlights.id;

-- name: CreateDateSlip :one
-- Record a change to a Goal's delivery date (milestone_id NULL) or a Milestone's
-- date, keeping the old and new dates and the reason (CONTEXT.md: Date Slip).
INSERT INTO date_slips (goal_id, checkin_id, milestone_id, old_date, new_date, reason, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListDateSlips :many
-- A Goal's Date Slips, earliest first, so a date's history reads in order.
SELECT * FROM date_slips WHERE goal_id = ? ORDER BY created_at, id;

-- name: SetGoalDeliveryDate :exec
UPDATE goals SET delivery_date = ? WHERE id = ?;

-- name: SetMilestoneTargetDate :exec
UPDATE milestones SET target_date = ? WHERE id = ?;

-- name: SetMilestoneStatus :exec
-- Mark a Milestone Done or Removed in a Check-in; Removed carries its reason.
UPDATE milestones SET status = ?, removed_reason = ? WHERE id = ?;

-- name: CountMilestoneChurn :one
-- Milestone Churn: Milestones added since the Goal became Active plus those
-- removed (CONTEXT.md: Milestone Churn). Removal only happens in a Check-in, so
-- only ever on an Active Goal.
SELECT CAST(
    (SELECT COUNT(*) FROM milestones m WHERE m.goal_id = @goal_id AND m.added_while_active = 1)
  + (SELECT COUNT(*) FROM milestones m WHERE m.goal_id = @goal_id AND m.status = 'Removed')
AS INTEGER) AS churn;
