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
	cfg := openConfig{synchronous: "NORMAL"}
	for _, opt := range opts {
		opt(&cfg)
	}
	sqlDB, err := sql.Open("sqlite", "file:"+(&url.URL{Path: path}).EscapedPath()+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous("+cfg.synchronous+")&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(8)
	return sqlDB, nil
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
