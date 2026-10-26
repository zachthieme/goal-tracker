-- name: RecordReportLogEntry :exec
-- Keep one change to a Report Definition in its Report log.
INSERT INTO report_log (report_definition_id, actor_id, kind, detail, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: ListReportLog :many
-- A Report Definition's Report log, newest first, with the Account that made
-- each change resolved for display.
SELECT sqlc.embed(report_log), sqlc.embed(accounts)
FROM report_log
JOIN accounts ON accounts.id = report_log.actor_id
WHERE report_log.report_definition_id = ?
ORDER BY report_log.id DESC;

-- name: ListReportLogHighlightNotes :many
-- The notes of the given Highlights, for the Report log to name the ones a
-- narrative picked or dropped.
SELECT id, note FROM highlights
WHERE id IN (sqlc.slice(ids));

-- name: LatestReportLogEntryID :one
-- The id of a Report Definition's newest Report log entry, or 0 when it has
-- none: a publish compares it before and after drafting to tell whether the
-- Report changed meanwhile.
SELECT CAST(COALESCE(MAX(id), 0) AS INTEGER) FROM report_log
WHERE report_definition_id = ?;
