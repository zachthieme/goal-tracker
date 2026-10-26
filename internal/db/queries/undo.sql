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

-- name: CountLaterHandoffs :one
-- How many Handoffs of a Goal came after this one.
SELECT COUNT(*) FROM handoffs WHERE goal_id = @goal_id AND id > @id;

-- name: DeleteUndoTokensIssuedBefore :execrows
-- Deletes the tokens issued before cutoff, compared as times.
DELETE FROM undo_tokens WHERE julianday(issued_at) < julianday(CAST(sqlc.arg(cutoff) AS TEXT));
