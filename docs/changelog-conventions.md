# Changelog: the format, the audience tags, and what counts as breaking

The rule in [`CLAUDE.md`](../CLAUDE.md) is that every change landing on `main`
gets a bullet in the top milestone of [`CHANGELOG.md`](../CHANGELOG.md). This
file holds the format, so `CLAUDE.md` only has to carry the obligation. The format
follows jjforge's.

## Fragments: how a campaign agent writes an entry

If every ticket edited `CHANGELOG.md`, tickets in the same campaign wave would all
touch the same file, conflict at merge, and halt the campaign. So **a campaign agent
doesn't edit `CHANGELOG.md`.** It writes a per-issue fragment to
`changelog.d/<issue>.md` (committed, not gitignored). When a wave merges, vetinari
folds that wave's fragments into `CHANGELOG.md` in one commit. It only does this
when the wave passes, so a halted wave's fragments stay for the retry.

A fragment names its section label and has one or more audience-tagged bullets:

```
section: New features
- [user] <entry text> (#<issue>).
```

It uses the section labels and audience tags below. It can have more than one
`section:` block when a change spans sections. When fragments are collected, their
bullets go into the top milestone if it's already dated today, or into a new
milestone dated today. They're grouped one block per section label.

**Never cite `changelog.d/<issue>.md` in a ticket's file-set marker.** The planner
compares files by basename, so every ticket would share the basename `<issue>.md`
and no two tickets could run in the same wave.

`vetinari changelog collect` runs the same fold by hand. Interactive work that isn't
racing a wave can edit `CHANGELOG.md` directly instead.

## Format

`CHANGELOG.md` is a curated history of dated milestones, newest first. It isn't
Keep-a-Changelog: there's no `[Unreleased]` section, and no `Added`/`Changed`
labels.

- Headings: `### <Short milestone name> — <Month DD, YYYY>`.
- **Bold** section labels, in this order: `**Breaking changes:**`, `**New features:**`,
  `**Improvements:**`, `**Bug fixes:**`, `**Security:**`, `**Infrastructure:**`,
  `**Architecture:**`, `**Testing:**`, `**Code quality:**`, `**Documentation:**`.
  **Each label appears at most once per milestone.** Add a bullet to the milestone's
  existing block for that label rather than starting a second block.
- `-` bullets in present or imperative tense, ending with a `(#NN)` issue ref where
  one exists.
- Keep the `For usage details, see [README](README.md).` footer last.

## Every bullet opens with an audience tag

Every change that lands gets a bullet, internal work included.

| Tag | The change is visible to |
| --- | --- |
| `[user]` | someone **using** Goal Tracker: web UI, emails, behaviour they can observe |
| `[ops]` | someone **running** it: env vars, migrations, deploy steps. Use it whenever an operator must *act*, even if the code change looks internal |
| `[api]` | a **programmatic contract**: see Breaking below |
| `[internal]` | nothing externally observable: refactors, CI, tests, docs, plumbing |

Bullets say what changed and **why**, not a list of commits. Purely mechanical
churn, such as a formatting pass or a dependency bump that changes no behaviour, can
share a single `[internal]` bullet.

## A breaking change is a section, not an adjective

When a milestone has breaking changes, `**Breaking changes:**` comes **first**. Each
entry names **which contract broke** and what the reader has to do about it:

- **Spreadsheet import format**: the column names and meanings in
  [`docs/import-format.md`](import-format.md) that someone's existing spreadsheet
  depends on.
- **Database schema / migrations**: a migration that isn't backward-compatible, or
  one that requires deploy steps in a particular order.
- **Configuration**: a renamed or removed `GOAL_TRACKER_*` environment variable, or
  a change to one's meaning.
