-- 0032_rejected_link_requests: a record of each rejected link request, so the
-- parent's Owner who rejected it can Undo the rejection (ticket #96;
-- CONTEXT.md: Contributes to).
--
-- Rejecting a pending request still deletes its row from links; this table
-- keeps what the request was (its two Goals, note, requester and when it was
-- made) and who rejected it when. Undo puts the request back as pending, so it
-- is allowed only against a row here, only for its rejecter, and only once:
-- restored_at is set the first time it is undone and refuses every later
-- attempt.

CREATE TABLE rejected_link_requests (
    id                 INTEGER PRIMARY KEY,
    child_id           INTEGER NOT NULL REFERENCES goals(id),
    parent_id          INTEGER NOT NULL REFERENCES goals(id),
    note               TEXT    NOT NULL DEFAULT '',
    requested_by       INTEGER NOT NULL REFERENCES accounts(id),
    request_created_at TEXT    NOT NULL,
    rejected_by        INTEGER NOT NULL REFERENCES accounts(id),
    rejected_at        TEXT    NOT NULL,
    restored_at        TEXT
);

CREATE INDEX idx_rejected_link_requests_pair ON rejected_link_requests (child_id, parent_id);
