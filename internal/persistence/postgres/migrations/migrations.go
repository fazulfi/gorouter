package migrations

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Pool defines the minimal interface required to run migrations.
// Both *pgxpool.Pool and pgxmock implement this interface.
type Pool interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Begin(ctx context.Context) (pgx.Tx, error)
	Close()
}

// Runner executes database migrations against a Pool.
type Runner struct {
	pool Pool
}

// NewRunner creates a new migration Runner backed by the given Pool.
func NewRunner(pool Pool) *Runner {
	return &Runner{pool: pool}
}

func (r *Runner) ensureMigrationsTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS gorouter_migrations (
			id         SERIAL PRIMARY KEY,
			version    VARCHAR(255) NOT NULL,
			name       VARCHAR(255) NOT NULL,
			checksum   VARCHAR(64) NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	_, err := r.pool.Exec(ctx, query)
	return err
}

func (r *Runner) getAppliedMigrations(ctx context.Context) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, "SELECT version, name FROM gorouter_migrations ORDER BY version")
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

func (r *Runner) applyMigration(ctx context.Context, m Migration) error {
	tx, err := r.pool.Begin(ctx)
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

func (r *Runner) removeMigrationRecord(ctx context.Context, version string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM gorouter_migrations WHERE version = $1", version)
	return err
}

// Migrate runs pending migrations in the given direction.
func (r *Runner) Migrate(ctx context.Context, dir Direction) (*Result, error) {
	if r.pool == nil {
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
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("create migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
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
			if err := r.applyMigration(ctx, m); err != nil {
				return nil, err
			}
			if err := r.removeMigrationRecord(ctx, m.Version); err != nil {
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
		if err := r.applyMigration(ctx, m); err != nil {
			return nil, err
		}
		result.Applied = append(result.Applied, m.Name)
	}
	return &result, nil
}

// Up runs pending up migrations.
func (r *Runner) Up(ctx context.Context) (*Result, error) {
	return r.Migrate(ctx, DirectionUp)
}

// Down runs down migrations for all applied versions.
func (r *Runner) Down(ctx context.Context) (*Result, error) {
	return r.Migrate(ctx, DirectionDown)
}

// ListPending returns migrations that have not yet been applied, ordered by version.
func (r *Runner) ListPending(ctx context.Context, dir Direction) ([]Migration, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, nil
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}
	var pending []Migration
	for _, m := range migrations {
		if _, wasApplied := applied[m.Version]; !wasApplied {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil, nil
	}
	return pending, nil
}

// AppliedVersions returns the sorted list of applied migration versions.
func (r *Runner) AppliedVersions(ctx context.Context) ([]string, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}
	if len(applied) == 0 {
		return nil, nil
	}
	versions := make([]string, 0, len(applied))
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return versions, nil
}

// HasMigration checks whether a specific version has been applied.
func (r *Runner) HasMigration(ctx context.Context, version string) (bool, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return false, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return false, fmt.Errorf("get applied migrations: %w", err)
	}
	_, ok := applied[version]
	return ok, nil
}

// Lookup returns the migration for the given version, or an error if not found.
func (r *Runner) Lookup(ctx context.Context, version string, dir Direction) (*Migration, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return nil, err
	}
	for _, m := range migrations {
		if m.Version == version {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("migration version %q not found", version)
}

// VerifyChecksums verifies the stored checksums match embedded migration files.
func (r *Runner) VerifyChecksums(ctx context.Context, dir Direction) error {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return err
	}
	byVersion := make(map[string]Migration, len(migrations))
	for _, m := range migrations {
		byVersion[m.Version] = m
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}
	rows, err := r.pool.Query(ctx, "SELECT version, checksum FROM gorouter_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("query applied checksums: %w", err)
	}
	defer rows.Close()
	var mismatches []string
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return fmt.Errorf("scan checksum: %w", err)
		}
		m, ok := byVersion[version]
		if !ok {
			mismatches = append(mismatches,
				fmt.Sprintf("version %s: applied but no matching migration file found", version))
			continue
		}
		if m.Checksum != checksum {
			mismatches = append(mismatches,
				fmt.Sprintf("version %s: checksum mismatch (file=%s db=%s)", version, m.Checksum, checksum))
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows iteration: %w", err)
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("checksum verification failed:\n%s", strings.Join(mismatches, "\n"))
	}
	return nil
}

// Status describes the current state of a migration version.
type Status string

const (
	StatusPending Status = "pending"
	StatusApplied Status = "applied"
)

// VersionStatus holds the status of a single migration version.
type VersionStatus struct {
	Version string
	Name    string
	Status  Status
}

// Inspect returns the current status of each migration version.
func (r *Runner) Inspect(ctx context.Context, dir Direction) ([]VersionStatus, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, nil
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}
	result := make([]VersionStatus, 0, len(migrations))
	for _, m := range migrations {
		s := VersionStatus{
			Version: m.Version,
			Name:    m.Name,
			Status:  StatusPending,
		}
		if _, wasApplied := applied[m.Version]; wasApplied {
			s.Status = StatusApplied
		}
		result = append(result, s)
	}
	return result, nil
}

// Version returns the latest applied migration version, or empty string if none.
func (r *Runner) Version(ctx context.Context) (string, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return "", fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return "", fmt.Errorf("get applied migrations: %w", err)
	}
	if len(applied) == 0 {
		return "", nil
	}
	versions := make([]string, 0, len(applied))
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return versions[len(versions)-1], nil
}

// PendingCount returns the number of migrations not yet applied.
func (r *Runner) PendingCount(ctx context.Context, dir Direction) (int, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return 0, err
	}
	if len(migrations) == 0 {
		return 0, nil
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return 0, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return 0, fmt.Errorf("get applied migrations: %w", err)
	}
	var count int
	for _, m := range migrations {
		if _, wasApplied := applied[m.Version]; !wasApplied {
			count++
		}
	}
	return count, nil
}

// AppliedCount returns the number of applied migration versions.
func (r *Runner) AppliedCount(ctx context.Context) (int, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return 0, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return 0, fmt.Errorf("get applied migrations: %w", err)
	}
	return len(applied), nil
}

// MustMigrate calls Migrate and panics on error. Useful for init functions.
func (r *Runner) MustMigrate(ctx context.Context, dir Direction) *Result {
	result, err := r.Migrate(ctx, dir)
	if err != nil {
		panic(fmt.Errorf("must migrate: %w", err))
	}
	return result
}

// DryRun lists the migration files that would be applied without executing them.
func (r *Runner) DryRun(ctx context.Context, dir Direction) ([]string, error) {
	if r.pool == nil {
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
		return nil, nil
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}

	var names []string
	if dir == DirectionDown {
		for i := len(migrations) - 1; i >= 0; i-- {
			m := migrations[i]
			if _, wasApplied := applied[m.Version]; !wasApplied {
				continue
			}
			names = append(names, m.Name)
		}
	} else {
		for _, m := range migrations {
			if _, wasApplied := applied[m.Version]; wasApplied {
				continue
			}
			names = append(names, m.Name)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	return names, nil
}

// Validate checks that every migration has a parseable version and valid content.
func (r *Runner) Validate(ctx context.Context, dir Direction) error {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return fmt.Errorf("parse migrations: %w", err)
	}
	if len(migrations) == 0 {
		return fmt.Errorf("no %s migration files found", dir)
	}
	// Verify all versions are parseable
	seen := make(map[string]string)
	for _, m := range migrations {
		if m.Version == "" {
			return fmt.Errorf("migration %s has empty version", m.Name)
		}
		if m.Content == "" {
			return fmt.Errorf("migration %s has empty content", m.Name)
		}
		if m.Checksum == "" {
			return fmt.Errorf("migration %s has empty checksum", m.Name)
		}
		if prev, ok := seen[m.Version]; ok {
			return fmt.Errorf("duplicate version %s: %s and %s", m.Version, prev, m.Name)
		}
		seen[m.Version] = m.Name
	}
	return nil
}

// RemoveApplied removes the tracking record for a specific version.
func (r *Runner) RemoveApplied(ctx context.Context, version string) error {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}
	return r.removeMigrationRecord(ctx, version)
}

// Search returns migrations whose name or version contains the given query string.
func (r *Runner) Search(ctx context.Context, query string, dir Direction) ([]Migration, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, nil
	}
	query = strings.ToLower(query)
	var results []Migration
	for _, m := range migrations {
		if strings.Contains(strings.ToLower(m.Version), query) ||
			strings.Contains(strings.ToLower(m.Name), query) {
			results = append(results, m)
		}
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// IsApplied checks whether a specific migration version has been applied,
// without ensuring the tracking table exists first. For a versioned check
// that creates the table if needed, use HasMigration.
func (r *Runner) IsApplied(ctx context.Context, version string) (bool, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT 1 FROM gorouter_migrations WHERE version = $1", version)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if rows.Next() {
		return true, nil
	}
	return false, rows.Err()
}

// IsUpToDate checks whether all known migrations have been applied.
func (r *Runner) IsUpToDate(ctx context.Context) (bool, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return false, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return false, fmt.Errorf("get applied migrations: %w", err)
	}
	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		return false, fmt.Errorf("parse migrations: %w", err)
	}
	if len(upMigrations) == 0 && len(applied) == 0 {
		return true, nil
	}
	missing := false
	for _, m := range upMigrations {
		if _, ok := applied[m.Version]; !ok {
			missing = true
			break
		}
	}
	return !missing, nil
}

// ChecksumMap returns a map of version to checksum for applied migrations.
func (r *Runner) ChecksumMap(ctx context.Context) (map[string]string, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure migrations table: %w", err)
	}
	rows, err := r.pool.Query(ctx, "SELECT version, checksum FROM gorouter_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("query checksums: %w", err)
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scan checksum: %w", err)
		}
		result[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return result, nil
}

// CountMigrations returns the total number of embedded migration files for the given direction.
func (r *Runner) CountMigrations(ctx context.Context, dir Direction) (int, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return 0, err
	}
	return len(migrations), nil
}

// RangeApplied returns applied migrations whose version falls between from and to (inclusive).
func (r *Runner) RangeApplied(ctx context.Context, from, to string) ([]Migration, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure migrations table: %w", err)
	}
	rows, err := r.pool.Query(ctx,
		"SELECT version, name FROM gorouter_migrations WHERE version >= $1 AND version <= $2 ORDER BY version",
		from, to)
	if err != nil {
		return nil, fmt.Errorf("query range: %w", err)
	}
	defer rows.Close()
	var results []Migration
	for rows.Next() {
		var version, name string
		if err := rows.Scan(&version, &name); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		results = append(results, Migration{
			Version: version,
			Name:    name,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// UpgradeTo applies pending up migrations up to (and including) the given version.
func (r *Runner) UpgradeTo(ctx context.Context, targetVersion string) (*Result, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("migrations: pool is nil")
	}
	migrations, err := parseMigrations(DirectionUp)
	if err != nil {
		return nil, fmt.Errorf("parse migrations: %w", err)
	}
	if len(migrations) == 0 {
		return &Result{}, nil
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("create migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}

	var result Result
	found := false
	for _, m := range migrations {
		if _, wasApplied := applied[m.Version]; wasApplied {
			result.Skipped = append(result.Skipped, m.Name)
			if m.Version == targetVersion {
				found = true
			}
			continue
		}
		if err := r.applyMigration(ctx, m); err != nil {
			return nil, err
		}
		result.Applied = append(result.Applied, m.Name)
		if m.Version == targetVersion {
			found = true
			break
		}
	}
	if !found {
		return &result, fmt.Errorf("target version %s not found in pending migrations", targetVersion)
	}
	return &result, nil
}

// RollbackTo rolls back applied down migrations down to (but not including) the given version.
func (r *Runner) RollbackTo(ctx context.Context, targetVersion string) (*Result, error) {
	if r.pool == nil {
		return nil, fmt.Errorf("migrations: pool is nil")
	}
	downMigrations, err := parseMigrations(DirectionDown)
	if err != nil {
		return nil, fmt.Errorf("parse migrations: %w", err)
	}
	if len(downMigrations) == 0 {
		return &Result{}, nil
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("create migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}

	var result Result
	for i := len(downMigrations) - 1; i >= 0; i-- {
		m := downMigrations[i]
		if _, wasApplied := applied[m.Version]; !wasApplied {
			result.Skipped = append(result.Skipped, m.Name)
			continue
		}
		if m.Version == targetVersion {
			break
		}
		if err := r.applyMigration(ctx, m); err != nil {
			return nil, err
		}
		if err := r.removeMigrationRecord(ctx, m.Version); err != nil {
			return nil, fmt.Errorf("remove record %s: %w", m.Version, err)
		}
		result.Applied = append(result.Applied, m.Name)
	}
	if len(result.Applied) == 0 && len(result.Skipped) == 0 {
		return &result, nil
	}
	return &result, nil
}

// FirstVersion returns the earliest applied migration version, or empty string if none.
func (r *Runner) FirstVersion(ctx context.Context) (string, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return "", fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return "", fmt.Errorf("get applied migrations: %w", err)
	}
	if len(applied) == 0 {
		return "", nil
	}
	versions := make([]string, 0, len(applied))
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return versions[0], nil
}

// HasAnyApplied returns whether at least one migration has been applied.
func (r *Runner) HasAnyApplied(ctx context.Context) (bool, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return false, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return false, fmt.Errorf("get applied migrations: %w", err)
	}
	return len(applied) > 0, nil
}

// VersionName returns the name of the migration for a given version, or empty if not applied.
func (r *Runner) VersionName(ctx context.Context, version string) (string, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return "", fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return "", fmt.Errorf("get applied migrations: %w", err)
	}
	return applied[version], nil
}

// MigrationsBetween returns all embedded migration files whose version is between from and to (inclusive).
func (r *Runner) MigrationsBetween(ctx context.Context, from, to string, dir Direction) ([]Migration, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, nil
	}
	var results []Migration
	for _, m := range migrations {
		if m.Version >= from && m.Version <= to {
			results = append(results, m)
		}
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// UnappliedSince returns all migrations that have not been applied since the given version.
func (r *Runner) UnappliedSince(ctx context.Context, sinceVersion string) ([]Migration, error) {
	migrations, err := parseMigrations(DirectionUp)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, nil
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}
	var unapplied []Migration
	for _, m := range migrations {
		if m.Version <= sinceVersion {
			continue
		}
		if _, wasApplied := applied[m.Version]; !wasApplied {
			unapplied = append(unapplied, m)
		}
	}
	if len(unapplied) == 0 {
		return nil, nil
	}
	return unapplied, nil
}

// TableExists checks whether the gorouter_migrations tracking table exists.
func (r *Runner) TableExists(ctx context.Context) (bool, error) {
	_, err := r.pool.Exec(ctx, "SELECT 1 FROM gorouter_migrations LIMIT 1")
	if err != nil {
		return false, nil
	}
	return true, nil
}

// Ping checks whether the pool connection is alive by executing a trivial query.
func (r *Runner) Ping(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, "SELECT 1")
	return err
}

// AllVersions returns all migration versions available in the embedded files for the given direction.
func (r *Runner) AllVersions(ctx context.Context, dir Direction) ([]string, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, nil
	}
	versions := make([]string, len(migrations))
	for i, m := range migrations {
		versions[i] = m.Version
	}
	return versions, nil
}

// ScriptContent returns the SQL content of a specific embedded migration.
func (r *Runner) ScriptContent(ctx context.Context, version string, dir Direction) (string, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return "", err
	}
	for _, m := range migrations {
		if m.Version == version {
			return m.Content, nil
		}
	}
	return "", fmt.Errorf("version %s not found in embedded %s migrations", version, dir)
}

// NextVersion returns the next migration version after the given one in the specified direction.
func (r *Runner) NextVersion(ctx context.Context, version string, dir Direction) (string, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return "", err
	}
	for _, m := range migrations {
		if m.Version > version {
			return m.Version, nil
		}
	}
	return "", nil
}

// PrevVersion returns the previous migration version before the given one in the specified direction.
func (r *Runner) PrevVersion(ctx context.Context, version string, dir Direction) (string, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return "", err
	}
	prev := ""
	for _, m := range migrations {
		if m.Version >= version {
			break
		}
		prev = m.Version
	}
	return prev, nil
}

// MigrationByIndex returns the migration at the given index (0-based) in version order.
func (r *Runner) MigrationByIndex(ctx context.Context, index int, dir Direction) (*Migration, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(migrations) {
		return nil, fmt.Errorf("migration index %d out of range (0-%d)", index, len(migrations)-1)
	}
	return &migrations[index], nil
}

// Summary returns a concise summary of the migration state.
func (r *Runner) Summary(ctx context.Context) (map[string]int, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}
	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		return nil, err
	}

	total := len(upMigrations)
	appliedCount := len(applied)
	pendingCount := total - appliedCount
	if pendingCount < 0 {
		pendingCount = 0
	}

	return map[string]int{
		"total":   total,
		"applied": appliedCount,
		"pending": pendingCount,
	}, nil
}

// Names returns the names of all embedded migration files for the given direction.
func (r *Runner) Names(ctx context.Context, dir Direction) ([]string, error) {
	migrations, err := parseMigrations(dir)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, nil
	}
	names := make([]string, len(migrations))
	for i, m := range migrations {
		names[i] = m.Name
	}
	return names, nil
}

// ForceApply applies a migration by version even if it is already recorded as applied.
// It first removes the existing record, then runs the up migration.
func (r *Runner) ForceApply(ctx context.Context, version string) error {
	if r.pool == nil {
		return fmt.Errorf("migrations: pool is nil")
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}
	migrations, err := parseMigrations(DirectionUp)
	if err != nil {
		return fmt.Errorf("parse migrations: %w", err)
	}
	var target *Migration
	for _, m := range migrations {
		if m.Version == version {
			target = &m
			break
		}
	}
	if target == nil {
		return fmt.Errorf("migration version %s not found", version)
	}
	_ = r.removeMigrationRecord(ctx, version)
	return r.applyMigration(ctx, *target)
}

// MustEnsureTable panics if the migrations tracking table cannot be created.
func (r *Runner) MustEnsureTable(ctx context.Context) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		panic(fmt.Errorf("must ensure migrations table: %w", err))
	}
}

// SchemaVersion returns both the oldest and newest applied migration versions.
func (r *Runner) SchemaVersion(ctx context.Context) (oldest, newest string, err error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return "", "", fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return "", "", fmt.Errorf("get applied migrations: %w", err)
	}
	if len(applied) == 0 {
		return "", "", nil
	}
	versions := make([]string, 0, len(applied))
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return versions[0], versions[len(versions)-1], nil
}

// AppliedNames returns the names of all applied migrations.
func (r *Runner) AppliedNames(ctx context.Context) ([]string, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("get applied migrations: %w", err)
	}
	if len(applied) == 0 {
		return nil, nil
	}
	versions := make([]string, 0, len(applied))
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(versions)))
	names := make([]string, len(versions))
	for i, v := range versions {
		names[i] = applied[v]
	}
	return names, nil
}

// Exists returns whether the tracking table exists and has at least one row.
func (r *Runner) Exists(ctx context.Context) (bool, error) {
	rows, err := r.pool.Query(ctx, "SELECT 1 FROM gorouter_migrations LIMIT 1")
	if err != nil {
		return false, nil
	}
	defer rows.Close()
	hasRow := rows.Next()
	if err := rows.Err(); err != nil {
		return false, nil
	}
	return hasRow, nil
}

// Ready returns true if the pool is non-nil and the tracking table can be queried.
func (r *Runner) Ready(ctx context.Context) bool {
	if r.pool == nil {
		return false
	}
	_, err := r.pool.Exec(ctx, "SELECT 1 FROM gorouter_migrations LIMIT 1")
	return err == nil
}

// CheckAndApply ensures the tracking table exists and applies a specific migration
// if it has not already been applied. Returns true if applied, false if skipped.
func (r *Runner) CheckAndApply(ctx context.Context, m Migration) (bool, error) {
	if r.pool == nil {
		return false, fmt.Errorf("migrations: pool is nil")
	}
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return false, fmt.Errorf("ensure migrations table: %w", err)
	}
	applied, err := r.getAppliedMigrations(ctx)
	if err != nil {
		return false, fmt.Errorf("get applied migrations: %w", err)
	}
	if _, wasApplied := applied[m.Version]; wasApplied {
		return false, nil
	}
	if err := r.applyMigration(ctx, m); err != nil {
		return false, fmt.Errorf("apply migration: %w", err)
	}
	return true, nil
}

// LatestChecksum returns the checksum for the most recently applied migration.
func (r *Runner) LatestChecksum(ctx context.Context) (string, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return "", fmt.Errorf("ensure migrations table: %w", err)
	}
	rows, err := r.pool.Query(ctx, "SELECT checksum FROM gorouter_migrations ORDER BY version DESC LIMIT 1")
	if err != nil {
		return "", fmt.Errorf("query checksum: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var checksum string
		if err := rows.Scan(&checksum); err != nil {
			return "", fmt.Errorf("scan checksum: %w", err)
		}
		return checksum, rows.Err()
	}
	return "", rows.Err()
}

// FirstChecksum returns the checksum for the earliest applied migration.
func (r *Runner) FirstChecksum(ctx context.Context) (string, error) {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return "", fmt.Errorf("ensure migrations table: %w", err)
	}
	rows, err := r.pool.Query(ctx, "SELECT checksum FROM gorouter_migrations ORDER BY version ASC LIMIT 1")
	if err != nil {
		return "", fmt.Errorf("query checksum: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var checksum string
		if err := rows.Scan(&checksum); err != nil {
			return "", fmt.Errorf("scan checksum: %w", err)
		}
		return checksum, rows.Err()
	}
	return "", rows.Err()
}
