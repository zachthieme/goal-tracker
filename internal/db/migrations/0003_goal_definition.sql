-- 0003_goal_definition: everything an Owner fills in on a Proposed Goal before
-- it can be activated (CONTEXT.md: Dated Goal, Ongoing Goal, Milestone, Metric,
-- Contributor, So What, Check-in cadence).
--
-- A Goal is marked Dated (kind 'Dated', with a delivery_date) or Ongoing (kind
-- 'Ongoing', no delivery_date); a fresh Proposed Goal is neither yet (kind '').
-- cadence_days is how often a Check-in is expected (7 by default). Milestones
-- and Metrics hang off a Goal. Contributors are people listed for information
-- only (no update duty). Every So What is kept as a revision so edits are
-- auditable and viewable.

ALTER TABLE goals ADD COLUMN kind          TEXT    NOT NULL DEFAULT '';
ALTER TABLE goals ADD COLUMN delivery_date TEXT    NOT NULL DEFAULT '';
ALTER TABLE goals ADD COLUMN cadence_days  INTEGER NOT NULL DEFAULT 7;

CREATE TABLE milestones (
    id          INTEGER PRIMARY KEY,
    goal_id     INTEGER NOT NULL REFERENCES goals(id),
    name        TEXT    NOT NULL,
    target_date TEXT    NOT NULL,
    created_at  TEXT    NOT NULL
);

CREATE INDEX idx_milestones_goal ON milestones (goal_id);

CREATE TABLE metrics (
    id          INTEGER PRIMARY KEY,
    goal_id     INTEGER NOT NULL REFERENCES goals(id),
    name        TEXT    NOT NULL,
    unit        TEXT    NOT NULL,
    direction   TEXT    NOT NULL,
    baseline    REAL    NOT NULL,
    target      REAL    NOT NULL,
    target_date TEXT    NOT NULL,
    created_at  TEXT    NOT NULL
);

CREATE INDEX idx_metrics_goal ON metrics (goal_id);

CREATE TABLE contributors (
    id         INTEGER PRIMARY KEY,
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL,
    UNIQUE (goal_id, account_id)
);

CREATE INDEX idx_contributors_goal ON contributors (goal_id);

CREATE TABLE so_what_revisions (
    id         INTEGER PRIMARY KEY,
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    so_what    TEXT    NOT NULL,
    author_id  INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL
);

CREATE INDEX idx_so_what_revisions_goal ON so_what_revisions (goal_id);
