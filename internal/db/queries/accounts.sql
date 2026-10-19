-- name: GetAccountByEmail :one
-- An email names one Account whatever its case or surrounding spaces
-- (CONTEXT.md: Account): CreateAccount stores it trimmed and lowercased, and
-- this lookup folds its argument the same way, so no caller has to.
SELECT * FROM accounts WHERE email = lower(trim(sqlc.arg(email))) LIMIT 1;

-- name: CreateAccount :one
INSERT INTO accounts (email, is_admin, created_at)
VALUES (lower(trim(sqlc.arg(email))), sqlc.arg(is_admin), sqlc.arg(created_at))
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = ? LIMIT 1;

-- name: SetAccountName :exec
UPDATE accounts SET name = ? WHERE id = ?;

-- name: ListDepartedAccounts :many
SELECT * FROM accounts WHERE departed = 1 ORDER BY email;

-- name: SetAccountManager :exec
UPDATE accounts SET manager_id = ? WHERE id = ?;

-- name: ListAccountsUnder :many
-- The people whose Manager is one of the given Accounts: one level of an Org.
SELECT * FROM accounts WHERE manager_id IN (sqlc.slice(manager_ids)) ORDER BY id;

-- name: ListManagedAccounts :many
-- Every Account with a Manager, for finding the cycles the directory gave.
SELECT * FROM accounts WHERE manager_id IS NOT NULL ORDER BY id;

-- name: ListAdminAccounts :many
SELECT * FROM accounts WHERE is_admin = 1 ORDER BY email;

-- name: SetAccountAdmin :exec
UPDATE accounts SET is_admin = ? WHERE id = ?;

-- name: SetAccountTimezone :exec
UPDATE accounts SET timezone = ? WHERE id = ?;
