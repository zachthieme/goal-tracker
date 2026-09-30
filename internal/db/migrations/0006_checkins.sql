-- 0006_checkins: an Owner's routine update to one Goal (CONTEXT.md: Check-in).
--
-- A Check-in records the Goal's Health (Green/Yellow/Red), a short status, and —
-- when the Health is Yellow or Red — a Path to Green: the plan to get back to
-- Green, with the text and the target date for being back to Green (CONTEXT.md:
-- Path to Green). path_to_green and path_target_date are empty when the Health
-- is Green.
--
-- Check-ins are immutable: a new row is inserted each time, never updated, so the
-- full history is kept and the Goal's current Health, status, and Path to Green
-- are simply the latest row's. Each Check-in records both its author and the
-- Owner it was written for, so a Delegate's Check-in still credits the Owner it
-- speaks for (CONTEXT.md: Delegate).

CREATE TABLE checkins (
    id               INTEGER PRIMARY KEY,
    goal_id          INTEGER NOT NULL REFERENCES goals(id),
    author_id        INTEGER NOT NULL REFERENCES accounts(id),
    owner_id         INTEGER NOT NULL REFERENCES accounts(id),
    health           TEXT    NOT NULL,
    status           TEXT    NOT NULL,
    path_to_green    TEXT    NOT NULL DEFAULT '',
    path_target_date TEXT    NOT NULL DEFAULT '',
    created_at       TEXT    NOT NULL
);

CREATE INDEX idx_checkins_goal ON checkins (goal_id);
