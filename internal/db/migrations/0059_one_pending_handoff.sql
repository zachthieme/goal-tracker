-- 0059_one_pending_handoff: a Goal has at most one pending Handoff
-- (CONTEXT.md: Handoff), now held by the schema as well as by StartHandoff and
-- RestoreHandoff, so two Handoffs started together can't both be left pending.
--
-- It settles nothing: a database already holding two pending Handoffs of one
-- Goal fails this migration on the index, naming handoffs.goal_id, and keeps
-- both, so an operator decides by hand which one stays pending.

CREATE UNIQUE INDEX idx_handoffs_one_pending ON handoffs (goal_id) WHERE status = 'pending';
