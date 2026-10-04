-- name: CreateLink :one
INSERT INTO links (child_id, parent_id, status, note, requested_by, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLink :one
SELECT * FROM links WHERE id = ? LIMIT 1;

-- name: GetLinkByChildParent :one
SELECT * FROM links WHERE child_id = ? AND parent_id = ? LIMIT 1;

-- name: SetLinkStatus :exec
UPDATE links SET status = ? WHERE id = ?;

-- name: DeleteLink :exec
DELETE FROM links WHERE id = ?;

-- name: ListParentGoals :many
-- The Goals a Goal contributes to (its accepted parents), for navigation, each
-- with the id of the link that can remove it.
SELECT links.id AS link_id, sqlc.embed(goals), sqlc.embed(accounts)
FROM links
JOIN goals ON goals.id = links.parent_id
JOIN accounts ON accounts.id = goals.owner_id
WHERE links.child_id = @child_id AND links.status = 'accepted'
ORDER BY goals.created_at DESC, goals.id DESC;

-- name: ListPendingParentLinks :many
-- The Goals a Goal has asked to contribute to and is waiting on (its Pending
-- parents), each with the id of its request.
SELECT links.id AS link_id, sqlc.embed(goals), sqlc.embed(accounts)
FROM links
JOIN goals ON goals.id = links.parent_id
JOIN accounts ON accounts.id = goals.owner_id
WHERE links.child_id = @child_id AND links.status = 'pending'
ORDER BY links.created_at, links.id;

-- name: ListChildGoals :many
-- The Goals that contribute to a Goal (its accepted children), for navigation,
-- each with the id of the link that can remove it.
SELECT links.id AS link_id, sqlc.embed(goals), sqlc.embed(accounts)
FROM links
JOIN goals ON goals.id = links.child_id
JOIN accounts ON accounts.id = goals.owner_id
WHERE links.parent_id = @parent_id AND links.status = 'accepted'
ORDER BY goals.created_at DESC, goals.id DESC;

-- name: ListPendingLinksForOwner :many
-- Pending requests awaiting a decision from the Owner of the parent Goal, with
-- both endpoint Goals and their Owners resolved for display.
SELECT sqlc.embed(links), sqlc.embed(child), sqlc.embed(child_owner), sqlc.embed(parent), sqlc.embed(parent_owner)
FROM links
JOIN goals child ON child.id = links.child_id
JOIN accounts child_owner ON child_owner.id = child.owner_id
JOIN goals parent ON parent.id = links.parent_id
JOIN accounts parent_owner ON parent_owner.id = parent.owner_id
WHERE parent.owner_id = @owner_id AND links.status = 'pending'
ORDER BY links.created_at, links.id;

-- name: CreateLinkRemoval :one
-- Records an accepted link as it was when removed, so its remover can Undo it.
INSERT INTO link_removals (child_id, parent_id, note, requested_by, link_created_at, removed_by, removed_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLinkRemoval :one
SELECT * FROM link_removals WHERE id = ? LIMIT 1;

-- name: MarkLinkRemovalRestored :execrows
-- Marks a removal undone, only if it isn't already, so an Undo succeeds once.
UPDATE link_removals SET restored_at = ? WHERE id = ? AND restored_at IS NULL;

-- name: CreateRejectedLinkRequest :one
-- Records a pending request as it was when rejected, so its rejecter can Undo it.
INSERT INTO rejected_link_requests (child_id, parent_id, note, requested_by, request_created_at, rejected_by, rejected_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetRejectedLinkRequest :one
SELECT * FROM rejected_link_requests WHERE id = ? LIMIT 1;

-- name: MarkRejectedLinkRequestRestored :execrows
-- Marks a rejection undone, only if it isn't already, so an Undo succeeds once.
UPDATE rejected_link_requests SET restored_at = ? WHERE id = ? AND restored_at IS NULL;

-- name: CountLaterRejectedLinkRequests :one
-- How many requests between the same two Goals were rejected after this one:
-- any means the link was requested again since.
SELECT COUNT(*) FROM rejected_link_requests
WHERE child_id = @child_id AND parent_id = @parent_id AND id > @id;

-- name: ListLinkRemovalTimes :many
-- When each removal of the link between two Goals happened.
SELECT removed_at FROM link_removals WHERE child_id = ? AND parent_id = ?;
