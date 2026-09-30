# Starting point for a vetinari task image: node + git + gh + the resumable agent
# CLIs vetinari can drive — Claude Code, pi, and Codex (selectable per run with
# `--agent`, ADR 0016) — running as a non-root `agent` user whose UID/GID match the
# host's (sandcastle passes AGENT_UID/AGENT_GID when it builds).
#
# Credentials stay per-provider and user-supplied in `.vetinari.local/.env`:
#   claude → CLAUDE_CODE_OAUTH_TOKEN (or ANTHROPIC_API_KEY)
#   pi     → ANTHROPIC_API_KEY
#   codex  → OPENAI_API_KEY
# A run whose selected provider has no credential fails a preflight before launch.
#
# ADD YOUR PROJECT'S TOOLCHAIN — compilers, package managers, system libraries.
# The gates run inside this image, so anything your test command needs must be
# here. Prove it with `vetinari baseline` before running an agent.
#
# Build (then prove it with `vetinari baseline`):
#   docker build -t vetinari-goal-tracker \
#     --build-arg AGENT_UID=$(id -u) --build-arg AGENT_GID=$(id -g) \
#     -f vetinari/Dockerfile .
#
# That is byte-for-byte what sandcastle's own `buildImage` runs once a
# `--dockerfile` is given, so the image is identical to a `vetinari build` one.
# `vetinari build` itself cannot be used from this repo: it shells `npx sandcastle`,
# which resolves against THIS project (not the vetinari checkout) and fetches an
# unrelated registry package named `sandcastle`, and sandcastle's `build-image` CLI
# then demands a `.sandcastle/` dir that vetinari deliberately does not create —
# it sets `stateDir` so sandbox state lives under `.vetinari.local/`.
# Tracked upstream as jjforge/vetinari#395 and #396.

FROM node:22-bookworm

# System deps: sandcastle's own (git, curl, jq) plus build-essential — `make test`
# runs `go test -race`, which needs cgo and so a C compiler.
RUN apt-get update && apt-get install -y \
  git \
  curl \
  jq \
  build-essential \
  && rm -rf /var/lib/apt/lists/*

# GitHub CLI — needed if your fetchTask or agent reads issues in-container.
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
  && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
  | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
  && apt-get update && apt-get install -y gh \
  && rm -rf /var/lib/apt/lists/*

# pi and Codex agent CLIs — installed globally as root, before the drop to the
# non-root agent user (Claude Code is installed below, as that user, via its own
# installer). Any of the three can drive a run; `--agent` selects which (ADR 0016).
RUN npm install -g @mariozechner/pi-coding-agent @openai/codex

# --- goal-tracker's toolchain ---------------------------------------------
# Go, copied from the official image rather than installed by hand, so the patch
# level tracks the golang:1.27 tag. templ and sqlc are NOT installed here:
# they are pinned as `tool` deps in go.mod and run via `go tool`, so the
# generator versions travel with the repo, not the image.
COPY --from=golang:1.27-bookworm /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:/home/agent/go/bin:$PATH"

# golangci-lint pinned (same version as pike) so the `lint` gate is stable: a
# newer release enabling new checks would make the gate stricter than main and
# park clean work.
ARG GOLANGCI_LINT_VERSION=v2.13.2
RUN curl -fsSL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
  | sh -s -- -b /usr/local/bin "$GOLANGCI_LINT_VERSION"
# ------------------------------------------------------------------------------

ARG AGENT_UID=1000
ARG AGENT_GID=1000

RUN groupmod -o -g $AGENT_GID node && usermod -o -u $AGENT_UID -g $AGENT_GID -d /home/agent -m -l agent node
USER ${AGENT_UID}:${AGENT_GID}

RUN curl -fsSL https://claude.ai/install.sh | bash
ENV PATH="/home/agent/.local/bin:$PATH"

# Matt Pocock's Claude Code skills, at PERSONAL scope so every task's agent
# auto-discovers them — sandcastle runs the plain `claude --print` CLI (no
# --bare) with HOME=/home/agent, and the CLI loads ~/.claude/skills/<name>/SKILL.md
# with no flag or setting. Personal scope keeps them out of your repo and off
# every agent branch.
#
# This mirrors the `mattpocock-skills` PLUGIN as installed on the host (v1.2.3):
# SKILLS_REF is that version's commit, and the exact skill set is read from the
# plugin's own `.claude-plugin/plugin.json` manifest — 25 curated skills, not
# every dir in the repo. The CLI has no plugin machinery here, so the skills are
# flattened to ~/.claude/skills/<name>/ (discovery is not recursive into the
# repo's category dirs). Bump SKILLS_REF to the new plugin commit to update, then
# rebuild and re-run `baseline`.
ARG SKILLS_REF=84fdeffd12f2ee307994d1eb6feb48173b6e0502
RUN git clone https://github.com/mattpocock/skills /tmp/mp-skills \
 && git -C /tmp/mp-skills checkout "$SKILLS_REF" \
 && mkdir -p /home/agent/.claude/skills \
 && jq -r '.skills[]' /tmp/mp-skills/.claude-plugin/plugin.json \
    | while read -r rel; do cp -a "/tmp/mp-skills/$rel" /home/agent/.claude/skills/; done \
 && rm -rf /tmp/mp-skills

WORKDIR /home/agent

# Sandcastle bind-mounts the git worktree at /home/agent/workspace and sets it
# as the working directory at container start.
ENTRYPOINT ["sleep", "infinity"]
