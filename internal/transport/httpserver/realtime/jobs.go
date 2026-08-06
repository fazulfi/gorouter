package realtime

import (
	"context"
	"net/http"
	"time"
)

// JobRunPayload is one job's current or last run status event.
type JobRunPayload struct {
	Type   string     `json:"type"`
	Status string     `json:"status"`
	At     *time.Time `json:"at,omitempty"`
}

// JobsSnapshot carries the scheduler current and last run status per job
// type.
type JobsSnapshot struct {
	Current []JobRunPayload `json:"current"`
	Last    []JobRunPayload `json:"last"`
}

// JobsStreamSource is the read seam for the jobs stream. Updates signals a
// scheduler status change; the handler re-reads the snapshot and emits a
// jobStatus event only for job types whose current run changed.
type JobsStreamSource interface {
	Updates() <-chan struct{}
	Snapshot(ctx context.Context) (JobsSnapshot, error)
}

// NewJobsStream returns the jobs SSE handler: an initial `jobs` snapshot
// (current and last run status per job type), then a quiet `jobStatus`
// event per job transition, keepalive comments at the interval bound, and
// cleanup when the request context is done. keepalive <= 0 selects the
// registry cadence (25 s).
func NewJobsStream(src JobsStreamSource, keepalive time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := openStream(w)
		if err != nil {
			return
		}
		ctx := r.Context()
		snap, err := src.Snapshot(ctx)
		if err != nil {
			return
		}
		if err := writeEvent(w, f, JobsInitEvent, snap); err != nil {
			return
		}
		prev := currentByType(snap.Current)
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
				next, err := src.Snapshot(ctx)
				if err != nil {
					return
				}
				for _, j := range next.Current {
					if p, ok := prev[j.Type]; !ok || !jobRunEqual(p, j) {
						if err := writeEvent(w, f, JobStatusEvent, j); err != nil {
							return
						}
					}
					prev[j.Type] = j
				}
			}
		}
	}
}

// currentByType indexes the current run list by job type.
func currentByType(rows []JobRunPayload) map[string]JobRunPayload {
	m := make(map[string]JobRunPayload, len(rows))
	for _, j := range rows {
		m[j.Type] = j
	}
	return m
}

// jobRunEqual reports whether two job runs are identical.
func jobRunEqual(a, b JobRunPayload) bool {
	if a.Type != b.Type || a.Status != b.Status {
		return false
	}
	if a.At == nil || b.At == nil {
		return a.At == nil && b.At == nil
	}
	return a.At.Equal(*b.At)
}
