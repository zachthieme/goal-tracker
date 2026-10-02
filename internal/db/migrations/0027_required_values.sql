-- 0027_required_values: an Admin marks a Dimension or a Field required, and can
-- unmark it (ticket #75; CONTEXT.md: Incomplete).
--
-- required is 1 for a Dimension or Field every Active Goal should have a value
-- in. A Proposed Goal lacking a value in one that is required and not Retired
-- can't become Active; an Active Goal lacking one is Incomplete, which is worked
-- out when read and never stored, so marking or unmarking takes effect on every
-- Goal at once. Nothing is required until an Admin says so.

ALTER TABLE dimensions ADD COLUMN required INTEGER NOT NULL DEFAULT 0;

ALTER TABLE fields ADD COLUMN required INTEGER NOT NULL DEFAULT 0;
