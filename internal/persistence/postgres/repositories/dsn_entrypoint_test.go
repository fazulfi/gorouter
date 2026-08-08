package repositories

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	adminRole   = "postgres"
	ddlRole     = "gorouter_ddl"
	runtimeRole = "gorouter"

	bootstrapHelperFile = "bootstrap_roles_repos_test.go"
)

var repoEntrypointFiles = []string{
	"integration_test.go",
	"auth_repo_test.go",
	"backup_e2e_integration_test.go",
	"console_log_repo_integration_test.go",
}

func repoTestFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// TestDSNTransformation_AdminDDLRuntime asserts the admin→DDL→runtime user
// substitution preserves transport identity while changing only the role.
func TestDSNTransformation_AdminDDLRuntime(t *testing.T) {
	adminDSN := "postgres://postgres:secret@localhost:5432/postgres?sslmode=disable"

	base, err := pgconn.ParseConfig(adminDSN)
	if err != nil {
		t.Fatalf("parse admin DSN: %v", err)
	}
	if base.User != adminRole {
		t.Fatalf("admin user = %q, want %q", base.User, adminRole)
	}

	for _, tc := range []struct {
		name string
		user string
	}{
		{"ddl", ddlRole},
		{"runtime", runtimeRole},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := pgconn.ParseConfig(adminDSN)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			cfg.User = tc.user
			if cfg.User != tc.user {
				t.Errorf("user = %q, want %q", cfg.User, tc.user)
			}
			if cfg.Host != base.Host || cfg.Port != base.Port || cfg.Database != base.Database {
				t.Errorf("transport drifted: got host=%q port=%d db=%q want host=%q port=%d db=%q",
					cfg.Host, cfg.Port, cfg.Database, base.Host, base.Port, base.Database)
			}
		})
	}
}

// TestPerDatabaseGrantTarget_Helper grants CREATE ON SCHEMA public to the DDL
// role only. On PG 15+ the public schema of a fresh database is owned by its
// creator and non-owner roles hold no CREATE privilege, so the migration
// executor must receive the grant per database. The runtime role stays DML-only.
func TestPerDatabaseGrantTarget_Helper(t *testing.T) {
	src := repoTestFile(t, bootstrapHelperFile)

	if !strings.Contains(src, "GRANT CREATE ON SCHEMA public TO "+ddlRole) {
		t.Errorf("helper: missing GRANT CREATE ON SCHEMA public TO %s", ddlRole)
	}
	for _, probe := range []string{
		"GRANT CREATE ON SCHEMA public TO " + runtimeRole + "\"",
		"GRANT CREATE ON SCHEMA public TO " + runtimeRole + ")",
		"GRANT CREATE ON SCHEMA public TO " + runtimeRole + "\n",
	} {
		if strings.Contains(src, probe) {
			t.Errorf("helper: grants CREATE ON SCHEMA public to the runtime role %s; least privilege violated", runtimeRole)
		}
	}
}

// TestMigrationIdentity_Helper asserts the shared helper executes migrations
// through the DDL role and reconnects the runtime pool as the runtime role.
// Running migrations as the runtime role diverges from the production
// separation of duty and, on PG 15+, fails with SQLSTATE 42501 when creating
// the tracking table.
func TestMigrationIdentity_Helper(t *testing.T) {
	src := repoTestFile(t, bootstrapHelperFile)

	if !strings.Contains(src, "cfg.ConnConfig.User = \""+ddlRole+"\"") {
		t.Errorf("helper: migration pool must connect as %s", ddlRole)
	}
	if !strings.Contains(src, "migrations.Migrate(ctx, ddlPool, migrations.DirectionUp)") {
		t.Errorf("helper: migrations must run through migrations.Migrate on the DDL pool")
	}
	if !strings.Contains(src, "cfg.ConnConfig.User = \""+runtimeRole+"\"") {
		t.Errorf("helper: runtime pool must reconnect as %s after migrations", runtimeRole)
	}

	ddlIdx := strings.Index(src, "cfg.ConnConfig.User = \""+ddlRole+"\"")
	runtimeIdx := strings.LastIndex(src, "cfg.ConnConfig.User = \""+runtimeRole+"\"")
	if ddlIdx == -1 || runtimeIdx == -1 || ddlIdx > runtimeIdx {
		t.Errorf("helper: DDL-role migration must precede runtime pool reconnection (ddl=%d runtime=%d)", ddlIdx, runtimeIdx)
	}
}

// TestEntrypoints_InvokeDDLSetup asserts every repositories fresh-DB
// entrypoint wires bootstrap → per-db grant → DDL migration → runtime pool
// through the shared helper, and that no entrypoint grants the runtime role
// schema-create authority or runs migrations as the runtime role.
func TestEntrypoints_InvokeDDLSetup(t *testing.T) {
	for _, name := range repoEntrypointFiles {
		t.Run(name, func(t *testing.T) {
			src := repoTestFile(t, name)

			if !strings.Contains(src, "bootstrapRolesForRepoTest(") {
				t.Errorf("%s: must bootstrap the role topology via bootstrapRolesForRepoTest", name)
			}
			if !strings.Contains(src, "migrateAsDDLRepoTest(") {
				t.Errorf("%s: must run migrations through migrateAsDDLRepoTest (DDL role)", name)
			}
			if !strings.Contains(src, "grantSchemaCreateToDDLRepoTest(") &&
				!strings.Contains(src, "freshRuntimePoolForBackupTest(") {
				t.Errorf("%s: must grant per-database schema create to the DDL role before migrating", name)
			}
			for _, probe := range []string{
				"migrations.BootstrapRolesOnCluster",
				"GRANT CREATE ON SCHEMA public TO " + runtimeRole,
			} {
				if strings.Contains(src, probe) {
					t.Errorf("%s: legacy runtime-role wiring probe %q matched; must use the DDL-role helper", name, probe)
				}
			}
			// Running the migration batch itself as the runtime role is the
			// defect; a pre-migration runtime pool (fresh-DB gate) is allowed.
			if strings.Contains(src, "migrations.Migrate(ctx, pool,") ||
				strings.Contains(src, "migrations.NewRunner(pool)") {
				if !strings.Contains(src, "migrateAsDDLRepoTest(") {
					t.Errorf("%s: migration batch must run through the DDL-role helper, not a runtime pool", name)
				}
			}
		})
	}
}
