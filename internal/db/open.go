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
func Open(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+(&url.URL{Path: path}).EscapedPath()+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate")
}
