# No built-in Team or org chart: Goals cross organizational boundaries

Goals belong to what they drive, not to a team. The org chart is not part of the model. An org that wants to group by team adds a Team Dimension, and a Report scoped to "my org" selects its Goals by that Dimension (ADR 0007; this ADR first scoped such a Report by root Goals and everything that contributes to them). We chose this so the graph shows how work actually connects to outcomes, not how reporting lines are drawn, and so it doesn't have to follow every reorg.

A Goal doesn't have to stay inside its Owner's part of the org. Who may own a Goal, and what it may contribute to, never depends on who reports to whom. One person can own a Goal that contributes to their VP's Goal and another that contributes to the Goal of an SVP outside their reporting chain, and a single Goal can contribute to both (ADR 0001). The only gate on a Contributes to link is the parent Owner's consent.

## Consequences

- Work that doesn't contribute to anything still disappears from leadership's view of the graph. To keep it visible, Active Goals with no parent are flagged Unaligned and listed where leadership can see them. A Report no longer depends on links (ADR 0007), so an Unaligned Goal still appears in any Report whose rules it matches.
- A person's Goals can't be found by walking the org chart alone. Their Goals may serve leaders outside their chain, and a leader's Goals may be served by people who don't report to them. "Everything that contributes to this Goal" and "every Goal owned by someone who reports to me" are different sets, and neither contains the other.
- Reporting lines may still be read from the org's sign-in to scope what a page or Report shows, such as the Goals owned by a leader's reporting chain (#259). That is a way of finding Goals, never a rule about them: the org chart is never edited in the tool, a reorg changes what such a scope finds and nothing about any Goal or link, and no link is refused, flagged or ranked lower for crossing a boundary.
- If anyone asks for a team tree view, or for links to be limited to a person's own org or reporting chain, point them to this ADR.
