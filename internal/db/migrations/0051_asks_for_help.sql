-- 0051_asks_for_help: a Check-in's Path to Green can be marked as asking for
-- help, an Ask for help (ticket #241; CONTEXT.md: Path to Green). Whether an
-- ask is open, and whom it is addressed to, are worked out when read, never
-- stored. Existing Check-ins ask for nothing.

ALTER TABLE checkins ADD COLUMN asks_for_help INTEGER NOT NULL DEFAULT 0;
