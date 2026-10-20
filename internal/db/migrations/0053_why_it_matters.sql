-- 0053_why_it_matters: a Goal's lead text is called Why it matters, no longer
-- So What (ticket #381; CONTEXT.md: Why it matters), so its column, its
-- revisions table and that table's column and index take the new name. The
-- data is kept. SQLite has no ALTER INDEX, so the index is made again.

ALTER TABLE goals RENAME COLUMN so_what TO why_it_matters;

DROP INDEX idx_so_what_revisions_goal;
ALTER TABLE so_what_revisions RENAME TO why_it_matters_revisions;
ALTER TABLE why_it_matters_revisions RENAME COLUMN so_what TO why_it_matters;
CREATE INDEX idx_why_it_matters_revisions_goal ON why_it_matters_revisions (goal_id);
