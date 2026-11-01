// vetinari project config for goal-tracker — committed and versioned here.
//
// Machine-local state (logs, parked tasks, secrets) lives in .vetinari.local/,
// which is gitignored and never committed. Modeled on pike's config (also Go).
import { resolve } from "node:path";
import { defaultFileSet, defineConfig, githubTracker } from "vetinari";

export default defineConfig({
  project: "goal-tracker",
  // Built from vetinari/Dockerfile: node + the agent CLIs + Go 1.27 and
  // golangci-lint. templ/sqlc run via `go tool`, pinned in go.mod.
  image: "vetinari-goal-tracker",
  baseBranch: "main",

  // Pin the agent model: vetinari's built-in claude default (claude-opus-4-8) lags
  // the current Opus. Per-invocation --model still overrides this.
  agent: { provider: "claude", model: "claude-opus-5-5", effort: "high" },

  stateDir: ".vetinari.local",

  // The gate — what "done" means. Each exits non-zero on failure. The
  // generate gate regenerates the templ/sqlc code and fails if any generated
  // file is modified or untracked, or if go.mod/go.sum isn't tidy, so stale
  // or never-committed generated files park instead of merging. Lint (seconds)
  // runs before test (minutes), so a lint failure surfaces without waiting on
  // the tests.
  //
  // The e2e gate runs the Playwright suite (e2e/README.md), the only check the
  // inline JavaScript in the templ files gets: the highlight editor, the Goal
  // form's live checks, the Check-in submit bar, the Report builder (#470). On
  // a branch it runs only when the branch changes what the browser gets or the
  // suite itself: a template, a non-test Go file in internal/web (handlers and
  // generated templ code), internal/web/static/, e2e/, or the seed the suite's
  // template databases are built from. It also runs on a change under
  // internal/db/ (migrations, queries, generated code), because the seed and
  // its manifest, which picks the specs' Goals and people (e2e/manifest.ts),
  // are built on the schema and the generated queries. `when` is tested
  // against the changed files, one per line, hence the `m` flag. `when` scopes
  // the gate on a branch only: the merged-base gate after each wave runs every
  // gate, e2e included, whatever the wave changed (vetinari's gateMergedBase
  // passes `all`), so a change `when` skips, such as one to internal/domain, is
  // still covered there. budgetSeconds: `make e2e` took 21-25s (73 tests) in
  // the campaign container, so 180 leaves room for parallel sandboxes and a
  // cold Go build. The image sets PLAYWRIGHT_BROWSERS_PATH to its installed
  // Chromium, so the gate's `playwright install chromium` downloads nothing.
  gates: [
    { cmd: "make generate-check", label: "generate" },
    { cmd: "make lint", label: "lint" },
    { cmd: "make test", label: "test" },
    {
      cmd: "make e2e",
      label: "e2e",
      when: /^(?:internal\/web\/.*\.templ$|internal\/web\/[^/]*(?<!_test)\.go$|internal\/web\/static\/|e2e\/|internal\/seed\/|cmd\/seed\/|internal\/db\/)/m,
      budgetSeconds: 180,
    },
  ],

  // Warm the module cache once per sandbox so the first gate isn't also the
  // first download, and install the e2e suite's npm packages once, so the e2e
  // gate doesn't run `npm ci` itself (its Makefile prerequisite then finds
  // e2e/node_modules up to date).
  setup: ["go mod download", "npm ci --prefix e2e"],

  // Go's module and build caches are concurrency-safe, so parallel sandboxes
  // share them instead of each paying a cold compile.
  mounts: [
    { hostPath: ".vetinari.local/cache/gomod", sandboxPath: "/home/agent/go/pkg/mod" },
    { hostPath: ".vetinari.local/cache/gocache", sandboxPath: "/home/agent/.cache/go-build" },
  ],

  // Fails the image fast, before an agent is launched, if the toolchain is missing.
  toolchainProbe: "go version && golangci-lint --version && claude --version && git --version",

  // The GitHub tracker preset wires every tracker seam against this repo's
  // `origin` (zachthieme/goal-tracker): fetchTask (closed issues are rejected),
  // blockedBy for dependency-ordered waves, listByLabel for `campaign
  // ready-for-agent`, onIssueMerged (`pending-verify`), reportFinding (filed from
  // the host, since the container holds no GitHub credentials; `needs-triage` by
  // default), and findOpenIssue + postComment, so a repeat finding gets a "seen
  // again" comment instead of a duplicate issue.
  ...githubTracker(),

  // Which files a ticket touches, for keeping a wave file-disjoint.
  fileSet: defaultFileSet(),

  // Files two co-wave tickets may both edit: the planner leaves them out of the
  // collision keys, and integration's conflict park catches the rare clash.
  // DESIGN.md is on nearly every UI ticket's Touches: line, so counting it put
  // one ticket in each wave (a 40-issue campaign planned 32 waves, about 14
  // without it); replaying 23 pairs of nearby campaign branches that both edited
  // it with git merge-tree gave no conflict. styles_test.go stays counted: its
  // tests are appended at the end of the file, and the merge replay in
  // jjforge/vetinari#611 found a real conflict in it.
  waivedFiles: ["DESIGN.md"],

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
