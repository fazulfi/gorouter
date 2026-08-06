package realtime

import (
	"context"
	"net/http"
	"time"
)

// ProviderStatusPayload is one provider's quiet status/cooldown refresh.
type ProviderStatusPayload struct {
	Provider      string     `json:"provider"`
	Status        string     `json:"status"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
}

// ProvidersStatusPayload is the initial full-array provider status push.
type ProvidersStatusPayload struct {
	Providers []ProviderStatusPayload `json:"providers"`
}

// ProvidersStreamSource is the read seam for the providers stream. Updates
// signals a status or cooldown change; the handler re-reads the full status
// list and emits a providerStatus event only for providers whose entry
// changed, keeping the stream quiet.
type ProvidersStreamSource interface {
	Updates() <-chan struct{}
	Status(ctx context.Context) ([]ProviderStatusPayload, error)
}

// NewProvidersStream returns the providers SSE handler: an initial
// full-array `providers` push, then a quiet `providerStatus` event per
// provider whose status or cooldown changed, keepalive comments at the
// interval bound, and cleanup when the request context is done. keepalive <=
// 0 selects the registry cadence (25 s).
func NewProvidersStream(src ProvidersStreamSource, keepalive time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := openStream(w)
		if err != nil {
			return
		}
		ctx := r.Context()
		statuses, err := src.Status(ctx)
		if err != nil {
			return
		}
		if err := writeEvent(w, f, ProvidersInitEvent, ProvidersStatusPayload{Providers: statuses}); err != nil {
			return
		}
		prev := make(map[string]ProviderStatusPayload, len(statuses))
		for _, s := range statuses {
			prev[s.Provider] = s
		}
		if c, ok := src.(Canceler); ok {
			defer c.Cancel()
		}
		ticker := time.NewTicker(interval(keepalive))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				writeKeepalive(w, f)
			case <-src.Updates():
				next, err := src.Status(ctx)
				if err != nil {
					return
				}
				for _, s := range next {
					if p, ok := prev[s.Provider]; !ok || !providerStatusEqual(p, s) {
						if err := writeEvent(w, f, ProviderStatusEvent, s); err != nil {
							return
						}
					}
					prev[s.Provider] = s
				}
			}
		}
	}
}

// providerStatusEqual reports whether two provider entries are identical.
func providerStatusEqual(a, b ProviderStatusPayload) bool {
	if a.Provider != b.Provider || a.Status != b.Status {
		return false
	}
	if a.CooldownUntil == nil || b.CooldownUntil == nil {
		return a.CooldownUntil == nil && b.CooldownUntil == nil
	}
	return a.CooldownUntil.Equal(*b.CooldownUntil)
}
