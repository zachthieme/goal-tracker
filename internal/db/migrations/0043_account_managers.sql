-- 0043_account_managers: each Account can have a Manager, another Account,
-- as the org's directory records it (ticket #262; CONTEXT.md: Manager, Chain;
-- ADR 0008). Only the directory sync sets it. Existing Accounts have none.

ALTER TABLE accounts ADD COLUMN manager_id INTEGER REFERENCES accounts(id);

CREATE INDEX idx_accounts_manager ON accounts (manager_id);
