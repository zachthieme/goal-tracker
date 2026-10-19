-- 0049_goal_comments: a Comment can be made on the Goal itself, not only on its
-- block in a published Report (ticket #350; CONTEXT.md: Comment).
--
-- A Comment on the Goal itself has publication_id 0, as parent_id 0 means "no
-- thread". 0016 declared publication_id a reference to report_publications;
-- SQLite can't drop a column constraint in place, so the table is rebuilt
-- without it: every existing Comment is copied across with its id,
-- publication, Goal, parent, author, body and timestamp, so the Action Items
-- raised from it still find it. The Goal index serves the Goal page's thread
-- and its History.

CREATE TABLE comments_new (
    id             INTEGER PRIMARY KEY,
    publication_id INTEGER NOT NULL DEFAULT 0,
    goal_id        INTEGER NOT NULL REFERENCES goals(id),
    parent_id      INTEGER NOT NULL DEFAULT 0,
    author_id      INTEGER NOT NULL REFERENCES accounts(id),
    body           TEXT    NOT NULL,
    created_at     TEXT    NOT NULL
);

INSERT INTO comments_new (id, publication_id, goal_id, parent_id, author_id, body, created_at)
SELECT id, publication_id, goal_id, parent_id, author_id, body, created_at FROM comments;

DROP TABLE comments;
ALTER TABLE comments_new RENAME TO comments;

CREATE INDEX idx_comments_publication ON comments (publication_id);
CREATE INDEX idx_comments_goal ON comments (goal_id);
