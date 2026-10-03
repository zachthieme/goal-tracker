-- 0034_milestone_changes: which Check-in added a Milestone, marked one Done or
-- marked one Removed (ticket #107; CONTEXT.md: Check-in, Milestone).
--
-- A Check-in's Milestone date moves were already linked to it as Date Slips;
-- its other Milestone changes only updated the Milestone. Each one now leaves a
-- row here, written in the Check-in's transaction: the Check-in, the Milestone,
-- the kind of change (Added, Done or Removed) and, for Removed, its reason.
--
-- Check-ins made before this migration have no rows, and Milestones added
-- outside a Check-in (while the Goal is Proposed, or from the Goal page) are
-- never recorded. Milestone Churn still counts from milestones.

CREATE TABLE milestone_changes (
    id           INTEGER PRIMARY KEY,
    checkin_id   INTEGER NOT NULL REFERENCES checkins(id),
    milestone_id INTEGER NOT NULL REFERENCES milestones(id),
    kind         TEXT    NOT NULL,
    reason       TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL
);

CREATE INDEX idx_milestone_changes_checkin ON milestone_changes (checkin_id);
