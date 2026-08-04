package v1

import (
	"context"
	"net/http"

	"gorouter/internal/domain/console"
)

// ConsoleService is the application seam for the console group. The stream
// surface is session-cookie-only (P1-5); the SSE transport is owned by the
// realtime lane.
type ConsoleService interface {
	ListAfter(ctx context.Context, seq int64, limit int) ([]console.ConsoleLog, error)
}

type consoleGroup struct{ svc ConsoleService }

// Stream resolves the /console/stream surface. The realtime lane replaces
// this placeholder; the route never 404s.
func (g *consoleGroup) Stream(w http.ResponseWriter, r *http.Request) {
	backendUnavailable(w, r)
}
