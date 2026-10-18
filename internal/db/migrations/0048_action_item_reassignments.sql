-- 0048_action_item_reassignments: an open Action Item may be handed to a new
-- owner (ticket #321; CONTEXT.md: Action Item).
--
-- Each row records one reassignment: who owned the item before and after, who
-- reassigned it, and when. action_items.owner_id holds the current owner; the
-- latest row here names who had it before. The index serves an item's own
-- history and its latest reassignment.

CREATE TABLE action_item_reassignments (
    id             INTEGER PRIMARY KEY,
    action_item_id INTEGER NOT NULL REFERENCES action_items(id),
    from_owner_id  INTEGER NOT NULL REFERENCES accounts(id),
    to_owner_id    INTEGER NOT NULL REFERENCES accounts(id),
    reassigned_by  INTEGER NOT NULL REFERENCES accounts(id),
    reassigned_at  TEXT    NOT NULL
);

CREATE INDEX idx_action_item_reassignments_item ON action_item_reassignments (action_item_id);
