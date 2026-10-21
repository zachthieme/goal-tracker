-- 0057_fk_indexes: an index on each foreign-key column a query filters or
-- joins on (ticket #427). goals had no index at all, so finding a person's
-- Goals read the whole table, and the queries joining a row to its author,
-- actor or other Account did the same.
--
-- The foreign-key columns left without one are only ever read with their own
-- row; TestEveryForeignKeyColumnLeadsAnIndex lists them, each with why.

CREATE INDEX idx_goals_owner ON goals (owner_id);

CREATE INDEX idx_checkins_author ON checkins (author_id);
CREATE INDEX idx_checkins_owner ON checkins (owner_id);

CREATE INDEX idx_comments_author ON comments (author_id);

CREATE INDEX idx_action_items_publication ON action_items (publication_id);

CREATE INDEX idx_contributors_account ON contributors (account_id);

CREATE INDEX idx_date_slips_milestone ON date_slips (milestone_id);

CREATE INDEX idx_definition_changes_actor ON definition_changes (actor_id);

CREATE INDEX idx_draft_highlights_logged_by ON draft_highlights (logged_by);

CREATE INDEX idx_goal_field_values_field ON goal_field_values (field_id);

CREATE INDEX idx_goal_title_revisions_author ON goal_title_revisions (author_id);
CREATE INDEX idx_why_it_matters_revisions_author ON why_it_matters_revisions (author_id);

CREATE INDEX idx_goal_value_changes_actor ON goal_value_changes (actor_id);

CREATE INDEX idx_handoffs_from ON handoffs (from_owner);
CREATE INDEX idx_handoffs_initiated_by ON handoffs (initiated_by);

CREATE INDEX idx_link_events_actor ON link_events (actor_id);

-- Removals are only looked up by the pair of Goals, as rejected link requests
-- already are (idx_rejected_link_requests_pair).
CREATE INDEX idx_link_removals_pair ON link_removals (child_id, parent_id);

CREATE INDEX idx_nudges_sent_by ON nudges (sent_by);

CREATE INDEX idx_parent_suggestions_suggested_by ON parent_suggestions (suggested_by);

CREATE INDEX idx_report_delegates_account ON report_delegates (account_id);
