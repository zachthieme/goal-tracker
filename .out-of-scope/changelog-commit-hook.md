# A commit hook that enforces the changelog

This project does not refuse commits that change code without a changelog entry, whether through a git pre-commit hook or a similar per-commit check.

## Why this is out of scope

`CLAUDE.md` requires a changelog entry for every change, and nothing mechanical enforces it. A pre-commit hook looks like the obvious fix. Measured against the history on 2026-10-03, it would have caused more trouble than it prevented.

**Agents commit in steps.** Vetinari's agents typically commit the code first and the changelog fragment in a later commit on the same branch. Of 317 commits tagged with an issue number, 222 (70%) changed code without touching the changelog. A per-commit rule would have refused all of them, unless every agent changed how it commits.

**The entries weren't missing.** Grouped by issue, all 91 issues had their changelog entry. 88 added it in one of their own commits, and the other three (#24, #65, #69) got it through a commit without the issue tag. The manual changelog checks seen in interactive sessions were people double-checking, not catching misses. The two commit collisions that did happen came from vetinari collecting fragments, which a hook wouldn't prevent.

**Where a miss is likely, there's already a tool.** An entry is most likely forgotten in interactive work at the end of a session. The `finishing-up` skill commits and updates the changelog when a session wraps up.

If the record ever changes, and merged issues start arriving without entries, the right shape is a **per-branch** check rather than a per-commit one. For example, a script that fails when a branch's commits change code but add no `changelog.d/` fragment, run as a vetinari gate. That fits how agents commit.

## Prior requests

- #115 — "A pre-commit hook that refuses code changes without a changelog entry"
