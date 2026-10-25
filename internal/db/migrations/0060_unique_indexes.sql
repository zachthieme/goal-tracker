-- 0060_unique_indexes: the domain's no-duplicates rules, now held by the
-- schema as well, so a bug or a race that slips past the domain can't store a
-- duplicate. The domain's checks stay the rule people meet, with its messages;
-- these indexes are the backstop. A name or value is compared COLLATE NOCASE,
-- which folds ASCII letters only: enough for a backstop to the domain's
-- case-blind comparison. Each index is named for its table, so a failure names
-- the table whether SQLite reports the index or its columns.
--
-- It settles nothing: a database already holding a duplicate fails this
-- migration on that index, naming the table, and keeps both rows, so an
-- operator decides by hand which one stays. Each new index leads with the
-- column the plain index it replaces held, so that one is dropped.

-- A Check-in records at most one reading of each Metric: the Check-in form
-- reads one value per Metric (readingsFromForm).
CREATE UNIQUE INDEX idx_metric_readings_unique_metric ON metric_readings (checkin_id, metric_id);
DROP INDEX idx_metric_readings_checkin;

-- A Report Definition chooses each Field it shows once: saving it drops a
-- Field chosen twice (normalizeReportDefinition).
CREATE UNIQUE INDEX idx_report_definition_fields_unique_field ON report_definition_fields (report_definition_id, field_id);
DROP INDEX idx_report_definition_fields_definition;

-- A Dimension holds no two values that differ only by case, Retired ones
-- included: defining it drops a value matching an earlier one
-- (DefineDimension), adding a value it already has finds that one
-- (matchingValue), and renaming a value to another's name is refused
-- (requireNoOtherValueNamed).
CREATE UNIQUE INDEX idx_dimension_values_unique_value ON dimension_values (dimension_id, value COLLATE NOCASE);
DROP INDEX idx_dimension_values_dimension;

-- No two Dimensions share a name whatever its case, Retired ones included, and
-- no two Fields do (requireFreeAttributeName). That check also refuses a
-- Dimension and a Field sharing a name, but an index can't span two tables, so
-- that clash stays the domain's check alone.
CREATE UNIQUE INDEX idx_dimensions_unique_name ON dimensions (name COLLATE NOCASE);
CREATE UNIQUE INDEX idx_fields_unique_name ON fields (name COLLATE NOCASE);
