#!/usr/bin/env bash
# Applies blueprints/goal-tracker.yaml to a running local Authentik now, and
# waits until it has. Authentik re-applies the file by itself when the file
# changes, but not when only a value it reads with !Env does, such as
# GOAL_TRACKER_APP_URL; `make start` runs this after starting
# Authentik so the app's tailnet address is registered.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
set -a
# shellcheck source=/dev/null
. "$here/.env"
set +a

base=${AUTHENTIK_URL:-http://localhost:9000}

api() {
	curl -fsS -H "Authorization: Bearer $AUTHENTIK_BOOTSTRAP_TOKEN" "$@"
}

for _ in $(seq 120); do
	curl -fsS -o /dev/null "$base/-/health/ready/" 2>/dev/null && break
	sleep 2
done
blueprint=
for _ in $(seq 120); do
	blueprint=$(api "$base/api/v3/managed/blueprints/?path=custom/goal-tracker.yaml" 2>/dev/null | jq -r '.results[0].pk // empty' || true)
	[[ -n $blueprint ]] && break
	sleep 2
done
if [[ -z $blueprint ]]; then
	echo "Authentik never discovered blueprints/goal-tracker.yaml; see \`docker compose logs worker\`" >&2
	exit 1
fi

# Applying is a queued background task: wait for one that finished after this
# request. A status from before it, such as an earlier failure, isn't this
# apply's.
asked=$(date -u +%s)
api -o /dev/null -X POST "$base/api/v3/managed/blueprints/$blueprint/apply/"
for _ in $(seq 120); do
	applied=$(api "$base/api/v3/managed/blueprints/$blueprint/" | jq -r --argjson asked "$asked" '
		if (.last_applied | sub("\\.[0-9]+"; "") | fromdate) < $asked then "no"
		elif .status == "error" then "error"
		elif .status == "successful" then "yes"
		else "no" end')
	case $applied in
	yes) echo "applied blueprints/goal-tracker.yaml"; exit 0 ;;
	error) break ;;
	esac
	sleep 1
done
echo "applying blueprints/goal-tracker.yaml failed; see \`docker compose logs worker\`" >&2
exit 1
