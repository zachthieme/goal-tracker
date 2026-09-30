-- name: CreateComment :one
INSERT INTO comments (publication_id, goal_id, parent_id, author_id, body, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetComment :one
SELECT sqlc.embed(comments), sqlc.embed(accounts)
FROM comments
JOIN accounts ON accounts.id = comments.author_id
WHERE comments.id = ?
LIMIT 1;

-- name: ListPublicationComments :many
-- A publication's comments with their authors, oldest first, so each thread
-- reads in the order it was written.
SELECT sqlc.embed(comments), sqlc.embed(accounts)
FROM comments
JOIN accounts ON accounts.id = comments.author_id
WHERE comments.publication_id = ?
ORDER BY comments.id;

-- name: ListThreadAuthors :many
-- The distinct authors of a thread (its first comment and every reply), for
-- alerting everyone in it to a new reply.
SELECT DISTINCT accounts.*
FROM comments
JOIN accounts ON accounts.id = comments.author_id
WHERE comments.id = sqlc.arg(thread_id) OR comments.parent_id = sqlc.arg(thread_id)
ORDER BY accounts.email;
