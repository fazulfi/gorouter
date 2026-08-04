package v1

import (
	"context"
	"net/http"

	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
)

// AliasesService is the application seam for model aliases (/models/alias).
type AliasesService interface {
	List(ctx context.Context) ([]enginerouting.Alias, error)
	Set(ctx context.Context, alias *enginerouting.Alias) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type aliasesGroup struct{ svc AliasesService }

func (g *aliasesGroup) AliasGet(w http.ResponseWriter, r *http.Request) {
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

func (g *aliasesGroup) AliasUpdate(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var alias enginerouting.Alias
	if err := decodeBody(r, &alias); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Set(r.Context(), &alias); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, alias)
}

func (g *aliasesGroup) AliasDelete(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body struct {
		ID uuid.UUID `json:"id"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Delete(r.Context(), body.ID); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
