-- 0005_dimensions: admin-defined attributes for slicing the Goal list
-- (CONTEXT.md: Dimension).
--
-- A Dimension is an attribute with a fixed list of values (e.g. pillar, quarter,
-- goal kind) that an Admin defines. Its values live in dimension_values; a value
-- can be retired so it is no longer offered for new assignments, yet stays
-- readable on the Goals that already carry it. Renaming a value keeps its row, so
-- every Goal assigned it follows the rename.
--
-- An Owner assigns Dimension values to their Goals through goal_dimension_values.
-- A Goal carries at most one value per Dimension (an attribute has one value), so
-- assigning a new value in a Dimension replaces the Goal's previous one; the
-- Goal list then filters and groups on those assignments.

CREATE TABLE dimensions (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

CREATE TABLE dimension_values (
    id           INTEGER PRIMARY KEY,
    dimension_id INTEGER NOT NULL REFERENCES dimensions(id),
    value        TEXT    NOT NULL,
    retired      INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT    NOT NULL
);

CREATE INDEX idx_dimension_values_dimension ON dimension_values (dimension_id);

CREATE TABLE goal_dimension_values (
    id                 INTEGER PRIMARY KEY,
    goal_id            INTEGER NOT NULL REFERENCES goals(id),
    dimension_value_id INTEGER NOT NULL REFERENCES dimension_values(id),
    created_at         TEXT    NOT NULL,
    UNIQUE (goal_id, dimension_value_id)
);

CREATE INDEX idx_goal_dimension_values_goal ON goal_dimension_values (goal_id);
CREATE INDEX idx_goal_dimension_values_value ON goal_dimension_values (dimension_value_id);
