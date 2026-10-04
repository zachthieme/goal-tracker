# A report's scope comes from Goal attributes, not the Goal graph

A Report definition today selects Goals by starting at root Goals and descending accepted contributes-to links to a Depth. That makes the Goal graph the main filter. In the builder it reads as "pick the c-suite Goals", and it treats a DAG (ADR 0001) as a tree. We decided that a report's scope comes either from rules on Goal attributes (Dimensions, Fields, Owner, Lifecycle, Health, Top-level) or from a hand-picked list. Explicit Also include and Leave out lists override the rules. Selection is (rule matches ∪ Also include) − Leave out, or the picked list, and selecting never walks links.

## Consequences

- New definitions have no Depth. Existing definitions with depth 0 become hand-picked. Those with a depth above 0 are converted once, by migration, to a fixed set of Goals.
- The Goals in a report change only when a Goal's attributes change or an author edits the lists. Linking or unlinking a Goal doesn't move it in or out of a report.
- A group of Goals that has no shared attribute is brought in with a Dimension (for example "In report"), not with links.
- If anyone proposes adding a Depth or "and its descendants" back to the scope, point them to this ADR. A one-off action that copies a Goal's contributors into Also include is still open: it adds nothing live, so it would be consistent with this ADR.
