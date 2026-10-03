-- 0033_undo_tokens: the one-time token behind each Undo a toast offers (ticket
-- #106).
--
-- Removing a link, rejecting a link request, rejecting a Handoff and retiring a
-- Dimension value each offer an Undo. The action records a random token here,
-- naming what it was (kind and subject_id: the link removal, link rejection,
-- Handoff or value) and who may undo it (account_id). An Undo succeeds only
-- with a token that matches its action and person, within 15 minutes of
-- issued_at, and only once: used_at is set the first time the token is
-- presented, even when the Undo is then refused. detail keeps what the Undo
-- must find unchanged, for a retired value its name when retired.
--
-- Actions taken before this migration have no token, so they can't be undone.

CREATE TABLE undo_tokens (
    id         INTEGER PRIMARY KEY,
    token      TEXT    NOT NULL UNIQUE,
    kind       TEXT    NOT NULL,
    subject_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    detail     TEXT    NOT NULL DEFAULT '',
    issued_at  TEXT    NOT NULL,
    used_at    TEXT
);
