-- 0042_milestone_changes_outside_checkins: a Milestone added from the Goal
-- page, outside a Check-in, is recorded too (ticket #190; CONTEXT.md:
-- Milestone, Check-in).
--
-- 0034 recorded only the Milestone changes a Check-in made, so checkin_id was
-- required. The Owner or a Delegate can now add a Milestone from the Goal page
-- at any time; one added while the Goal is Active or On Hold leaves a change
-- with no Check-in, carrying its own author. SQLite can't drop NOT NULL in
-- place, so the table is rebuilt: every existing change is copied across with
-- its id and columns, and takes as its author the author of the Check-in that
-- made it. Additions while Proposed are still never recorded.

CREATE TABLE milestone_changes_new (
    id           INTEGER PRIMARY KEY,
    checkin_id   INTEGER REFERENCES checkins(id),
    milestone_id INTEGER NOT NULL REFERENCES milestones(id),
    kind         TEXT    NOT NULL,
    reason       TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL,
    name         TEXT    NOT NULL DEFAULT '',
    author_id    INTEGER REFERENCES accounts(id)
);

INSERT INTO milestone_changes_new (id, checkin_id, milestone_id, kind, reason, created_at, name, author_id)
SELECT mc.id, mc.checkin_id, mc.milestone_id, mc.kind, mc.reason, mc.created_at, mc.name,
       (SELECT c.author_id FROM checkins c WHERE c.id = mc.checkin_id)
FROM milestone_changes mc;

DROP TABLE milestone_changes;
ALTER TABLE milestone_changes_new RENAME TO milestone_changes;

CREATE INDEX idx_milestone_changes_checkin ON milestone_changes (checkin_id);
CREATE INDEX idx_milestone_changes_milestone ON milestone_changes (milestone_id);
