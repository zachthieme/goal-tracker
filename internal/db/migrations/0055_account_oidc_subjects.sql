-- 0055_account_oidc_subjects: each Account can be linked to the subject (sub)
-- the org's OpenID Connect provider gives the person, so a sign-in finds its
-- Account by who the provider says it is, not by an email the provider may
-- later give to someone else (ticket #426). An Account is linked on its first
-- OIDC sign-in; existing Accounts have none until then. A subject links at
-- most one Account.

ALTER TABLE accounts ADD COLUMN oidc_subject TEXT;

CREATE UNIQUE INDEX idx_accounts_oidc_subject ON accounts (oidc_subject) WHERE oidc_subject IS NOT NULL;
