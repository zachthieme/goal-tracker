-- name: UpsertPasswordLink :exec
-- Issues an Account's set-password link, replacing any it had.
INSERT INTO password_links (account_id, token_hash, expires_at) VALUES (?, ?, ?)
ON CONFLICT (account_id) DO UPDATE SET token_hash = excluded.token_hash, expires_at = excluded.expires_at;

-- name: GetPasswordLink :one
SELECT * FROM password_links WHERE token_hash = ?;

-- name: DeletePasswordLink :exec
DELETE FROM password_links WHERE account_id = ?;

-- name: GetAccountPassword :one
-- An Account's password and failed sign-ins; no row means no password.
SELECT * FROM account_passwords WHERE account_id = ?;

-- name: SetAccountPassword :exec
-- Stores an Account's password hash and clears its failed sign-ins and lock.
INSERT INTO account_passwords (account_id, password_hash, failed_signins, locked_until) VALUES (?, ?, 0, NULL)
ON CONFLICT (account_id) DO UPDATE SET password_hash = excluded.password_hash, failed_signins = 0, locked_until = NULL;

-- name: SetPasswordFailures :exec
-- Records an Account's failed sign-ins since its last success, and the lock
-- they put it under, if any.
UPDATE account_passwords SET failed_signins = ?, locked_until = ? WHERE account_id = ?;
