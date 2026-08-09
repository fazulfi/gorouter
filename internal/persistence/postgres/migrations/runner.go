// Package migrations provides a file-based SQL migration runner
// for gorouter's PostgreSQL schema.
package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var sqlFiles embed.FS

// Direction indicates whether to migrate up or down.
type Direction string

const (
	DirectionUp   Direction = "up"
	DirectionDown Direction = "down"
)

// Migration represents a single migration file with parsed metadata.
type Migration struct {
	Version  string
	Name     string
	Content  string
	Checksum string
}

// Result contains the outcome of a migration run.
type Result struct {
	Applied []string
	Skipped []string
}

// parseMigrations reads embedded SQL files and returns them sorted by version.
// Only files matching the given direction (.up.sql or .down.sql) are included.
func parseMigrations(dir Direction) ([]Migration, error) {
	entries, err := fs.ReadDir(sqlFiles, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	wantSuffix := "." + string(dir) + ".sql"
	var migrations []Migration

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		if !strings.HasSuffix(name, wantSuffix) {
			continue
		}

		version, err := parseVersion(name)
		if err != nil {
			continue
		}

		content, err := fs.ReadFile(sqlFiles, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		checksum := fmt.Sprintf("%x", sha256.Sum256(content))

		migrations = append(migrations, Migration{
			Version:  version,
			Name:     name,
			Content:  string(content),
			Checksum: checksum,
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// ParseMigrations is the exported form of parseMigrations. It reads embedded
// SQL files for the given direction and returns them sorted by version. The
// backup bootstrap gate uses it to compute pending migrations read-only, so the
// gate can run on a DML-only runtime connection that lacks CREATE on the schema.
func ParseMigrations(dir Direction) ([]Migration, error) {
	return parseMigrations(dir)
}

func parseVersion(name string) (string, error) {
	// Must match: VERSION_NAME.direction.sql
	if !strings.HasSuffix(name, ".sql") {
		return "", fmt.Errorf("invalid migration filename: %s", name)
	}
	base := strings.SplitN(name, ".", 2)[0]
	parts := strings.SplitN(base, "_", 2)
	if len(parts) < 2 || parts[0] == "" {
		return "", fmt.Errorf("invalid migration filename: %s", name)
	}
	return parts[0], nil
}

// ensureMigrationsTable creates the gorouter_migrations tracking table if it does not exist.
func ensureMigrationsTable(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
		CREATE TABLE IF NOT EXISTS gorouter_migrations (
			id         SERIAL PRIMARY KEY,
			version    VARCHAR(255) NOT NULL,
			name       VARCHAR(255) NOT NULL,
			checksum   VARCHAR(64) NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	_, err := pool.Exec(ctx, query)
	return err
}

// getAppliedMigrations returns a map of version -> name for already-applied migrations.
func getAppliedMigrations(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	rows, err := pool.Query(ctx, "SELECT version, name FROM gorouter_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]string)
	for rows.Next() {
		var version, name string
		if err := rows.Scan(&version, &name); err != nil {
			return nil, err
		}
		applied[version] = name
	}
	return applied, rows.Err()
}

// applyMigration runs a single migration within a transaction and records it.
func applyMigration(ctx context.Context, pool *pgxpool.Pool, m Migration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, m.Content); err != nil {
		return fmt.Errorf("execute %s: %w", m.Name, err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO gorouter_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
		m.Version, m.Name, m.Checksum,
	); err != nil {
		return fmt.Errorf("record %s: %w", m.Name, err)
	}

	return tx.Commit(ctx)
}

// removeMigrationRecord deletes the tracking record for a down-migrated version.
func removeMigrationRecord(ctx context.Context, pool *pgxpool.Pool, version string) error {
	_, err := pool.Exec(ctx, "DELETE FROM gorouter_migrations WHERE version = $1", version)
	return err
}

// Migrate runs pending migrations in the given direction.
// For DirectionUp: applies migrations that have not yet been applied, in version order.
// For DirectionDown: applies down migrations in reverse version order for applied versions.
// Idempotent: already-applied migrations are skipped.
// Requires a non-nil pool; returns an error if pool is nil.
func Migrate(ctx context.Context, pool *pgxpool.Pool, dir Direction) (*Result, error) {
	if pool == nil {
		return nil, fmt.Errorf("migrations: pool is nil")
	}

	if dir != DirectionUp && dir != DirectionDown {
		return nil, fmt.Errorf("migrations: invalid direction %q", dir)
	}

	migrations, err := parseMigrations(dir)
	if err != nil {
		return nil, fmt.Errorf("parse migrations: %w", err)
	}

	if len(migrations) == 0 {
		return &Result{}, nil
	}

	if err := ensureMigrationsTable(ctx, pool); err != nil {
		return nil, fmt.Errorf("create migrations table: %w", err)
	}

	applied, err := getAppliedMigrations(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}

	var result Result

	if dir == DirectionDown {
		for i := len(migrations) - 1; i >= 0; i-- {
			m := migrations[i]
			if _, wasApplied := applied[m.Version]; !wasApplied {
				result.Skipped = append(result.Skipped, m.Name)
				continue
			}
			if err := applyMigration(ctx, pool, m); err != nil {
				return nil, err
			}
			if err := removeMigrationRecord(ctx, pool, m.Version); err != nil {
				return nil, fmt.Errorf("remove record %s: %w", m.Version, err)
			}
			result.Applied = append(result.Applied, m.Name)
		}
		return &result, nil
	}

	for _, m := range migrations {
		if _, wasApplied := applied[m.Version]; wasApplied {
			result.Skipped = append(result.Skipped, m.Name)
			continue
		}
		if err := applyMigration(ctx, pool, m); err != nil {
			return nil, err
		}
		result.Applied = append(result.Applied, m.Name)
	}

	return &result, nil
}
