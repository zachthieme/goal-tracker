-- 0001_init: accounts and goals, the walking-skeleton tables.
--
-- Accounts are created on first development sign-in; the Admin flag is set from
-- config at creation time. A Goal is the single unit of work being tracked; at
-- creation it is Proposed and carries a title, its So What, and an Owner.

CREATE TABLE accounts (
    id         INTEGER PRIMARY KEY,
    email      TEXT    NOT NULL UNIQUE,
    is_admin   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL
);

CREATE TABLE goals (
    id         INTEGER PRIMARY KEY,
    title      TEXT    NOT NULL,
    so_what    TEXT    NOT NULL,
    owner_id   INTEGER NOT NULL REFERENCES accounts(id),
    lifecycle  TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);
