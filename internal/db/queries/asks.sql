-- name: ListOpenAsksForHelp :many
-- Every open Ask for help: each Active Goal, with its Owner, whose latest
-- Check-in (picked as ListActiveGoalsWithLatestCheckin picks it) is Yellow or
-- Red and asks for help, with that Check-in's Health and Path to Green
-- (CONTEXT.md: Path to Green, Ask for help). A Green Check-in, or one that
-- takes the Goal out of Active, ends the Run off Green, so closes the ask.
SELECT sqlc.embed(goals), sqlc.embed(accounts), latest.health, latest.path_to_green
FROM goals
JOIN accounts ON accounts.id = goals.owner_id
JOIN checkins latest ON latest.id = (
    SELECT c.id FROM checkins c
    WHERE c.goal_id = goals.id
    ORDER BY c.created_at DESC, c.id DESC
    LIMIT 1
)
WHERE goals.lifecycle = 'Active'
  AND latest.asks_for_help = 1
  AND latest.health IN ('Yellow', 'Red')
ORDER BY goals.id;

-- name: GetOpenAskForHelp :one
-- The Goal's open Ask for help, as ListOpenAsksForHelp finds them; no row when
-- it has none.
SELECT sqlc.embed(goals), sqlc.embed(accounts), latest.health, latest.path_to_green
FROM goals
JOIN accounts ON accounts.id = goals.owner_id
JOIN checkins latest ON latest.id = (
    SELECT c.id FROM checkins c
    WHERE c.goal_id = goals.id
    ORDER BY c.created_at DESC, c.id DESC
    LIMIT 1
)
WHERE goals.id = @goal_id
  AND goals.lifecycle = 'Active'
  AND latest.asks_for_help = 1
  AND latest.health IN ('Yellow', 'Red');

-- name: ListAskWalkAccounts :many
-- Every Account, for walking each ask's Owner up their reporting line in
-- memory.
SELECT * FROM accounts ORDER BY id;
