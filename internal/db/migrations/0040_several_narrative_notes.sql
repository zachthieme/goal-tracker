-- 0040_several_narrative_notes: a Report author may write several of their own
-- notes into each narrative section (ticket #187; CONTEXT.md: Highlight,
-- Report).
--
-- 0018 keyed narrative_texts by Report Definition and section, so a section
-- held at most one note. SQLite can't drop a primary key in place, so the
-- table is rebuilt with an id instead: each existing note is copied across as
-- its section's first note. Within a section, notes read in id order, the
-- order they were entered.

CREATE TABLE narrative_texts_new (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    section              TEXT    NOT NULL,
    text                 TEXT    NOT NULL
);

INSERT INTO narrative_texts_new (report_definition_id, section, text)
SELECT report_definition_id, section, text FROM narrative_texts
ORDER BY report_definition_id, section;

DROP TABLE narrative_texts;
ALTER TABLE narrative_texts_new RENAME TO narrative_texts;

CREATE INDEX idx_narrative_texts_definition ON narrative_texts (report_definition_id);
