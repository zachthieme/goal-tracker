-- 0001_baseline: the whole schema, as the migrations before it left it.
--
-- Each statement is as SQLite stores it in sqlite_schema, so a database
-- migrated from here has the schema the earlier chain built. A database that
-- ran that chain is refused (Migrate) and is recreated with make seed. A new
-- migration continues from 0002.

-- Accounts: each person who signs in, with their Manager and Timezone.
CREATE TABLE "accounts" (
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

-- Finds accounts by manager_id.
CREATE INDEX idx_accounts_manager ON accounts (manager_id);

-- At most one Account per OIDC subject.
CREATE UNIQUE INDEX idx_accounts_oidc_subject ON accounts (oidc_subject) WHERE oidc_subject IS NOT NULL;

-- An Account's password hash, and its failed sign-ins and lockout.
CREATE TABLE account_passwords (
    account_id     INTEGER PRIMARY KEY REFERENCES accounts(id),
    password_hash  TEXT,
    failed_signins INTEGER NOT NULL DEFAULT 0,
    locked_until   TEXT
);

-- An Account's session generation; moving it on signs every session out.
CREATE TABLE account_sessions (
    account_id INTEGER PRIMARY KEY REFERENCES accounts(id),
    generation INTEGER NOT NULL
);

-- One-time links that set an Account's password.
CREATE TABLE password_links (
    account_id INTEGER PRIMARY KEY REFERENCES accounts(id),
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL
);

-- Emails waiting to be sent, retried until they go.
CREATE TABLE "email_outbox" (
    id              INTEGER PRIMARY KEY,
    recipient       TEXT    NOT NULL,
    account_id      INTEGER NOT NULL REFERENCES accounts(id),
    subject         TEXT    NOT NULL,
    body            TEXT    NOT NULL,
    created_at      TEXT    NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT    NOT NULL,
    sent_at         TEXT,
    last_error      TEXT    NOT NULL DEFAULT ''
);

-- Finds the emails not yet sent, oldest first.
CREATE INDEX idx_email_outbox_unsent ON email_outbox (id) WHERE sent_at IS NULL;

-- The last slot each scheduled job ran for.
CREATE TABLE scheduled_runs (
    job  TEXT PRIMARY KEY,
    slot TEXT NOT NULL
);

-- Tokens that undo a just-made change, once.
CREATE TABLE "undo_tokens" (
    id         INTEGER PRIMARY KEY,
    token      TEXT    NOT NULL UNIQUE,
    kind       TEXT    NOT NULL
               CONSTRAINT undo_tokens_kind_valid CHECK (kind IN ('link-removal', 'link-rejection', 'handoff-rejection', 'value-retire', 'parent-suggestion-decline')),
    subject_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    detail     TEXT    NOT NULL DEFAULT '',
    issued_at  TEXT    NOT NULL,
    used_at    TEXT
);

-- Goals: the single unit of work being tracked, with its Owner and Lifecycle.
CREATE TABLE "goals" (
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

-- Finds goals by owner_id.
CREATE INDEX idx_goals_owner ON goals (owner_id);

-- Every earlier title of a Goal.
CREATE TABLE goal_title_revisions (
    id         INTEGER PRIMARY KEY,
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    old_title  TEXT    NOT NULL,
    new_title  TEXT    NOT NULL,
    author_id  INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL
);

-- Finds goal_title_revisions by goal_id.
CREATE INDEX idx_goal_title_revisions_goal ON goal_title_revisions (goal_id);

-- Finds goal_title_revisions by author_id.
CREATE INDEX idx_goal_title_revisions_author ON goal_title_revisions (author_id);

-- Every earlier Why it matters of a Goal.
CREATE TABLE "why_it_matters_revisions" (
    id         INTEGER PRIMARY KEY,
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    why_it_matters    TEXT    NOT NULL,
    author_id  INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL
);

-- Finds why_it_matters_revisions by goal_id.
CREATE INDEX idx_why_it_matters_revisions_goal ON why_it_matters_revisions (goal_id);

-- Finds why_it_matters_revisions by author_id.
CREATE INDEX idx_why_it_matters_revisions_author ON why_it_matters_revisions (author_id);

-- Delegates: Accounts that may update a Goal for its Owner.
CREATE TABLE delegates (
    id         INTEGER PRIMARY KEY,
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL,
    UNIQUE (goal_id, account_id)
);

-- Finds delegates by goal_id.
CREATE INDEX idx_delegates_goal ON delegates (goal_id);

-- Finds delegates by account_id.
CREATE INDEX idx_delegates_account ON delegates (account_id);

-- Handoffs of a Goal from one Owner to another.
CREATE TABLE "handoffs" (
    id           INTEGER PRIMARY KEY,
    goal_id      INTEGER NOT NULL REFERENCES goals(id),
    from_owner   INTEGER NOT NULL REFERENCES accounts(id),
    to_owner     INTEGER NOT NULL REFERENCES accounts(id),
    status       TEXT    NOT NULL
                 CONSTRAINT handoffs_status_valid CHECK (status IN ('pending', 'accepted', 'rejected', 'cancelled', 'reassigned')),
    initiated_by INTEGER NOT NULL REFERENCES accounts(id),
    created_at   TEXT    NOT NULL
);

-- Finds handoffs by from_owner.
CREATE INDEX idx_handoffs_from ON handoffs (from_owner);

-- Finds handoffs by goal_id.
CREATE INDEX idx_handoffs_goal ON handoffs (goal_id);

-- Finds handoffs by initiated_by.
CREATE INDEX idx_handoffs_initiated_by ON handoffs (initiated_by);

-- Finds handoffs by to_owner.
CREATE INDEX idx_handoffs_to ON handoffs (to_owner);

-- At most one pending Handoff per Goal.
CREATE UNIQUE INDEX idx_handoffs_one_pending ON handoffs (goal_id) WHERE status = 'pending';

-- Metrics a Goal is measured by.
CREATE TABLE "metrics" (
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

-- Finds metrics by goal_id.
CREATE INDEX idx_metrics_goal ON metrics (goal_id);

-- Milestones of a Goal.
CREATE TABLE "milestones" (
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

-- Finds milestones by goal_id.
CREATE INDEX idx_milestones_goal ON milestones (goal_id);

-- Check-ins: a Goal's Health and status at a point in time.
CREATE TABLE "checkins" (
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

-- Finds checkins by author_id.
CREATE INDEX idx_checkins_author ON checkins (author_id);

-- Finds checkins by goal_id, created_at DESC, id DESC.
CREATE INDEX idx_checkins_goal_latest ON checkins (goal_id, created_at DESC, id DESC);

-- Finds checkins by owner_id.
CREATE INDEX idx_checkins_owner ON checkins (owner_id);

-- A Metric's reading taken in a Check-in.
CREATE TABLE metric_readings (
    id         INTEGER PRIMARY KEY,
    checkin_id INTEGER NOT NULL REFERENCES checkins(id),
    metric_id  INTEGER NOT NULL REFERENCES metrics(id),
    value      REAL    NOT NULL,
    created_at TEXT    NOT NULL
);

-- Finds metric_readings by metric_id.
CREATE INDEX idx_metric_readings_metric ON metric_readings (metric_id);

-- At most one metric_readings row per checkin_id, metric_id.
CREATE UNIQUE INDEX idx_metric_readings_unique_metric ON metric_readings (checkin_id, metric_id);

-- Milestones added, done or removed, in a Check-in or outside one.
CREATE TABLE "milestone_changes" (
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

-- Finds milestone_changes by checkin_id.
CREATE INDEX idx_milestone_changes_checkin ON milestone_changes (checkin_id);

-- Finds milestone_changes by milestone_id.
CREATE INDEX idx_milestone_changes_milestone ON milestone_changes (milestone_id);

-- Date Slips: a Goal's or Milestone's date moved by a Check-in.
CREATE TABLE "date_slips" (
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

-- Finds date_slips by goal_id.
CREATE INDEX idx_date_slips_goal ON date_slips (goal_id);

-- Finds date_slips by milestone_id.
CREATE INDEX idx_date_slips_milestone ON date_slips (milestone_id);

-- Highlights recorded in a Check-in.
CREATE TABLE "highlights" (
    id         INTEGER PRIMARY KEY,
    checkin_id INTEGER NOT NULL REFERENCES checkins(id),
    kind       TEXT    NOT NULL
               CONSTRAINT highlights_kind_valid CHECK (kind IN ('Insight', 'Accomplishment', 'Miss')),
    note       TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

-- Finds highlights by checkin_id.
CREATE INDEX idx_highlights_checkin ON highlights (checkin_id);

-- Draft Highlights noted on a Goal between Check-ins.
CREATE TABLE "draft_highlights" (
    id                   INTEGER PRIMARY KEY,
    goal_id              INTEGER NOT NULL REFERENCES goals(id),
    kind                 TEXT    NOT NULL DEFAULT ''
                         CONSTRAINT draft_highlights_kind_valid CHECK (kind IN ('', 'Insight', 'Accomplishment', 'Miss')),
    note                 TEXT    NOT NULL,
    logged_by            INTEGER NOT NULL REFERENCES accounts(id),
    created_at           TEXT    NOT NULL,
    discarded_checkin_id INTEGER REFERENCES checkins(id)
);

-- Finds draft_highlights by discarded_checkin_id.
CREATE INDEX idx_draft_highlights_discarded ON draft_highlights (discarded_checkin_id);

-- Finds draft_highlights by goal_id.
CREATE INDEX idx_draft_highlights_goal ON draft_highlights (goal_id);

-- Finds draft_highlights by logged_by.
CREATE INDEX idx_draft_highlights_logged_by ON draft_highlights (logged_by);

-- Nudges sent to a Goal's Owner for a Check-in.
CREATE TABLE nudges (
    id         INTEGER PRIMARY KEY,
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    sent_by    INTEGER NOT NULL REFERENCES accounts(id),
    nudged_on  TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

-- At most one Nudge of a Goal per day.
CREATE UNIQUE INDEX idx_nudges_goal_day ON nudges (goal_id, nudged_on);

-- Finds nudges by sent_by.
CREATE INDEX idx_nudges_sent_by ON nudges (sent_by);

-- Finds nudges by nudged_on.
CREATE INDEX idx_nudges_day ON nudges (nudged_on);

-- Links: a Goal contributes to a parent Goal, once accepted.
CREATE TABLE "links" (
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

-- Finds links by child_id.
CREATE INDEX idx_links_child ON links (child_id);

-- Finds links by parent_id.
CREATE INDEX idx_links_parent ON links (parent_id);

-- Every request, acceptance, rejection and removal of a Link.
CREATE TABLE "link_events" (
    id         INTEGER PRIMARY KEY,
    child_id   INTEGER NOT NULL REFERENCES goals(id),
    parent_id  INTEGER NOT NULL REFERENCES goals(id),
    kind       TEXT    NOT NULL
               CONSTRAINT link_events_kind_valid CHECK (kind IN ('requested', 'linked', 'accepted', 'rejected', 'rejection-undone', 'removed', 'removal-undone')),
    actor_id   INTEGER NOT NULL REFERENCES accounts(id),
    created_at TEXT    NOT NULL
);

-- Finds link_events by actor_id.
CREATE INDEX idx_link_events_actor ON link_events (actor_id);

-- Finds link_events by child_id.
CREATE INDEX idx_link_events_child ON link_events (child_id);

-- Finds link_events by parent_id.
CREATE INDEX idx_link_events_parent ON link_events (parent_id);

-- Removed Links, kept so a removal can be undone.
CREATE TABLE link_removals (
    id              INTEGER PRIMARY KEY,
    child_id        INTEGER NOT NULL REFERENCES goals(id),
    parent_id       INTEGER NOT NULL REFERENCES goals(id),
    note            TEXT    NOT NULL DEFAULT '',
    requested_by    INTEGER NOT NULL REFERENCES accounts(id),
    link_created_at TEXT    NOT NULL,
    removed_by      INTEGER NOT NULL REFERENCES accounts(id),
    removed_at      TEXT    NOT NULL,
    restored_at     TEXT
);

-- Finds link_removals by child_id, parent_id.
CREATE INDEX idx_link_removals_pair ON link_removals (child_id, parent_id);

-- Rejected Link requests, kept so a rejection can be undone.
CREATE TABLE rejected_link_requests (
    id                 INTEGER PRIMARY KEY,
    child_id           INTEGER NOT NULL REFERENCES goals(id),
    parent_id          INTEGER NOT NULL REFERENCES goals(id),
    note               TEXT    NOT NULL DEFAULT '',
    requested_by       INTEGER NOT NULL REFERENCES accounts(id),
    request_created_at TEXT    NOT NULL,
    rejected_by        INTEGER NOT NULL REFERENCES accounts(id),
    rejected_at        TEXT    NOT NULL,
    restored_at        TEXT
);

-- Finds rejected_link_requests by child_id, parent_id.
CREATE INDEX idx_rejected_link_requests_pair ON rejected_link_requests (child_id, parent_id);

-- Parent suggestions: a parent proposed for a Goal.
CREATE TABLE "parent_suggestions" (
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

-- Finds parent_suggestions by goal_id.
CREATE INDEX idx_parent_suggestions_goal ON parent_suggestions (goal_id);

-- Finds parent_suggestions by parent_id.
CREATE INDEX idx_parent_suggestions_parent ON parent_suggestions (parent_id);

-- Finds parent_suggestions by suggested_by.
CREATE INDEX idx_parent_suggestions_suggested_by ON parent_suggestions (suggested_by);

-- Comments on a Goal, in a Report or on its own.
CREATE TABLE "comments" (
    id             INTEGER PRIMARY KEY,
    publication_id INTEGER NOT NULL DEFAULT 0,
    goal_id        INTEGER NOT NULL REFERENCES goals(id),
    parent_id      INTEGER NOT NULL DEFAULT 0,
    author_id      INTEGER NOT NULL REFERENCES accounts(id),
    body           TEXT    NOT NULL,
    created_at     TEXT    NOT NULL
);

-- Finds comments by publication_id.
CREATE INDEX idx_comments_publication ON comments (publication_id);

-- Finds comments by goal_id.
CREATE INDEX idx_comments_goal ON comments (goal_id);

-- Finds comments by author_id.
CREATE INDEX idx_comments_author ON comments (author_id);

-- Dimensions an Admin defines to sort Goals by.
CREATE TABLE "dimensions" (
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

-- At most one Dimension per name, whatever its case.
CREATE UNIQUE INDEX idx_dimensions_unique_name ON dimensions (name COLLATE NOCASE);

-- A Dimension's values.
CREATE TABLE "dimension_values" (
    id           INTEGER PRIMARY KEY,
    dimension_id INTEGER NOT NULL REFERENCES dimensions(id),
    value        TEXT    NOT NULL,
    retired      INTEGER NOT NULL DEFAULT 0
                 CONSTRAINT dimension_values_retired_valid CHECK (retired IN (0, 1)),
    created_at   TEXT    NOT NULL,
    position     INTEGER NOT NULL DEFAULT 0
);

-- At most one value per Dimension, whatever its case.
CREATE UNIQUE INDEX idx_dimension_values_unique_value ON dimension_values (dimension_id, value COLLATE NOCASE);

-- A Goal's values for each Dimension.
CREATE TABLE goal_dimension_values (
    id                 INTEGER PRIMARY KEY,
    goal_id            INTEGER NOT NULL REFERENCES goals(id),
    dimension_value_id INTEGER NOT NULL REFERENCES dimension_values(id),
    created_at         TEXT    NOT NULL,
    UNIQUE (goal_id, dimension_value_id)
);

-- Finds goal_dimension_values by goal_id.
CREATE INDEX idx_goal_dimension_values_goal ON goal_dimension_values (goal_id);

-- Finds goal_dimension_values by dimension_value_id.
CREATE INDEX idx_goal_dimension_values_value ON goal_dimension_values (dimension_value_id);

-- Fields an Admin defines for Goals to fill in.
CREATE TABLE "fields" (
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

-- At most one Field per name, whatever its case.
CREATE UNIQUE INDEX idx_fields_unique_name ON fields (name COLLATE NOCASE);

-- A Goal's value for each Field.
CREATE TABLE goal_field_values (
    goal_id    INTEGER NOT NULL REFERENCES goals(id),
    field_id   INTEGER NOT NULL REFERENCES fields(id),
    value      TEXT    NOT NULL,
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (goal_id, field_id)
);

-- Finds goal_field_values by field_id.
CREATE INDEX idx_goal_field_values_field ON goal_field_values (field_id);

-- Every change to a Goal's Dimension or Field values.
CREATE TABLE "goal_value_changes" (
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

-- Finds goal_value_changes by actor_id.
CREATE INDEX idx_goal_value_changes_actor ON goal_value_changes (actor_id);

-- Finds goal_value_changes by goal_id.
CREATE INDEX idx_goal_value_changes_goal ON goal_value_changes (goal_id);

-- The Definition log: every change to a Dimension or Field.
CREATE TABLE definition_changes (
    id           INTEGER PRIMARY KEY,
    actor_id     INTEGER NOT NULL REFERENCES accounts(id),
    dimension_id INTEGER REFERENCES dimensions(id),
    field_id     INTEGER REFERENCES fields(id),
    summary      TEXT    NOT NULL,
    created_at   TEXT    NOT NULL,
    CHECK ((dimension_id IS NULL) <> (field_id IS NULL))
);

-- Finds definition_changes by dimension_id.
CREATE INDEX idx_definition_changes_dimension ON definition_changes (dimension_id);

-- Finds definition_changes by field_id.
CREATE INDEX idx_definition_changes_field ON definition_changes (field_id);

-- Finds definition_changes by actor_id.
CREATE INDEX idx_definition_changes_actor ON definition_changes (actor_id);

-- Refuses any edit: the log is append-only.
CREATE TRIGGER definition_changes_never_edited
BEFORE UPDATE ON definition_changes
BEGIN
    SELECT RAISE(ABORT, 'the Definition log is never edited');
END;

-- Refuses any delete: the log is append-only.
CREATE TRIGGER definition_changes_never_removed
BEFORE DELETE ON definition_changes
BEGIN
    SELECT RAISE(ABORT, 'the Definition log is never edited');
END;

-- Report Definitions: what a Report covers and how it picks its Goals.
CREATE TABLE "report_definitions" (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    introduction TEXT    NOT NULL,
    created_by   INTEGER NOT NULL REFERENCES accounts(id),
    created_at   TEXT    NOT NULL,
    mode         TEXT    NOT NULL DEFAULT 'rules'
                 CONSTRAINT report_definitions_mode_valid CHECK (mode IN ('rules', 'picked'))
);

-- Report Delegates: Accounts that may edit a Report Definition.
CREATE TABLE report_delegates (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    account_id           INTEGER NOT NULL REFERENCES accounts(id),
    created_at           TEXT    NOT NULL,
    UNIQUE (report_definition_id, account_id)
);

-- Finds report_delegates by account_id.
CREATE INDEX idx_report_delegates_account ON report_delegates (account_id);

-- Fields a Report Definition shows.
CREATE TABLE report_definition_fields (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    field_id             INTEGER NOT NULL REFERENCES fields(id)
);

-- At most one report_definition_fields row per report_definition_id, field_id.
CREATE UNIQUE INDEX idx_report_definition_fields_unique_field ON report_definition_fields (report_definition_id, field_id);

-- Goals a Report Definition picks, includes or leaves out.
CREATE TABLE "report_definition_goals" (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    -- 'picked', 'include' (Also include) or 'exclude' (Leave out).
    list                 TEXT    NOT NULL
                         CONSTRAINT report_definition_goals_list_valid CHECK (list IN ('picked', 'include', 'exclude')),
    goal_id              INTEGER NOT NULL REFERENCES goals(id)
);

-- Finds report_definition_goals by report_definition_id.
CREATE INDEX idx_report_definition_goals_definition ON report_definition_goals (report_definition_id);

-- Report rules that pick a Report Definition's Goals.
CREATE TABLE "report_rules" (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    attribute            TEXT    NOT NULL
                         CONSTRAINT report_rules_attribute_valid CHECK (attribute IN ('dimension', 'owner', 'org', 'lifecycle', 'health', 'top-level')),
    dimension_id         INTEGER NOT NULL DEFAULT 0,
    op                   TEXT    NOT NULL
                         CONSTRAINT report_rules_op_valid CHECK (op IN ('is', 'is any of', 'is not'))
);

-- Finds report_rules by report_definition_id.
CREATE INDEX idx_report_rules_definition ON report_rules (report_definition_id);

-- The values a Report rule matches.
CREATE TABLE report_rule_values (
    id             INTEGER PRIMARY KEY,
    report_rule_id INTEGER NOT NULL REFERENCES report_rules(id),
    value          TEXT    NOT NULL
);

-- Finds report_rule_values by report_rule_id.
CREATE INDEX idx_report_rule_values_rule ON report_rule_values (report_rule_id);

-- Highlights picked into a Report Definition's narrative.
CREATE TABLE "narrative_picks" (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    highlight_id         INTEGER NOT NULL REFERENCES highlights(id),
    section              TEXT    NOT NULL
                         CONSTRAINT narrative_picks_section_valid CHECK (section IN ('Insight', 'Accomplishment', 'Miss')),
    UNIQUE (report_definition_id, highlight_id)
);

-- Text written into a Report Definition's narrative.
CREATE TABLE "narrative_texts" (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    section              TEXT    NOT NULL
                         CONSTRAINT narrative_texts_section_valid CHECK (section IN ('Insight', 'Accomplishment', 'Miss')),
    text                 TEXT    NOT NULL
);

-- Finds narrative_texts by report_definition_id.
CREATE INDEX idx_narrative_texts_definition ON narrative_texts (report_definition_id);

-- Reports published from a Report Definition.
CREATE TABLE report_publications (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    published_by         INTEGER NOT NULL REFERENCES accounts(id),
    published_at         TEXT    NOT NULL,
    snapshot             TEXT    NOT NULL
);

-- Finds report_publications by report_definition_id.
CREATE INDEX idx_report_publications_definition
    ON report_publications (report_definition_id);

-- Action Items raised on a published Report.
CREATE TABLE action_items (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    publication_id       INTEGER NOT NULL REFERENCES report_publications(id),
    comment_id           INTEGER NOT NULL DEFAULT 0,
    text                 TEXT    NOT NULL,
    owner_id             INTEGER NOT NULL REFERENCES accounts(id),
    due_date             TEXT    NOT NULL,
    created_by           INTEGER NOT NULL REFERENCES accounts(id),
    created_at           TEXT    NOT NULL,
    closed_at            TEXT    NOT NULL DEFAULT '',
    closing_note         TEXT    NOT NULL DEFAULT ''
, goal_id INTEGER NOT NULL DEFAULT 0);

-- Finds action_items by report_definition_id.
CREATE INDEX idx_action_items_definition ON action_items (report_definition_id);

-- Finds action_items by goal_id.
CREATE INDEX idx_action_items_goal ON action_items (goal_id);

-- Finds action_items by owner_id.
CREATE INDEX idx_action_items_owner ON action_items (owner_id);

-- Finds action_items by publication_id.
CREATE INDEX idx_action_items_publication ON action_items (publication_id);

-- Finds the open Action Items by due date.
CREATE INDEX idx_action_items_open ON action_items (due_date, id) WHERE closed_at = '';

-- Every reassignment of an Action Item.
CREATE TABLE action_item_reassignments (
    id             INTEGER PRIMARY KEY,
    action_item_id INTEGER NOT NULL REFERENCES action_items(id),
    from_owner_id  INTEGER NOT NULL REFERENCES accounts(id),
    to_owner_id    INTEGER NOT NULL REFERENCES accounts(id),
    reassigned_by  INTEGER NOT NULL REFERENCES accounts(id),
    reassigned_at  TEXT    NOT NULL
);

-- Finds action_item_reassignments by action_item_id.
CREATE INDEX idx_action_item_reassignments_item ON action_item_reassignments (action_item_id);

-- The Report log: every change to a Report Definition.
CREATE TABLE report_log (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    actor_id             INTEGER NOT NULL REFERENCES accounts(id),
    kind                 TEXT    NOT NULL CHECK (kind IN ('created', 'edited', 'delegate', 'narrative', 'published')),
    detail               TEXT    NOT NULL,
    created_at           TEXT    NOT NULL
);

-- Finds report_log by report_definition_id.
CREATE INDEX idx_report_log_definition ON report_log (report_definition_id);

-- Finds report_log by actor_id.
CREATE INDEX idx_report_log_actor ON report_log (actor_id);

-- Refuses any edit: the log is append-only.
CREATE TRIGGER report_log_never_edited
BEFORE UPDATE ON report_log
BEGIN
    SELECT RAISE(ABORT, 'the Report log is never edited');
END;

-- Refuses any delete: the log is append-only.
CREATE TRIGGER report_log_never_removed
BEFORE DELETE ON report_log
BEGIN
    SELECT RAISE(ABORT, 'the Report log is never edited');
END;
