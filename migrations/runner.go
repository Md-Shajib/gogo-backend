package migrations

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Run executes all pending migrations in the given direction ("up" or "down").
// Migration versions are tracked in the schema_migrations table.
func Run(db *sql.DB, direction string) error {
	if direction != "up" && direction != "down" {
		return fmt.Errorf("invalid direction %q — use 'up' or 'down'", direction)
	}

	if err := ensureMigrationsTable(db); err != nil {
		return err
	}

	files, err := loadFiles(direction)
	if err != nil {
		return err
	}

	if direction == "down" {
		sort.Sort(sort.Reverse(sort.StringSlice(files)))
	}

	for _, file := range files {
		version := extractVersion(file)

		applied, err := isApplied(db, version)
		if err != nil {
			return err
		}

		if direction == "up" && applied {
			continue
		}
		if direction == "down" && !applied {
			continue
		}

		if err := execFile(db, file); err != nil {
			return fmt.Errorf("migration %s failed: %w", version, err)
		}

		if direction == "up" {
			if err := markApplied(db, version); err != nil {
				return err
			}
			slog.Info("migration applied", "version", version)
		} else {
			if err := markReverted(db, version); err != nil {
				return err
			}
			slog.Info("migration reverted", "version", version)
		}
	}

	return nil
}

// ensureMigrationsTable creates the schema_migrations table if it does not exist.
func ensureMigrationsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}
	return nil
}

// loadFiles returns all migration file paths for the given direction, sorted alphabetically.
func loadFiles(direction string) ([]string, error) {
	pattern := fmt.Sprintf("migrations/sql/*.%s.sql", direction)

	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration files: %w", err)
	}

	sort.Strings(files)
	return files, nil
}

// extractVersion strips the directory and extension from a migration filename.
// e.g. "migrations/sql/000001_users.up.sql" → "000001_users"
func extractVersion(file string) string {
	base := filepath.Base(file)
	// remove .up.sql or .down.sql
	parts := strings.SplitN(base, ".", 2)
	return parts[0]
}

// isApplied checks whether a migration version has already been applied.
func isApplied(db *sql.DB, version string) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check migration status: %w", err)
	}
	return count > 0, nil
}

// execFile reads a .sql file and executes it as a single statement.
func execFile(db *sql.DB, file string) error {
	content, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", file, err)
	}

	if _, err := db.Exec(string(content)); err != nil {
		return fmt.Errorf("failed to execute %s: %w", file, err)
	}

	return nil
}

// markApplied inserts a version into schema_migrations.
func markApplied(db *sql.DB, version string) error {
	_, err := db.Exec(
		`INSERT INTO schema_migrations (version) VALUES ($1)`, version,
	)
	if err != nil {
		return fmt.Errorf("failed to record migration %s: %w", version, err)
	}
	return nil
}

// markReverted removes a version from schema_migrations.
func markReverted(db *sql.DB, version string) error {
	_, err := db.Exec(
		`DELETE FROM schema_migrations WHERE version = $1`, version,
	)
	if err != nil {
		return fmt.Errorf("failed to remove migration record %s: %w", version, err)
	}
	return nil
}
