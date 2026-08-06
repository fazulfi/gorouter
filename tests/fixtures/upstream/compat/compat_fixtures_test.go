// Package compat holds the machine-checkable compatibility contract fixtures
// for the Administration API v1 (internal/transport/httpserver/compatibility/
// ROUTES.md).
//
// The fixtures (management_fixtures.yaml, excluded_fixtures.yaml,
// engine_fixtures.yaml) freeze the historical external routes of the pinned
// upstream baseline and how each maps onto an Administration API v1 resource
// and app use case — or, for excluded/engine rows, why it has no
// Administration twin.
//
// Binding invariant enforced here (the compatibility-contract freeze):
//   - the fixture identifier set equals the ROUTES.md mapping-table identifier
//     set (bidirectional — no table row without a fixture, no fixture without a
//     table row);
//   - every PASS row targeting the administration surface has exactly one
//     twin registered in the Administration API route registry, with the
//     declared method and auth tier;
//   - every EXCLUDE row records a reason and, where a nearest administration
//     surface is declared, that surface is registered;
//   - engine rows (the model-compatible endpoints and rewrite-layer paths such
//     as /v1/v1, /codex/*, /responses, /v1beta) never claim an Administration
//     API twin.
package compat

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gorouter/internal/domain/auth"
	"gorouter/internal/transport/httpserver/adminapi/v1"

	"gopkg.in/yaml.v3"
)

// fixtureRow is one compatibility-contract fixture row.
type fixtureRow struct {
	ID               string `yaml:"id"`
	UpstreamPath     string `yaml:"upstream_path"`
	Method           string `yaml:"method"`
	ExternalAuth     string `yaml:"external_auth"`
	Disposition      string `yaml:"disposition"`
	Target           string `yaml:"target"`
	RequestShape     string `yaml:"request_shape"`
	ResponseEnvelope string `yaml:"response_envelope"`
	StatusErrors     string `yaml:"status_errors"`
	CORS             string `yaml:"cors"`
	Headers          string `yaml:"headers"`
	AdminPath        string `yaml:"admin_path"`
	AdminMethod      string `yaml:"admin_method"`
	AdminAuthTier    string `yaml:"admin_auth_tier"`
	Nearest          string `yaml:"nearest_admin_surface"`
	Reason           string `yaml:"reason"`
	Source           string `yaml:"source"`
	HostGated        bool   `yaml:"host_gated"`
	ArrivesAt        string `yaml:"arrives_at"`
}

type fixtureDoc struct {
	Version   int          `yaml:"version"`
	PinCommit string       `yaml:"pin_commit"`
	Rows      []fixtureRow `yaml:"rows"`
}

// repoRoot returns the repository root relative to this package.
func repoRoot() string {
	return filepath.Join("..", "..", "..", "..")
}

// routesDocPath is the authoritative mapping table the fixtures must mirror.
func routesDocPath() string {
	return filepath.Join(repoRoot(), "internal", "transport", "httpserver", "compatibility", "ROUTES.md")
}

// loadFixtures reads every *.yaml fixture in this directory.
func loadFixtures(t *testing.T) []fixtureRow {
	t.Helper()
	var all []fixtureRow
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var doc fixtureDoc
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		all = append(all, doc.Rows...)
	}
	if len(all) == 0 {
		t.Fatal("no compat fixture rows loaded")
	}
	return all
}

// routesDocIDs returns the set of CMP-… identifiers that appear as the first
// cell of a mapping-table row in ROUTES.md.
func routesDocIDs(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(routesDocPath())
	if err != nil {
		t.Fatalf("read ROUTES.md: %v", err)
	}
	bq := "`"
	re := regexp.MustCompile(`(?m)^\|\s*` + bq + `(CMP-[A-Z0-9]+-\d+)` + bq + `\s*\|`)
	ids := map[string]bool{}
	for _, m := range re.FindAllSubmatch(data, -1) {
		ids[string(m[1])] = true
	}
	if len(ids) == 0 {
		t.Fatal("ROUTES.md mapping-table rows carry no CMP-… identifiers")
	}
	return ids
}

// nopAuth is a no-op AuthHandlers seam so the route registry includes the auth
// surface when built by this test.
type nopAuth struct{}

func (nopAuth) Login(w http.ResponseWriter, r *http.Request)  {}
func (nopAuth) Logout(w http.ResponseWriter, r *http.Request) {}
func (nopAuth) Me(w http.ResponseWriter, r *http.Request)     {}
func (nopAuth) Status(w http.ResponseWriter, r *http.Request) {}
func (nopAuth) ValidateSession(ctx context.Context, s string) (*auth.Actor, error) {
	return nil, nil
}
func (nopAuth) SessionCookieName() string { return "gorouter_session" }

// nopOIDC is a no-op OIDCHandlers seam so the optional OIDC routes
// (start/callback/test) are present in the registry under test.
type nopOIDC struct{}

func (nopOIDC) OIDCStart(w http.ResponseWriter, r *http.Request)    {}
func (nopOIDC) OIDCCallback(w http.ResponseWriter, r *http.Request) {}
func (nopOIDC) OIDCTest(w http.ResponseWriter, r *http.Request)     {}

// adminRegistry builds the Administration API route registry and returns the
// (method, path) key → auth-mode map plus the set of registered paths.
func adminRegistry(t *testing.T) (map[string]v1.AuthMode, map[string]bool) {
	t.Helper()
	h := v1.New(v1.Config{Auth: nopAuth{}, OIDC: nopOIDC{}})
	key := map[string]v1.AuthMode{}
	path := map[string]bool{}
	for _, rt := range v1.RouteTable(h) {
		key[rt.Method+" "+rt.Path] = rt.Mode
		path[rt.Path] = true
	}
	if len(key) == 0 {
		t.Fatal("admin route registry is empty")
	}
	return key, path
}

// modeOf maps the fixture admin auth tier string to the route registry tier.
func modeOf(tier string) (v1.AuthMode, bool) {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "public":
		return v1.AuthPublic, true
	case "session":
		return v1.AuthSession, true
	case "session+pat":
		return v1.AuthSessionPAT, true
	default:
		return v1.AuthPublic, false
	}
}

// validateFixtures cross-checks the fixtures against the mapping document and
// the Administration route registry. It returns the list of contract
// violations. The freeze invariant: no compatibility fixture has an
// administration twin unless that twin exists in the registry.
func validateFixtures(fixtures []fixtureRow, docIDs map[string]bool,
	regKey map[string]v1.AuthMode, regPath map[string]bool) []string {

	var out []string
	fixtureIDs := map[string]bool{}
	seenTwin := map[string]bool{}

	for _, r := range fixtures {
		fixtureIDs[r.ID] = true
		if !docIDs[r.ID] {
			out = append(out, fmt.Sprintf("%s: fixture row has no ROUTES.md mapping-table row", r.ID))
		}
		cmp := r.Target + "/" + r.Disposition
		switch cmp {
		case "admin/PASS":
			if r.AdminPath == "" || r.AdminMethod == "" {
				out = append(out, fmt.Sprintf("%s: PASS administration row lacks an admin twin", r.ID))
				continue
			}
			keytw := r.AdminMethod + " " + r.AdminPath
			if regKey == nil {
				if seenTwin[keytw] {
					out = append(out, fmt.Sprintf("%s: duplicate admin twin %s (exactly one mapping required)", r.ID, keytw))
				}
				seenTwin[keytw] = true
				continue
			}
			got, ok := regKey[keytw]
			if !ok {
				out = append(out, fmt.Sprintf("%s: admin twin %s is not registered in the Administration API route registry", r.ID, keytw))
			}
			if want, valid := modeOf(r.AdminAuthTier); ok && valid && want != got {
				out = append(out, fmt.Sprintf("%s: admin twin %s auth tier is %v, fixture declares %s", r.ID, keytw, got, r.AdminAuthTier))
			}
			if seenTwin[keytw] {
				out = append(out, fmt.Sprintf("%s: duplicate admin twin %s (exactly one mapping required)", r.ID, keytw))
			}
			seenTwin[keytw] = true

		case "admin/EXCLUDE":
			if r.AdminPath != "" {
				out = append(out, fmt.Sprintf("%s: EXCLUDE row must not declare an admin twin (%s)", r.ID, r.AdminPath))
			}
			if r.Reason == "" {
				out = append(out, fmt.Sprintf("%s: EXCLUDE row has no reconciliation reason", r.ID))
			}
			if regPath != nil && r.Nearest != "" && !regPath[r.Nearest] {
				out = append(out, fmt.Sprintf("%s: nearest admin surface %s is not registered", r.ID, r.Nearest))
			}

		case "engine/PASS":
			if r.AdminPath != "" {
				out = append(out, fmt.Sprintf("%s: engine row must not claim an admin twin (%s)", r.ID, r.AdminPath))
			}

		default:
			out = append(out, fmt.Sprintf("%s: unknown target/disposition %s", r.ID, cmp))
		}
	}
	for id := range docIDs {
		if !fixtureIDs[id] {
			out = append(out, fmt.Sprintf("ROUTES.md mapping-table row %s has no fixture row", id))
		}
	}
	return out
}

// TestCompatFixtureDocParity asserts the fixture identifier set equals the
// ROUTES.md mapping-table identifier set (bidirectional).
func TestCompatFixtureDocParity(t *testing.T) {
	fixtures := loadFixtures(t)
	docIDs := routesDocIDs(t)
	violations := validateFixtures(fixtures, docIDs, nil, nil)
	assertNoViolations(t, violations)
}

// TestCompatAdminTwinRegistered asserts every PASS administration row has
// exactly one registered twin with the declared method and auth tier (no
// adapter without a mapping row, and no mapping without a registered twin).
func TestCompatAdminTwinRegistered(t *testing.T) {
	fixtures := loadFixtures(t)
	docIDs := routesDocIDs(t)
	regKey, regPath := adminRegistry(t)
	violations := validateFixtures(fixtures, docIDs, regKey, regPath)
	assertNoViolations(t, violations)
}

// TestCompatExclusions asserts the reconciliation rows (providers
// [id]/models·test·test-models, proxy-pools [id]/test, usage
// codex-reset-credits, reset-password, backups restore) are EXCLUDE rows with
// a reason and no declared twin.
func TestCompatExclusions(t *testing.T) {
	fixtures := loadFixtures(t)
	for _, r := range fixtures {
		if r.Disposition != "EXCLUDE" {
			continue
		}
		if r.AdminPath != "" {
			t.Errorf("%s: EXCLUDE row declares an admin twin %s", r.ID, r.AdminPath)
		}
		if r.Reason == "" {
			t.Errorf("%s: EXCLUDE row has no reconciliation reason", r.ID)
		}
	}
}

// TestCompatEngineRows asserts the model-compatible endpoints and the rewrite
// layer (double-/v1, /codex/*, /responses, /v1beta) are engine rows that never
// reference an Administration API twin.
func TestCompatEngineRows(t *testing.T) {
	fixtures := loadFixtures(t)
	for _, r := range fixtures {
		if r.Target != "engine" {
			continue
		}
		if r.AdminPath != "" {
			t.Errorf("%s: engine row claims an admin twin %s", r.ID, r.AdminPath)
		}
		if r.UpstreamPath == "" {
			t.Errorf("%s: engine row has no external path", r.ID)
		}
	}
}

// TestCompatValidatorCatchesDrift proves the validator can catch a
// fixture/mapping drift: a fabricated PASS row whose admin twin is not
// registered, an EXCLUDE row with no reason, and a row with no ROUTES.md entry
// must all be reported. This keeps the drift check provably live.
func TestCompatValidatorCatchesDrift(t *testing.T) {
	docIDs := routesDocIDs(t)
	regKey, regPath := adminRegistry(t)
	violations := validateFixtures(
		[]fixtureRow{
			driftRow("CMP-TEST-01", "/ghost", "GET", "PASS", "admin", "/ghost", "GET", "session+pat"),
			driftRow("CMP-TEST-02", "/ghost", "GET", "EXCLUDE", "admin", "", "", ""),
		},
		docIDs, regKey, regPath)
	var sawUnregistered, sawMissingDoc, sawNoReason bool
	for _, v := range violations {
		if strings.Contains(v, "CMP-TEST-01") && strings.Contains(v, "not registered") {
			sawUnregistered = true
		}
		if strings.Contains(v, "CMP-TEST-01") && strings.Contains(v, "no ROUTES.md") {
			sawMissingDoc = true
		}
		if strings.Contains(v, "CMP-TEST-02") && strings.Contains(v, "no reconciliation reason") {
			sawNoReason = true
		}
	}
	if !sawUnregistered {
		t.Errorf("validator did not report an unregistered admin twin:\n%s", strings.Join(violations, "\n"))
	}
	if !sawMissingDoc {
		t.Errorf("validator did not report a fixture row with no ROUTES.md row:\n%s", strings.Join(violations, "\n"))
	}
	if !sawNoReason {
		t.Errorf("validator did not report an EXCLUDE row with no reason:\n%s", strings.Join(violations, "\n"))
	}
}

// driftRow builds a synthetic fixture row for the drift test.
func driftRow(id, up, meth, disp, target, ap, am, tier string) fixtureRow {
	return fixtureRow{ID: id, UpstreamPath: up, Method: meth, Disposition: disp, Target: target, AdminPath: ap, AdminMethod: am, AdminAuthTier: tier}
}

func assertNoViolations(t *testing.T, violations []string) {
	t.Helper()
	if len(violations) > 0 {
		t.Errorf("compatibility contract violations (%d):\n%s", len(violations), strings.Join(violations, "\n"))
	}
}
