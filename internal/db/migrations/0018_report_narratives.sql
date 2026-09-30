-- 0018_report_narratives: the narrative an author curates for a Report
-- Definition's next publication (ticket #18; CONTEXT.md: Highlight, Report).
--
-- While preparing a publication, the author picks which Highlights go into the
-- narrative's Insights, Accomplishments, and Misses (section holds the
-- Highlight kind the author reads it as, whatever it was flagged as), and adds
-- their own text to each section. This is the draft narrative: publishing
-- freezes it into the publication's snapshot and clears it, so the next
-- publication's narrative starts empty.

CREATE TABLE narrative_picks (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    highlight_id         INTEGER NOT NULL REFERENCES highlights(id),
    section              TEXT    NOT NULL,
    UNIQUE (report_definition_id, highlight_id)
);

CREATE TABLE narrative_texts (
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    section              TEXT    NOT NULL,
    text                 TEXT    NOT NULL,
    PRIMARY KEY (report_definition_id, section)
);
