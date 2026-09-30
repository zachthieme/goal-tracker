-- name: ListActiveGoalsWithLatestCheckin :many
-- Every Active Goal with the parts of its latest Check-in that judge its
-- freshness: when it was written, its Health, and its Path to Green's target
-- date. Each is empty when the Goal has no Check-in yet (CONTEXT.md: Stale,
-- Path to Green).
SELECT sqlc.embed(goals), sqlc.embed(accounts),
       CAST(COALESCE(latest.created_at, '') AS TEXT)       AS checkin_created_at,
       CAST(COALESCE(latest.health, '') AS TEXT)           AS checkin_health,
       CAST(COALESCE(latest.path_target_date, '') AS TEXT) AS checkin_path_target_date
FROM goals
JOIN accounts ON accounts.id = goals.owner_id
LEFT JOIN checkins latest ON latest.id = (
    SELECT c.id FROM checkins c
    WHERE c.goal_id = goals.id
    ORDER BY c.created_at DESC, c.id DESC
    LIMIT 1
)
WHERE goals.lifecycle = 'Active'
ORDER BY goals.created_at DESC, goals.id DESC;
