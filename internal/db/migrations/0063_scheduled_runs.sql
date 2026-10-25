-- 0063_scheduled_runs: the slot each scheduled job last ran for (ticket
-- #497), so a restart neither skips a week's emails nor sends them twice. The
-- schedule lived only in memory: restarted just after its slot, the weekly
-- emails waited a whole week.
--
-- job names the job (the weekly emails are "weekly emails"), and slot is the
-- time it was due to run, in UTC, RFC 3339: a row says the job has run for
-- that slot and every one before it. A job's run records its slot in the
-- transaction that does its work, and only a slot later than the one
-- recorded, so two runs of one slot do its work once.

CREATE TABLE scheduled_runs (
    job  TEXT PRIMARY KEY,
    slot TEXT NOT NULL
);
