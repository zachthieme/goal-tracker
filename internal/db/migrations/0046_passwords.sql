-- 0046_passwords: password sign-in (ticket #300). account_passwords holds an
-- Account's password hash and its failed sign-ins; no row means no password.
-- password_links holds the one set-password link an Account may have
-- outstanding, by the SHA-256 of its token, never the token itself. Both are
-- kept out of accounts, whose columns sqlc writes into every query that reads
-- an Account.

CREATE TABLE account_passwords (
    account_id     INTEGER PRIMARY KEY REFERENCES accounts(id),
    password_hash  TEXT,
    failed_signins INTEGER NOT NULL DEFAULT 0,
    locked_until   TEXT
);

CREATE TABLE password_links (
    account_id INTEGER PRIMARY KEY REFERENCES accounts(id),
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL
);
