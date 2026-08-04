package v1

import (
	"context"
	"net/http"
)

// UsageService is the application seam for the usage group. Query results
// pass through as JSON; the durable history and aggregates are wired by the
// backend lane.
type UsageService interface {
	Stats(ctx context.Context) (any, error)
	History(ctx context.Context) (any, error)
	Chart(ctx context.Context) (any, error)
	Providers(ctx context.Context) (any, error)
	RequestDetails(ctx context.Context) (any, error)
	RequestLogs(ctx context.Context) (any, error)
	Logs(ctx context.Context) (any, error)
	Connection(ctx context.Context, connectionID string) (any, error)
}

type usageGroup struct {
	svc    UsageService
	stream http.HandlerFunc
}

func (g *usageGroup) query(w http.ResponseWriter, r *http.Request, kind string) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	ctx := r.Context()
	var out any
	var err error
	switch kind {
	case "stats":
		out, err = g.svc.Stats(ctx)
	case "history":
		out, err = g.svc.History(ctx)
	case "chart":
		out, err = g.svc.Chart(ctx)
	case "providers":
		out, err = g.svc.Providers(ctx)
	case "request-details":
		out, err = g.svc.RequestDetails(ctx)
	case "request-logs":
		out, err = g.svc.RequestLogs(ctx)
	case "logs":
		out, err = g.svc.Logs(ctx)
	default:
		writeError(w, r, errInvalid)
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *usageGroup) Stats(w http.ResponseWriter, r *http.Request) {
	g.query(w, r, "stats")
}
func (g *usageGroup) History(w http.ResponseWriter, r *http.Request) {
	g.query(w, r, "history")
}
func (g *usageGroup) Chart(w http.ResponseWriter, r *http.Request) {
	g.query(w, r, "chart")
}
func (g *usageGroup) Providers(w http.ResponseWriter, r *http.Request) {
	g.query(w, r, "providers")
}
func (g *usageGroup) RequestDetails(w http.ResponseWriter, r *http.Request) {
	g.query(w, r, "request-details")
}
func (g *usageGroup) RequestLogs(w http.ResponseWriter, r *http.Request) {
	g.query(w, r, "request-logs")
}
func (g *usageGroup) Logs(w http.ResponseWriter, r *http.Request) {
	g.query(w, r, "logs")
}
func (g *usageGroup) Connection(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := g.svc.Connection(r.Context(), pathParam(r, "connectionId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// Stream is the session-cookie-only usage stream surface (P1-5). When the
// realtime stream handler is wired it serves the SSE stream; otherwise the
// route resolves with backend-unavailable.
func (g *usageGroup) Stream(w http.ResponseWriter, r *http.Request) {
	if g.stream != nil {
		g.stream(w, r)
		return
	}
	backendUnavailable(w, r)
}
