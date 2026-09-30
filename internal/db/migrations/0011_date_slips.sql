-- 0011_date_slips: Date Slips and Milestone changes recorded in a Check-in
-- (ticket #5; CONTEXT.md: Date Slip, Milestone, Milestone Churn).
--
-- A Date Slip is a recorded change to a Goal's delivery date or a Milestone's
-- date, always with a reason (CONTEXT.md: Date Slip). The full history is kept:
-- each row keeps the old and new dates and the Check-in that recorded it.
-- milestone_id is NULL for a delivery-date slip.
CREATE TABLE date_slips (
    id           INTEGER PRIMARY KEY,
    goal_id      INTEGER NOT NULL REFERENCES goals(id),
    checkin_id   INTEGER NOT NULL REFERENCES checkins(id),
    milestone_id INTEGER REFERENCES milestones(id),
    old_date     TEXT    NOT NULL,
    new_date     TEXT    NOT NULL,
    reason       TEXT    NOT NULL,
    created_at   TEXT    NOT NULL
);

CREATE INDEX idx_date_slips_goal ON date_slips (goal_id);

-- A Milestone is Planned until a Check-in marks it Done or Removed; Removed
-- requires a reason. added_while_active marks a Milestone added once its Goal
-- was Active, so Milestone Churn (added plus removed since the Goal became
-- Active) can be counted without reinterpreting timestamps. Milestones that
-- predate this migration count as planned before activation.
ALTER TABLE milestones ADD COLUMN status             TEXT    NOT NULL DEFAULT 'Planned';
ALTER TABLE milestones ADD COLUMN removed_reason     TEXT    NOT NULL DEFAULT '';
ALTER TABLE milestones ADD COLUMN added_while_active INTEGER NOT NULL DEFAULT 0;
