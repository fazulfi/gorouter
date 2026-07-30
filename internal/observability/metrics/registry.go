// Package metrics provides a Prometheus-based metrics registry with gorouter-
// specific helpers for HTTP request tracking, database latency, and pool stats.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// DBPoolStats holds database connection pool statistics for metrics export.
type DBPoolStats struct {
	ConnsInUse int
	IdleConns  int
	WaitCount  int64
}

// MetricsRegistry wraps a prometheus.Registerer and exposes gorouter-specific
// metric helpers. All metrics are prefixed with "gorouter_" through the
// Namespace field and carry the common labels app="gorouter" and env=<env>.
type MetricsRegistry struct {
	registry *prometheus.Registry

	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	activeRequests      *prometheus.GaugeVec
	dbLatency           *prometheus.HistogramVec
	dbPoolConnsInUse    prometheus.Gauge
	dbPoolIdleConns     prometheus.Gauge
	dbPoolWaitCount     prometheus.Gauge
}

// NewRegistry creates a new MetricsRegistry. The env parameter is used as a
// constant label on all metrics.
func NewRegistry(env string) *MetricsRegistry {
	reg := prometheus.NewRegistry()
	factory := promauto.With(reg)

	common := prometheus.Labels{"app": "gorouter", "env": env}

	r := &MetricsRegistry{
		registry: reg,
		httpRequestsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "gorouter",
			Subsystem:   "http",
			Name:        "requests_total",
			Help:        "Total number of HTTP requests processed.",
			ConstLabels: common,
		}, []string{"handler", "method", "status"}),
		httpRequestDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "gorouter",
			Subsystem:   "http",
			Name:        "request_duration_ms",
			Help:        "Duration of HTTP requests in milliseconds.",
			ConstLabels: common,
			Buckets:     prometheus.DefBuckets,
		}, []string{"handler", "method"}),
		activeRequests: factory.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "gorouter",
			Subsystem:   "http",
			Name:        "active_requests",
			Help:        "Current number of active HTTP requests.",
			ConstLabels: common,
		}, []string{"handler"}),
		dbLatency: factory.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "gorouter",
			Subsystem:   "db",
			Name:        "latency_ms",
			Help:        "Database operation latency in milliseconds.",
			ConstLabels: common,
			Buckets:     prometheus.DefBuckets,
		}, []string{"operation"}),
		dbPoolConnsInUse: factory.NewGauge(prometheus.GaugeOpts{
			Namespace:   "gorouter",
			Subsystem:   "db",
			Name:        "pool_conns_in_use",
			Help:        "Current number of database connections in use.",
			ConstLabels: common,
		}),
		dbPoolIdleConns: factory.NewGauge(prometheus.GaugeOpts{
			Namespace:   "gorouter",
			Subsystem:   "db",
			Name:        "pool_idle_conns",
			Help:        "Current number of idle database connections.",
			ConstLabels: common,
		}),
		dbPoolWaitCount: factory.NewGauge(prometheus.GaugeOpts{
			Namespace:   "gorouter",
			Subsystem:   "db",
			Name:        "pool_wait_count",
			Help:        "Total number of waits on the database pool.",
			ConstLabels: common,
		}),
	}

	return r
}

// HTTPRequestTotal increments the HTTP request counter for the given handler,
// method, and status code.
func (r *MetricsRegistry) HTTPRequestTotal(handler, method, status string) {
	r.httpRequestsTotal.WithLabelValues(handler, method, status).Inc()
}

// HTTPRequestDuration observes an HTTP request duration in milliseconds.
func (r *MetricsRegistry) HTTPRequestDuration(handler, method string, dur time.Duration) {
	r.httpRequestDuration.WithLabelValues(handler, method).Observe(float64(dur.Milliseconds()))
}

// ActiveRequests increments the active request gauge for a handler.
func (r *MetricsRegistry) ActiveRequests(handler string, delta int) {
	r.activeRequests.WithLabelValues(handler).Add(float64(delta))
}

// DBLatency observes a database operation latency in milliseconds.
func (r *MetricsRegistry) DBLatency(operation string, dur time.Duration) {
	r.dbLatency.WithLabelValues(operation).Observe(float64(dur.Milliseconds()))
}

// DBPoolStats updates the three database pool gauge metrics.
func (r *MetricsRegistry) DBPoolStats(stats DBPoolStats) {
	r.dbPoolConnsInUse.Set(float64(stats.ConnsInUse))
	r.dbPoolIdleConns.Set(float64(stats.IdleConns))
	r.dbPoolWaitCount.Set(float64(stats.WaitCount))
}

// NewCounter creates and registers a new counter metric with the gorouter_
// prefix and common labels.
func (r *MetricsRegistry) NewCounter(name, help string) prometheus.Counter {
	return promauto.With(r.registry).NewCounter(prometheus.CounterOpts{
		Namespace:   "gorouter",
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels{"app": "gorouter"},
	})
}

// NewHistogram creates and registers a new histogram metric with the gorouter_
// prefix, common labels, and the supplied buckets.
func (r *MetricsRegistry) NewHistogram(name, help string, buckets []float64) prometheus.Histogram {
	return promauto.With(r.registry).NewHistogram(prometheus.HistogramOpts{
		Namespace:   "gorouter",
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels{"app": "gorouter"},
		Buckets:     buckets,
	})
}

// Handler returns an http.Handler that serves Prometheus metrics at /metrics.
func (r *MetricsRegistry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}
