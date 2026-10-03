-- 0035_milestone_change_names: a Check-in's Milestone changes keep the
-- Milestone's name as it was (ticket #117; CONTEXT.md: Check-in, Milestone).
--
-- A Milestone can be renamed after the Check-in that added it, marked it Done
-- or marked it Removed. Reading its name through a join rewrote those earlier
-- entries, so each change now stores the name it was made under. Nothing
-- recorded the earlier name for existing rows, so they take the Milestone's
-- current name, the best available.

ALTER TABLE milestone_changes ADD COLUMN name TEXT NOT NULL DEFAULT '';

UPDATE milestone_changes SET name = (SELECT name FROM milestones WHERE id = milestone_id);
