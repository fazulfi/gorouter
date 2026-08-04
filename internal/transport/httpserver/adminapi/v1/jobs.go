package v1

import (
	"context"
	"net/http"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/jobs"
)

// JobsService is the application seam for the jobs group. Run-now receives
// the actor so the service audits with job provenance.
type JobsService interface {
	List(ctx context.Context) ([]jobs.Job, error)
	History(ctx context.Context, jobType string) ([]jobs.Job, error)
	RunNow(ctx context.Context, actor *auth.Actor, jobType string) error
}

type jobsGroup struct{ svc JobsService }

func (g *jobsGroup) List(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.List(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (g *jobsGroup) RunNow(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.RunNow(r.Context(), actor, pathParam(r, "type")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (g *jobsGroup) History(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.History(r.Context(), pathParam(r, "type"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// Stream resolves the session-cookie-only jobs stream surface (P1-5). The
// realtime lane owns the SSE transport and replaces this placeholder.
func (g *jobsGroup) Stream(w http.ResponseWriter, r *http.Request) {
	backendUnavailable(w, r)
}