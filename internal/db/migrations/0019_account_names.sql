-- 0019_account_names: a person's Name (ticket #41; CONTEXT.md: Name).
--
-- The Name comes from the org's sign-in and is never typed into the tool, so
-- it is nullable: an Account nobody has named yet — every Account that exists
-- when this runs — has none, and is shown by its email's local part.

ALTER TABLE accounts ADD COLUMN name TEXT;
