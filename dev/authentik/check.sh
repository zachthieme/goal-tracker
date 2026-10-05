#!/usr/bin/env bash
# Checks a running local Authentik against what the blueprint promises: the
# Goal Tracker application, the manager scope, every seeded person with their
# manager, password sign-in, and the manager claim Goal Tracker will read.
# Run from anywhere after `docker compose up -d`; reads .env beside this script.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
set -a
# shellcheck source=/dev/null
. "$here/.env"
set +a

base=${AUTHENTIK_URL:-http://localhost:9000}
failures=0

pass() { printf 'ok    %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; failures=$((failures + 1)); }

# api GET path: an authenticated Authentik API call as akadmin.
api() {
	curl -fsS -H "Authorization: Bearer $AUTHENTIK_BOOTSTRAP_TOKEN" "$base/api/v3/$1"
}

# applied_since epoch: whether Authentik has successfully applied this
# blueprint file, as it is now, at or after epoch: "yes", "no" or "error".
want_hash=$(sha512sum "$here/blueprints/goal-tracker.yaml" | cut -d' ' -f1)
applied_since() {
	api "managed/blueprints/$blueprint/" | jq -r --arg hash "$want_hash" --argjson since "$1" '
		if .status == "error" then "error"
		elif .status == "successful" and .last_applied_hash == $hash
			and (.last_applied | sub("\\.[0-9]+"; "") | fromdate) >= $since then "yes"
		else "no" end'
}

# Wait for the server and for the worker to have discovered the blueprint.
for _ in $(seq 120); do
	curl -fsS -o /dev/null "$base/-/health/ready/" 2>/dev/null && break
	sleep 2
done
blueprint=
for _ in $(seq 120); do
	blueprint=$(api "managed/blueprints/?path=custom/goal-tracker.yaml" 2>/dev/null | jq -r '.results[0].pk // empty' || true)
	[[ -n $blueprint ]] && break
	sleep 2
done
if [[ -z $blueprint ]]; then
	echo "Authentik never discovered blueprints/goal-tracker.yaml" >&2
	exit 1
fi

# On a fresh volume Authentik applies the blueprint itself; give it time. After
# an edit it may not have looked yet, so apply it now. Applying is a queued
# background task and one queued earlier may land first, so wait for an apply
# that finished after the request and read the file as it is.
applied=no
for _ in $(seq 60); do
	applied=$(applied_since 0)
	[[ $applied == no ]] || break
	sleep 2
done
if [[ $applied == yes ]]; then
	echo "Authentik has applied blueprints/goal-tracker.yaml"
elif [[ $applied == no ]]; then
	echo "Authentik hasn't applied blueprints/goal-tracker.yaml as it is now; applying it"
	asked=$(date -u +%s)
	curl -fsS -o /dev/null -X POST -H "Authorization: Bearer $AUTHENTIK_BOOTSTRAP_TOKEN" \
		"$base/api/v3/managed/blueprints/$blueprint/apply/"
	for _ in $(seq 120); do
		applied=$(applied_since "$asked")
		[[ $applied == no ]] || break
		sleep 1
	done
fi
if [[ $applied != yes ]]; then
	echo "applying blueprints/goal-tracker.yaml failed; see \`docker compose logs worker\`" >&2
	exit 1
fi

# --- The application ---------------------------------------------------------

app=$(api "core/applications/?slug=goal-tracker" | jq '.results[0] // empty')
if [[ -n $app && $(jq -r .name <<<"$app") == "Goal Tracker" ]]; then
	pass "Goal Tracker application exists"
else
	fail "Goal Tracker application exists"
fi

# --- The directory -----------------------------------------------------------

# Every Account the seed creates (internal/seed/org.go), as email, Name and
# manager's email; the CEO has no manager.
people='
ceo@example.com|Dana Whitfield|
cto@example.com|Priya Raman|ceo@example.com
cpo@example.com|Marcus Bell|ceo@example.com
admin@example.com|Robin Ellis|ceo@example.com
platform-lead@example.com|Jonas Lindqvist|cto@example.com
data-lead@example.com|Sofia Marquez|cto@example.com
payments-lead@example.com|Amara Nwosu|cto@example.com
growth-lead@example.com|Elena Petrova|cpo@example.com
mobile-lead@example.com|Kenji Watanabe|cpo@example.com
support-lead@example.com|Rahul Iyer|cpo@example.com
mei.lin@example.com|Mei Lin|platform-lead@example.com
ada.okafor@example.com|Ada Okafor|platform-lead@example.com
tomas.berg@example.com|Tomas Berg|platform-lead@example.com
luca.romano@example.com|Luca Romano|payments-lead@example.com
noor.haddad@example.com|Noor Haddad|payments-lead@example.com
sam.kowalski@example.com|Sam Kowalski|payments-lead@example.com
hana.sato@example.com|Hana Sato|growth-lead@example.com
ines.duarte@example.com|Ines Duarte|growth-lead@example.com
kwame.mensah@example.com|Kwame Mensah|growth-lead@example.com
olu.adeyemi@example.com|Olu Adeyemi|mobile-lead@example.com
freya.nilsen@example.com|Freya Nilsen|mobile-lead@example.com
diego.alvarez@example.com|Diego Alvarez|mobile-lead@example.com
ben.fischer@example.com|Ben Fischer|data-lead@example.com
anika.rao@example.com|Anika Rao|data-lead@example.com
yara.nasser@example.com|Yara Nasser|data-lead@example.com
grace.oduya@example.com|Grace Oduya|support-lead@example.com
matteo.ricci@example.com|Matteo Ricci|support-lead@example.com
leah.cohen@example.com|Leah Cohen|support-lead@example.com
'

while IFS='|' read -r email name manager; do
	[[ -z $email ]] && continue
	user=$(api "core/users/?email=$email" | jq '.results[0] // empty')
	if [[ -z $user ]]; then
		fail "$email exists"
		continue
	fi
	got=$(jq -r '[.name, .is_active, (.attributes.manager // ""), (.attributes.email_verified // false)] | join("|")' <<<"$user")
	want="$name|true|$manager|true"
	if [[ $got == "$want" ]]; then
		pass "$email is $name, active, verified, manager ${manager:-none}"
	else
		fail "$email: want $want (name|active|manager|verified), got $got"
	fi
done <<<"$people"

# --- Signing in -------------------------------------------------------------

jar=$(mktemp)
trap 'rm -f "$jar"' EXIT

# executor flow query json: post one stage's answer to a flow, following the
# redirect back to the next challenge.
executor() {
	curl -fsSL -b "$jar" -c "$jar" -H 'Content-Type: application/json' -d "$3" \
		"$base/api/v3/flows/executor/$1/?query=$(jq -rn --arg q "$2" '$q|@uri')"
}

# sign_in email password: a fresh session through the default authentication
# flow; prints the flow's last challenge component.
sign_in() {
	: >"$jar"
	curl -fsS -o /dev/null -b "$jar" -c "$jar" "$base/api/v3/flows/executor/default-authentication-flow/?query="
	executor default-authentication-flow "" "$(jq -n --arg uid "$1" '{component: "ak-stage-identification", uid_field: $uid}')" >/dev/null
	executor default-authentication-flow "" "$(jq -n --arg pw "$2" '{component: "ak-stage-password", password: $pw}')" | jq -r .component
}

signed_in_as() {
	curl -fsS -b "$jar" "$base/api/v3/core/users/me/" 2>/dev/null | jq -r '.user.email // empty'
}

if [[ $(sign_in noor.haddad@example.com "$DEV_USER_PASSWORD") == xak-flow-redirect && $(signed_in_as) == noor.haddad@example.com ]]; then
	pass "noor.haddad@example.com signs in with DEV_USER_PASSWORD"
else
	fail "noor.haddad@example.com signs in with DEV_USER_PASSWORD"
fi
sign_in noor.haddad@example.com "not-$DEV_USER_PASSWORD" >/dev/null
if [[ -z $(signed_in_as) ]]; then
	pass "a wrong password is refused"
else
	fail "a wrong password is refused"
fi

# --- The manager claim -------------------------------------------------------

# tokens email redirect_uri: sign in as email and run Goal Tracker's
# authorization code flow with scope "openid email profile manager"; prints the
# token response.
tokens() {
	sign_in "$1" "$DEV_USER_PASSWORD" >/dev/null
	local redirect_uri=$2 query location code
	query="client_id=goal-tracker&response_type=code&scope=openid+email+profile+manager&state=check&nonce=check&redirect_uri=$(jq -rn --arg u "$redirect_uri" '$u|@uri')"
	location=$(curl -fsS -o /dev/null -w '%{redirect_url}' -b "$jar" -c "$jar" "$base/application/o/authorize/?$query")
	# Authentik hands the request to its consent flow; run that flow too.
	if [[ $location == "$base/if/flow/"* ]]; then
		local flow=${location#"$base/if/flow/"}
		flow=${flow%%/*}
		location=$(curl -fsS -b "$jar" -c "$jar" "$base/api/v3/flows/executor/$flow/?query=$(jq -rn --arg q "$query" '$q|@uri')" | jq -r '.to // empty')
	fi
	code=$(sed -n 's/.*[?&]code=\([^&]*\).*/\1/p' <<<"$location")
	if [[ -z $code ]]; then
		echo "no authorization code for $1; redirected to ${location:-nowhere}" >&2
		return 1
	fi
	curl -fsS -u "goal-tracker:$GOAL_TRACKER_OIDC_CLIENT_SECRET" \
		-d grant_type=authorization_code -d "code=$code" --data-urlencode "redirect_uri=$redirect_uri" \
		"$base/application/o/token/"
}

# jwt_payload token: the decoded claims of a JWT.
jwt_payload() {
	local p
	p=$(cut -d. -f2 <<<"$1" | tr '_-' '/+')
	while ((${#p} % 4)); do p+='='; done
	base64 -d <<<"$p"
}

# The app's default base URL is http://localhost:8080; 127.0.0.1 is the same
# app, so both callbacks are registered.
for case in \
	noor.haddad@example.com=payments-lead@example.com=http://127.0.0.1:8080/auth/callback \
	payments-lead@example.com=cto@example.com=http://127.0.0.1:8080/auth/callback \
	ceo@example.com==http://localhost:8080/auth/callback; do
	IFS='=' read -r email want redirect <<<"$case"
	if ! response=$(tokens "$email" "$redirect"); then
		fail "$email: manager claim (no tokens)"
		continue
	fi
	declare -A claims=(
		[userinfo]=$(curl -fsS -H "Authorization: Bearer $(jq -r .access_token <<<"$response")" "$base/application/o/userinfo/")
		[id_token]=$(jwt_payload "$(jq -r .id_token <<<"$response")")
	)
	for source in userinfo id_token; do
		if [[ $email == ceo@example.com ]]; then
			got=$(jq -c '{email, email_verified, name}' <<<"${claims[$source]}")
			want_id='{"email":"ceo@example.com","email_verified":true,"name":"Dana Whitfield"}'
			if [[ $got == "$want_id" ]]; then
				pass "$email: $source carries a verified email and the Name"
			else
				fail "$email: $source identity claims: want $want_id, got $got"
			fi
		fi
		got=$(jq -r 'if has("manager") then .manager else "(none)" end' <<<"${claims[$source]}")
		if [[ $got == "${want:-(none)}" ]]; then
			pass "$email: $source manager claim is ${want:-absent}"
		else
			fail "$email: $source manager claim: want ${want:-absent}, got $got"
		fi
	done
done

echo
if ((failures)); then
	echo "$failures check(s) failed"
	exit 1
fi
echo "all checks passed"
