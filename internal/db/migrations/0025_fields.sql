-- 0025_fields: admin-defined attributes whose value is typed directly on a Goal
-- (ticket #73; CONTEXT.md: Field, Retired; ADR 0005).
--
-- A Field describes a Goal and never filters, groups or sums. type is fixed at
-- creation: 'number', 'short_text', 'long_text' or 'date'. unit is a number
-- Field's optional label (e.g. "$", "FTE") and empty for the other types.
-- retired is 1 for a Field an Admin has withdrawn: it is no longer offered for
-- entry, yet Goals with a value in it still show it. Nothing is deleted, so
-- restoring it (back to 0) offers it again.
--
-- goal_field_values holds at most one value per Goal and Field, as text: a
-- number in its plain decimal form and a date as YYYY-MM-DD. Clearing a value
-- deletes its row.

CREATE TABLE fields (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    type       TEXT    NOT NULL CHECK (type IN ('number', 'short_text', 'long_text', 'date')),
    unit       TEXT    NOT NULL DEFAULT '',
    retired    INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL
);

CREATE TABLE goal_field_values (
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    field_id   INTEGER NOT NULL REFERENCES fields(id),
    value      TEXT    NOT NULL,
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (goal_id, field_id)
);
