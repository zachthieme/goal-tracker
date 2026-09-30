// vetinari project config for goal-tracker — committed and versioned here.
//
// Machine-local state (logs, parked tasks, secrets) lives in .vetinari.local/,
// which is gitignored and never committed. Modeled on pike's config (also Go).
import { resolve } from "node:path";
import { defineConfig, githubBlockedBy, githubFetchTask, githubIssuesByLabel, githubMarkPendingVerify } from "vetinari";

export default defineConfig({
  project: "goal-tracker",
  // Built from vetinari/Dockerfile: node + the agent CLIs + Go 1.27 and
  // golangci-lint. templ/sqlc run via `go tool`, pinned in go.mod.
  image: "vetinari-goal-tracker",
  baseBranch: "main",

  stateDir: ".vetinari.local",

  // The gate — what "done" means. Each exits non-zero on failure. The
  // generate gate regenerates the templ/sqlc code and fails on any diff, so
  // stale generated files park instead of merging.
  gates: [
    { cmd: "make generate-check", label: "generate" },
    { cmd: "make test", label: "test" },
    { cmd: "make lint", label: "lint" },
  ],

  // Warm the module cache once per sandbox so the first gate isn't also the
  // first download.
  setup: ["go mod download"],

  // Go's module and build caches are concurrency-safe, so parallel sandboxes
  // share them instead of each paying a cold compile.
  mounts: [
    { hostPath: ".vetinari.local/cache/gomod", sandboxPath: "/home/agent/go/pkg/mod" },
    { hostPath: ".vetinari.local/cache/gocache", sandboxPath: "/home/agent/.cache/go-build" },
  ],

  // Fails the image fast, before an agent is launched, if the toolchain is missing.
  toolchainProbe: "go version && golangci-lint --version && claude --version && git --version",

  // Tasks are GitHub issues; closed issues are rejected rather than worked on.
  fetchTask: githubFetchTask("zachthieme/goal-tracker"),

  // Native GitHub "blocked by" links drive dependency-ordered campaign waves.
  blockedBy: githubBlockedBy("zachthieme/goal-tracker"),

  // Lets `campaign ready-for-agent` select its issue set from the tracker.
  listByLabel: githubIssuesByLabel("zachthieme/goal-tracker"),

  // After a merged wave's base gate passes: add `pending-verify`, drop
  // `ready-for-agent`. Best-effort — a failed label write never fails the run.
  onIssueMerged: githubMarkPendingVerify("zachthieme/goal-tracker"),

  // Sandcastle needs a writable global git config host-side; this machine's
  // real one is a read-only nix store symlink. Host-side only — never the
  // container, where GIT_CONFIG_GLOBAL would override the agent's HOME.
  //
  // That file MUST `[include] path = ~/.config/git/config`. Pointing
  // GIT_CONFIG_GLOBAL at a missing or bare file drops the real config's
  // `gh auth git-credential` helper, so the host-side `git fetch origin` before
  // worktree creation prompts for a GitHub password on a terminal and times the
  // worktree out after 30s (#6 failed this way).
  hostEnv: { GIT_CONFIG_GLOBAL: resolve(".vetinari.local/gitconfig") },
});
