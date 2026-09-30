-- 0014_goal_activation: when a Goal became Active (ticket #10; CONTEXT.md:
-- Stale).
--
-- An Active Goal with no Check-in yet is judged Stale from its activation, so the
-- activation instant is kept. It is empty on a Goal that was never activated;
-- Goals activated before this migration fall back to their creation time.
ALTER TABLE goals ADD COLUMN activated_at TEXT NOT NULL DEFAULT '';
