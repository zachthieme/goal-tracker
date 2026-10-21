-- 0056_email_outbox: the email outbox, the emails a command queues inside its
-- transaction to be sent once it has committed (ticket #412). Sending inside
-- the transaction held the database's write lock for as long as the mail
-- server took, and a failure partway rolled the command back after some of
-- its emails had gone out.
--
-- Each row is one email to recipient, queued at created_at. attempts counts
-- the sends that failed, last_error is why the latest did, and next_attempt_at
-- is when it is next tried: created_at at first, then later after each
-- failure. sent_at is when it was sent, and NULL until then. A row that has
-- failed too often is left unsent and no longer tried. Rows are kept once
-- sent.

CREATE TABLE email_outbox (
    id              INTEGER PRIMARY KEY,
    recipient       TEXT    NOT NULL,
    subject         TEXT    NOT NULL,
    body            TEXT    NOT NULL,
    created_at      TEXT    NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT    NOT NULL,
    sent_at         TEXT,
    last_error      TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_email_outbox_unsent ON email_outbox (id) WHERE sent_at IS NULL;
