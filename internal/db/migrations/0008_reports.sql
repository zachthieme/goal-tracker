-- 0008_reports: saved, reusable selections of Goals for a Report (CONTEXT.md:
-- Report Definition).
--
-- A Report Definition selects the Goals a Report covers in one of three ways:
-- root Goals traversed down the accepted "contributes to" links to a depth,
-- Dimension or Owner filters, or both. It also carries an introduction the
-- Report's narrative opens with. The selection itself is computed when read
-- (traversal + filters live in the domain, ADR-0004), so only the definition is
-- stored here, never the resulting Goal set.
--
-- The roots and the Dimension-value filters are each a set, so they live in
-- their own child tables. The Owner filter is a single Account, stored inline as
-- owner_filter_id where 0 means "no Owner filter" (the codebase uses 0/"" for
-- absent scalars rather than NULL, e.g. a Goal's empty delivery date).

CREATE TABLE report_definitions (
    id              INTEGER PRIMARY KEY,
    name            TEXT    NOT NULL,
    introduction    TEXT    NOT NULL,
    depth           INTEGER NOT NULL,
    owner_filter_id INTEGER NOT NULL DEFAULT 0,
    created_by      INTEGER NOT NULL REFERENCES accounts(id),
    created_at      TEXT    NOT NULL
);

CREATE TABLE report_definition_roots (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    goal_id              INTEGER NOT NULL REFERENCES goals(id)
);

CREATE INDEX idx_report_definition_roots_definition
    ON report_definition_roots (report_definition_id);

CREATE TABLE report_definition_filters (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    dimension_value_id   INTEGER NOT NULL REFERENCES dimension_values(id)
);

CREATE INDEX idx_report_definition_filters_definition
    ON report_definition_filters (report_definition_id);
