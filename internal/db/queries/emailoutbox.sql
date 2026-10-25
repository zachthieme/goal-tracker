-- name: QueueEmail :exec
INSERT INTO email_outbox (recipient, account_id, subject, body, created_at, next_attempt_at)
VALUES (?, ?, ?, ?, ?, ?);

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

-- name: DeleteStaleEmail :execrows
-- Deletes the emails sent before cutoff, and those given up on (unsent and
-- tried max_attempts times) that were queued before it. An email still being
-- tried is kept however old. Times are compared as times: RFC 3339 text
-- doesn't sort, as its trailing zeros are trimmed.
DELETE FROM email_outbox
WHERE (sent_at IS NOT NULL AND julianday(sent_at) < julianday(CAST(sqlc.arg(cutoff) AS TEXT)))
   OR (sent_at IS NULL AND attempts >= sqlc.arg(max_attempts)
       AND julianday(created_at) < julianday(CAST(sqlc.arg(cutoff) AS TEXT)));
