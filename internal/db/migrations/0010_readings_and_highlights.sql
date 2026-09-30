-- 0010_readings_and_highlights: the Metric values and optional Highlight an
-- Owner records in a Check-in (ticket #6; CONTEXT.md: Metric, Highlight).
--
-- A Metric's current value is recorded at each Check-in (CONTEXT.md: Metric), so
-- a reading ties a value to the Check-in that recorded it and the Metric it
-- measures. Readings ordered over time give the Metric's trend against target.
CREATE TABLE metric_readings (
    id         INTEGER PRIMARY KEY,
    checkin_id INTEGER NOT NULL REFERENCES checkins(id),
    metric_id  INTEGER NOT NULL REFERENCES metrics(id),
    value      REAL    NOT NULL,
    created_at TEXT    NOT NULL
);

CREATE INDEX idx_metric_readings_metric ON metric_readings (metric_id);
CREATE INDEX idx_metric_readings_checkin ON metric_readings (checkin_id);

-- A Check-in may carry one optional Highlight, marked as an Insight,
-- Accomplishment, or Miss, that a Report author may pull into a Report's
-- narrative; the Goal's Owner is credited (CONTEXT.md: Highlight). At most one
-- per Check-in (UNIQUE checkin_id). Report curation queries Highlights by Goal
-- and by time range, reaching them through the Check-in they belong to (its
-- goal_id and created_at).
CREATE TABLE highlights (
    id         INTEGER PRIMARY KEY,
    checkin_id INTEGER NOT NULL UNIQUE REFERENCES checkins(id),
    kind       TEXT    NOT NULL,
    note       TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);

CREATE INDEX idx_highlights_checkin ON highlights (checkin_id);
