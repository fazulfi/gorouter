package v1

import (
	"context"
	"net/http"

	appproviders "gorouter/internal/app/providers"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/provider"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PoolsService is the application seam for the proxy-pools group. Mutations
// receive the actor so the service audits with actor, origin, ip and target.
type PoolsService interface {
	List(ctx context.Context) ([]provider.ProxyPool, error)
	Get(ctx context.Context, id uuid.UUID) (*provider.ProxyPool, error)
	Create(ctx context.Context, actor *auth.Actor, pool *provider.ProxyPool) (*provider.ProxyPool, error)
	Update(ctx context.Context, actor *auth.Actor, pool *provider.ProxyPool) (*provider.ProxyPool, error)
	Delete(ctx context.Context, actor *auth.Actor, id uuid.UUID) error
}

type poolsGroup struct{ svc PoolsService }

func (g *poolsGroup) List(w http.ResponseWriter, r *http.Request) {
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

func (g *poolsGroup) Create(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var pool provider.ProxyPool
	if err := decodeBody(r, &pool); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	created, err := g.svc.Create(r.Context(), actor, &pool)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (g *poolsGroup) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, errInvalid)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	pool, err := g.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pool)
}

func (g *poolsGroup) Update(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, errInvalid)
		return
	}
	var pool provider.ProxyPool
	if err := decodeBody(r, &pool); err != nil {
		writeError(w, r, err)
		return
	}
	pool.ID = id
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	updated, err := g.svc.Update(r.Context(), actor, &pool)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (g *poolsGroup) Delete(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, errInvalid)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Delete(r.Context(), actor, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deployBody carries the pool id and base URL consumed by the deploy config
// generators.
type deployBody struct {
	ID      uuid.UUID `json:"id"`
	BaseURL string    `json:"base_url"`
}

func (g *poolsGroup) deploy(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r)
		if actor == nil {
			writeError(w, r, errUnauthorized)
			return
		}
		var body deployBody
		if err := decodeBody(r, &body); err != nil {
			writeError(w, r, err)
			return
		}
		var cfg map[string]any
		switch kind {
		case "cloudflare":
			cfg = appproviders.GenerateCloudflareConfig(body.ID, body.BaseURL)
		case "deno":
			cfg = appproviders.GenerateDenoConfig(body.ID, body.BaseURL)
		case "vercel":
			cfg = appproviders.GenerateVercelConfig(body.ID, body.BaseURL)
		default:
			writeError(w, r, errInvalid)
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func (g *poolsGroup) CloudflareDeploy(w http.ResponseWriter, r *http.Request) {
	g.deploy("cloudflare")(w, r)
}
func (g *poolsGroup) DenoDeploy(w http.ResponseWriter, r *http.Request)   { g.deploy("deno")(w, r) }
func (g *poolsGroup) VercelDeploy(w http.ResponseWriter, r *http.Request) { g.deploy("vercel")(w, r) }
