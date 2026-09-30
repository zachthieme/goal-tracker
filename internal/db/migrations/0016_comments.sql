-- 0016_comments: questions on a published Report (ticket #19).
--
-- A reader comments on one Goal's block in a publication, starting a thread
-- that is routed to the Goal's Owner; the Owner and others reply in it. A
-- thread's first comment has parent_id 0; every reply points at that first
-- comment, so a thread is one level deep. Comments are never edited.

CREATE TABLE comments (
    id             INTEGER PRIMARY KEY,
    publication_id INTEGER NOT NULL REFERENCES report_publications(id),
    goal_id        INTEGER NOT NULL REFERENCES goals(id),
    parent_id      INTEGER NOT NULL DEFAULT 0,
    author_id      INTEGER NOT NULL REFERENCES accounts(id),
    body           TEXT    NOT NULL,
    created_at     TEXT    NOT NULL
);

CREATE INDEX idx_comments_publication ON comments (publication_id);
