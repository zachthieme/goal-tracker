-- 0022_extendable_dimensions: an Admin chooses whether a Dimension's list is
-- Fixed or Extendable (ticket #70; CONTEXT.md: Fixed, Extendable; ADR 0005).
--
-- list is 'fixed' (only an Admin adds values, the behaviour every Dimension
-- had before) or 'extendable' (anyone setting a Goal's value in the Dimension
-- may add one). Switching back to 'fixed' only stops further additions; values
-- already added stay. Every existing Dimension stays Fixed.

ALTER TABLE dimensions ADD COLUMN list TEXT NOT NULL DEFAULT 'fixed'
    CHECK (list IN ('fixed', 'extendable'));
