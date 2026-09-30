-- 0015_report_publications: published Reports (ticket #17; CONTEXT.md: Report
-- Definition).
--
-- Publishing a Report Definition freezes the Report as it read at that moment:
-- the whole rendered view model is kept as JSON in snapshot, so later edits to
-- the Goals it covers never change it. A row is written once and never updated.
-- The next publication of the same definition reads its changes against the
-- previous one's published_at.

CREATE TABLE report_publications (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    published_by         INTEGER NOT NULL REFERENCES accounts(id),
    published_at         TEXT    NOT NULL,
    snapshot             TEXT    NOT NULL
);

CREATE INDEX idx_report_publications_definition
    ON report_publications (report_definition_id);
