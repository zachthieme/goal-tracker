-- 0037_parent_suggestions: Parent suggestions, each proposing a parent for
-- someone else's Goal (ticket #144; CONTEXT.md: Parent suggestion; ADR 0006).
--
-- A suggestion is its own record, not a link status: goal_id is the Goal a
-- parent is suggested for, parent_id the suggested parent, suggested_by who
-- suggested it, with an optional note, made at created_at. Only the Goal's
-- Owner decides it. Every suggestion is kept with its outcome in status:
-- 'open' until one of 'accepted', 'declined', 'withdrawn' (by its suggester)
-- or 'no longer applies' (the link came about another way, or the Goal or
-- parent ended), at closed_at. An Undo of a decline puts it back to 'open',
-- clearing closed_at.

CREATE TABLE parent_suggestions (
    id           INTEGER PRIMARY KEY,
    goal_id      INTEGER NOT NULL REFERENCES goals(id),
    parent_id    INTEGER NOT NULL REFERENCES goals(id),
    suggested_by INTEGER NOT NULL REFERENCES accounts(id),
    note         TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'open',
    created_at   TEXT    NOT NULL,
    closed_at    TEXT
);

CREATE INDEX idx_parent_suggestions_goal ON parent_suggestions (goal_id);
CREATE INDEX idx_parent_suggestions_parent ON parent_suggestions (parent_id);
