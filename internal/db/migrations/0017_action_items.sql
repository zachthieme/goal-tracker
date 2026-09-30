-- 0017_action_items: follow-ups raised while a Report is discussed (ticket #19;
-- CONTEXT.md: Action Item).
--
-- An Action Item belongs to a Report Definition and is raised on one of its
-- publications, either from a comment (comment_id) or directly (comment_id 0).
-- It carries into each new publication of the Definition until its owner
-- closes it with a note; closed_at is '' while it is open.

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
);

CREATE INDEX idx_action_items_definition ON action_items (report_definition_id);
