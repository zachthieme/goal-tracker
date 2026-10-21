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
