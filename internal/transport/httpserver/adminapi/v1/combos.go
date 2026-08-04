package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/combo"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// CombosService is the application seam for the combos group. Mutations
// receive the actor so the service audits.
type CombosService interface {
	List(ctx context.Context) ([]combo.Definition, error)
	Get(ctx context.Context, id uuid.UUID) (*combo.Definition, error)
	Create(ctx context.Context, actor *auth.Actor, name string, strategy combo.Strategy, config json.RawMessage, members []combo.Member) (*combo.Definition, error)
	Update(ctx context.Context, actor *auth.Actor, def *combo.Definition) error
	Delete(ctx context.Context, actor *auth.Actor, id uuid.UUID) error
}

type combosGroup struct{ svc CombosService }

// comboView is the wire projection of a combo definition.
type comboView struct {
	ID        uuid.UUID        `json:"id"`
	Name      string           `json:"name"`
	Strategy  string           `json:"strategy"`
	IsActive  bool             `json:"is_active"`
	Config    json.RawMessage `json:"config,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func projectCombo(d *combo.Definition) comboView {
	return comboView{
		ID: d.ID, Name: d.Name, Strategy: string(d.Strategy), IsActive: d.IsActive,
		Config: sanitizeJSON(d.Config), CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func (g *combosGroup) List(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.List(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var out []comboView
	for i := range rows {
		out = append(out, projectCombo(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *combosGroup) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, errInvalid)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	def, err := g.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectCombo(def))
}

func (g *combosGroup) Create(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body struct {
		Name     string           `json:"name"`
		Strategy combo.Strategy   `json:"strategy"`
		Config   json.RawMessage  `json:"config"`
		Members  []combo.Member   `json:"members"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if body.Name == "" || body.Strategy == "" {
		writeError(w, r, errInvalid)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	def, err := g.svc.Create(r.Context(), actor, body.Name, body.Strategy, sanitizeJSON(body.Config), body.Members)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectCombo(def))
}

func (g *combosGroup) Update(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		Name     string          `json:"name"`
		Strategy combo.Strategy  `json:"strategy"`
		Config   json.RawMessage `json:"config"`
		IsActive *bool           `json:"is_active"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	def, err := g.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if body.Name != "" {
		def.Name = body.Name
	}
	if body.Strategy != "" {
		def.Strategy = body.Strategy
	}
	if body.Config != nil {
		def.Config = body.Config
	}
	if body.IsActive != nil {
		def.IsActive = *body.IsActive
	}
	if err := g.svc.Update(r.Context(), actor, def); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectCombo(def))
}

func (g *combosGroup) Delete(w http.ResponseWriter, r *http.Request) {
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