package v1

import (
	"context"
	"net/http"
	"time"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/pricing"

	"github.com/google/uuid"
)

// PricingService is the application seam for the pricing group. Overrides
// receive the actor so the service stamps and audits the author.
type PricingService interface {
	ListOverrides(ctx context.Context) ([]pricing.PriceOverride, error)
	ApplyOverride(ctx context.Context, actor *auth.Actor, override pricing.PriceOverride) (pricing.PriceOverride, error)
	Reset(ctx context.Context, actor *auth.Actor) error
}

type pricingGroup struct{ svc PricingService }

type pricingView struct {
	ModelID     string    `json:"model_id"`
	ProviderID  string    `json:"provider_id"`
	InputPrice  float64   `json:"input_price"`
	OutputPrice float64   `json:"output_price"`
	UpdatedBy   uuid.UUID `json:"updated_by,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

func projectOverride(o pricing.PriceOverride) pricingView {
	return pricingView{
		ModelID: o.ModelID, ProviderID: o.ProviderID,
		InputPrice: o.InputPrice, OutputPrice: o.OutputPrice,
		UpdatedBy: o.UpdatedBy, UpdatedAt: o.UpdatedAt,
	}
}

func (g *pricingGroup) Get(w http.ResponseWriter, r *http.Request) {
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.ListOverrides(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var out []pricingView
	for i := range rows {
		out = append(out, projectOverride(rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *pricingGroup) Update(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body pricing.PriceOverride
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	override, err := g.svc.ApplyOverride(r.Context(), actor, body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectOverride(override))
}

func (g *pricingGroup) Reset(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Reset(r.Context(), actor); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
