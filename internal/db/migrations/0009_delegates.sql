-- 0009_delegates: the people an Owner authorizes to write Check-ins on a Goal.
--
-- A Delegate is a person an Owner authorizes to write and submit Check-ins for a
-- Goal; accountability stays with the Owner, and each Check-in still records who
-- wrote it (CONTEXT.md: Delegate). The delegates table lists, per Goal, which
-- Accounts the Owner has authorized. An Account may be a Delegate on a Goal only
-- once (UNIQUE), and removing a Delegate deletes its row.

CREATE TABLE delegates (
    id         INTEGER PRIMARY KEY,
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL,
    UNIQUE (goal_id, account_id)
);

CREATE INDEX idx_delegates_goal ON delegates (goal_id);
CREATE INDEX idx_delegates_account ON delegates (account_id);
