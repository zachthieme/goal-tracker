-- 0065_drop_contributors: the Contributor concept is gone (ticket #593). A
-- Contributor was a person listed on a Goal for information only, and nothing
-- read the listing but the list itself. The app has no production users, so
-- the table goes with its rows.

DROP INDEX idx_contributors_goal;
DROP INDEX idx_contributors_account;
DROP TABLE contributors;
