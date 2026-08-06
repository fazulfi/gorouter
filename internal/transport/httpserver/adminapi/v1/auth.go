package v1

import (
	"net/http"

	"gorouter/internal/transport/httpserver/health"
)

// healthGroup serves the health surfaces. The public probe is anonymous
// (design §6); the detailed surface rides the session+pat boundary and is
// therefore protected by the router before this handler runs.
type healthGroup struct{}

func (healthGroup) Public(w http.ResponseWriter, r *http.Request) {
	health.PublicHandler()(w, r)
}

func (healthGroup) Detailed(w http.ResponseWriter, r *http.Request) {
	detailedHandler(w, r)
}

// detailedHandler is a package-level detailed-health handler with no DB
// wiring; the transport atom only resolves the surface.
var detailedHandler = health.DetailedHandler(health.DetailedHandlerConfig{})

// handleInit is the first-run bootstrap surface. It has no application
// service yet; the route resolves (never 404) and reports the backend
// surface unavailable until the bootstrap service is wired.
func handleInit(w http.ResponseWriter, r *http.Request) {
	backendUnavailable(w, r)
}

// handleLocale is the public locale catalog surface. The i18n catalog is
// bundled client-side (design §12); this upstream-parity route resolves
// without a backend service.
func handleLocale(w http.ResponseWriter, r *http.Request) {
	backendUnavailable(w, r)
}

// handleTags is the public tags surface.
func handleTags(w http.ResponseWriter, r *http.Request) {
	backendUnavailable(w, r)
}
