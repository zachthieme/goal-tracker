# Hierarchical Dimension values

This project does not let a Dimension's values nest under one another, such as Org > Team > Squad, with a Goal under a child value counting under its parents too.

## Why this is out of scope

A Dimension is a flat list (ADR 0005). Each value is a label that filters and groups Goals, and nothing more. Nesting values would bring back the team tree that ADR 0002 rejects. The values would form an org chart in all but name: a parent for every value, rollups up the tree, and the tree to rework at every reorg.

ADR 0002 keeps the org chart out of the model so the Goal graph shows how work connects to outcomes, not how reporting lines are drawn. "My org's Goals" is already answered two ways without a tree:

- **By a Dimension rule.** A Report scoped to an org selects Goals whose Team is any of that org's teams (ADR 0007).
- **By more than one flat Dimension.** An org that wants both levels adds an Org Dimension and a Team Dimension, and sets both on a Goal. A Report can filter by one and group by the other.

Nesting would also make grouping ambiguous. A Goal tagged with a Team would appear under its Org as well, on top of the multi-parent double counting ADR 0003 and ADR 0005 already guard against.

## Prior requests

- #82 — "Deferred from the Dimensions and Fields work", where it was listed as rejected rather than deferred during the grilling session of 2026-10-01 on extending Dimensions (#69 to #81)
