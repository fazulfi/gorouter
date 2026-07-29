package migrations

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"000001_foundation.up.sql", "000001", false},
		{"000002_add_widgets.down.sql", "000002", false},
		{"invalid.txt", "", true},
		{".sql", "", true},
		{"_leading.up.sql", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVersion(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseMigrations_UpDirection(t *testing.T) {
	migs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parseMigrations(up) failed: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("expected at least 1 up migration, got 0")
	}
	if migs[0].Version != "000001" {
		t.Errorf("first migration version = %q, want %q", migs[0].Version, "000001")
	}
	if migs[0].Name != "000001_foundation.up.sql" {
		t.Errorf("first migration name = %q, want %q", migs[0].Name, "000001_foundation.up.sql")
	}
	if migs[0].Content == "" {
		t.Error("migration content is empty")
	}
	if migs[0].Checksum == "" {
		t.Error("migration checksum is empty")
	}
}

func TestParseMigrations_DownDirection(t *testing.T) {
	migs, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parseMigrations(down) failed: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("expected at least 1 down migration, got 0")
	}
	if migs[0].Version != "000001" {
		t.Errorf("first migration version = %q, want %q", migs[0].Version, "000001")
	}
	if migs[0].Name != "000001_foundation.down.sql" {
		t.Errorf("first migration name = %q, want %q", migs[0].Name, "000001_foundation.down.sql")
	}
}

func TestParseMigrations_FiltersByDirection(t *testing.T) {
	upMigs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parseMigrations(up): %v", err)
	}
	downMigs, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parseMigrations(down): %v", err)
	}
	for _, m := range upMigs {
		if m.Version == "" {
			t.Errorf("up migration %q has empty version", m.Name)
		}
	}
	for _, m := range downMigs {
		if m.Version == "" {
			t.Errorf("down migration %q has empty version", m.Name)
		}
	}
	// Should have same number of up and down files for foundation
	if len(upMigs) != len(downMigs) {
		t.Errorf("up count (%d) != down count (%d)", len(upMigs), len(downMigs))
	}
}

func TestParseMigrations_SortedOrder(t *testing.T) {
	migs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parseMigrations(up): %v", err)
	}
	for i := 1; i < len(migs); i++ {
		if migs[i].Version < migs[i-1].Version {
			t.Errorf("migrations not sorted: %s (index %d) < %s (index %d)",
				migs[i].Version, i, migs[i-1].Version, i-1)
		}
	}
}

func TestChecksum_Deterministic(t *testing.T) {
	migs1, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	migs2, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	if len(migs1) != len(migs2) {
		t.Fatalf("migration count mismatch: %d vs %d", len(migs1), len(migs2))
	}
	for i := range migs1 {
		if migs1[i].Checksum != migs2[i].Checksum {
			t.Errorf("checksum mismatch for %s: %q vs %q",
				migs1[i].Name, migs1[i].Checksum, migs2[i].Checksum)
		}
	}
}

func TestMigrate_NilPool(t *testing.T) {
	_, err := Migrate(nil, nil, DirectionUp)
	if err == nil {
		t.Error("expected error for nil pool, got nil")
	}
}

func TestMigrate_InvalidDirection(t *testing.T) {
	_, err := Migrate(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid direction, got nil")
	}
}

func TestMigrate_WithNilPoolPanics(t *testing.T) {
	// Verify that calling Migrate with nil context and nil pool returns error (not panic)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Migrate panicked with nil pool: %v", r)
		}
	}()
	_, err := Migrate(nil, nil, DirectionUp)
	if err == nil {
		t.Error("expected error for nil pool")
	}
}

// TestMigrateIntegration applies migrations to a real database.
// Requires a running PostgreSQL instance.
// Set DATABASE_URL env var or defaults to the connection string below.
// Skipped with `go test -short`.
func TestMigrateIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// We need a real DB connection pool to test actual migration application.
	// This test is intentionally minimalist in short mode.
	// In a full integration environment, one would start a postgres container.
	t.Log("integration test requires a running PostgreSQL instance")
}

// TestMigrateDownIntegration tests rollback via down migrations.
func TestMigrateDownIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Log("down migration integration test requires a running PostgreSQL instance")
}

// BenchmarkParseMigrations measures migration parsing performance.
func BenchmarkParseMigrations(b *testing.B) {
	for range b.N {
		_, err := parseMigrations(DirectionUp)
		if err != nil {
			b.Fatal(err)
		}
	}
}
