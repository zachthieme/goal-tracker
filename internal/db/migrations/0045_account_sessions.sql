-- 0045_account_sessions: each Account's session generation, which its session
-- cookies carry (ticket #299). Moving it on ends every session issued before,
-- so all of a person's sessions can be ended at once. No row means generation
-- 0. Kept out of accounts, whose columns sqlc writes into every query that
-- reads an Account.

CREATE TABLE account_sessions (
    account_id INTEGER PRIMARY KEY REFERENCES accounts(id),
    generation INTEGER NOT NULL
);
