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

-- Back-fill, once, the changes the existing rows record, in the order they
-- happened. An Accepted link stands for being linked and a Pending one for its
-- request, by its requester when it was made: when or by whom an Accepted link
-- was accepted was never kept. A rejection, or a removal, is preceded by the
-- request, or the link, it ended unless it was undone, which put that row
-- back in links. Who undid it was never kept either, but only whoever
-- rejected or removed the link may.
INSERT INTO link_events (child_id, parent_id, kind, actor_id, created_at)
SELECT child_id, parent_id, kind, actor_id, created_at
FROM (
    SELECT child_id, parent_id,
           CASE status WHEN 'accepted' THEN 'linked' ELSE 'requested' END AS kind,
           requested_by AS actor_id, created_at, 0 AS step
    FROM links
    UNION ALL
    SELECT child_id, parent_id, 'requested', requested_by, request_created_at, 0
    FROM rejected_link_requests WHERE restored_at IS NULL
    UNION ALL
    SELECT child_id, parent_id, 'rejected', rejected_by, rejected_at, 1
    FROM rejected_link_requests
    UNION ALL
    SELECT child_id, parent_id, 'rejection-undone', rejected_by, restored_at, 2
    FROM rejected_link_requests WHERE restored_at IS NOT NULL
    UNION ALL
    SELECT child_id, parent_id, 'linked', requested_by, link_created_at, 0
    FROM link_removals WHERE restored_at IS NULL
    UNION ALL
    SELECT child_id, parent_id, 'removed', removed_by, removed_at, 1
    FROM link_removals
    UNION ALL
    SELECT child_id, parent_id, 'removal-undone', removed_by, restored_at, 2
    FROM link_removals WHERE restored_at IS NOT NULL
)
ORDER BY created_at, step;
