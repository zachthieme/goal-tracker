-- 0062_email_outbox_account: each queued email names the Account it is for
-- (ticket #498), so the outbox's log lines name the recipient by Account ID
-- rather than by their address.
--
-- account_id is whose address recipient is. SQLite can't ADD COLUMN a NOT
-- NULL foreign key, so the table is rebuilt: every existing email is copied
-- across with its id and columns, and takes the Account whose address it was
-- sent to. An email to an address no Account has is dropped.
--
-- Rows are no longer kept for good: a sent email, and one given up on, is
-- deleted 90 days on (PruneStale).

CREATE TABLE email_outbox_new (
    id              INTEGER PRIMARY KEY,
    recipient       TEXT    NOT NULL,
    account_id      INTEGER NOT NULL REFERENCES accounts(id),
    subject         TEXT    NOT NULL,
    body            TEXT    NOT NULL,
    created_at      TEXT    NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT    NOT NULL,
    sent_at         TEXT,
    last_error      TEXT    NOT NULL DEFAULT ''
);

INSERT INTO email_outbox_new (id, recipient, account_id, subject, body, created_at, attempts, next_attempt_at, sent_at, last_error)
SELECT o.id, o.recipient, a.id, o.subject, o.body, o.created_at, o.attempts, o.next_attempt_at, o.sent_at, o.last_error
FROM email_outbox o JOIN accounts a ON a.email = o.recipient;

DROP TABLE email_outbox;
ALTER TABLE email_outbox_new RENAME TO email_outbox;

CREATE INDEX idx_email_outbox_unsent ON email_outbox (id) WHERE sent_at IS NULL;
