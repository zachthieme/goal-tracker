# Verifying a change in the browser

How to check a change on a fresh copy of current `main`, for `/verify-pending` step 4 or any time a change needs eyes on the page.

- `scripts/scratch-app start` builds the working tree, seeds a fresh database and serves it on a free port; `scripts/scratch-app stop` when done.
- `node scripts/shots.mjs <url> <shots>` takes screenshots in light, dark and 390px, and prints the facts a reviewer checks (overflow, focus, toast, dialog). Read the PNGs that matter.
- `scripts/contrast.py` whenever a colour token changed.

The `issue-verifier` subagent (`.claude/agents/issue-verifier.md`) is this repo's verifier for `/verify-pending`; it knows the Go test overlay for probes.
