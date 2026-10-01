-- 0020_lowercase_account_emails: an email names one Account whatever its case
-- (ticket #49; CONTEXT.md: Account).
--
-- Accounts are now created with their email trimmed and lowercased, and looked
-- up the same way; this folds the emails stored before that. It merges
-- nothing: if two Accounts differ only by case, the UNIQUE constraint on email
-- fails this migration, and an operator resolves the duplicate by hand.

UPDATE accounts SET email = lower(trim(email)) WHERE email <> lower(trim(email));
