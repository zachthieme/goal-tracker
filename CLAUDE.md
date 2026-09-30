# CLAUDE.md

Goal Tracker: an MBR-caliber goal-tracking tool. Goals, Metrics, Milestones, Check-ins, and Reports. The domain language is in `CONTEXT.md` and the key decisions are in `docs/adr/`.

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues for `zachthieme/goal-tracker`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Uses the five default labels: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
