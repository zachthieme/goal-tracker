-- name: CreateParentSuggestion :one
INSERT INTO parent_suggestions (goal_id, parent_id, suggested_by, note, status, created_at)
VALUES (?, ?, ?, ?, 'open', ?)
RETURNING *;

-- name: GetParentSuggestion :one
-- A suggestion with its Goal, its parent, their Owners and its suggester.
SELECT sqlc.embed(parent_suggestions), sqlc.embed(goal), sqlc.embed(goal_owner), sqlc.embed(parent), sqlc.embed(parent_owner), sqlc.embed(suggester)
FROM parent_suggestions
JOIN goals goal ON goal.id = parent_suggestions.goal_id
JOIN accounts goal_owner ON goal_owner.id = goal.owner_id
JOIN goals parent ON parent.id = parent_suggestions.parent_id
JOIN accounts parent_owner ON parent_owner.id = parent.owner_id
JOIN accounts suggester ON suggester.id = parent_suggestions.suggested_by
WHERE parent_suggestions.id = ? LIMIT 1;

-- name: ListParentSuggestionsForGoal :many
-- Every suggestion made for a Goal, whatever its outcome, oldest first.
SELECT sqlc.embed(parent_suggestions), sqlc.embed(goal), sqlc.embed(goal_owner), sqlc.embed(parent), sqlc.embed(parent_owner), sqlc.embed(suggester)
FROM parent_suggestions
JOIN goals goal ON goal.id = parent_suggestions.goal_id
JOIN accounts goal_owner ON goal_owner.id = goal.owner_id
JOIN goals parent ON parent.id = parent_suggestions.parent_id
JOIN accounts parent_owner ON parent_owner.id = parent.owner_id
JOIN accounts suggester ON suggester.id = parent_suggestions.suggested_by
WHERE parent_suggestions.goal_id = @goal_id
ORDER BY parent_suggestions.created_at, parent_suggestions.id;

-- name: ListOpenParentSuggestionsForOwner :many
-- The open suggestions on the Goals a person Owns, awaiting their decision,
-- oldest first.
SELECT sqlc.embed(parent_suggestions), sqlc.embed(goal), sqlc.embed(goal_owner), sqlc.embed(parent), sqlc.embed(parent_owner), sqlc.embed(suggester)
FROM parent_suggestions
JOIN goals goal ON goal.id = parent_suggestions.goal_id
JOIN accounts goal_owner ON goal_owner.id = goal.owner_id
JOIN goals parent ON parent.id = parent_suggestions.parent_id
JOIN accounts parent_owner ON parent_owner.id = parent.owner_id
JOIN accounts suggester ON suggester.id = parent_suggestions.suggested_by
WHERE goal.owner_id = @owner_id AND parent_suggestions.status = 'open'
ORDER BY parent_suggestions.created_at, parent_suggestions.id;

-- name: GetOpenParentSuggestionFor :one
-- The open suggestion of a parent for a Goal, if there is one, with its
-- suggester.
SELECT sqlc.embed(parent_suggestions), sqlc.embed(suggester)
FROM parent_suggestions
JOIN accounts suggester ON suggester.id = parent_suggestions.suggested_by
WHERE parent_suggestions.goal_id = ? AND parent_suggestions.parent_id = ? AND parent_suggestions.status = 'open'
LIMIT 1;

-- name: CloseParentSuggestion :execrows
-- Gives an open suggestion its outcome, only while it is still open, so a
-- suggestion is decided once.
UPDATE parent_suggestions SET status = ?, closed_at = ? WHERE id = ? AND status = 'open';

-- name: ReopenParentSuggestion :execrows
-- Puts a declined suggestion back to open, only while it is still declined.
UPDATE parent_suggestions SET status = 'open', closed_at = NULL WHERE id = ? AND status = 'declined';

-- name: CloseOpenParentSuggestionsForLink :exec
-- Closes the open suggestions of parent_id for goal_id with an outcome, when
-- the link they propose comes about another way.
UPDATE parent_suggestions SET status = ?, closed_at = ?
WHERE goal_id = ? AND parent_id = ? AND status = 'open';

-- name: CloseOpenParentSuggestionsOfGoal :exec
-- Closes the open suggestions a Goal is either end of with an outcome, when
-- it ends.
UPDATE parent_suggestions SET status = @status, closed_at = @closed_at
WHERE (goal_id = @goal_id OR parent_id = @goal_id) AND status = 'open';
