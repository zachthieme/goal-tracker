-- name: CreateUndoToken :one
-- Records the one-time token that lets account_id undo an action.
INSERT INTO undo_tokens (token, kind, subject_id, account_id, detail, issued_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUndoToken :one
SELECT * FROM undo_tokens WHERE token = ? LIMIT 1;

-- name: UseUndoToken :execrows
-- Spends a token, only if it isn't already, so it undoes at most once.
UPDATE undo_tokens SET used_at = ? WHERE id = ? AND used_at IS NULL;

-- name: CountLaterLinkRemovals :one
-- How many removals of the link between two Goals came after this one: any
-- means the link was made and removed again since.
SELECT COUNT(*) FROM link_removals
WHERE child_id = @child_id AND parent_id = @parent_id AND id > @id;

-- name: ListLinkRejectionTimes :many
-- When each rejected request for the link between two Goals was rejected.
SELECT rejected_at FROM rejected_link_requests WHERE child_id = ? AND parent_id = ?;

-- name: CountLaterHandoffs :one
-- How many Handoffs of a Goal came after this one.
SELECT COUNT(*) FROM handoffs WHERE goal_id = @goal_id AND id > @id;
