-- 0007_checkin_explanation: the Owner's explanation of a Rolled-up Health
-- difference (ADR-0003).
--
-- The Rolled-up Health is the worst Owner-set Health among a Goal's Active
-- children (CONTEXT.md: Rolled-up Health). When an Owner's own Health differs
-- from it, the Check-in must explain why; that explanation is recorded here and
-- becomes the "so what" at every level of the graph. It is empty when the Health
-- matches the roll-up, or when there is nothing to roll up.

ALTER TABLE checkins ADD COLUMN explanation TEXT NOT NULL DEFAULT '';
