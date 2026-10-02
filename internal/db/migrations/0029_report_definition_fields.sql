-- 0029_report_definition_fields: the Fields a Report Definition shows beside
-- each Goal (ticket #78; CONTEXT.md: Field, Report Definition; ADR 0005).
--
-- A Report shows no Fields unless its author chooses some, so every existing
-- Report Definition has none and renders as before. Like the roots and the
-- Dimension-value filters, the chosen Fields are a set in their own child
-- table, kept in the order they were saved. A Field is retired, never deleted,
-- so a chosen Field stays readable after it is retired. A publication freezes
-- the Field names, units and values it showed in its snapshot, so nothing here
-- is read back by a published Report.

CREATE TABLE report_definition_fields (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    field_id             INTEGER NOT NULL REFERENCES fields(id)
);

CREATE INDEX idx_report_definition_fields_definition
    ON report_definition_fields (report_definition_id);
