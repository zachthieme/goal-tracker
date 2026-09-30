-- 0012_lifecycle_changes: Lifecycle changes recorded in a Check-in (ticket #7;
-- CONTEXT.md: Lifecycle).
--
-- An Owner puts a Goal On Hold, Cancels it, marks it Done, or resumes an On Hold
-- Goal in a Check-in, so each change sits in the Goal's Check-in history with
-- who made it and when. lifecycle_from and lifecycle_to are empty when the
-- Check-in left the Lifecycle alone. Leaving Active for any state other than
-- Done requires a reason (lifecycle_reason); reaching Done requires a one-line
-- outcome, and the same Check-in's Metric readings are the final values.
ALTER TABLE checkins ADD COLUMN lifecycle_from   TEXT NOT NULL DEFAULT '';
ALTER TABLE checkins ADD COLUMN lifecycle_to     TEXT NOT NULL DEFAULT '';
ALTER TABLE checkins ADD COLUMN lifecycle_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE checkins ADD COLUMN outcome          TEXT NOT NULL DEFAULT '';
