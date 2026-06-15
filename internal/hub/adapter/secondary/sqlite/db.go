package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// connDSNParams are applied to *every* connection the driver opens. They MUST
// live in the DSN rather than a one-off `PRAGMA` Exec: *sql.DB is a connection
// pool and an Exec lands on an arbitrary pooled connection, so a per-connection
// pragma like busy_timeout would stick to only that one connection. Any other
// connection would keep busy_timeout=0 and fail a contended write *immediately*
// with SQLITE_BUSY instead of waiting — which is exactly how a "SessionExited"
// write racing a heartbeat read silently lost the exit status.
//
//   - busy_timeout=5000 — wait up to 5s for a lock instead of erroring at once.
//   - journal_mode=WAL  — readers don't block the single writer.
//   - foreign_keys=ON   — enforce FK constraints (off by default in SQLite).
//   - _txlock=immediate — write txns take the write lock up front, so two
//     deferred txns can't both read then deadlock on upgrade (busy_timeout
//     cannot resolve that case and returns SQLITE_BUSY immediately).
var connDSNParams = []string{
	"_pragma=busy_timeout(5000)",
	"_pragma=journal_mode(WAL)",
	"_pragma=foreign_keys(ON)",
	"_txlock=immediate",
}

// Open opens (or creates) the SQLite database at path with pragmas applied to
// every pooled connection via the DSN.
func Open(path string) (*sql.DB, error) {
	dsn := path + "?" + strings.Join(connDSNParams, "&")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
	}

	// Verify connectivity (and that at least one connection applies the pragmas)
	// before handing the pool back to callers.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: ping %q: %w", path, err)
	}

	return db, nil
}
