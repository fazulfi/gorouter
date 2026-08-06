package v1

import (
	"context"
	"net/http"
)

// CLIToolsService is the application seam for the cli-tools group
// (all-statuses + the 16 exact upstream tool routes + antigravity-mitm
// alias).
type CLIToolsService interface {
	AllStatuses(ctx context.Context) (any, error)
	ToolGet(ctx context.Context, tool string) (any, error)
	ToolAction(ctx context.Context, tool string, in any) (any, error)
	AntigravityAlias(ctx context.Context, in any) (any, error)
}

type cliToolsGroup struct{ svc CLIToolsService }

func (g *cliToolsGroup) AllStatuses(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := g.svc.AllStatuses(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *cliToolsGroup) ToolGet(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := g.svc.ToolGet(r.Context(), pathParam(r, "tool"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *cliToolsGroup) ToolAction(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := g.svc.ToolAction(r.Context(), pathParam(r, "tool"), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *cliToolsGroup) AntigravityAlias(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := g.svc.AntigravityAlias(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
