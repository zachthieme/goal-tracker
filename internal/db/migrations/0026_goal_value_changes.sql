-- 0026_goal_value_changes: a Goal's Value history, every change to its
-- Dimension values and Fields (ticket #74; CONTEXT.md: Field — changes to a
-- Goal's Fields and Dimension values are kept in the Goal's history).
--
-- Each row is one change: who made it (actor_id), when, which Dimension or
-- Field (exactly one of dimension_id and field_id is set), and the value before
-- and after as text, empty for none. attribute is the Dimension's or Field's
-- name, and before_value and after_value the values' text, all as they were at
-- the time, so a later rename or merge doesn't rewrite the history. several is
-- 1 for a value added (before empty) or removed (after empty) in a Dimension
-- that took several values at the time. Changes made before this migration
-- have no history.

CREATE TABLE goal_value_changes (
    id           INTEGER PRIMARY KEY,
    goal_id      INTEGER NOT NULL REFERENCES goals(id),
    actor_id     INTEGER NOT NULL REFERENCES accounts(id),
    dimension_id INTEGER REFERENCES dimensions(id),
    field_id     INTEGER REFERENCES fields(id),
    attribute    TEXT    NOT NULL,
    several      INTEGER NOT NULL DEFAULT 0,
    before_value TEXT    NOT NULL,
    after_value  TEXT    NOT NULL,
    created_at   TEXT    NOT NULL,
    CHECK ((dimension_id IS NULL) <> (field_id IS NULL))
);

CREATE INDEX idx_goal_value_changes_goal ON goal_value_changes (goal_id);
