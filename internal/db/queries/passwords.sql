-- name: UpsertPasswordLink :exec
-- Issues an Account's set-password link, replacing any it had.
INSERT INTO password_links (account_id, token_hash, expires_at) VALUES (?, ?, ?)
ON CONFLICT (account_id) DO UPDATE SET token_hash = excluded.token_hash, expires_at = excluded.expires_at;

-- name: GetPasswordLink :one
SELECT * FROM password_links WHERE token_hash = ?;

-- name: UseUpPasswordLink :execrows
-- Deletes the set-password link with the token hash, reporting whether there
-- was one, so of two uses of a link only one finds it.
DELETE FROM password_links WHERE token_hash = ?;

-- name: GetAccountPassword :one
-- An Account's password and failed sign-ins; no row means no password.
SELECT * FROM account_passwords WHERE account_id = ?;

-- name: SetAccountPassword :exec
-- Stores an Account's password hash and clears its failed sign-ins and lock.
INSERT INTO account_passwords (account_id, password_hash, failed_signins, locked_until) VALUES (?, ?, 0, NULL)
ON CONFLICT (account_id) DO UPDATE SET password_hash = excluded.password_hash, failed_signins = 0, locked_until = NULL;

-- name: RecordFailedSignin :exec
-- Counts a wrong password against an Account. The one that brings the count
-- to sqlc.arg(lock_at) locks the Account until sqlc.arg(locked_until) and
-- starts the count again.
UPDATE account_passwords SET
    failed_signins = CASE WHEN failed_signins + 1 >= sqlc.arg(lock_at) THEN 0 ELSE failed_signins + 1 END,
    locked_until = CASE WHEN failed_signins + 1 >= sqlc.arg(lock_at) THEN sqlc.arg(locked_until) ELSE locked_until END
WHERE account_id = sqlc.arg(account_id);

-- name: ClearFailedSignins :exec
UPDATE account_passwords SET failed_signins = 0, locked_until = NULL WHERE account_id = ?;
