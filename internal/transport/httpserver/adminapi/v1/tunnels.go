package v1

import (
	"context"
	"net/http"
)

// TunnelsService is the application seam for the tunnel and tailscale group.
// Mutations are host-gated by the router (closed default); reads resolve
// through this seam.
type TunnelsService interface {
	Get(ctx context.Context) (any, error)
	Status(ctx context.Context) (any, error)
	TailscaleCheck(ctx context.Context) (any, error)
	Enable(ctx context.Context) (any, error)
	Disable(ctx context.Context) (any, error)
	TailscaleInstall(ctx context.Context) (any, error)
	TailscaleEnable(ctx context.Context) (any, error)
	TailscaleDisable(ctx context.Context) (any, error)
}

type tunnelsGroup struct{ svc TunnelsService }

func (g *tunnelsGroup) run(w http.ResponseWriter, r *http.Request, action string) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	ctx := r.Context()
	var out any
	var err error
	switch action {
	case "get":
		out, err = g.svc.Get(ctx)
	case "status":
		out, err = g.svc.Status(ctx)
	case "tailscale-check":
		out, err = g.svc.TailscaleCheck(ctx)
	case "enable":
		out, err = g.svc.Enable(ctx)
	case "disable":
		out, err = g.svc.Disable(ctx)
	case "tailscale-install":
		out, err = g.svc.TailscaleInstall(ctx)
	case "tailscale-enable":
		out, err = g.svc.TailscaleEnable(ctx)
	case "tailscale-disable":
		out, err = g.svc.TailscaleDisable(ctx)
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

func (g *tunnelsGroup) Get(w http.ResponseWriter, r *http.Request)            { g.run(w, r, "get") }
func (g *tunnelsGroup) Status(w http.ResponseWriter, r *http.Request)         { g.run(w, r, "status") }
func (g *tunnelsGroup) TailscaleCheck(w http.ResponseWriter, r *http.Request) { g.run(w, r, "tailscale-check") }
func (g *tunnelsGroup) Enable(w http.ResponseWriter, r *http.Request)         { g.run(w, r, "enable") }
func (g *tunnelsGroup) Disable(w http.ResponseWriter, r *http.Request)        { g.run(w, r, "disable") }
func (g *tunnelsGroup) TailscaleInstall(w http.ResponseWriter, r *http.Request) {
	g.run(w, r, "tailscale-install")
}
func (g *tunnelsGroup) TailscaleEnable(w http.ResponseWriter, r *http.Request) {
	g.run(w, r, "tailscale-enable")
}
func (g *tunnelsGroup) TailscaleDisable(w http.ResponseWriter, r *http.Request) {
	g.run(w, r, "tailscale-disable")
}