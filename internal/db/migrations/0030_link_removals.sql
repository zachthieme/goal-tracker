-- 0030_link_removals: a record of each removed "contributes to" link, so its
-- remover can Undo the removal (ticket #83; CONTEXT.md: Contributes to).
--
-- Removing an accepted link still deletes its row from links; this table keeps
-- what the link was (its two Goals, note, requester and when it was made) and
-- who removed it when. Undo puts the link back as accepted without asking the
-- parent's Owner again, so it is allowed only against a row here, only for its
-- remover, and only once: restored_at is set the first time it is undone and
-- refuses every later attempt. Rejected requests leave no record.

CREATE TABLE link_removals (
    id              INTEGER PRIMARY KEY,
    child_id        INTEGER NOT NULL REFERENCES goals(id),
    parent_id       INTEGER NOT NULL REFERENCES goals(id),
    note            TEXT    NOT NULL DEFAULT '',
    requested_by    INTEGER NOT NULL REFERENCES accounts(id),
    link_created_at TEXT    NOT NULL,
    removed_by      INTEGER NOT NULL REFERENCES accounts(id),
    removed_at      TEXT    NOT NULL,
    restored_at     TEXT
);
