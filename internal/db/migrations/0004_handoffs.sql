-- 0004_handoffs: transferring a Goal to a new Owner, and Goals left Ownerless.
--
-- A Handoff records the transfer of a Goal to a new Owner (CONTEXT.md: Handoff).
-- It is started by the current Owner or an Admin, but takes effect only when the
-- new Owner accepts it (from_owner -> to_owner). It starts 'pending' and becomes
-- 'accepted' when the new Owner accepts, at which point the Goal's owner_id moves
-- to them; rejecting deletes the row. Only one Handoff is pending per Goal at a
-- time.
--
-- An Account is marked 'departed' when an Admin records that the person has left
-- the org. A Goal whose Owner is departed is Ownerless (CONTEXT.md: Ownerless):
-- the property is derived from the Owner, so an Admin reassigning the Goal to a
-- present Owner clears it with no per-Goal flag to keep in step.

ALTER TABLE accounts ADD COLUMN departed INTEGER NOT NULL DEFAULT 0;

CREATE TABLE handoffs (
    id           INTEGER PRIMARY KEY,
    goal_id      INTEGER NOT NULL REFERENCES goals(id),
    from_owner   INTEGER NOT NULL REFERENCES accounts(id),
    to_owner     INTEGER NOT NULL REFERENCES accounts(id),
    status       TEXT    NOT NULL,
    initiated_by INTEGER NOT NULL REFERENCES accounts(id),
    created_at   TEXT    NOT NULL
);

CREATE INDEX idx_handoffs_to ON handoffs (to_owner);
CREATE INDEX idx_handoffs_goal ON handoffs (goal_id);
