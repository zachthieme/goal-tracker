-- 0047_action_item_goals: an Action Item may be about one Goal in its Report
-- (ticket #320; CONTEXT.md: Action Item).
--
-- goal_id is 0 when the item is about the Report as a whole, as comment_id is
-- 0 for one raised directly. One raised from a Comment is about that
-- Comment's Goal, so existing items raised from a Comment take it; existing
-- items raised directly stay about the whole Report. The indexes serve the
-- Goal page's list of a Goal's items and each person's list of their own.

ALTER TABLE action_items ADD COLUMN goal_id INTEGER NOT NULL DEFAULT 0;

UPDATE action_items SET goal_id = (SELECT goal_id FROM comments WHERE id = comment_id)
WHERE comment_id <> 0 AND EXISTS (SELECT 1 FROM comments WHERE id = comment_id);

CREATE INDEX idx_action_items_goal ON action_items (goal_id);
CREATE INDEX idx_action_items_owner ON action_items (owner_id);
