# A report's scope comes from Goal attributes, not the Goal graph

A Report definition today selects Goals by starting at root Goals and descending accepted contributes-to links to a Depth. That makes the Goal graph the main filter. In the builder it reads as "pick the c-suite Goals", and it treats a DAG (ADR 0001) as a tree. We decided that a report's scope comes either from rules on Goal attributes (Dimensions, Owner, Lifecycle, Health, Top-level) or from a hand-picked list. Explicit Also include and Leave out lists override the rules. Selection is (rule matches ∪ Also include) − Leave out, or the picked list, and selecting never walks links. This supersedes ADR 0002's "a Report scoped to my org selects root Goals and everything that contributes to them": an org's Report is now a Dimension rule, such as Team is any of Platform, Identity.

## Consequences

- New definitions have no Depth. Existing definitions with depth 0 become hand-picked. Those with a depth above 0 are converted once, by migration, to a fixed set of Goals.
- The Goals in a report change only when a Goal's attributes change or an author edits the lists. Linking or unlinking a Goal doesn't move it in or out of a report.
- The Owner's Chain is a rule attribute (#264): "Owner is in the Chain of Priya" selects by an attribute of the Owner, so it walks no Contributes to links. The only walk is over Managers, from the org's directory (ADR 0008), as they are when the draft is computed.
- Fields are not a rule attribute. ADR 0005 keeps Fields for describing a Goal, not for slicing, so a report shows Fields but never selects by them. Anything a report needs to select by becomes a Dimension.
- A group of Goals that has no shared attribute is brought in with a Dimension (for example "In report"), not with links.
- If anyone proposes adding a Depth or "and its descendants" back to the scope, point them to this ADR. A one-off action that copies a Goal's contributors into Also include was considered and left out for now. It would add nothing live, so it wouldn't break this ADR, but hand-picking with search covers the need.
