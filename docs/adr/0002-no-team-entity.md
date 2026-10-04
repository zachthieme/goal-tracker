# No built-in Team or org chart

Goals belong to what they drive, not to a team. The org chart is not part of the model. An org that wants to group by team adds a Team Dimension, and a Report scoped to "my org" selects its Goals by that Dimension (ADR 0007; this ADR first scoped such a Report by root Goals and everything that contributes to them). We chose this so the graph shows how work actually connects to outcomes, not how reporting lines are drawn, and so it doesn't have to follow every reorg.

## Consequences

- Work that doesn't contribute to anything still disappears from leadership's view of the graph. To keep it visible, Active Goals with no parent are flagged Unaligned and listed where leadership can see them. A Report no longer depends on links (ADR 0007), so an Unaligned Goal still appears in any Report whose rules it matches.
- If anyone asks for a team tree view, point them to this ADR.
