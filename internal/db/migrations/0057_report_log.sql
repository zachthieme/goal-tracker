-- 0057_report_log: the Report log, every change to a Report Definition (ticket
-- #435; CONTEXT.md: Report log).
--
-- Each row is one change to the Definition report_definition_id: who made it
-- (actor_id), when, its kind — created, edited (its title, introduction,
-- rules, picked Goals, Also include, Leave out or Fields), delegate (a Report
-- Delegate added or removed), narrative (the next publication's narrative
-- curated) or published — and detail, what changed in words, before → after,
-- written with the names as they were at the time so a later rename doesn't
-- rewrite it. Each is written in the same transaction as its change, and
-- nothing edits or removes a row once written: the log records, it doesn't
-- undo. Changes made before this migration aren't in it.

CREATE TABLE report_log (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    actor_id             INTEGER NOT NULL REFERENCES accounts(id),
    kind                 TEXT    NOT NULL CHECK (kind IN ('created', 'edited', 'delegate', 'narrative', 'published')),
    detail               TEXT    NOT NULL,
    created_at           TEXT    NOT NULL
);

CREATE INDEX idx_report_log_definition ON report_log (report_definition_id);

CREATE TRIGGER report_log_never_edited
BEFORE UPDATE ON report_log
BEGIN
    SELECT RAISE(ABORT, 'the Report log is never edited');
END;

CREATE TRIGGER report_log_never_removed
BEFORE DELETE ON report_log
BEGIN
    SELECT RAISE(ABORT, 'the Report log is never edited');
END;
