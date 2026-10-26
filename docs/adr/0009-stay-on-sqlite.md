# Stay on SQLite until a trigger forces Postgres

ADR 0004 chose relational storage, "SQLite, later Postgres". In October 2026 a production-readiness review found the gaps a real install needs: foreign keys not enforced, rollback-journal mode, no pool limit, and email sent inside write transactions. We decided to close those gaps on SQLite (#409, #410, #412) rather than move to Postgres now. One small group of users fits easily on a single SQLite writer once nothing slow runs inside a transaction. Each gap is a line of configuration or a contained change. A move to Postgres would mean:

- rewriting 53 migrations, which store times as text;
- switching sqlc's engine and regenerating every query;
- replacing SQLite-specific SQL;
- running a database server for every test run.

## Consequences

- **What triggers the move:** any one of
  - more than one app process writing at once, for example two instances behind a load balancer;
  - writes visibly queueing on the single write lock after #410 and #412;
  - a hosting requirement for a managed database, with failover or point-in-time restore.

  "Postgres is more serious" is not a trigger.
- **Pragmas are per connection.** Foreign keys, WAL mode and the busy timeout are set on each connection through the connection string. Every place that opens the database builds its connection string with `db.DSN`, as `db.Open` and the test harness do, so they set the same ones.
- **Backups:** they use SQLite's online backup (`.backup` or `VACUUM INTO`), never a file copy. The WAL file beside the database is part of its state.
- **Testing stays portable.** Shifting time (`clock.Fixed`) and recording email (`email.Recorder`) live in the app, not the database. A fresh database per test, and failure injection by trigger, both have Postgres equivalents (`CREATE DATABASE … TEMPLATE`, PL/pgSQL triggers). A later move keeps every testing capability, only slower.
- **Keep the move cheap:** prefer portable SQL in new queries. Where SQLite-only behaviour is needed, keep it behind the domain service, as ADR 0004 already does for graph traversal.

## Notes

- 2026-10-26: migrations squashed into one baseline (`0001_baseline.sql`); the cost of rewriting the migration chain no longer applies. The decision stands (#617).
