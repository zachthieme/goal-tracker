-- 0013_top_level_goals: the Goals an Admin marks as the org's root outcomes
-- (ticket #11; CONTEXT.md: Top-level Goal, Unaligned).
--
-- A Top-level Goal isn't expected to contribute to anything, so it is never
-- Unaligned. Only an Admin marks or unmarks it; every Goal starts unmarked.
ALTER TABLE goals ADD COLUMN top_level INTEGER NOT NULL DEFAULT 0;
