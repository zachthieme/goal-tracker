-- 0058_report_log_actor_index: the Report log joins each entry to its actor
-- (reportlog.sql), so report_log.actor_id gets an index like the other actor
-- columns in 0057_fk_indexes. The two 0057 migrations landed in the same wave.

CREATE INDEX idx_report_log_actor ON report_log (actor_id);
