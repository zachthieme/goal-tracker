-- 0039_report_rules: a Report Definition selects its Goals by Report rules or
-- by a hand-picked list, never by following contributes-to links (ticket #147;
-- CONTEXT.md: Report Definition, Report rule; ADR 0007).
--
-- A definition's mode is 'rules' or 'picked'. A rules definition keeps its
-- rules in report_rules, in the order saved, each with its values in
-- report_rule_values: Dimension value ids or Account ids written as decimal
-- text, or Lifecycle or Health names. A Dimension rule names its Dimension in
-- dimension_id, which is 0 for every other attribute. The Goals a definition
-- lists by hand are in report_definition_goals: the picked list of a picked
-- definition, or the Also include and Leave out lists of a rules definition.
--
-- Existing definitions convert by what they have:
--   * No roots (filters only) becomes a rules definition: the Owner filter an
--     "Owner is" rule, and the Dimension filter, which ORs values within a
--     Dimension and ANDs across them, one "is any of" rule per Dimension.
--   * Roots becomes a picked definition, frozen to the Goals it selects now:
--     the roots, walked down accepted links to its depth (just the roots at
--     depth 0), then kept by the Owner filter and the Dimension filter. One
--     whose Goals all fail its filters is picked with an empty list.
-- Then the roots, the filters, depth and owner_filter_id are dropped.

ALTER TABLE report_definitions ADD COLUMN mode TEXT NOT NULL DEFAULT 'rules';

CREATE TABLE report_rules (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    attribute            TEXT    NOT NULL,
    dimension_id         INTEGER NOT NULL DEFAULT 0,
    op                   TEXT    NOT NULL
);

CREATE INDEX idx_report_rules_definition ON report_rules (report_definition_id);

CREATE TABLE report_rule_values (
    id             INTEGER PRIMARY KEY,
    report_rule_id INTEGER NOT NULL REFERENCES report_rules(id),
    value          TEXT    NOT NULL
);

CREATE INDEX idx_report_rule_values_rule ON report_rule_values (report_rule_id);

CREATE TABLE report_definition_goals (
    id                   INTEGER PRIMARY KEY,
    report_definition_id INTEGER NOT NULL REFERENCES report_definitions(id),
    -- 'picked', 'include' (Also include) or 'exclude' (Leave out).
    list                 TEXT    NOT NULL,
    goal_id              INTEGER NOT NULL REFERENCES goals(id)
);

CREATE INDEX idx_report_definition_goals_definition ON report_definition_goals (report_definition_id);

-- Filters only: the Owner filter becomes an "Owner is" rule.
INSERT INTO report_rules (report_definition_id, attribute, dimension_id, op)
SELECT d.id, 'owner', 0, 'is'
FROM report_definitions AS d
WHERE d.owner_filter_id != 0
  AND NOT EXISTS (SELECT 1 FROM report_definition_roots AS r WHERE r.report_definition_id = d.id)
ORDER BY d.id;

INSERT INTO report_rule_values (report_rule_id, value)
SELECT rr.id, CAST(d.owner_filter_id AS TEXT)
FROM report_rules AS rr
JOIN report_definitions AS d ON d.id = rr.report_definition_id
WHERE rr.attribute = 'owner'
ORDER BY rr.id;

-- Filters only: one "is any of" rule per filtered Dimension.
INSERT INTO report_rules (report_definition_id, attribute, dimension_id, op)
SELECT f.report_definition_id, 'dimension', v.dimension_id, 'is any of'
FROM report_definition_filters AS f
JOIN dimension_values AS v ON v.id = f.dimension_value_id
WHERE NOT EXISTS (SELECT 1 FROM report_definition_roots AS r WHERE r.report_definition_id = f.report_definition_id)
GROUP BY f.report_definition_id, v.dimension_id
ORDER BY f.report_definition_id, MIN(f.id);

INSERT INTO report_rule_values (report_rule_id, value)
SELECT rr.id, CAST(f.dimension_value_id AS TEXT)
FROM report_rules AS rr
JOIN report_definition_filters AS f ON f.report_definition_id = rr.report_definition_id
JOIN dimension_values AS v ON v.id = f.dimension_value_id AND v.dimension_id = rr.dimension_id
WHERE rr.attribute = 'dimension'
ORDER BY rr.id, f.id;

-- Roots: picked, frozen to the Goals the walk to depth and the filters select.
UPDATE report_definitions SET mode = 'picked'
WHERE EXISTS (SELECT 1 FROM report_definition_roots AS r WHERE r.report_definition_id = report_definitions.id);

WITH RECURSIVE reach (definition_id, goal_id, level, ord) AS (
    SELECT r.report_definition_id, r.goal_id, 0, r.id
    FROM report_definition_roots AS r
    UNION
    SELECT reach.definition_id, l.child_id, reach.level + 1, reach.ord
    FROM reach
    JOIN report_definitions AS d ON d.id = reach.definition_id
    JOIN links AS l ON l.parent_id = reach.goal_id AND l.status = 'accepted'
    WHERE reach.level < d.depth
)
INSERT INTO report_definition_goals (report_definition_id, list, goal_id)
SELECT reach.definition_id, 'picked', reach.goal_id
FROM reach
JOIN report_definitions AS d ON d.id = reach.definition_id
JOIN goals AS g ON g.id = reach.goal_id
WHERE (d.owner_filter_id = 0 OR g.owner_id = d.owner_filter_id)
  -- No filtered Dimension the Goal carries none of the filtered values in.
  AND NOT EXISTS (
    SELECT 1
    FROM report_definition_filters AS f
    JOIN dimension_values AS fv ON fv.id = f.dimension_value_id
    WHERE f.report_definition_id = d.id
      AND NOT EXISTS (
        SELECT 1
        FROM goal_dimension_values AS gv
        JOIN report_definition_filters AS f2 ON f2.dimension_value_id = gv.dimension_value_id
        JOIN dimension_values AS v2 ON v2.id = f2.dimension_value_id
        WHERE gv.goal_id = g.id
          AND f2.report_definition_id = d.id
          AND v2.dimension_id = fv.dimension_id
      )
  )
GROUP BY reach.definition_id, reach.goal_id
ORDER BY reach.definition_id, MIN(reach.level), MIN(reach.ord), reach.goal_id;

DROP TABLE report_definition_roots;
DROP TABLE report_definition_filters;
ALTER TABLE report_definitions DROP COLUMN depth;
ALTER TABLE report_definitions DROP COLUMN owner_filter_id;
