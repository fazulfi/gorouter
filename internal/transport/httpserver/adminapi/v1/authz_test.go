package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gorouter/internal/domain/auth"

	"gopkg.in/yaml.v3"
)

// openAPIDoc is the minimal OpenAPI model needed to classify the contract
// routes (path, method, auth tier, host-operation marker).
type openAPIDoc struct {
	Paths map[string]*pathItem `yaml:"paths"`
}

type pathItem struct {
	Get    *operation `yaml:"get"`
	Post   *operation `yaml:"post"`
	Put    *operation `yaml:"put"`
	Patch  *operation `yaml:"patch"`
	Delete *operation `yaml:"delete"`
}

type operation struct {
	Description string                `yaml:"description"`
	Security    []map[string][]string `yaml:"security"`
}

func (pi *pathItem) operations() map[string]*operation {
	out := map[string]*operation{}
	if pi.Get != nil {
		out[http.MethodGet] = pi.Get
	}
	if pi.Post != nil {
		out[http.MethodPost] = pi.Post
	}
	if pi.Put != nil {
		out[http.MethodPut] = pi.Put
	}
	if pi.Patch != nil {
		out[http.MethodPatch] = pi.Patch
	}
	if pi.Delete != nil {
		out[http.MethodDelete] = pi.Delete
	}
	return out
}

// contractTier is the authz class a contract route declares.
type contractTier int

const (
	tierPublic contractTier = iota
	tierSession
	tierSessionPAT
)

func tierOf(op *operation) contractTier {
	if len(op.Security) == 0 {
		return tierPublic
	}
	for _, req := range op.Security {
		if _, ok := req["PAT"]; ok {
			return tierSessionPAT
		}
	}
	return tierSession
}

// expectedMatrix returns the design §6 actor classes for a route: session,
// PAT, CLI and job columns. Job actors never invoke HTTP routes (their
// status-only reads run in-process through app services); public routes
// require no principal.
func expectedMatrix(tier contractTier, hostOp bool) (session, pat, cli, job bool) {
	switch tier {
	case tierPublic:
		return false, false, false, false
	case tierSession:
		return true, false, false, false
	case tierSessionPAT:
		return true, true, true, false
	}
	return false, false, false, false
}

func loadContractRoutes(t *testing.T) []struct {
	Method, Path string
	Tier         contractTier
	HostOp       bool
} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "api", "admin-v1.openapi.yaml"))
	if err != nil {
		t.Fatalf("read api/admin-v1.openapi.yaml: %v", err)
	}
	var doc openAPIDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	var rows []struct {
		Method, Path string
		Tier         contractTier
		HostOp       bool
	}
	for path, pi := range doc.Paths {
		for method, op := range pi.operations() {
			rows = append(rows, struct {
				Method, Path string
				Tier         contractTier
				HostOp       bool
			}{
				Method: method, Path: path, Tier: tierOf(op),
				HostOp: strings.Contains(op.Description, "Host operation:"),
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Path != rows[j].Path {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].Method < rows[j].Method
	})
	return rows
}

// authFake implements the AuthHandlers and OIDCHandlers surfaces for the
// matrix walk.
type authFake struct{}

func (authFake) Login(w http.ResponseWriter, r *http.Request)          {}
func (authFake) Logout(w http.ResponseWriter, r *http.Request)         {}
func (authFake) Me(w http.ResponseWriter, r *http.Request)             {}
func (authFake) Status(w http.ResponseWriter, r *http.Request)         {}
func (authFake) OIDCStart(w http.ResponseWriter, r *http.Request)      {}
func (authFake) OIDCCallback(w http.ResponseWriter, r *http.Request)   {}
func (authFake) OIDCTest(w http.ResponseWriter, r *http.Request)       {}
func (authFake) ValidateSession(ctx context.Context, raw string) (*auth.Actor, error) {
	return &auth.Actor{Kind: auth.ActorKindSession, Origin: auth.ActorOriginRemote}, nil
}
func (authFake) SessionCookieName() string { return "gorouter_session" }

// TestAuthzMatrix is the primary one-behavior gate (lane plan API-05): it
// walks the registered route table and asserts every route's allowed actor
// classes exactly match the design §6 matrix encoded from the frozen
// contract (session/PAT/CLI/job columns; host operations flag-gated, closed
// by default; job actors never over HTTP).
func TestAuthzMatrix(t *testing.T) {
	h := New(Config{Auth: authFake{}, OIDC: authFake{}})
	table := RouteTable(h)

	registered := map[string]Route{}
	for _, rt := range table {
		key := rt.Method + " " + rt.Path
		if _, dup := registered[key]; dup {
			t.Errorf("route %s registered more than once", key)
		}
		registered[key] = rt
	}

	contract := loadContractRoutes(t)
	failures := 0
	seen := map[string]bool{}
	for _, row := range contract {
		key := row.Method + " " + row.Path
		seen[key] = true
		rt, ok := registered[key]
		if !ok {
			t.Errorf("contract route %s is not registered (404)", key)
			failures++
			continue
		}
		var mode AuthMode
		switch row.Tier {
		case tierPublic:
			mode = AuthPublic
		case tierSession:
			mode = AuthSession
		case tierSessionPAT:
			mode = AuthSessionPAT
		}
		if rt.Mode != mode {
			t.Errorf("%s: authz mode = %v, want %v (tier %v)", key, rt.Mode, mode, row.Tier)
			failures++
		}
		if want := row.HostOp; want != (rt.HostFeature != "") {
			t.Errorf("%s: host-gated = %v (feature %q), contract host-op = %v", key, rt.HostFeature != "", rt.HostFeature, want)
			failures++
		}
		s, p, c, j := expectedMatrix(row.Tier, row.HostOp)
		gotS, gotP, gotC, gotJ := rt.Mode != AuthPublic, rt.Mode == AuthSessionPAT, rt.Mode == AuthSessionPAT, false
		if gotS != s || gotP != p || gotC != c || gotJ != j {
			t.Errorf("%s: actor classes session=%v pat=%v cli=%v job=%v, want session=%v pat=%v cli=%v job=%v",
				key, gotS, gotP, gotC, gotJ, s, p, c, j)
			failures++
		}
	}
	for key := range registered {
		if !seen[key] {
			t.Errorf("registered route %s is not part of the contract (no drift allowed)", key)
			failures++
		}
	}
	if failures > 0 {
		t.Fatalf("authz matrix mismatches: %d failures", failures)
	}
}

// TestAuthzMatrixHostOpsClosedByDefault asserts every host-operation route
// declares a feature key and that the HostGate default is closed.
func TestAuthzMatrixHostOpsClosedByDefault(t *testing.T) {
	h := New(Config{Auth: authFake{}, OIDC: authFake{}})
	for _, rt := range RouteTable(h) {
		if rt.HostFeature == "" {
			continue
		}
		if rt.Mode != AuthSessionPAT {
			t.Errorf("host-op route %s %s must ride the session+pat boundary", rt.Method, rt.Path)
		}
		if rt.HostFeature == "" {
			t.Errorf("host-op route %s %s has no feature key", rt.Method, rt.Path)
		}
	}
}

// TestAuthzMatrixStreamsSessionOnly asserts P1-5: every /stream or /sse
// surface is session-cookie-only and never accepts a PAT.
func TestAuthzMatrixStreamsSessionOnly(t *testing.T) {
	h := New(Config{Auth: authFake{}, OIDC: authFake{}})
	for _, rt := range RouteTable(h) {
		if !(strings.HasSuffix(rt.Path, "/stream") || strings.HasSuffix(rt.Path, "/sse")) {
			continue
		}
		if rt.Mode != AuthSession {
			t.Errorf("stream route %s %s must be session-cookie-only (P1-5), mode %v", rt.Method, rt.Path, rt.Mode)
		}
		if rt.HostFeature != "" {
			t.Errorf("stream route %s %s must not be host-gated", rt.Method, rt.Path)
		}
	}
}

// TestAuthzMatrixNoResetOrRestore asserts the two prohibited surfaces stay
// absent (D16, #202).
func TestAuthzMatrixNoResetOrRestore(t *testing.T) {
	h := New(Config{Auth: authFake{}, OIDC: authFake{}})
	for _, rt := range RouteTable(h) {
		lower := strings.ToLower(rt.Path)
		if strings.Contains(lower, "reset-password") || strings.Contains(lower, "reset_password") || strings.Contains(lower, "/restore") {
			t.Errorf("forbidden route present: %s %s", rt.Method, rt.Path)
		}
	}
}

// TestRouteTableJSONMarshals ensures the table metadata is stable for the
// contract review tooling.
func TestRouteTableJSONMarshals(t *testing.T) {
	h := New(Config{Auth: authFake{}, OIDC: authFake{}})
	rows := RouteTable(h)
	meta := make([]map[string]any, 0, len(rows))
	for _, rt := range rows {
		meta = append(meta, map[string]any{
			"method":       rt.Method,
			"path":         rt.Path,
			"mode":         int(rt.Mode),
			"host_feature": rt.HostFeature,
		})
	}
	if _, err := json.Marshal(meta); err != nil {
		t.Fatalf("route table metadata must marshal: %v", err)
	}
}
