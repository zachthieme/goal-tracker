package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every not-yet-applied migration in migrations/, in filename
// order, each in its own transaction. Applied versions are tracked in
// schema_migrations so it is safe to call on every startup. It is the single
// source of schema for both the server and the test harness, so tests run the
// same migrations production does.
//
// Migrations run with foreign keys off, as SQLite advises for schema changes:
// a table rebuilt by copy, drop and rename would otherwise trip its children's
// constraints part-way. The pragma is per connection and ignored inside a
// transaction, so every migration runs on one connection taken from the pool,
// and foreign keys are set back as they were before it is returned. Once the
// schema is current, Migrate checks every foreign key and fails if any row
// breaks one, so the app refuses to start on a database holding an orphan.
func Migrate(ctx context.Context, sqlDB *sql.DB) (err error) {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("take a connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	var foreignKeys int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		return fmt.Errorf("read foreign_keys: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("turn foreign keys off: %w", err)
	}
	defer func() {
		if _, restoreErr := conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA foreign_keys = %d`, foreignKeys)); restoreErr != nil {
			// Never hand the pool a connection with foreign keys left off.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			err = errors.Join(err, fmt.Errorf("restore foreign_keys: %w", restoreErr))
		}
	}()

	if err := applyMigrations(ctx, conn); err != nil {
		return err
	}
	return checkForeignKeys(ctx, conn)
}

func applyMigrations(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(entries)

	for _, path := range entries {
		version := path
		var exists int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version,
		).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if exists > 0 {
			continue
		}

		body, err := migrationsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}
	return nil
}

// maxForeignKeyViolationsListed caps how many rows a failed foreign-key check
// names; the rest are counted.
const maxForeignKeyViolationsListed = 20

// checkForeignKeys fails, naming each row that breaks a foreign key (its table,
// rowid and the parent table it points at), if any does.
func checkForeignKeys(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("check foreign keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var listed []string
	total := 0
	for rows.Next() {
		var (
			table, parent string
			rowid         sql.NullInt64
			fkid          int
		)
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return fmt.Errorf("read foreign key check: %w", err)
		}
		total++
		if len(listed) < maxForeignKeyViolationsListed {
			row := "without rowid"
			if rowid.Valid {
				row = fmt.Sprintf("rowid %d", rowid.Int64)
			}
			listed = append(listed, fmt.Sprintf("%s %s -> missing %s", table, row, parent))
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read foreign key check: %w", err)
	}
	if total == 0 {
		return nil
	}
	msg := strings.Join(listed, "; ")
	if rest := total - len(listed); rest > 0 {
		msg += fmt.Sprintf("; and %d more", rest)
	}
	return fmt.Errorf("foreign key check: %d rows break a foreign key: %s", total, msg)
}
