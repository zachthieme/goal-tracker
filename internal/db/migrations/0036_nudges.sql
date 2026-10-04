-- 0036_nudges: Nudges, each a one-off request from someone else to a Stale
-- or Path-to-Green-overdue Goal's Owner and Delegates to check in (ticket
-- #143; CONTEXT.md: Nudge).
--
-- goal_id is the Goal nudged, sent_by who nudged it, at created_at. A Goal is
-- nudged at most once a calendar day in the org's timezone, from anyone:
-- nudged_on is that day, and the unique index keeps a second Nudge out even
-- when two are sent at once. Every Nudge is kept, for the Goal's History.

CREATE TABLE nudges (
    id         INTEGER PRIMARY KEY,
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    sent_by    INTEGER NOT NULL REFERENCES accounts(id),
    nudged_on  TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

CREATE UNIQUE INDEX idx_nudges_goal_day ON nudges (goal_id, nudged_on);
