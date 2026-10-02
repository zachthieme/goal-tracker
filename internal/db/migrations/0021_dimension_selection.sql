-- 0021_dimension_selection: an Admin chooses whether a Goal takes one value or
-- several in a Dimension (ticket #69; CONTEXT.md: Dimension; ADR 0005).
--
-- selection is 'one' (assigning a value replaces the Goal's previous one in
-- that Dimension) or 'several' (a Goal carries any number of the Dimension's
-- values and appears under each when the Goal list is grouped by it). Every
-- existing Dimension keeps its one-value behaviour. goal_dimension_values
-- already allows several rows per Goal and Dimension, so it is unchanged.

ALTER TABLE dimensions ADD COLUMN selection TEXT NOT NULL DEFAULT 'one'
    CHECK (selection IN ('one', 'several'));
