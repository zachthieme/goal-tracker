# Authentik for local development

A local [Authentik](https://goauthentik.io) to build Goal Tracker's single
sign-on against. It's the org's identity provider and its directory: an OpenID
Connect application named **Goal Tracker**, and the seed's 28 people (see
`internal/seed/org.go`), each with their manager. A blueprint configures it
all when Authentik starts, so there's nothing to click.

It's pinned to Authentik 2026.8.3 (`compose.yml`), adapted from Authentik's
official compose file without the outpost integration.

## Start

Needs Docker with Compose. From this directory:

```sh
cp .env.example .env    # then fill in every value
docker compose up -d
./check.sh              # waits until it's ready, then checks the setup
```

The first start takes a minute or two. The admin UI is at
<http://localhost:9000>: sign in as `akadmin` with `AUTHENTIK_BOOTSTRAP_PASSWORD`.

`check.sh` needs `curl` and `jq`. It checks the application, every seeded
person and their manager, a password sign-in, and the claims Goal Tracker gets
through the authorization code flow. It prints `all checks passed` or lists
each failure.

## Stop, wipe, and re-apply

```sh
docker compose down       # stop; the database is kept
docker compose down -v    # stop and wipe the database
docker compose up -d      # a wiped database comes back configured from scratch
```

The configuration lives in `blueprints/goal-tracker.yaml`, mounted into
Authentik. Authentik applies it at start and again within about half a minute
of an edit; `./check.sh` applies it itself if Authentik hasn't caught up.
Applying a blueprint updates what it names and leaves anything else alone, so
to drop something you removed from the file, wipe the database.

## Point Goal Tracker at it

| Setting | Value |
| ------- | ----- |
| Issuer | `http://localhost:9000/application/o/goal-tracker/` (discovery at `.well-known/openid-configuration` under it) |
| Client ID | `goal-tracker` |
| Client secret | `GOAL_TRACKER_OIDC_CLIENT_SECRET` from `.env` |
| Redirect URI | `http://localhost:8080/auth/callback` or `http://127.0.0.1:8080/auth/callback` |
| Scopes | `openid email profile manager` |

Both redirect URIs are the app's default address, so run Goal Tracker on port
8080. To serve it somewhere else, add that address's `/auth/callback` to
`redirect_uris` in the blueprint. The client ID and secret also show in the
admin UI under **Applications → Providers → Goal Tracker**.

The tokens and userinfo carry `email` with `email_verified: true`, `name` (the
person's Name, as the seed has it), and `manager`: the email of the person they
report to. The CEO is at the top of the chain and has no `manager` claim. Each
person's manager is the `manager` attribute on their user in the blueprint.

## Sign in as a seeded person

Every seeded person signs in with their email and `DEV_USER_PASSWORD`, at
<http://localhost:9000> for Authentik's user portal, or through Goal Tracker
once it signs in with OIDC.

| Who | Email | Reports to |
| --- | ----- | ---------- |
| Dana Whitfield | `ceo@example.com` | — |
| Priya Raman, Marcus Bell, Robin Ellis | `cto@`, `cpo@`, `admin@example.com` | the CEO |
| Jonas Lindqvist, Sofia Marquez, Amara Nwosu | `platform-lead@`, `data-lead@`, `payments-lead@example.com` | the CTO |
| Elena Petrova, Kenji Watanabe, Rahul Iyer | `growth-lead@`, `mobile-lead@`, `support-lead@example.com` | the CPO |
| Everyone else | `firstname.lastname@example.com` | their team's lead |

The directory is written into the blueprint once and isn't kept in sync with
the seed. If the seed's people change, edit the blueprint and the table in
`check.sh` to match.
