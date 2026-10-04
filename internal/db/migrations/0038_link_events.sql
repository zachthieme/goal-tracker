-- 0038_link_events: a log of every change to a "contributes to" link, for the
-- History of both its Goals (ticket #145; CONTEXT.md: Contributes to).
--
-- Each row is one change to the link from child_id to parent_id: kind is one
-- of 'requested', 'linked' (made Accepted without a separate accept: a
-- request to a Goal the requester owns, or an import), 'accepted',
-- 'rejected', 'rejection-undone', 'removed' or 'removal-undone', made by
-- actor_id at created_at. The log is append-only; links,
-- rejected_link_requests and link_removals stay the record of a link's state.

CREATE TABLE link_events (
    id         INTEGER PRIMARY KEY,
    child_id   INTEGER NOT NULL REFERENCES goals(id),
    parent_id  INTEGER NOT NULL REFERENCES goals(id),
    kind       TEXT    NOT NULL,
    actor_id   INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL
);

CREATE INDEX idx_link_events_child ON link_events (child_id);
CREATE INDEX idx_link_events_parent ON link_events (parent_id);
