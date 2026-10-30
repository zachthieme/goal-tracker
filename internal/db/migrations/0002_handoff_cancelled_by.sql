-- 0002_handoff_cancelled_by: who cancelled each cancelled Handoff.

-- The Account that cancelled a Handoff: the person who started it, or the
-- Admin who cancelled it, reassigned its Goal or marked its new Owner
-- Departed. NULL for a Handoff not cancelled, or cancelled before this
-- column recorded who did.
ALTER TABLE handoffs ADD COLUMN cancelled_by INTEGER REFERENCES accounts(id);
