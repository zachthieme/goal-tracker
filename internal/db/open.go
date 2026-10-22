package db

import (
	"database/sql"
	"net/url"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (ADR 0004)
)

// Open opens the app's SQLite database at path for concurrent requests. The
// busy timeout makes a statement that meets another connection's lock wait up
// to 5s for it instead of failing at once with SQLITE_BUSY. Immediate
// transactions take the write lock at BEGIN, so two transactions that read and
// then write queue behind each other; a deferred one would fail on the
// read-to-write lock upgrade without waiting. Foreign keys are enforced, so a
// write that would leave a row pointing at a parent that isn't there fails
// with a foreign-key error rather than orphaning it; SQLite leaves them off
// unless each connection turns them on.
//
// The database runs in WAL mode, so a read sees the last commit and runs
// alongside a write instead of waiting behind it. WAL keeps a database
// uncorrupted through a power loss with synchronous NORMAL, which skips the
// fsync on each commit; the last commits before the loss may roll back.
//
// The pool is capped at 8 connections. WAL lets readers run alongside the one
// writer, but SQLite still takes one write at a time: past a handful of readers
// and the writer, more connections only queue on the write lock and wait out
// its busy timeout, where capped they wait for a free connection in the pool.
//
// Options adjust that for a caller other than the app, such as ForBulkLoad.
func Open(path string, opts ...OpenOption) (*sql.DB, error) {
	sqlDB, err := sql.Open("sqlite", DSN(path, opts...))
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(8)
	return sqlDB, nil
}

// DSN is the connection string Open opens the database at path with, every
// pragma above set on each connection. It is for a caller that opens the
// database itself, such as the test harness, so its pragmas can't drift from
// the app's (ADR 0009); the pool cap is Open's alone. The path is escaped
// because it's a URI: a '#' in it would otherwise end the path and open a
// different file.
func DSN(path string, opts ...OpenOption) string {
	cfg := openConfig{synchronous: "NORMAL"}
	for _, opt := range opts {
		opt(&cfg)
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(" + cfg.synchronous + ")&_txlock=immediate"
}

// An OpenOption adjusts how Open sets up the database's connections.
type OpenOption func(*openConfig)

type openConfig struct {
	synchronous string
}

// ForBulkLoad turns synchronous off, so a commit doesn't wait for its fsync.
// It is for loading a lot of data that can simply be loaded again if the
// machine goes down part-way, as a power loss can then corrupt the database.
// Every other pragma stays as Open sets it, foreign keys included.
func ForBulkLoad() OpenOption {
	return func(cfg *openConfig) { cfg.synchronous = "OFF" }
}
