package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Migrate applies any not-yet-recorded migrations from the embedded migrations/ directory.
// It is idempotent; already-applied migrations are skipped.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT    PRIMARY KEY,
			applied_at INTEGER NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("sqlite: create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("sqlite: read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version := e.Name()

		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version,
		).Scan(&count); err != nil {
			return fmt.Errorf("sqlite: check migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		body, err := migrationsFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("sqlite: read migration %s: %w", version, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("sqlite: begin tx for %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("sqlite: exec migration %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, strftime('%s','now'))`,
			version,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("sqlite: record migration %s: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("sqlite: commit migration %s: %w", version, err)
		}
	}

	return nil
}
