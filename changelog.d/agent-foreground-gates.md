section: Infrastructure
- [internal] Campaign agents run the gates in the foreground and commit before their turn ends: the agent image disables Claude Code's background tasks and raises its Bash timeout to 10 minutes, and CLAUDE.md says so. Agents that backgrounded `make test` and waited ended their single turn with nothing committed, parking #25, #28, #37, and #44 as "stalled, no-commit".
