# One Goal type, linked as a graph with no cycles

There is only one kind of node, a Goal. The same shape covers an org-wide outcome and a team project, and a Goal can contribute to many parents, as long as the links never form a cycle. We rejected typed OKR levels (Objective → Key Result → Project) because they cause arguments over what kind of thing a piece of work is. We rejected a strict tree because real work often serves more than one outcome, e.g. a displays migration that both reduces outages and cuts cost. The ban on cycles guarantees that roll-ups finish. A parent's Owner must accept each link, so a graph at the scale of 1000 people doesn't fill up with loosely related work.

## Consequences

- If a reader wants to know what kind of Goal something is, that's a Dimension, not a type.
- Nothing is summed up the graph (see ADR-0003). Because a Goal can have several parents, sums would count the same child more than once.
