-- 0044_goal_title_revisions: a Goal's title can be changed by its Owner or an
-- Admin, and each change is kept with the old and new titles so it shows in the
-- Goal's History (ticket #281; CONTEXT.md: Goal). Shaped like
-- so_what_revisions.

CREATE TABLE goal_title_revisions (
    id         INTEGER PRIMARY KEY,
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    old_title  TEXT    NOT NULL,
    new_title  TEXT    NOT NULL,
    author_id  INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL
);

CREATE INDEX idx_goal_title_revisions_goal ON goal_title_revisions (goal_id);
