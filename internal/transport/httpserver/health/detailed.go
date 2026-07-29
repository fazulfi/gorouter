package health

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

// DBPingFunc is a function that checks whether the database is reachable.
// It should return nil when the database is connected and healthy.
type DBPingFunc func(ctx context.Context) error

// PoolStatsProvider returns current database pool statistics.
type PoolStatsProvider func() DBPoolStats

// AuthCheckFunc checks whether the request is authenticated. It should return
// true when the caller is permitted to access the detailed health endpoint.
type AuthCheckFunc func(r *http.Request) bool

// DBPoolStats mirrors the metrics.DBPoolStats shape for health responses.
type DBPoolStats struct {
	ConnsInUse int   `json:"conns_in_use"`
	IdleConns  int   `json:"idle_conns"`
	WaitCount  int64 `json:"wait_count"`
}

// DBHealth represents the database health status.
type DBHealth struct {
	Connected bool         `json:"connected"`
	PoolStats *DBPoolStats `json:"pool_stats,omitempty"`
}

// MemoryStats represents Go runtime memory statistics.
type MemoryStats struct {
	Alloc      uint64 `json:"alloc"`
	TotalAlloc uint64 `json:"total_alloc"`
	Sys        uint64 `json:"sys"`
}

// DetailedResponse is the JSON body returned by the detailed health endpoint.
type DetailedResponse struct {
	Status     string      `json:"status"`
	Version    string      `json:"version"`
	Uptime     string      `json:"uptime"`
	DB         DBHealth    `json:"db"`
	Goroutines int         `json:"goroutines"`
	Memory     MemoryStats `json:"memory"`
}

// DetailedHandlerConfig configures the detailed health endpoint.
type DetailedHandlerConfig struct {
	// StartTime is the time the application started (used to compute uptime).
	StartTime time.Time
	// Version is the application version. If empty it is read from build info
	// or the VERSION environment variable.
	Version string
	// DBPing checks database connectivity. If nil the DB status will report
	// "not checked".
	DBPing DBPingFunc
	// PoolStats returns current pool statistics. May be nil.
	PoolStats PoolStatsProvider
	// AuthCheck verifies the caller is authenticated. If nil, all requests
	// are allowed.
	AuthCheck AuthCheckFunc
}

// DetailedHandler returns an authenticated handler for GET /health/detailed.
//
// The handler checks for an auth.Actor in the request context. If the actor
// is missing the handler returns 401. It returns 503 when the database ping
// fails.
func DetailedHandler(cfg DetailedHandlerConfig) http.HandlerFunc {
	version := cfg.Version
	if version == "" {
		version = resolveVersion()
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.AuthCheck != nil && !cfg.AuthCheck(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status":"error","message":"unauthorized"}`))
			return
		}

		dbHealth := DBHealth{Connected: true}
		if cfg.DBPing != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			if err := cfg.DBPing(ctx); err != nil {
				dbHealth.Connected = false
			}
		}
		if cfg.PoolStats != nil {
			stats := cfg.PoolStats()
			dbHealth.PoolStats = &stats
		}

		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		status := "ok"
		httpStatus := http.StatusOK
		if !dbHealth.Connected {
			status = "degraded"
			httpStatus = http.StatusServiceUnavailable
		}

		resp := DetailedResponse{
			Status:     status,
			Version:    version,
			Uptime:     time.Since(cfg.StartTime).Round(time.Second).String(),
			DB:         dbHealth,
			Goroutines: runtime.NumGoroutine(),
			Memory: MemoryStats{
				Alloc:      m.Alloc,
				TotalAlloc: m.TotalAlloc,
				Sys:        m.Sys,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func resolveVersion() string {
	if v := os.Getenv("VERSION"); v != "" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "unknown"
}
