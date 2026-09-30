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
-- The Goals a Goal contributes to (its accepted parents), for navigation.
SELECT sqlc.embed(goals), sqlc.embed(accounts)
FROM links
JOIN goals ON goals.id = links.parent_id
JOIN accounts ON accounts.id = goals.owner_id
WHERE links.child_id = @child_id AND links.status = 'accepted'
ORDER BY goals.created_at DESC, goals.id DESC;

-- name: ListChildGoals :many
-- The Goals that contribute to a Goal (its accepted children), for navigation.
SELECT sqlc.embed(goals), sqlc.embed(accounts)
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
