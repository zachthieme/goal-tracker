package domain

import (
	"context"
	"database/sql"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// writeOutsideTxGuard is the database a Service runs its queries on outside a
// transaction: it reads as the database does, and panics on a write, so a
// command that writes without WithinTx fails the first time it runs rather
// than leaving its checks and its writes in different transactions (#571). A
// Service bound to a transaction runs on the *sql.Tx, which has no guard.
type writeOutsideTxGuard struct {
	db *sql.DB
}

var _ db.DBTX = writeOutsideTxGuard{}

func (g writeOutsideTxGuard) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	refuseWriteOutsideTx(query)
	return g.db.ExecContext(ctx, query, args...)
}

func (g writeOutsideTxGuard) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	refuseWriteOutsideTx(query)
	return g.db.PrepareContext(ctx, query)
}

func (g writeOutsideTxGuard) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	refuseWriteOutsideTx(query)
	return g.db.QueryContext(ctx, query, args...)
}

func (g writeOutsideTxGuard) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	refuseWriteOutsideTx(query)
	return g.db.QueryRowContext(ctx, query, args...)
}

// refuseWriteOutsideTx panics, naming the query, when query writes.
func refuseWriteOutsideTx(query string) {
	if name, write := sqlcQueryWrites(query); write {
		panic("write outside a transaction: " + name)
	}
}

// sqlcQueryWrites reads a query sqlc generated: its name, from its "-- name:"
// line, and whether it writes, which it does when its first statement, after
// any comments, starts with INSERT, UPDATE, DELETE or REPLACE.
func sqlcQueryWrites(query string) (name string, write bool) {
	for line := range strings.Lines(query) {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "-- name:"); ok {
			if fields := strings.Fields(rest); len(fields) > 0 {
				name = fields[0]
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		verb, _, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "INSERT", "UPDATE", "DELETE", "REPLACE":
			write = true
		}
		break
	}
	return name, write
}
