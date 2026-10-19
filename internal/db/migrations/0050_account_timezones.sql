-- 0050_account_timezones: each Account can have a Timezone, an IANA name, as
-- the company's directory records it (ticket #353; CONTEXT.md: Timezone). Only
-- the directory sync sets it, and only to a name it has checked. Existing
-- Accounts have none, and use the company's.

ALTER TABLE accounts ADD COLUMN timezone TEXT NOT NULL DEFAULT '';
