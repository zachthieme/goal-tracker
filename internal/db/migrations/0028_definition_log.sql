-- 0028_definition_log: the Definition log, every change to what Dimensions and
-- Fields exist and how they're shaped (ticket #76).
--
-- Each row is one change: who made it (actor_id), when, the Dimension or Field
-- it is about (exactly one of dimension_id and field_id is set; a change to a
-- Dimension's value is about its Dimension), and what changed as a sentence,
-- written with the names as they were at the time so a later rename or merge
-- doesn't rewrite it. Only the tool writes it, and nothing edits or removes a
-- row once written. Values set on Goals aren't here; they are in the Goal's
-- Value history (goal_value_changes). Changes made before this migration
-- aren't in the log.

CREATE TABLE definition_changes (
    id           INTEGER PRIMARY KEY,
    actor_id     INTEGER NOT NULL REFERENCES accounts(id),
    dimension_id INTEGER REFERENCES dimensions(id),
    field_id     INTEGER REFERENCES fields(id),
    summary      TEXT    NOT NULL,
    created_at   TEXT    NOT NULL,
    CHECK ((dimension_id IS NULL) <> (field_id IS NULL))
);

CREATE INDEX idx_definition_changes_dimension ON definition_changes (dimension_id);
CREATE INDEX idx_definition_changes_field ON definition_changes (field_id);

CREATE TRIGGER definition_changes_never_edited
BEFORE UPDATE ON definition_changes
BEGIN
    SELECT RAISE(ABORT, 'the Definition log is never edited');
END;

CREATE TRIGGER definition_changes_never_removed
BEFORE DELETE ON definition_changes
BEGIN
    SELECT RAISE(ABORT, 'the Definition log is never edited');
END;
