// Package compatibility preserves the historical external management surface.
//
// Each row in ROUTES.md (this directory) is a compatibility route: an original
// external path/method that a thin adapter preserves while delegating to the
// same application use case as its Administration API v1 twin. An adapter is a
// pure registration: it re-hosts its twin's handler closure under the legacy
// path spellings inside the internal MountPrefix namespace. No business logic
// is re-implemented in this package.
package compatibility

import (
	adminapiv1 "gorouter/internal/transport/httpserver/adminapi/v1"
)

// MountPrefix is the internal mount namespace for every compatibility
// adapter: /api/compat/* preserves the historical /api/* spellings. It is an
// internal namespace, not the external contract (design §7).
const MountPrefix = "/api/compat"

// compatRow names one preserved historical management route by the exact
// Administration API v1 twin (method + path) that owns its application use
// case. The twin is authoritative: the adapter inherits the twin's handler,
// authz class and host-operation gate.
type compatRow struct {
	Method string
	Path   string
}

func (c compatRow) key() string { return c.Method + " " + c.Path }

// RouteTable returns the preserved compatibility surface: one thin adapter per
// frozen historical management row. Each adapter is the twin's registration
// re-hosted under MountPrefix with the twin's exact method, authz class,
// host-operation gate and application-use-case handling. A row whose twin is
// not registered is skipped (for example the optional OIDC surface when no
// OIDC source is wired) so no dangling route is emitted.
func RouteTable(h *adminapiv1.Handlers) []adminapiv1.Route {
	twins := make(map[string]adminapiv1.Route)
	for _, rt := range adminapiv1.RouteTable(h) {
		twins[rt.Method+" "+rt.Path] = rt
	}
	var out []adminapiv1.Route
	for _, row := range allRows() {
		twin, ok := twins[row.key()]
		if !ok {
			continue
		}
		compat := twin
		compat.Path = MountPrefix + twin.Path
		out = append(out, compat)
	}
	return out
}

// allRows returns every preserved historical row in mount order, grouped per
// management domain.
func allRows() []compatRow {
	var rows []compatRow
	groups := [][]compatRow{
		authRows(),
		keyRows(),
		providerRows(),
		nodeRows(),
		poolRows(),
		modelRows(),
		comboRows(),
		pricingRows(),
		usageRows(),
		cliToolRows(),
		mediaRows(),
		oauthRows(),
		tunnelRows(),
		hostOpRows(),
		settingRows(),
		translatorRows(),
		metaRows(),
	}
	for _, g := range groups {
		rows = append(rows, g...)
	}
	return rows
}
