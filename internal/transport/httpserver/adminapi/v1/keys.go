package v1

import (
	"context"
	"net/http"
	"time"

	"gorouter/internal/domain/keys"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// KeysService is the application seam for the keys group (design §7). The
// concrete backend service exposes create/revoke; list/get/update are wired
// when the backend lane lands them.
type KeysService interface {
	List(ctx context.Context, userID uuid.UUID) ([]keys.APIKey, error)
	Get(ctx context.Context, id uuid.UUID) (*keys.APIKey, error)
	Create(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (*keys.APIKey, string, error)
	Update(ctx context.Context, id uuid.UUID, name string, expiresAt *time.Time) (*keys.APIKey, error)
	Revoke(ctx context.Context, id uuid.UUID) error
}

type keysGroup struct{ svc KeysService }

// apiKeyView is the credential-safe projection: the full value is shown only
// in the one-time create response; list/get expose the prefix only.
type apiKeyView struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// createAPIKeyView is the one-time full-value response.
type createAPIKeyView struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Key       string    `json:"key"`
	KeyPrefix string    `json:"key_prefix"`
	CreatedAt time.Time `json:"created_at"`
}

func projectAPIKey(k *keys.APIKey) apiKeyView {
	return apiKeyView{
		ID: k.ID, Name: k.Name, KeyPrefix: k.KeyPrefix,
		ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt,
		CreatedAt: k.CreatedAt,
	}
}

func (g *keysGroup) List(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.List(r.Context(), actor.UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]apiKeyView, 0, len(rows))
	for i := range rows {
		out = append(out, projectAPIKey(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *keysGroup) Create(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if body.Name == "" {
		writeError(w, r, errInvalid)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	k, raw, err := g.svc.Create(r.Context(), actor.UserID, body.Name, body.ExpiresAt)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, createAPIKeyView{
		ID: k.ID, Name: k.Name, Key: raw, KeyPrefix: k.KeyPrefix, CreatedAt: k.CreatedAt,
	})
}

func (g *keysGroup) Get(w http.ResponseWriter, r *http.Request) {
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
	k, err := g.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectAPIKey(k))
}

func (g *keysGroup) Update(w http.ResponseWriter, r *http.Request) {
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
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	k, err := g.svc.Update(r.Context(), id, body.Name, body.ExpiresAt)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectAPIKey(k))
}

func (g *keysGroup) Delete(w http.ResponseWriter, r *http.Request) {
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
	if err := g.svc.Revoke(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
