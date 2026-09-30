-- 0002_links: the "contributes to" links that connect Goals into a graph.
--
-- A link records that a child Goal contributes to a parent Goal (CONTEXT.md:
-- Contributes to). It starts life 'pending' as a request from the child's Owner
-- and becomes 'accepted' only when the parent's Owner accepts it (ADR-0001);
-- rejecting or removing a link deletes the row. A Goal may have many parents and
-- many children, and the accepted links never form a cycle. An optional note
-- travels with the request.

CREATE TABLE links (
    id           INTEGER PRIMARY KEY,
    child_id     INTEGER NOT NULL REFERENCES goals(id),
    parent_id    INTEGER NOT NULL REFERENCES goals(id),
    status       TEXT    NOT NULL,
    note         TEXT    NOT NULL DEFAULT '',
    requested_by INTEGER NOT NULL REFERENCES accounts(id),
    created_at   TEXT    NOT NULL,
    UNIQUE (child_id, parent_id)
);

CREATE INDEX idx_links_parent ON links (parent_id);
CREATE INDEX idx_links_child ON links (child_id);
