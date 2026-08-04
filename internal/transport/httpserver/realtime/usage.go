package realtime

import (
	"context"
	"net/http"
	"time"
)

// StatsPayload is the full usage statistics push. Field names follow the
// contract UsageStats schema.
type StatsPayload struct {
	TotalRequests  int     `json:"total_requests"`
	TotalTokensIn  int64   `json:"total_tokens_in"`
	TotalTokensOut int64   `json:"total_tokens_out"`
	ActiveRequests int     `json:"active_requests"`
	ErrorRate      float64 `json:"error_rate"`
	Period         string  `json:"period,omitempty"`
}

// ActiveRequestsPayload is the lightweight active-request-count push.
type ActiveRequestsPayload struct {
	ActiveRequests int `json:"active_requests"`
}

// RecentRequestPayload is one row of the Recent Requests display: newest
// first, zero-token rows discarded, deduplicated on
// model|provider|prompt_tokens|completion_tokens|minute, at most 20 rows.
type RecentRequestPayload struct {
	Model            *string `json:"model,omitempty"`
	Provider         *string `json:"provider,omitempty"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Minute           string  `json:"minute,omitempty"`
	Status           *string `json:"status,omitempty"`
}

// RecentRequestsPayload is the full-array Recent Requests replacement: the
// complete computed display array is carried by every recentRequests event.
type RecentRequestsPayload struct {
	Entries []RecentRequestPayload `json:"entries"`
}

// ErrorProviderPayload lists the erroring providers.
type ErrorProviderPayload struct {
	Providers []string `json:"providers"`
}

// UsageStreamSource is the read seam for the usage stream. A production
// implementation reads stats from the usage aggregates and the Recent
// Requests display array from the process-local Recent Requests ring
// (internal/app/usage). Updates signals a state change; on each update the
// handler re-reads and re-pushes the current state, so the recent requests
// array is always a full-array replacement.
type UsageStreamSource interface {
	Updates() <-chan struct{}
	Stats(ctx context.Context) (StatsPayload, error)
	ActiveRequests(ctx context.Context) (int, error)
	RecentRequests(ctx context.Context) ([]RecentRequestPayload, error)
	ErrorProviders(ctx context.Context) ([]string, error)
}

// NewUsageStream returns the usage SSE handler: an initial full `stats`
// push plus lightweight activeRequests/recentRequests/errorProvider pushes,
// keepalive comments at the interval bound, and cleanup when the request
// context is done. On an authenticated reconnect the server re-pushes the
// full current state; no reconnection token is invented and no duplicate
// history is emitted. keepalive <= 0 selects the registry cadence (25 s).
func NewUsageStream(src UsageStreamSource, keepalive time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := openStream(w)
		if err != nil {
			return
		}
		ctx := r.Context()
		if err := pushUsageState(ctx, w, f, src); err != nil {
			return
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
				if err := pushUsageState(ctx, w, f, src); err != nil {
					return
				}
			}
		}
	}
}

// pushUsageState emits the full current usage state: stats, active requests
// count, the complete Recent Requests array (full-array replacement), and
// the erroring providers list.
func pushUsageState(ctx context.Context, w http.ResponseWriter, f http.Flusher, src UsageStreamSource) error {
	stats, err := src.Stats(ctx)
	if err != nil {
		return err
	}
	if err := writeEvent(w, f, UsageStatsEvent, stats); err != nil {
		return err
	}
	active, err := src.ActiveRequests(ctx)
	if err != nil {
		return err
	}
	if err := writeEvent(w, f, UsageActiveRequestsEvent, ActiveRequestsPayload{ActiveRequests: active}); err != nil {
		return err
	}
	recent, err := src.RecentRequests(ctx)
	if err != nil {
		return err
	}
	if err := writeEvent(w, f, UsageRecentRequestsEvent, RecentRequestsPayload{Entries: recent}); err != nil {
		return err
	}
	errs, err := src.ErrorProviders(ctx)
	if err != nil {
		return err
	}
	return writeEvent(w, f, UsageErrorProviderEvent, ErrorProviderPayload{Providers: errs})
}
