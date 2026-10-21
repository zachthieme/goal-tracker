-- 0054_report_delegates: the people a Report Definition's creator or an Admin
-- authorizes to change it, curate its narrative and publish it.
--
-- A Report Delegate may do everything the Definition's creator may except add
-- or remove Report Delegates (CONTEXT.md: Report Delegate; ticket #434). The
-- report_delegates table lists, per Definition, which Accounts are so
-- authorized. An Account may be a Report Delegate on a Definition only once
-- (UNIQUE), and removing one deletes its row.

CREATE TABLE report_delegates (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    account_id           INTEGER NOT NULL REFERENCES accounts(id),
    created_at           TEXT    NOT NULL,
    UNIQUE (report_definition_id, account_id)
);
