package v1

import (
	"context"
	"net/http"

	appproviders "gorouter/internal/app/providers"
	"gorouter/internal/domain/provider"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ProvidersService is the application seam for the providers group. The
// concrete backend service exposes validate/test-batch; the CRUD and
// projection routes are wired when the backend lane lands them.
type ProvidersService interface {
	List(ctx context.Context) ([]provider.Provider, error)
	Get(ctx context.Context, id uuid.UUID) (*provider.Provider, error)
	Create(ctx context.Context, in *provider.Provider) (*provider.Provider, error)
	Update(ctx context.Context, in *provider.Provider) (*provider.Provider, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Client(ctx context.Context) ([]provider.Provider, error)
	KiloFreeModels(ctx context.Context) ([]string, error)
	SuggestedModels(ctx context.Context) ([]string, error)
	TestBatch(ctx context.Context, ids []uuid.UUID, concurrencyLimit int) (appproviders.TestBatchResult, error)
	Validate(ctx context.Context, id uuid.UUID) error
}

type providersGroup struct{ svc ProvidersService }

// providerView is the credential-safe projection (upstream providers/client
// pattern): the plaintext API key value is never exposed; a has_credentials
// boolean reports whether a key is configured.
type providerView struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	BaseURL        string    `json:"base_url,omitempty"`
	Config         any       `json:"config,omitempty"`
	IsEnabled      bool      `json:"is_enabled"`
	HasCredentials bool      `json:"has_credentials"`
}

func projectProvider(p *provider.Provider) providerView {
	var cfg any
	if len(p.Config) > 0 {
		cfg = p.Config
	}
	return providerView{
		ID: p.ID, Name: p.Name, Type: string(p.Type), BaseURL: p.BaseURL,
		Config: cfg, IsEnabled: p.IsEnabled, HasCredentials: p.APIKeyValue != nil,
	}
}

func (g *providersGroup) List(w http.ResponseWriter, r *http.Request) {
	if actorFrom(r) == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.List(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var out []providerView
	for i := range rows {
		out = append(out, projectProvider(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *providersGroup) Client(w http.ResponseWriter, r *http.Request) {
	if actorFrom(r) == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.Client(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var out []providerView
	for i := range rows {
		out = append(out, projectProvider(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *providersGroup) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := g.idOrError(w, r)
	if !ok {
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	p, err := g.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectProvider(p))
}

func (g *providersGroup) Create(w http.ResponseWriter, r *http.Request) {
	if actorFrom(r) == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body provider.Provider
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	p, err := g.svc.Create(r.Context(), &body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectProvider(p))
}

func (g *providersGroup) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := g.idOrError(w, r)
	if !ok {
		return
	}
	var body provider.Provider
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	body.ID = id
	p, err := g.svc.Update(r.Context(), &body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectProvider(p))
}

func (g *providersGroup) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := g.idOrError(w, r)
	if !ok {
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *providersGroup) KiloFreeModels(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.KiloFreeModels(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (g *providersGroup) SuggestedModels(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.SuggestedModels(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (g *providersGroup) TestBatch(w http.ResponseWriter, r *http.Request) {
	if actorFrom(r) == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body struct {
		IDs        []uuid.UUID `json:"ids"`
		Concurrent int         `json:"concurrency_limit"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	limit := body.Concurrent
	if limit < 1 {
		limit = 4
	}
	res, err := g.svc.TestBatch(r.Context(), body.IDs, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (g *providersGroup) Validate(w http.ResponseWriter, r *http.Request) {
	if actorFrom(r) == nil {
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
	if err := g.svc.Validate(r.Context(), body.ID); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *providersGroup) idOrError(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, errInvalid)
		return uuid.Nil, false
	}
	return id, true
}
