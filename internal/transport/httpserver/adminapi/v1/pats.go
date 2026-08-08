package v1

import (
	"context"
	"net/http"
	"time"

	appkeys "gorouter/internal/app/keys"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PATsService is the application seam for the pats group. The concrete
// backend service exposes create/revoke; list/get are wired when the backend
// lane lands them.
type PATsService interface {
	List(ctx context.Context, userID uuid.UUID) ([]appkeys.PAT, error)
	Get(ctx context.Context, id uuid.UUID) (*appkeys.PAT, error)
	Create(ctx context.Context, userID uuid.UUID, description *string, expiresAt *time.Time) (*appkeys.PAT, string, error)
	Revoke(ctx context.Context, id uuid.UUID) error
}

type patsGroup struct{ svc PATsService }

// patView is the credential-safe projection: no token hash is ever exposed.
type patView struct {
	ID          uuid.UUID  `json:"id"`
	Description *string    `json:"description,omitempty"`
	TokenPrefix string     `json:"token_prefix"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// createPATView is the one-time full-value response.
type createPATView struct {
	ID          uuid.UUID `json:"id"`
	Description *string   `json:"description,omitempty"`
	Token       string    `json:"token"`
	TokenPrefix string    `json:"token_prefix"`
	CreatedAt   time.Time `json:"created_at"`
}

func projectPAT(p *appkeys.PAT) patView {
	prefix := ""
	if len(p.TokenHash) > 8 {
		prefix = p.TokenHash[:8]
	}
	return patView{
		ID: p.ID, Description: p.Description, TokenPrefix: prefix,
		ExpiresAt: p.ExpiresAt, LastUsedAt: p.LastUsedAt, RevokedAt: p.RevokedAt,
		CreatedAt: p.CreatedAt,
	}
}

func (g *patsGroup) List(w http.ResponseWriter, r *http.Request) {
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
	out := make([]patView, 0, len(rows))
	for i := range rows {
		out = append(out, projectPAT(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *patsGroup) Create(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body struct {
		Description *string    `json:"description"`
		ExpiresAt   *time.Time `json:"expires_at"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	p, raw, err := g.svc.Create(r.Context(), actor.UserID, body.Description, body.ExpiresAt)
	if err != nil {
		writeError(w, r, err)
		return
	}
	prefix := ""
	if len(raw) > 8 {
		prefix = raw[:8]
	}
	writeJSON(w, http.StatusCreated, createPATView{
		ID: p.ID, Description: p.Description, Token: raw, TokenPrefix: prefix, CreatedAt: p.CreatedAt,
	})
}

func (g *patsGroup) Revoke(w http.ResponseWriter, r *http.Request) {
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
