# Relational storage (SQLite, later Postgres), not a graph database

The Goal graph is stored in ordinary relational tables: Goals, plus a table of links between them. We don't use an embedded graph database. We considered embedded graph stores (Kuzu, Cayley, EliasDB, DuckDB with a graph extension) and rejected them for three reasons:

- **The graph is small and simple.** It has no cycles and holds hundreds to low thousands of Goals. The only graph queries are ancestors and descendants to a depth, and a cycle check before accepting a link. SQL's recursive queries (recursive CTEs) handle these easily, and a whole subgraph fits in memory for roll-ups.
- **Most of the data isn't graph-shaped.** Check-in history, Date Slips, Metric readings, and frozen Report snapshots are timestamped records. SQL handles those better than a graph store does.
- **The Go embedded graph options are risky.** They're either inactive, have small communities, or need a C compiler toolchain (cgo), and none of them has a clear path onto Starbucks-hosted infrastructure. SQLite moves to Postgres with little change.

## Consequences

- Graph traversal happens behind the domain service, so it could be swapped out if query patterns change. Roll-ups load the selected subgraph and compute in memory.
