-- name: QueueEmail :exec
INSERT INTO email_outbox (recipient, subject, body, created_at, next_attempt_at)
VALUES (?, ?, ?, ?, ?);

-- name: ListUnsentEmail :many
-- The emails not yet sent and tried fewer than max_attempts times, oldest
-- first. Which are due is left to the caller, which compares next_attempt_at
-- as a time.
SELECT * FROM email_outbox
WHERE sent_at IS NULL AND attempts < sqlc.arg(max_attempts)
ORDER BY id;

-- name: MarkEmailSent :exec
UPDATE email_outbox SET sent_at = ? WHERE id = ?;

-- name: RecordEmailFailure :exec
-- A failed send: how many have failed now, why the latest did, and when to
-- try again.
UPDATE email_outbox SET attempts = ?, last_error = ?, next_attempt_at = ? WHERE id = ?;
