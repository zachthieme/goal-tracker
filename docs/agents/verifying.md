# Verifying a change in the browser

How to check a change on a fresh copy of current `main`, for `/verify-pending` step 4 or any time a change needs eyes on the page.

- `scripts/scratch-app start` builds the working tree, seeds a fresh database and serves it on a free port; `scripts/scratch-app stop` when done.
- `node scripts/shots.mjs <url> <shots>` takes screenshots in light, dark and 390px, and prints the facts a reviewer checks (overflow, focus, toast, dialog). Read the PNGs that matter.
- `scripts/contrast.py` whenever a colour token changed.
- `make e2e` runs the Playwright suite in `e2e/`: flows across pages, with assertions, each test on its own freshly seeded app. Add a spec there when a check is worth keeping; `e2e/README.md` has the fixtures and conventions. It isn't a gate.

The `issue-verifier` subagent (`.claude/agents/issue-verifier.md`) is this repo's verifier for `/verify-pending`; it knows the Go test overlay for probes.
