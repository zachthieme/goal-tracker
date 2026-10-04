# A publishing cadence and "Publish due" signal for Reports

This project does not give a Report Definition a publishing cadence (weekly, monthly, …), and the Reports list doesn't flag a report as "Publish due" when that cadence passes.

## Why this is out of scope

The Reports list already answers "which reports need publishing?" from the data. It sorts reports with changes since their last publication first, with the number of changes shown, then by last published, oldest first (#150). A report that has changed and hasn't been published for a long time rises to the top on its own.

A cadence would add:

- a per-report setting that someone has to choose and keep current
- a definition of "due", which would have to say whether a report with no changes is still due
- a second ordering rule that competes with "changes since" in the same list

None of that changes what an author does next: open the report at the top and publish it. A Report is published when a reader needs it, at whatever rhythm they need (CONTEXT.md: Report), and that rhythm lives in people's calendars, not in the tool.

If authors start to miss publications that a cadence would have caught, the first step is a reminder (an email, as Nudges are), not a status in the list.

## Prior requests

- #158 — "Reports: a \"Publish due\" signal on the Reports list"
