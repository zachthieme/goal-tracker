-- 0031_several_highlights: a Check-in may carry several Highlights, of any mix
-- of kinds (ticket #104; CONTEXT.md: Highlight).
--
-- 0010 allowed at most one Highlight per Check-in (UNIQUE checkin_id). SQLite
-- can't drop a column constraint in place, so the table is rebuilt without it:
-- every existing Highlight is copied across with its id, Check-in, kind, note
-- and timestamp, so the narrative picks that name it by id still find it.
-- Within a Check-in, Highlights read in id order, the order they were entered.

CREATE TABLE highlights_new (
    id         INTEGER PRIMARY KEY,
    checkin_id INTEGER NOT NULL REFERENCES checkins(id),
    kind       TEXT    NOT NULL,
    note       TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

INSERT INTO highlights_new (id, checkin_id, kind, note, created_at)
SELECT id, checkin_id, kind, note, created_at FROM highlights;

DROP TABLE highlights;
ALTER TABLE highlights_new RENAME TO highlights;

CREATE INDEX idx_highlights_checkin ON highlights (checkin_id);
