-- name: CreateDraftHighlight :one
-- Log a Draft Highlight on a Goal (CONTEXT.md: Draft Highlight).
INSERT INTO draft_highlights (goal_id, kind, note, logged_by, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetDraftHighlight :one
SELECT sqlc.embed(draft_highlights), sqlc.embed(accounts)
FROM draft_highlights
JOIN accounts ON accounts.id = draft_highlights.logged_by
WHERE draft_highlights.id = ?;

-- name: ListPendingDraftHighlights :many
-- A Goal's pending Draft Highlights with who logged each, oldest first.
SELECT sqlc.embed(draft_highlights), sqlc.embed(accounts)
FROM draft_highlights
JOIN accounts ON accounts.id = draft_highlights.logged_by
WHERE draft_highlights.goal_id = ? AND draft_highlights.discarded_checkin_id IS NULL
ORDER BY draft_highlights.created_at, draft_highlights.id;

-- name: ListDiscardedDraftHighlightsByGoal :many
-- The Draft Highlights a Goal's Check-ins discarded, with who logged each,
-- oldest first.
SELECT sqlc.embed(draft_highlights), sqlc.embed(accounts)
FROM draft_highlights
JOIN accounts ON accounts.id = draft_highlights.logged_by
WHERE draft_highlights.goal_id = ? AND draft_highlights.discarded_checkin_id IS NOT NULL
ORDER BY draft_highlights.created_at, draft_highlights.id;

-- name: DeletePendingDraftHighlight :execrows
-- Delete a pending Draft Highlight: deleted by hand, or kept by a Check-in as
-- one of its Highlights. One already discarded is left alone.
DELETE FROM draft_highlights WHERE id = ? AND discarded_checkin_id IS NULL;

-- name: DiscardDraftHighlight :execrows
-- Discard a pending Draft Highlight with the Check-in that left it out.
UPDATE draft_highlights SET discarded_checkin_id = ?
WHERE id = ? AND discarded_checkin_id IS NULL;
