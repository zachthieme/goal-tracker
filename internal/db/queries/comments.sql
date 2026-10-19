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

-- name: ListGoalThreadComments :many
-- The Comments made on the Goal itself, not on a Report, with their authors,
-- oldest first, so each thread reads in the order it was written.
SELECT sqlc.embed(comments), sqlc.embed(accounts)
FROM comments
JOIN accounts ON accounts.id = comments.author_id
WHERE comments.goal_id = ? AND comments.publication_id = 0
ORDER BY comments.id;

-- name: ListGoalComments :many
-- Every Comment about the Goal, on the Goal itself or on any publication, with
-- its author, oldest first, for the Goal's History. A Report Comment carries
-- the Report's name and Definition; a Comment on the Goal has '' and 0.
SELECT sqlc.embed(comments), sqlc.embed(accounts),
       COALESCE(report_definitions.name, '') AS report_name,
       COALESCE(report_definitions.id, 0) AS definition_id
FROM comments
JOIN accounts ON accounts.id = comments.author_id
LEFT JOIN report_publications ON report_publications.id = comments.publication_id
LEFT JOIN report_definitions ON report_definitions.id = report_publications.report_definition_id
WHERE comments.goal_id = ?
ORDER BY comments.id;
