-- name: GetSessionGeneration :one
-- An Account's session generation; no row means 0.
SELECT generation FROM account_sessions WHERE account_id = ?;

-- name: EndAccountSessions :exec
-- Moves an Account's session generation on by one, from 0 when it has no row.
INSERT INTO account_sessions (account_id, generation) VALUES (?, 1)
ON CONFLICT (account_id) DO UPDATE SET generation = generation + 1;
