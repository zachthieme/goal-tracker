-- 0061_lookup_indexes: indexes for three lookups that read or sort a growing
-- table whole. Each comment gives the plan EXPLAIN QUERY PLAN shows before and
-- after; internal/db/indexes_test.go checks the after.

-- The Nudges sent on a day (ListNudgesOn, run by Nudge and by NudgedToday on
-- every Follow-ups page). idx_nudges_goal_day leads with goal_id, so can't
-- serve a lookup by day alone.
--   before: SCAN nudges
--   after:  SEARCH nudges USING INDEX idx_nudges_day (nudged_on=?)
CREATE INDEX idx_nudges_day ON nudges (nudged_on);

-- A Goal's latest Check-in, picked newest first (GetLatestCheckin, and the
-- subquery in ListLatestCheckinsForGoals, ListActiveGoalsWithLatestCheckin,
-- ListOpenAsksForHelp and GetOpenAskForHelp). The index holds each Goal's
-- Check-ins already in that order, so the first is read without a sort; a
-- Goal's Check-in history (ListCheckins) is read in that order too. It leads
-- with goal_id, so it replaces idx_checkins_goal for the foreign key.
--   before: SEARCH c USING INDEX idx_checkins_goal (goal_id=?);
--           USE TEMP B-TREE FOR ORDER BY
--   after:  SEARCH c USING COVERING INDEX idx_checkins_goal_latest (goal_id=?)
--           (GetLatestCheckin, reading the whole row: USING INDEX)
-- ListActiveGoalsWithLatestCheckin still sorts its Goals, which it reads
-- whole as ADR 0004 intends; that sort is not the Check-ins'.
CREATE INDEX idx_checkins_goal_latest ON checkins (goal_id, created_at DESC, id DESC);
DROP INDEX idx_checkins_goal;

-- The open Action Items, soonest due first. Only open ones are indexed, so
-- the index stays the size of the open list however many are closed.
-- ListOverdueActionItems searches it by due date; ListOpenActionItemsWithParties
-- returns every open Action Item, so reads it whole, in order, but never the
-- closed rows.
--   before: SCAN action_items; USE TEMP B-TREE FOR ORDER BY (both)
--   after:  SEARCH action_items USING INDEX idx_action_items_open (due_date<?)
--           SCAN action_items USING INDEX idx_action_items_open
CREATE INDEX idx_action_items_open ON action_items (due_date, id) WHERE closed_at = '';
