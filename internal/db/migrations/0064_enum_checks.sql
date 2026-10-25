-- 0064_enum_checks: every enum column and every 0/1 flag column now refuses a
-- value the domain never writes (ticket #518). Each CHECK allows exactly the
-- values the domain names for its column, '' only where the domain writes an
-- empty value, and is named <table>_<column>_valid, so a refusal names the
-- table and column.
--
-- SQLite can't add a CHECK in place, so each table is rebuilt as 0040 rebuilt
-- narrative_texts: create, copy, drop and rename, with foreign keys off (see
-- Migrate). Everything else about each table is kept: its columns, their
-- order, types, defaults and NOT NULLs, its keys and UNIQUE constraints, and
-- its indexes, which are made again as they were. A database already holding a
-- value outside a column's CHECK fails this migration, naming the CHECK, and
-- keeps the value: nothing here rewrites one.

-- accounts
CREATE TABLE accounts_new (
    id           INTEGER PRIMARY KEY,
    email        TEXT    NOT NULL UNIQUE,
    is_admin     INTEGER NOT NULL DEFAULT 0
                 CONSTRAINT accounts_is_admin_valid CHECK (is_admin IN (0, 1)),
    created_at   TEXT    NOT NULL,
    departed     INTEGER NOT NULL DEFAULT 0
                 CONSTRAINT accounts_departed_valid CHECK (departed IN (0, 1)),
    name         TEXT,
    manager_id   INTEGER REFERENCES accounts(id),
    timezone     TEXT    NOT NULL DEFAULT '',
    oidc_subject TEXT
);
INSERT INTO accounts_new (id, email, is_admin, created_at, departed, name, manager_id, timezone, oidc_subject)
SELECT id, email, is_admin, created_at, departed, name, manager_id, timezone, oidc_subject FROM accounts;
DROP TABLE accounts;
ALTER TABLE accounts_new RENAME TO accounts;
CREATE INDEX idx_accounts_manager ON accounts (manager_id);
CREATE UNIQUE INDEX idx_accounts_oidc_subject ON accounts (oidc_subject) WHERE oidc_subject IS NOT NULL;

-- goals
CREATE TABLE goals_new (
    id             INTEGER PRIMARY KEY,
    title          TEXT    NOT NULL,
    why_it_matters TEXT    NOT NULL,
    owner_id       INTEGER NOT NULL REFERENCES accounts(id),
    lifecycle      TEXT    NOT NULL
                   CONSTRAINT goals_lifecycle_valid CHECK (lifecycle IN ('Proposed', 'Active', 'On Hold', 'Done', 'Cancelled')),
    created_at     TEXT    NOT NULL,
    kind           TEXT    NOT NULL DEFAULT ''
                   CONSTRAINT goals_kind_valid CHECK (kind IN ('', 'Dated', 'Ongoing')),
    delivery_date  TEXT    NOT NULL DEFAULT '',
    cadence_days   INTEGER NOT NULL DEFAULT 7,
    top_level      INTEGER NOT NULL DEFAULT 0
                   CONSTRAINT goals_top_level_valid CHECK (top_level IN (0, 1)),
    activated_at   TEXT    NOT NULL DEFAULT ''
);
INSERT INTO goals_new (id, title, why_it_matters, owner_id, lifecycle, created_at, kind, delivery_date, cadence_days, top_level, activated_at)
SELECT id, title, why_it_matters, owner_id, lifecycle, created_at, kind, delivery_date, cadence_days, top_level, activated_at FROM goals;
DROP TABLE goals;
ALTER TABLE goals_new RENAME TO goals;
CREATE INDEX idx_goals_owner ON goals (owner_id);

-- checkins
CREATE TABLE checkins_new (
    id               INTEGER PRIMARY KEY,
    goal_id          INTEGER NOT NULL REFERENCES goals(id),
    author_id        INTEGER NOT NULL REFERENCES accounts(id),
    owner_id         INTEGER NOT NULL REFERENCES accounts(id),
    health           TEXT    NOT NULL
                     CONSTRAINT checkins_health_valid CHECK (health IN ('', 'Green', 'Yellow', 'Red')),
    status           TEXT    NOT NULL,
    path_to_green    TEXT    NOT NULL DEFAULT '',
    path_target_date TEXT    NOT NULL DEFAULT '',
    created_at       TEXT    NOT NULL,
    explanation      TEXT    NOT NULL DEFAULT '',
    lifecycle_from   TEXT    NOT NULL DEFAULT ''
                     CONSTRAINT checkins_lifecycle_from_valid CHECK (lifecycle_from IN ('', 'Proposed', 'Active', 'On Hold', 'Done', 'Cancelled')),
    lifecycle_to     TEXT    NOT NULL DEFAULT ''
                     CONSTRAINT checkins_lifecycle_to_valid CHECK (lifecycle_to IN ('', 'Proposed', 'Active', 'On Hold', 'Done', 'Cancelled')),
    lifecycle_reason TEXT    NOT NULL DEFAULT '',
    outcome          TEXT    NOT NULL DEFAULT '',
    asks_for_help    INTEGER NOT NULL DEFAULT 0
                     CONSTRAINT checkins_asks_for_help_valid CHECK (asks_for_help IN (0, 1))
);
INSERT INTO checkins_new (id, goal_id, author_id, owner_id, health, status, path_to_green, path_target_date, created_at, explanation, lifecycle_from, lifecycle_to, lifecycle_reason, outcome, asks_for_help)
SELECT id, goal_id, author_id, owner_id, health, status, path_to_green, path_target_date, created_at, explanation, lifecycle_from, lifecycle_to, lifecycle_reason, outcome, asks_for_help FROM checkins;
DROP TABLE checkins;
ALTER TABLE checkins_new RENAME TO checkins;
CREATE INDEX idx_checkins_author ON checkins (author_id);
CREATE INDEX idx_checkins_goal_latest ON checkins (goal_id, created_at DESC, id DESC);
CREATE INDEX idx_checkins_owner ON checkins (owner_id);

-- milestones
CREATE TABLE milestones_new (
    id                 INTEGER PRIMARY KEY,
    goal_id            INTEGER NOT NULL REFERENCES goals(id),
    name               TEXT    NOT NULL,
    target_date        TEXT    NOT NULL,
    created_at         TEXT    NOT NULL,
    status             TEXT    NOT NULL DEFAULT 'Planned'
                       CONSTRAINT milestones_status_valid CHECK (status IN ('Planned', 'Done', 'Removed')),
    removed_reason     TEXT    NOT NULL DEFAULT '',
    added_while_active INTEGER NOT NULL DEFAULT 0
                       CONSTRAINT milestones_added_while_active_valid CHECK (added_while_active IN (0, 1))
);
INSERT INTO milestones_new (id, goal_id, name, target_date, created_at, status, removed_reason, added_while_active)
SELECT id, goal_id, name, target_date, created_at, status, removed_reason, added_while_active FROM milestones;
DROP TABLE milestones;
ALTER TABLE milestones_new RENAME TO milestones;
CREATE INDEX idx_milestones_goal ON milestones (goal_id);

-- milestone_changes
CREATE TABLE milestone_changes_new (
    id           INTEGER PRIMARY KEY,
    checkin_id   INTEGER REFERENCES checkins(id),
    milestone_id INTEGER NOT NULL REFERENCES milestones(id),
    kind         TEXT    NOT NULL
                 CONSTRAINT milestone_changes_kind_valid CHECK (kind IN ('Added', 'Done', 'Removed')),
    reason       TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL,
    name         TEXT    NOT NULL DEFAULT '',
    author_id    INTEGER REFERENCES accounts(id)
);
INSERT INTO milestone_changes_new (id, checkin_id, milestone_id, kind, reason, created_at, name, author_id)
SELECT id, checkin_id, milestone_id, kind, reason, created_at, name, author_id FROM milestone_changes;
DROP TABLE milestone_changes;
ALTER TABLE milestone_changes_new RENAME TO milestone_changes;
CREATE INDEX idx_milestone_changes_checkin ON milestone_changes (checkin_id);
CREATE INDEX idx_milestone_changes_milestone ON milestone_changes (milestone_id);

-- date_slips
CREATE TABLE date_slips_new (
    id            INTEGER PRIMARY KEY,
    goal_id       INTEGER NOT NULL REFERENCES goals(id),
    checkin_id    INTEGER NOT NULL REFERENCES checkins(id),
    milestone_id  INTEGER REFERENCES milestones(id),
    old_date      TEXT    NOT NULL,
    new_date      TEXT    NOT NULL,
    reason        TEXT    NOT NULL,
    created_at    TEXT    NOT NULL,
    back_to_green INTEGER NOT NULL DEFAULT 0
                  CONSTRAINT date_slips_back_to_green_valid CHECK (back_to_green IN (0, 1))
);
INSERT INTO date_slips_new (id, goal_id, checkin_id, milestone_id, old_date, new_date, reason, created_at, back_to_green)
SELECT id, goal_id, checkin_id, milestone_id, old_date, new_date, reason, created_at, back_to_green FROM date_slips;
DROP TABLE date_slips;
ALTER TABLE date_slips_new RENAME TO date_slips;
CREATE INDEX idx_date_slips_goal ON date_slips (goal_id);
CREATE INDEX idx_date_slips_milestone ON date_slips (milestone_id);

-- links
CREATE TABLE links_new (
    id           INTEGER PRIMARY KEY,
    child_id     INTEGER NOT NULL REFERENCES goals(id),
    parent_id    INTEGER NOT NULL REFERENCES goals(id),
    status       TEXT    NOT NULL
                 CONSTRAINT links_status_valid CHECK (status IN ('pending', 'accepted')),
    note         TEXT    NOT NULL DEFAULT '',
    requested_by INTEGER NOT NULL REFERENCES accounts(id),
    created_at   TEXT    NOT NULL,
    UNIQUE (child_id, parent_id)
);
INSERT INTO links_new (id, child_id, parent_id, status, note, requested_by, created_at)
SELECT id, child_id, parent_id, status, note, requested_by, created_at FROM links;
DROP TABLE links;
ALTER TABLE links_new RENAME TO links;
CREATE INDEX idx_links_child ON links (child_id);
CREATE INDEX idx_links_parent ON links (parent_id);

-- link_events
CREATE TABLE link_events_new (
    id         INTEGER PRIMARY KEY,
    child_id   INTEGER NOT NULL REFERENCES goals(id),
    parent_id  INTEGER NOT NULL REFERENCES goals(id),
    kind       TEXT    NOT NULL
               CONSTRAINT link_events_kind_valid CHECK (kind IN ('requested', 'linked', 'accepted', 'rejected', 'rejection-undone', 'removed', 'removal-undone')),
    actor_id   INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL
);
INSERT INTO link_events_new (id, child_id, parent_id, kind, actor_id, created_at)
SELECT id, child_id, parent_id, kind, actor_id, created_at FROM link_events;
DROP TABLE link_events;
ALTER TABLE link_events_new RENAME TO link_events;
CREATE INDEX idx_link_events_actor ON link_events (actor_id);
CREATE INDEX idx_link_events_child ON link_events (child_id);
CREATE INDEX idx_link_events_parent ON link_events (parent_id);

-- parent_suggestions
CREATE TABLE parent_suggestions_new (
    id           INTEGER PRIMARY KEY,
    goal_id      INTEGER NOT NULL REFERENCES goals(id),
    parent_id    INTEGER NOT NULL REFERENCES goals(id),
    suggested_by INTEGER NOT NULL REFERENCES accounts(id),
    note         TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'open'
                 CONSTRAINT parent_suggestions_status_valid CHECK (status IN ('open', 'accepted', 'declined', 'withdrawn', 'no longer applies')),
    created_at   TEXT    NOT NULL,
    closed_at    TEXT
);
INSERT INTO parent_suggestions_new (id, goal_id, parent_id, suggested_by, note, status, created_at, closed_at)
SELECT id, goal_id, parent_id, suggested_by, note, status, created_at, closed_at FROM parent_suggestions;
DROP TABLE parent_suggestions;
ALTER TABLE parent_suggestions_new RENAME TO parent_suggestions;
CREATE INDEX idx_parent_suggestions_goal ON parent_suggestions (goal_id);
CREATE INDEX idx_parent_suggestions_parent ON parent_suggestions (parent_id);
CREATE INDEX idx_parent_suggestions_suggested_by ON parent_suggestions (suggested_by);

-- handoffs
CREATE TABLE handoffs_new (
    id           INTEGER PRIMARY KEY,
    goal_id      INTEGER NOT NULL REFERENCES goals(id),
    from_owner   INTEGER NOT NULL REFERENCES accounts(id),
    to_owner     INTEGER NOT NULL REFERENCES accounts(id),
    status       TEXT    NOT NULL
                 CONSTRAINT handoffs_status_valid CHECK (status IN ('pending', 'accepted', 'rejected', 'cancelled', 'reassigned')),
    initiated_by INTEGER NOT NULL REFERENCES accounts(id),
    created_at   TEXT    NOT NULL
);
INSERT INTO handoffs_new (id, goal_id, from_owner, to_owner, status, initiated_by, created_at)
SELECT id, goal_id, from_owner, to_owner, status, initiated_by, created_at FROM handoffs;
DROP TABLE handoffs;
ALTER TABLE handoffs_new RENAME TO handoffs;
CREATE INDEX idx_handoffs_from ON handoffs (from_owner);
CREATE INDEX idx_handoffs_goal ON handoffs (goal_id);
CREATE INDEX idx_handoffs_initiated_by ON handoffs (initiated_by);
CREATE INDEX idx_handoffs_to ON handoffs (to_owner);
CREATE UNIQUE INDEX idx_handoffs_one_pending ON handoffs (goal_id) WHERE status = 'pending';

-- metrics
CREATE TABLE metrics_new (
    id          INTEGER PRIMARY KEY,
    goal_id     INTEGER NOT NULL REFERENCES goals(id),
    name        TEXT    NOT NULL,
    unit        TEXT    NOT NULL,
    direction   TEXT    NOT NULL
                CONSTRAINT metrics_direction_valid CHECK (direction IN ('up', 'down')),
    baseline    REAL    NOT NULL,
    target      REAL    NOT NULL,
    target_date TEXT    NOT NULL,
    created_at  TEXT    NOT NULL
);
INSERT INTO metrics_new (id, goal_id, name, unit, direction, baseline, target, target_date, created_at)
SELECT id, goal_id, name, unit, direction, baseline, target, target_date, created_at FROM metrics;
DROP TABLE metrics;
ALTER TABLE metrics_new RENAME TO metrics;
CREATE INDEX idx_metrics_goal ON metrics (goal_id);

-- highlights
CREATE TABLE highlights_new (
    id         INTEGER PRIMARY KEY,
    checkin_id INTEGER NOT NULL REFERENCES checkins(id),
    kind       TEXT    NOT NULL
               CONSTRAINT highlights_kind_valid CHECK (kind IN ('Insight', 'Accomplishment', 'Miss')),
    note       TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);
INSERT INTO highlights_new (id, checkin_id, kind, note, created_at)
SELECT id, checkin_id, kind, note, created_at FROM highlights;
DROP TABLE highlights;
ALTER TABLE highlights_new RENAME TO highlights;
CREATE INDEX idx_highlights_checkin ON highlights (checkin_id);

-- draft_highlights
CREATE TABLE draft_highlights_new (
    id                   INTEGER PRIMARY KEY,
    goal_id              INTEGER NOT NULL REFERENCES goals(id),
    kind                 TEXT    NOT NULL DEFAULT ''
                         CONSTRAINT draft_highlights_kind_valid CHECK (kind IN ('', 'Insight', 'Accomplishment', 'Miss')),
    note                 TEXT    NOT NULL,
    logged_by            INTEGER NOT NULL REFERENCES accounts(id),
    created_at           TEXT    NOT NULL,
    discarded_checkin_id INTEGER REFERENCES checkins(id)
);
INSERT INTO draft_highlights_new (id, goal_id, kind, note, logged_by, created_at, discarded_checkin_id)
SELECT id, goal_id, kind, note, logged_by, created_at, discarded_checkin_id FROM draft_highlights;
DROP TABLE draft_highlights;
ALTER TABLE draft_highlights_new RENAME TO draft_highlights;
CREATE INDEX idx_draft_highlights_discarded ON draft_highlights (discarded_checkin_id);
CREATE INDEX idx_draft_highlights_goal ON draft_highlights (goal_id);
CREATE INDEX idx_draft_highlights_logged_by ON draft_highlights (logged_by);

-- report_definitions
CREATE TABLE report_definitions_new (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    introduction TEXT    NOT NULL,
    created_by   INTEGER NOT NULL REFERENCES accounts(id),
    created_at   TEXT    NOT NULL,
    mode         TEXT    NOT NULL DEFAULT 'rules'
                 CONSTRAINT report_definitions_mode_valid CHECK (mode IN ('rules', 'picked'))
);
INSERT INTO report_definitions_new (id, name, introduction, created_by, created_at, mode)
SELECT id, name, introduction, created_by, created_at, mode FROM report_definitions;
DROP TABLE report_definitions;
ALTER TABLE report_definitions_new RENAME TO report_definitions;

-- report_definition_goals
CREATE TABLE report_definition_goals_new (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    -- 'picked', 'include' (Also include) or 'exclude' (Leave out).
    list                 TEXT    NOT NULL
                         CONSTRAINT report_definition_goals_list_valid CHECK (list IN ('picked', 'include', 'exclude')),
    goal_id              INTEGER NOT NULL REFERENCES goals(id)
);
INSERT INTO report_definition_goals_new (id, report_definition_id, list, goal_id)
SELECT id, report_definition_id, list, goal_id FROM report_definition_goals;
DROP TABLE report_definition_goals;
ALTER TABLE report_definition_goals_new RENAME TO report_definition_goals;
CREATE INDEX idx_report_definition_goals_definition ON report_definition_goals (report_definition_id);

-- report_rules
CREATE TABLE report_rules_new (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    attribute            TEXT    NOT NULL
                         CONSTRAINT report_rules_attribute_valid CHECK (attribute IN ('dimension', 'owner', 'org', 'lifecycle', 'health', 'top-level')),
    dimension_id         INTEGER NOT NULL DEFAULT 0,
    op                   TEXT    NOT NULL
                         CONSTRAINT report_rules_op_valid CHECK (op IN ('is', 'is any of', 'is not'))
);
INSERT INTO report_rules_new (id, report_definition_id, attribute, dimension_id, op)
SELECT id, report_definition_id, attribute, dimension_id, op FROM report_rules;
DROP TABLE report_rules;
ALTER TABLE report_rules_new RENAME TO report_rules;
CREATE INDEX idx_report_rules_definition ON report_rules (report_definition_id);

-- narrative_picks
CREATE TABLE narrative_picks_new (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    highlight_id         INTEGER NOT NULL REFERENCES highlights(id),
    section              TEXT    NOT NULL
                         CONSTRAINT narrative_picks_section_valid CHECK (section IN ('Insight', 'Accomplishment', 'Miss')),
    UNIQUE (report_definition_id, highlight_id)
);
INSERT INTO narrative_picks_new (id, report_definition_id, highlight_id, section)
SELECT id, report_definition_id, highlight_id, section FROM narrative_picks;
DROP TABLE narrative_picks;
ALTER TABLE narrative_picks_new RENAME TO narrative_picks;

-- narrative_texts
CREATE TABLE narrative_texts_new (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    section              TEXT    NOT NULL
                         CONSTRAINT narrative_texts_section_valid CHECK (section IN ('Insight', 'Accomplishment', 'Miss')),
    text                 TEXT    NOT NULL
);
INSERT INTO narrative_texts_new (id, report_definition_id, section, text)
SELECT id, report_definition_id, section, text FROM narrative_texts;
DROP TABLE narrative_texts;
ALTER TABLE narrative_texts_new RENAME TO narrative_texts;
CREATE INDEX idx_narrative_texts_definition ON narrative_texts (report_definition_id);

-- undo_tokens
CREATE TABLE undo_tokens_new (
    id         INTEGER PRIMARY KEY,
    token      TEXT    NOT NULL UNIQUE,
    kind       TEXT    NOT NULL
               CONSTRAINT undo_tokens_kind_valid CHECK (kind IN ('link-removal', 'link-rejection', 'handoff-rejection', 'value-retire')),
    subject_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    detail     TEXT    NOT NULL DEFAULT '',
    issued_at  TEXT    NOT NULL,
    used_at    TEXT
);
INSERT INTO undo_tokens_new (id, token, kind, subject_id, account_id, detail, issued_at, used_at)
SELECT id, token, kind, subject_id, account_id, detail, issued_at, used_at FROM undo_tokens;
DROP TABLE undo_tokens;
ALTER TABLE undo_tokens_new RENAME TO undo_tokens;

-- dimensions
CREATE TABLE dimensions_new (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    created_at TEXT    NOT NULL,
    selection  TEXT    NOT NULL DEFAULT 'one' CHECK (selection IN ('one', 'several')),
    list       TEXT    NOT NULL DEFAULT 'fixed' CHECK (list IN ('fixed', 'extendable')),
    retired    INTEGER NOT NULL DEFAULT 0
               CONSTRAINT dimensions_retired_valid CHECK (retired IN (0, 1)),
    required   INTEGER NOT NULL DEFAULT 0
               CONSTRAINT dimensions_required_valid CHECK (required IN (0, 1))
);
INSERT INTO dimensions_new (id, name, created_at, selection, list, retired, required)
SELECT id, name, created_at, selection, list, retired, required FROM dimensions;
DROP TABLE dimensions;
ALTER TABLE dimensions_new RENAME TO dimensions;
CREATE UNIQUE INDEX idx_dimensions_unique_name ON dimensions (name COLLATE NOCASE);

-- dimension_values
CREATE TABLE dimension_values_new (
    id           INTEGER PRIMARY KEY,
    dimension_id INTEGER NOT NULL REFERENCES dimensions(id),
    value        TEXT    NOT NULL,
    retired      INTEGER NOT NULL DEFAULT 0
                 CONSTRAINT dimension_values_retired_valid CHECK (retired IN (0, 1)),
    created_at   TEXT    NOT NULL,
    position     INTEGER NOT NULL DEFAULT 0
);
INSERT INTO dimension_values_new (id, dimension_id, value, retired, created_at, position)
SELECT id, dimension_id, value, retired, created_at, position FROM dimension_values;
DROP TABLE dimension_values;
ALTER TABLE dimension_values_new RENAME TO dimension_values;
CREATE UNIQUE INDEX idx_dimension_values_unique_value ON dimension_values (dimension_id, value COLLATE NOCASE);

-- fields
CREATE TABLE fields_new (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    type       TEXT    NOT NULL CHECK (type IN ('number', 'short_text', 'long_text', 'date')),
    unit       TEXT    NOT NULL DEFAULT '',
    retired    INTEGER NOT NULL DEFAULT 0
               CONSTRAINT fields_retired_valid CHECK (retired IN (0, 1)),
    created_at TEXT    NOT NULL,
    required   INTEGER NOT NULL DEFAULT 0
               CONSTRAINT fields_required_valid CHECK (required IN (0, 1))
);
INSERT INTO fields_new (id, name, type, unit, retired, created_at, required)
SELECT id, name, type, unit, retired, created_at, required FROM fields;
DROP TABLE fields;
ALTER TABLE fields_new RENAME TO fields;
CREATE UNIQUE INDEX idx_fields_unique_name ON fields (name COLLATE NOCASE);

-- goal_value_changes
CREATE TABLE goal_value_changes_new (
    id           INTEGER PRIMARY KEY,
    goal_id      INTEGER NOT NULL REFERENCES goals(id),
    actor_id     INTEGER NOT NULL REFERENCES accounts(id),
    dimension_id INTEGER REFERENCES dimensions(id),
    field_id     INTEGER REFERENCES fields(id),
    attribute    TEXT    NOT NULL,
    several      INTEGER NOT NULL DEFAULT 0
                 CONSTRAINT goal_value_changes_several_valid CHECK (several IN (0, 1)),
    before_value TEXT    NOT NULL,
    after_value  TEXT    NOT NULL,
    created_at   TEXT    NOT NULL,
    CHECK ((dimension_id IS NULL) <> (field_id IS NULL))
);
INSERT INTO goal_value_changes_new (id, goal_id, actor_id, dimension_id, field_id, attribute, several, before_value, after_value, created_at)
SELECT id, goal_id, actor_id, dimension_id, field_id, attribute, several, before_value, after_value, created_at FROM goal_value_changes;
DROP TABLE goal_value_changes;
ALTER TABLE goal_value_changes_new RENAME TO goal_value_changes;
CREATE INDEX idx_goal_value_changes_actor ON goal_value_changes (actor_id);
CREATE INDEX idx_goal_value_changes_goal ON goal_value_changes (goal_id);
