-- name: CreateActionItem :one
INSERT INTO action_items (report_definition_id, publication_id, comment_id, goal_id, text, owner_id, due_date, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetActionItem :one
SELECT * FROM action_items WHERE id = ? LIMIT 1;

-- name: CloseActionItem :exec
UPDATE action_items SET closed_at = ?, closing_note = ? WHERE id = ? AND closed_at = '';

-- name: ListOpenActionItems :many
-- A Report Definition's open Action Items, soonest due first.
SELECT * FROM action_items
WHERE report_definition_id = ? AND closed_at = ''
ORDER BY due_date, id;

-- name: ListPublicationActionItems :many
-- The Action Items raised on one publication, in the order they were raised.
SELECT * FROM action_items
WHERE publication_id = ?
ORDER BY id;

-- The reads below list Action Items outside their Report, so each carries
-- its Report's name and the title of the Goal it is about ('' when it is
-- about the whole Report).

-- name: ListOpenActionItemsForOwner :many
-- One person's open Action Items, soonest due first.
SELECT sqlc.embed(action_items), rd.name AS report_name, COALESCE(g.title, '') AS goal_title
FROM action_items
JOIN report_definitions rd ON rd.id = action_items.report_definition_id
LEFT JOIN goals g ON g.id = action_items.goal_id
WHERE action_items.owner_id = @owner_id AND action_items.closed_at = ''
ORDER BY action_items.due_date, action_items.id;

-- name: ListActionItemsForGoal :many
-- The Action Items about one Goal, open and closed, soonest due first.
SELECT sqlc.embed(action_items), rd.name AS report_name, COALESCE(g.title, '') AS goal_title
FROM action_items
JOIN report_definitions rd ON rd.id = action_items.report_definition_id
LEFT JOIN goals g ON g.id = action_items.goal_id
WHERE action_items.goal_id = @goal_id
ORDER BY action_items.due_date, action_items.id;

-- name: ListAllOpenActionItems :many
-- Every open Action Item, grouped by owner, each owner's soonest due first.
SELECT sqlc.embed(action_items), rd.name AS report_name, COALESCE(g.title, '') AS goal_title
FROM action_items
JOIN report_definitions rd ON rd.id = action_items.report_definition_id
LEFT JOIN goals g ON g.id = action_items.goal_id
WHERE action_items.closed_at = ''
ORDER BY action_items.owner_id, action_items.due_date, action_items.id;

-- name: ListOverdueActionItems :many
-- The open Action Items about a Goal whose due date is before today (a
-- YYYY-MM-DD date in the org's timezone), soonest due first.
SELECT sqlc.embed(action_items), rd.name AS report_name, COALESCE(g.title, '') AS goal_title
FROM action_items
JOIN report_definitions rd ON rd.id = action_items.report_definition_id
LEFT JOIN goals g ON g.id = action_items.goal_id
WHERE action_items.closed_at = ''
  AND action_items.goal_id <> 0
  AND action_items.due_date < sqlc.arg(today)
ORDER BY action_items.due_date, action_items.id;

-- name: ReassignActionItem :execrows
-- Hands an open Action Item to a new owner; no rows change once it is closed.
UPDATE action_items SET owner_id = ? WHERE id = ? AND closed_at = '';

-- name: CreateActionItemReassignment :one
INSERT INTO action_item_reassignments (action_item_id, from_owner_id, to_owner_id, reassigned_by, reassigned_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLatestActionItemReassignment :one
-- An Action Item's most recent reassignment, naming who had it before.
SELECT * FROM action_item_reassignments
WHERE action_item_id = ?
ORDER BY id DESC
LIMIT 1;

-- name: ListActionItemReassignments :many
-- An Action Item's reassignments, oldest first.
SELECT * FROM action_item_reassignments
WHERE action_item_id = ?
ORDER BY id;

-- name: ListActionItemReassignmentsForGoal :many
-- The reassignments of the Action Items about one Goal, oldest first.
SELECT action_item_reassignments.*
FROM action_item_reassignments
JOIN action_items ON action_items.id = action_item_reassignments.action_item_id
WHERE action_items.goal_id = ?
ORDER BY action_item_reassignments.id;
