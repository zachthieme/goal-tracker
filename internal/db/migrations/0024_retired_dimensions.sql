-- 0024_retired_dimensions: an Admin retires a whole Dimension, and can restore
-- it (ticket #72; CONTEXT.md: Retired; ADR 0005).
--
-- retired is 1 for a Dimension an Admin has withdrawn: it is no longer offered
-- when setting a Goal's values, nor in the Goal list's filter and grouping or
-- the Report Definition form, yet the Goals carrying its values still show
-- them and saved Report Definitions filtering on them keep selecting the same
-- Goals. Nothing is deleted, so restoring it (back to 0) returns it everywhere.
-- Every existing Dimension stays in use.

ALTER TABLE dimensions ADD COLUMN retired INTEGER NOT NULL DEFAULT 0;
