-- name: CreateLinkEvent :one
INSERT INTO link_events (child_id, parent_id, kind, actor_id, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListLinkEventsForGoal :many
-- Every change to a link of a Goal, whether it is the link's child or its
-- parent, with both Goals' titles and who made the change, oldest first.
SELECT sqlc.embed(link_events), child.title AS child_title, parent.title AS parent_title, sqlc.embed(accounts)
FROM link_events
JOIN goals child ON child.id = link_events.child_id
JOIN goals parent ON parent.id = link_events.parent_id
JOIN accounts ON accounts.id = link_events.actor_id
WHERE link_events.child_id = sqlc.arg(goal_id) OR link_events.parent_id = sqlc.arg(goal_id)
ORDER BY link_events.created_at, link_events.id;
