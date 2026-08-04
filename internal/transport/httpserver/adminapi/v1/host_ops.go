package v1

import (
	"context"
	"net/http"
)

// HostOpsService is the application seam for host operations (headroom,
// pxpipe, mcp, shutdown, updater, version). Every action is feature-flag
// gated by the router (closed default) and audited (P0-1); the host layer
// implements the actual adapters.
type HostOpsService interface {
	Action(ctx context.Context, feature, action string, in any) (any, error)
	Version(ctx context.Context) (any, error)
}

type hostOpsGroup struct {
	svc     HostOpsService
	auditor Auditor
}

func (g *hostOpsGroup) run(w http.ResponseWriter, r *http.Request, feature, action string) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := g.svc.Action(r.Context(), feature, action, body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *hostOpsGroup) Version(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := g.svc.Version(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *hostOpsGroup) HeadroomStart(w http.ResponseWriter, r *http.Request)  { g.run(w, r, "headroom", "start") }
func (g *hostOpsGroup) HeadroomStop(w http.ResponseWriter, r *http.Request)   { g.run(w, r, "headroom", "stop") }
func (g *hostOpsGroup) HeadroomRestart(w http.ResponseWriter, r *http.Request) { g.run(w, r, "headroom", "restart") }
func (g *hostOpsGroup) HeadroomStatus(w http.ResponseWriter, r *http.Request) { g.run(w, r, "headroom", "status") }
func (g *hostOpsGroup) HeadroomExtras(w http.ResponseWriter, r *http.Request) { g.run(w, r, "headroom", "extras") }
func (g *hostOpsGroup) HeadroomProxyGet(w http.ResponseWriter, r *http.Request) {
	g.run(w, r, "headroom", "proxy:"+pathParam(r, "path"))
}
func (g *hostOpsGroup) HeadroomProxyAction(w http.ResponseWriter, r *http.Request) {
	g.run(w, r, "headroom", "proxy:"+pathParam(r, "path"))
}

func (g *hostOpsGroup) PxpipeInstall(w http.ResponseWriter, r *http.Request)  { g.run(w, r, "pxpipe", "install") }
func (g *hostOpsGroup) PxpipeStart(w http.ResponseWriter, r *http.Request)    { g.run(w, r, "pxpipe", "start") }
func (g *hostOpsGroup) PxpipeStop(w http.ResponseWriter, r *http.Request)     { g.run(w, r, "pxpipe", "stop") }
func (g *hostOpsGroup) PxpipeRestart(w http.ResponseWriter, r *http.Request)  { g.run(w, r, "pxpipe", "restart") }
func (g *hostOpsGroup) PxpipeStatus(w http.ResponseWriter, r *http.Request)   { g.run(w, r, "pxpipe", "status") }
func (g *hostOpsGroup) PxpipeHealth(w http.ResponseWriter, r *http.Request)   { g.run(w, r, "pxpipe", "health") }
func (g *hostOpsGroup) PxpipeHealthAction(w http.ResponseWriter, r *http.Request) {
	g.run(w, r, "pxpipe", "health")
}
func (g *hostOpsGroup) PxpipeLogs(w http.ResponseWriter, r *http.Request)   { g.run(w, r, "pxpipe", "logs") }
func (g *hostOpsGroup) PxpipeStats(w http.ResponseWriter, r *http.Request)  { g.run(w, r, "pxpipe", "stats") }

func (g *hostOpsGroup) MCPMessage(w http.ResponseWriter, r *http.Request) {
	g.run(w, r, "mcp", "message:"+pathParam(r, "plugin"))
}

// MCPSse resolves the session-cookie-only mcp stream surface (P1-5). The
// realtime lane owns the SSE transport and replaces this placeholder.
func (g *hostOpsGroup) MCPSse(w http.ResponseWriter, r *http.Request) {
	backendUnavailable(w, r)
}

func (g *hostOpsGroup) Shutdown(w http.ResponseWriter, r *http.Request)       { g.run(w, r, "shutdown", "shutdown") }
func (g *hostOpsGroup) VersionUpdate(w http.ResponseWriter, r *http.Request)  { g.run(w, r, "updater", "update") }
func (g *hostOpsGroup) VersionShutdown(w http.ResponseWriter, r *http.Request) { g.run(w, r, "shutdown", "shutdown") }