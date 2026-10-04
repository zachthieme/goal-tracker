-- 0041_draft_highlights: Draft Highlights, each logged on a Goal between
-- Check-ins and waiting for the next one (ticket #188; CONTEXT.md: Draft
-- Highlight).
--
-- goal_id is the Goal it was logged on, logged_by who logged it, at
-- created_at, with its note and kind ('' for none, else 'Insight',
-- 'Accomplishment' or 'Miss'). A row is pending while discarded_checkin_id is
-- NULL. The next Check-in offers each pending one: one kept becomes a
-- Highlight of that Check-in and its row is deleted; one left out is
-- discarded, which sets discarded_checkin_id to that Check-in, kept for the
-- Goal's History. A pending one deleted by its Goal's Owner or a Delegate is
-- deleted outright.

CREATE TABLE draft_highlights (
    id                   INTEGER PRIMARY KEY,
    goal_id              INTEGER NOT NULL REFERENCES goals(id),
    kind                 TEXT    NOT NULL DEFAULT '',
    note                 TEXT    NOT NULL,
    logged_by            INTEGER NOT NULL REFERENCES accounts(id),
    created_at           TEXT    NOT NULL,
    discarded_checkin_id INTEGER REFERENCES checkins(id)
);

CREATE INDEX idx_draft_highlights_goal ON draft_highlights (goal_id);
CREATE INDEX idx_draft_highlights_discarded ON draft_highlights (discarded_checkin_id);
