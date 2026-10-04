# Parent suggestions are their own record, not a link status

Anyone other than a Goal's Owner may propose a parent for it, but only the Owner decides what their Goal contributes to. We made a Parent suggestion its own record, decided by the child Goal's Owner, rather than a "suggested" status on the link. A link is decided by the parent's Owner, so a status on it would have two different people deciding one record at different stages. It would also make "the link exists only once the parent's Owner accepts it" untrue for a record that sits in the link table, and a declined suggestion would reach the parent's Owner's view of their requests. Accepting a suggestion requests the link exactly as if the Owner had, so links keep one path and one decider.

## Consequences

- A Goal can carry open Parent suggestions and Pending link requests at once. A suggestion for a parent that is already linked or Pending is refused, and one whose link comes about another way closes as no longer applying.
- When the suggester owns the suggested parent, accepting creates the link already accepted, as a link request from a parent's Owner does.
- If anyone proposes folding suggestions into the link table to save a table, point them to this ADR.
