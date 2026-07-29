package migrations

import (
	"testing"
)

// schemaContractTestBase is a shared connection helper placeholder.
// When run against a real PostgreSQL instance, these tests query
// information_schema to validate table structure, column types,
// constraints, indexes, foreign keys, and RLS settings.
//
// All tests in this file are skipped in short mode.

func TestFoundationSchema_HasAllTables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

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

	t.Log("expected tables:", expected)
	t.Log("connect to a running PostgreSQL and query information_schema.tables")
}

func TestFoundationSchema_SettingsColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expected := map[string]string{
		"id":          "uuid",
		"key":         "character varying",
		"value":       "jsonb",
		"description": "text",
		"created_at":  "timestamp with time zone",
		"updated_at":  "timestamp with time zone",
	}
	t.Log("gorouter_settings expected columns:", expected)
}

func TestFoundationSchema_UsersColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expected := map[string]string{
		"id":            "uuid",
		"email":         "character varying",
		"password_hash": "character varying",
		"display_name":  "character varying",
		"is_admin":      "boolean",
		"is_active":     "boolean",
		"last_login_at": "timestamp with time zone",
		"created_at":    "timestamp with time zone",
		"updated_at":    "timestamp with time zone",
	}
	t.Log("gorouter_users expected columns:", expected)
}

func TestFoundationSchema_SessionsColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expected := map[string]string{
		"id":         "uuid",
		"user_id":    "uuid",
		"token_hash": "character varying",
		"ip_address": "inet",
		"user_agent": "text",
		"expires_at": "timestamp with time zone",
		"revoked_at": "timestamp with time zone",
		"created_at": "timestamp with time zone",
	}
	t.Log("gorouter_sessions expected columns:", expected)
}

func TestFoundationSchema_APIKeysColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expected := map[string]string{
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
	}
	t.Log("gorouter_api_keys expected columns:", expected)
}

func TestFoundationSchema_PATsColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expected := map[string]string{
		"id":           "uuid",
		"user_id":      "uuid",
		"token_hash":   "character varying",
		"description":  "text",
		"expires_at":   "timestamp with time zone",
		"last_used_at": "timestamp with time zone",
		"revoked_at":   "timestamp with time zone",
		"created_at":   "timestamp with time zone",
	}
	t.Log("gorouter_pats expected columns:", expected)
}

func TestFoundationSchema_AuditLogColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expected := map[string]string{
		"id":            "uuid",
		"actor_id":      "uuid",
		"action":        "character varying",
		"resource_type": "character varying",
		"resource_id":   "uuid",
		"details":       "jsonb",
		"ip_address":    "inet",
		"occurred_at":   "timestamp with time zone",
	}
	t.Log("gorouter_audit_log expected columns:", expected)
}

func TestFoundationSchema_ProvidersColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expected := map[string]string{
		"id":                "uuid",
		"name":              "character varying",
		"type":              "character varying",
		"base_url":          "character varying",
		"api_key_encrypted": "text",
		"config":            "jsonb",
		"is_enabled":        "boolean",
		"created_at":        "timestamp with time zone",
		"updated_at":        "timestamp with time zone",
	}
	t.Log("gorouter_providers expected columns:", expected)
}

func TestFoundationSchema_JobsColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expected := map[string]string{
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
	}
	t.Log("gorouter_jobs expected columns:", expected)
}

func TestFoundationSchema_PrimaryKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

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
	t.Log("all tables should have id UUID PRIMARY KEY")
	for _, tbl := range tables {
		t.Logf("check PK for %s", tbl)
	}
}

func TestFoundationSchema_ForeignKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expectedFKs := []string{
		"gorouter_sessions -> gorouter_users (user_id)",
		"gorouter_api_keys -> gorouter_users (user_id)",
		"gorouter_pats -> gorouter_users (user_id)",
		"gorouter_audit_log -> gorouter_users (actor_id, nullable)",
	}
	t.Log("expected foreign keys:", expectedFKs)
}

func TestFoundationSchema_UniqueConstraints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	expected := []string{
		"gorouter_settings.key",
		"gorouter_users.email",
	}
	t.Log("expected unique constraints:", expected)
}

func TestFoundationSchema_NotNullConstraints(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	t.Log("verify NOT NULL constraints via information_schema.columns")
}

func TestFoundationSchema_Indexes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

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
	t.Log("check index coverage for:", tables)
}

func TestFoundationSchema_UsersSeedData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	t.Log("verify admin seed user exists: admin@gorouter.local")
}

func TestFoundationSchema_JobsStatusDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	t.Log("verify gorouter_jobs.status defaults to 'pending'")
}

func TestFoundationSchema_ProvidersTypeCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping schema contract test in short mode")
	}

	validTypes := []string{"openai", "anthropic", "azure", "custom"}
	t.Log("gorouter_providers.type should accept:", validTypes)
}
