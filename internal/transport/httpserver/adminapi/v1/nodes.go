package v1

import (
	"context"
	"net/http"

	"gorouter/internal/domain/auth"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/go-chi/chi/v5"
)

// NodesService is the application seam for the provider-nodes group.
type NodesService interface {
	List(ctx context.Context) ([]enginerouting.ProviderNode, error)
	Get(ctx context.Context, id string) (*enginerouting.ProviderNode, error)
	Save(ctx context.Context, actor *auth.Actor, node enginerouting.ProviderNode) error
	Update(ctx context.Context, node enginerouting.ProviderNode) error
	Delete(ctx context.Context, id string) error
	Validate(ctx context.Context, id string) error
}

type nodesGroup struct{ svc NodesService }

func (g *nodesGroup) List(w http.ResponseWriter, r *http.Request) {
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

func (g *nodesGroup) Create(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var node enginerouting.ProviderNode
	if err := decodeBody(r, &node); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Save(r.Context(), actor, node); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (g *nodesGroup) Get(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	node, err := g.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (g *nodesGroup) Update(w http.ResponseWriter, r *http.Request) {
	var node enginerouting.ProviderNode
	if err := decodeBody(r, &node); err != nil {
		writeError(w, r, err)
		return
	}
	if node.ID == "" {
		node.ID = chi.URLParam(r, "id")
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Update(r.Context(), node); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (g *nodesGroup) Delete(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *nodesGroup) Validate(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Validate(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
