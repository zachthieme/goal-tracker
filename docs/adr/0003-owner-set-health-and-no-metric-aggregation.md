# The Owner sets Health, the Rolled-up Health sits beside it, and Metrics are never aggregated

A Goal's Health is set by its Owner, not computed. The tool shows a Rolled-up Health (the worst Health among its active children) next to it, and when the two differ, the Owner has to explain why in that Check-in. We rejected computing Health because one Red child out of 40 would turn every ancestor Red. We rejected Owner-only Health because it lets bad news get buried. Metrics are never summed or averaged across Goals: with several parents per Goal, aggregation double counts. Each Goal states its own Metrics.

## Consequences

- Stale children don't change the Rolled-up Health color. The count of Stale children is shown next to it instead.
- The Owner's explanation, written whenever their Health differs from the Rolled-up Health, becomes the "so what" at every level of the graph.
