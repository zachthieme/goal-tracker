-- 0023_dimension_value_order: an Admin sets the order of a Dimension's values
-- (ticket #71; CONTEXT.md: Dimension).
--
-- position is a value's place in its Dimension's list, lowest first, and is
-- the order values are listed in everywhere: the Goal page, the Goal list's
-- filter, and its groups. Every existing value starts in today's alphabetical
-- order (value, then id, as the list was sorted before). A value added later
-- takes the next position after the Dimension's last, so it lands at the end.

ALTER TABLE dimension_values ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

UPDATE dimension_values SET position = (
    SELECT COUNT(*) FROM dimension_values AS earlier
    WHERE earlier.dimension_id = dimension_values.dimension_id
      AND (earlier.value < dimension_values.value
           OR (earlier.value = dimension_values.value AND earlier.id < dimension_values.id))
);
