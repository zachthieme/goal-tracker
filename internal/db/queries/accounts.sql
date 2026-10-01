-- name: GetAccountByEmail :one
SELECT * FROM accounts WHERE email = ? LIMIT 1;

-- name: CreateAccount :one
INSERT INTO accounts (email, is_admin, created_at)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = ? LIMIT 1;

-- name: SetAccountName :exec
UPDATE accounts SET name = ? WHERE id = ?;

-- name: ListDepartedAccounts :many
SELECT * FROM accounts WHERE departed = 1 ORDER BY email;
