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

type consoleGroup struct {
	svc    ConsoleService
	stream http.HandlerFunc
}

// Stream serves the /console/stream surface. When the realtime stream
// handler is wired it serves the SSE stream; otherwise the route resolves
// with backend-unavailable.
func (g *consoleGroup) Stream(w http.ResponseWriter, r *http.Request) {
	if g.stream != nil {
		g.stream(w, r)
		return
	}
	backendUnavailable(w, r)
}
