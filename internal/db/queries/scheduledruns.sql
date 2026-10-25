-- name: GetScheduledRun :one
-- The slot a scheduled job last ran for.
SELECT slot FROM scheduled_runs WHERE job = ?;

-- name: RecordScheduledRun :execrows
-- Records slot as the job's last run, unless the slot recorded is the same or
-- later: no row changes then. Slots are UTC RFC 3339 times to the second, so
-- they sort as text.
INSERT INTO scheduled_runs (job, slot) VALUES (@job, @slot)
ON CONFLICT (job) DO UPDATE SET slot = excluded.slot
WHERE excluded.slot > scheduled_runs.slot;
