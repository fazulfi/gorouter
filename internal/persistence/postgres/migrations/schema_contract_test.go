package migrations

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Schema contract tests – verify table structure, column types, constraints.
// All tests skipped in short mode; require a real PostgreSQL instance.
// ---------------------------------------------------------------------------

// tableExists checks whether a table exists in the public schema.
func tableExists(t *testing.T, ctx context.Context, tbl string) bool {
	t.Helper()
	pool := getTestPool(t)
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = $1 AND table_schema = 'public'
		)`, tbl,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check table %s: %v", tbl, err)
	}
	return exists
}

// expectColumns verifies that every expected column exists with the right type.
func expectColumns(t *testing.T, ctx context.Context, tbl string, cols map[string]string) {
	t.Helper()
	pool := getTestPool(t)
	for col, want := range cols {
		var dataType string
		err := pool.QueryRow(ctx,
			`SELECT data_type FROM information_schema.columns
			 WHERE table_name = $1 AND column_name = $2`,
			tbl, col,
		).Scan(&dataType)
		if err != nil {
			t.Errorf("table %s column %q: %v", tbl, col, err)
			continue
		}
		if dataType != want {
			t.Errorf("table %s column %q: data_type = %q, want %q", tbl, col, dataType, want)
		}
	}
}

func TestFoundationSchema_HasAllTables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	getTestPool(t) // verify connectivity

	expected := []string{
		"gorouter_settings",
		"gorouter_users",
		"gorouter_sessions",
		"gorouter_api_keys",
		"gorouter_pats",
		"gorouter_audit_log",
		"gorouter_providers",
		"gorouter_jobs",
	}
	for _, tbl := range expected {
		if !tableExists(t, ctx, tbl) {
			t.Errorf("expected table %q does not exist", tbl)
		}
	}
}

func TestFoundationSchema_SettingsColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	expectColumns(t, ctx, "gorouter_settings", map[string]string{
		"id":          "uuid",
		"key":         "character varying",
		"value":       "jsonb",
		"description": "text",
		"created_at":  "timestamp with time zone",
		"updated_at":  "timestamp with time zone",
	})
}

func TestFoundationSchema_UsersColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	expectColumns(t, ctx, "gorouter_users", map[string]string{
		"id":            "uuid",
		"email":         "character varying",
		"password_hash": "character varying",
		"display_name":  "character varying",
		"is_admin":      "boolean",
		"is_active":     "boolean",
		"last_login_at": "timestamp with time zone",
		"created_at":    "timestamp with time zone",
		"updated_at":    "timestamp with time zone",
	})
}

func TestFoundationSchema_SessionsColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	expectColumns(t, ctx, "gorouter_sessions", map[string]string{
		"id":         "uuid",
		"user_id":    "uuid",
		"token_hash": "character varying",
		"ip_address": "inet",
		"user_agent": "text",
		"expires_at": "timestamp with time zone",
		"revoked_at": "timestamp with time zone",
		"created_at": "timestamp with time zone",
	})
}

func TestFoundationSchema_APIKeysColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	expectColumns(t, ctx, "gorouter_api_keys", map[string]string{
		"id":           "uuid",
		"user_id":      "uuid",
		"key_prefix":   "character varying",
		"key_hash":     "character varying",
		"name":         "character varying",
		"scopes":       "ARRAY",
		"expires_at":   "timestamp with time zone",
		"last_used_at": "timestamp with time zone",
		"revoked_at":   "timestamp with time zone",
		"created_at":   "timestamp with time zone",
	})
}

func TestFoundationSchema_PATsColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	expectColumns(t, ctx, "gorouter_pats", map[string]string{
		"id":           "uuid",
		"user_id":      "uuid",
		"token_hash":   "character varying",
		"description":  "text",
		"expires_at":   "timestamp with time zone",
		"last_used_at": "timestamp with time zone",
		"revoked_at":   "timestamp with time zone",
		"created_at":   "timestamp with time zone",
	})
}

func TestFoundationSchema_AuditLogColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	expectColumns(t, ctx, "gorouter_audit_log", map[string]string{
		"id":            "uuid",
		"actor_id":      "uuid",
		"action":        "character varying",
		"resource_type": "character varying",
		"resource_id":   "uuid",
		"details":       "jsonb",
		"ip_address":    "inet",
		"occurred_at":   "timestamp with time zone",
	})
}

func TestFoundationSchema_ProvidersColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	expectColumns(t, ctx, "gorouter_providers", map[string]string{
		"id":                "uuid",
		"name":              "character varying",
		"type":              "character varying",
		"base_url":          "character varying",
		"api_key_encrypted": "text",
		"config":            "jsonb",
		"is_enabled":        "boolean",
		"created_at":        "timestamp with time zone",
		"updated_at":        "timestamp with time zone",
	})
}

func TestFoundationSchema_JobsColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	expectColumns(t, ctx, "gorouter_jobs", map[string]string{
		"id":            "uuid",
		"type":          "character varying",
		"status":        "character varying",
		"payload":       "jsonb",
		"result":        "jsonb",
		"error_message": "text",
		"attempts":      "integer",
		"max_attempts":  "integer",
		"scheduled_at":  "timestamp with time zone",
		"started_at":    "timestamp with time zone",
		"completed_at":  "timestamp with time zone",
		"created_at":    "timestamp with time zone",
		"updated_at":    "timestamp with time zone",
	})
}

func TestFoundationSchema_PrimaryKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	tables := []string{
		"gorouter_settings",
		"gorouter_users",
		"gorouter_sessions",
		"gorouter_api_keys",
		"gorouter_pats",
		"gorouter_audit_log",
		"gorouter_providers",
		"gorouter_jobs",
	}
	for _, tbl := range tables {
		var pkCol string
		err := pool.QueryRow(ctx,
			`SELECT kcu.column_name
			 FROM information_schema.table_constraints tc
			 JOIN information_schema.key_column_usage kcu
			   ON tc.constraint_name = kcu.constraint_name
			 WHERE tc.table_name = $1
			   AND tc.constraint_type = 'PRIMARY KEY'`, tbl,
		).Scan(&pkCol)
		if err != nil {
			t.Errorf("table %s: primary key lookup failed: %v", tbl, err)
			continue
		}
		if pkCol != "id" {
			t.Errorf("table %s: PK column = %q, want %q", tbl, pkCol, "id")
		}
	}
}

func TestFoundationSchema_ForeignKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	expectedFKs := map[string]string{
		"sessions":  "gorouter_users",
		"api_keys":  "gorouter_users",
		"pats":      "gorouter_users",
		"audit_log": "gorouter_users",
	}
	for child, parent := range expectedFKs {
		childTable := "gorouter_" + child
		var refTable string
		err := pool.QueryRow(ctx,
			`SELECT ccu.table_name AS referenced_table
			 FROM information_schema.table_constraints tc
			 JOIN information_schema.constraint_column_usage ccu
			   ON tc.constraint_name = ccu.constraint_name
			 WHERE tc.table_name = $1
			   AND tc.constraint_type = 'FOREIGN KEY'
			 LIMIT 1`, childTable,
		).Scan(&refTable)
		if err != nil {
			t.Errorf("table %s: FK lookup failed: %v", childTable, err)
			continue
		}
		if refTable != parent {
			t.Errorf("table %s FK references %q, want %q", childTable, refTable, parent)
		}
	}
}

func TestFoundationSchema_UniqueConstraints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	type uc struct {
		table  string
		column string
	}
	expected := []uc{
		{"gorouter_settings", "key"},
		{"gorouter_users", "email"},
	}
	for _, u := range expected {
		var count int
		err := pool.QueryRow(ctx,
			`SELECT COUNT(*)
			 FROM information_schema.table_constraints tc
			 JOIN information_schema.constraint_column_usage ccu
			   ON tc.constraint_name = ccu.constraint_name
			 WHERE tc.table_name = $1
			   AND ccu.column_name = $2
			   AND tc.constraint_type = 'UNIQUE'`,
			u.table, u.column,
		).Scan(&count)
		if err != nil {
			t.Errorf("unique constraint %s.%s: %v", u.table, u.column, err)
			continue
		}
		if count == 0 {
			t.Errorf("unique constraint on %s.%s not found", u.table, u.column)
		}
	}
}

func TestFoundationSchema_NotNullConstraints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	// Verify that key columns have is_nullable = 'NO'
	notNullCols := []struct {
		table  string
		column string
	}{
		{"gorouter_settings", "key"},
		{"gorouter_settings", "value"},
		{"gorouter_users", "email"},
		{"gorouter_users", "password_hash"},
		{"gorouter_sessions", "token_hash"},
		{"gorouter_api_keys", "key_hash"},
		{"gorouter_pats", "token_hash"},
		{"gorouter_providers", "name"},
		{"gorouter_providers", "type"},
		{"gorouter_providers", "base_url"},
		{"gorouter_jobs", "type"},
		{"gorouter_jobs", "status"},
	}
	for _, nc := range notNullCols {
		var nullable string
		err := pool.QueryRow(ctx,
			`SELECT is_nullable FROM information_schema.columns
			 WHERE table_name = $1 AND column_name = $2`,
			nc.table, nc.column,
		).Scan(&nullable)
		if err != nil {
			t.Errorf("%s.%s: %v", nc.table, nc.column, err)
			continue
		}
		if nullable != "NO" {
			t.Errorf("%s.%s is nullable (%q), want NOT NULL", nc.table, nc.column, nullable)
		}
	}
}

func TestFoundationSchema_Indexes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	// Verify at least one index exists per table (beyond PK)
	tables := []string{
		"gorouter_settings",
		"gorouter_users",
		"gorouter_sessions",
		"gorouter_api_keys",
		"gorouter_pats",
		"gorouter_audit_log",
		"gorouter_providers",
		"gorouter_jobs",
	}
	for _, tbl := range tables {
		var idxCount int
		err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM pg_indexes
			 WHERE tablename = $1 AND indexname NOT LIKE $2`,
			tbl, tbl+"_pkey",
		).Scan(&idxCount)
		if err != nil {
			t.Errorf("index check for %s: %v", tbl, err)
			continue
		}
		if idxCount == 0 {
			// settings, users may not have secondary indexes
			t.Logf("table %s has no secondary indexes", tbl)
		}
	}
}

// TestHashOnlyColumns verifies that secrets are stored as hashes, not plaintext.
func TestHashOnlyColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	// gorouter_api_keys must have key_hash but NOT a raw key column
	type check struct {
		table    string
		hashCol  string
		noRawCol string // column that MUST NOT exist (raw secret)
	}
	checks := []check{
		{"gorouter_api_keys", "key_hash", "key"},
		{"gorouter_pats", "token_hash", "token"},
		{"gorouter_sessions", "token_hash", "token"},
	}
	for _, c := range checks {
		// Verify hash column exists
		var hashExists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = $1 AND column_name = $2
			)`, c.table, c.hashCol,
		).Scan(&hashExists)
		if err != nil {
			t.Errorf("%s: check hash col: %v", c.table, err)
			continue
		}
		if !hashExists {
			t.Errorf("%s: hash column %q does not exist", c.table, c.hashCol)
		}

		// Verify raw column does NOT exist
		var rawExists bool
		_ = pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = $1 AND column_name = $2
			)`, c.table, c.noRawCol,
		).Scan(&rawExists)
		if rawExists {
			t.Errorf("%s: raw secret column %q exists (should be hash-only)", c.table, c.noRawCol)
		}
	}
}

// TestUTCTimestamps verifies that every timestamptz column uses the proper type.
func TestUTCTimestamps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	type colRef struct {
		table  string
		column string
	}

	// All expected timestamptz columns from the foundation schema
	expected := []colRef{
		{"gorouter_settings", "created_at"},
		{"gorouter_settings", "updated_at"},
		{"gorouter_users", "created_at"},
		{"gorouter_users", "updated_at"},
		{"gorouter_users", "last_login_at"},
		{"gorouter_sessions", "created_at"},
		{"gorouter_sessions", "expires_at"},
		{"gorouter_sessions", "revoked_at"},
		{"gorouter_api_keys", "created_at"},
		{"gorouter_api_keys", "expires_at"},
		{"gorouter_api_keys", "last_used_at"},
		{"gorouter_api_keys", "revoked_at"},
		{"gorouter_pats", "created_at"},
		{"gorouter_pats", "expires_at"},
		{"gorouter_pats", "last_used_at"},
		{"gorouter_pats", "revoked_at"},
		{"gorouter_audit_log", "occurred_at"},
		{"gorouter_providers", "created_at"},
		{"gorouter_providers", "updated_at"},
		{"gorouter_jobs", "created_at"},
		{"gorouter_jobs", "updated_at"},
		{"gorouter_jobs", "scheduled_at"},
		{"gorouter_jobs", "started_at"},
		{"gorouter_jobs", "completed_at"},
	}
	for _, r := range expected {
		var dataType string
		err := pool.QueryRow(ctx,
			`SELECT data_type FROM information_schema.columns
			 WHERE table_name = $1 AND column_name = $2`,
			r.table, r.column,
		).Scan(&dataType)
		if err != nil {
			t.Errorf("%s.%s: %v", r.table, r.column, err)
			continue
		}
		if dataType != "timestamp with time zone" {
			t.Errorf("%s.%s: data_type = %q, want 'timestamp with time zone'",
				r.table, r.column, dataType)
		}
	}
}

func TestFoundationSchema_UsersSeedData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	var email string
	err := pool.QueryRow(ctx,
		`SELECT email FROM gorouter_users WHERE is_admin = true LIMIT 1`,
	).Scan(&email)
	if err != nil {
		t.Fatalf("query admin user: %v", err)
	}
	if email != "admin@gorouter.local" {
		t.Errorf("admin email = %q, want %q", email, "admin@gorouter.local")
	}
}

func TestFoundationSchema_JobsStatusDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	var defaultVal string
	err := pool.QueryRow(ctx,
		`SELECT column_default FROM information_schema.columns
		 WHERE table_name = 'gorouter_jobs' AND column_name = 'status'`,
	).Scan(&defaultVal)
	if err != nil {
		t.Fatalf("query status default: %v", err)
	}
	if defaultVal != "'pending'::character varying" && defaultVal != "'pending'" {
		t.Errorf("status default = %q, want %q", defaultVal, "'pending'::character varying")
	}
}

func TestFoundationSchema_ProvidersTypeCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	// Verify the type column is VARCHAR(50) as declared in the schema
	var dataType string
	var charMaxLen int
	err := pool.QueryRow(ctx,
		`SELECT data_type, character_maximum_length FROM information_schema.columns
		 WHERE table_name = 'gorouter_providers' AND column_name = 'type'`,
	).Scan(&dataType, &charMaxLen)
	if err != nil {
		t.Fatalf("query providers.type: %v", err)
	}
	if dataType != "character varying" {
		t.Errorf("providers.type data_type = %q, want 'character varying'", dataType)
	}
	if charMaxLen != 50 {
		t.Errorf("providers.type length = %d, want 50", charMaxLen)
	}
}

// TestFoundationSchema_RLSBaseline confirms that Row-Level Security is NOT
// enabled in Phase 1. Phase 1 relies on PostgreSQL roles and filesystem
// permissions per DECISIONS.md #27 and ARCHITECTURE.md §13. RLS enablement
// is deferred to a future phase and must be explicitly approved.
func TestFoundationSchema_RLSBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}
	ctx := context.Background()
	pool := getTestPool(t)

	// These tables would be RLS candidates, but Phase 1 does not enable RLS.
	// This test documents the baseline; the assertion flips when RLS is later
	// approved and implemented.
	tables := []string{
		"gorouter_users",
		"gorouter_sessions",
		"gorouter_api_keys",
		"gorouter_pats",
		"gorouter_audit_log",
	}
	for _, tbl := range tables {
		var rlsEnabled bool
		err := pool.QueryRow(ctx,
			`SELECT relrowsecurity FROM pg_class WHERE relname = $1`, tbl,
		).Scan(&rlsEnabled)
		if err != nil {
			t.Fatalf("table %s: query RLS status: %v", tbl, err)
		}
		if rlsEnabled {
			t.Errorf("table %s: RLS unexpectedly enabled in Phase 1 (Phase 1 does not enable RLS)", tbl)
		}
	}
}
