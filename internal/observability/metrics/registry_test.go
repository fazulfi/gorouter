package metrics

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry("testenv")
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}

	// Verify registry is initialized.
	if r.registry == nil {
		t.Error("registry is nil")
	}

	// Verify all metric fields are initialized.
	if r.httpRequestsTotal == nil {
		t.Error("httpRequestsTotal is nil")
	}
	if r.httpRequestDuration == nil {
		t.Error("httpRequestDuration is nil")
	}
	if r.activeRequests == nil {
		t.Error("activeRequests is nil")
	}
	if r.dbLatency == nil {
		t.Error("dbLatency is nil")
	}
	if r.dbPoolConnsInUse == nil {
		t.Error("dbPoolConnsInUse is nil")
	}
	if r.dbPoolIdleConns == nil {
		t.Error("dbPoolIdleConns is nil")
	}
	if r.dbPoolWaitCount == nil {
		t.Error("dbPoolWaitCount is nil")
	}

	// Verify env label is applied and app="gorouter" by recording a metric
	// and comparing the gathered output.
	r.HTTPRequestTotal("check", "GET", "200")
	expected := `
# HELP gorouter_http_requests_total Total number of HTTP requests processed.
# TYPE gorouter_http_requests_total counter
gorouter_http_requests_total{app="gorouter",env="testenv",handler="check",method="GET",status="200"} 1
`
	if err := testutil.GatherAndCompare(r.registry, bytes.NewBufferString(expected), "gorouter_http_requests_total"); err != nil {
		t.Fatalf("unexpected metric output: %v", err)
	}
}

func TestHTTPRequestTotal(t *testing.T) {
	r := NewRegistry("test")

	// Increment counter with different label combinations.
	r.HTTPRequestTotal("h1", "POST", "201")
	r.HTTPRequestTotal("h1", "POST", "201") // double increment
	r.HTTPRequestTotal("h2", "GET", "200")

	expected := `
# HELP gorouter_http_requests_total Total number of HTTP requests processed.
# TYPE gorouter_http_requests_total counter
gorouter_http_requests_total{app="gorouter",env="test",handler="h1",method="POST",status="201"} 2
gorouter_http_requests_total{app="gorouter",env="test",handler="h2",method="GET",status="200"} 1
`
	if err := testutil.GatherAndCompare(r.registry, bytes.NewBufferString(expected), "gorouter_http_requests_total"); err != nil {
		t.Fatalf("unexpected counter output: %v", err)
	}
}

func TestHTTPRequestDuration(t *testing.T) {
	r := NewRegistry("test")

	r.HTTPRequestDuration("h1", "GET", 100*time.Millisecond)
	r.HTTPRequestDuration("h1", "GET", 200*time.Millisecond)

	expected := `
# HELP gorouter_http_request_duration_ms Duration of HTTP requests in milliseconds.
# TYPE gorouter_http_request_duration_ms histogram
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="0.005"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="0.01"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="0.025"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="0.05"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="0.1"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="0.25"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="0.5"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="1"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="2.5"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="5"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="10"} 0
gorouter_http_request_duration_ms_bucket{app="gorouter",env="test",handler="h1",method="GET",le="+Inf"} 2
gorouter_http_request_duration_ms_sum{app="gorouter",env="test",handler="h1",method="GET"} 300
gorouter_http_request_duration_ms_count{app="gorouter",env="test",handler="h1",method="GET"} 2
`
	if err := testutil.GatherAndCompare(r.registry, bytes.NewBufferString(expected), "gorouter_http_request_duration_ms"); err != nil {
		t.Fatalf("unexpected histogram output: %v", err)
	}
}

func TestActiveRequests(t *testing.T) {
	r := NewRegistry("test")

	// Add 5 active requests.
	r.ActiveRequests("h1", 5)

	expected := `
# HELP gorouter_http_active_requests Current number of active HTTP requests.
# TYPE gorouter_http_active_requests gauge
gorouter_http_active_requests{app="gorouter",env="test",handler="h1"} 5
`
	if err := testutil.GatherAndCompare(r.registry, bytes.NewBufferString(expected), "gorouter_http_active_requests"); err != nil {
		t.Fatalf("unexpected gauge output after add: %v", err)
	}

	// Subtract 2 leaving 3.
	r.ActiveRequests("h1", -2)

	expected2 := `
# HELP gorouter_http_active_requests Current number of active HTTP requests.
# TYPE gorouter_http_active_requests gauge
gorouter_http_active_requests{app="gorouter",env="test",handler="h1"} 3
`
	if err := testutil.GatherAndCompare(r.registry, bytes.NewBufferString(expected2), "gorouter_http_active_requests"); err != nil {
		t.Fatalf("unexpected gauge output after subtract: %v", err)
	}
}

func TestDBLatency(t *testing.T) {
	r := NewRegistry("test")

	r.DBLatency("query", 50*time.Millisecond)
	r.DBLatency("insert", 30*time.Millisecond)

	expected := `
# HELP gorouter_db_latency_ms Database operation latency in milliseconds.
# TYPE gorouter_db_latency_ms histogram
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="0.005"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="0.01"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="0.025"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="0.05"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="0.1"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="0.25"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="0.5"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="1"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="2.5"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="5"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="10"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="insert",le="+Inf"} 1
gorouter_db_latency_ms_sum{app="gorouter",env="test",operation="insert"} 30
gorouter_db_latency_ms_count{app="gorouter",env="test",operation="insert"} 1
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="0.005"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="0.01"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="0.025"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="0.05"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="0.1"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="0.25"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="0.5"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="1"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="2.5"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="5"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="10"} 0
gorouter_db_latency_ms_bucket{app="gorouter",env="test",operation="query",le="+Inf"} 1
gorouter_db_latency_ms_sum{app="gorouter",env="test",operation="query"} 50
gorouter_db_latency_ms_count{app="gorouter",env="test",operation="query"} 1
`
	if err := testutil.GatherAndCompare(r.registry, bytes.NewBufferString(expected), "gorouter_db_latency_ms"); err != nil {
		t.Fatalf("unexpected db latency output: %v", err)
	}
}

func TestDBPoolStats(t *testing.T) {
	r := NewRegistry("test")

	stats := DBPoolStats{ConnsInUse: 3, IdleConns: 5, WaitCount: 7}
	r.DBPoolStats(stats)

	if got := testutil.ToFloat64(r.dbPoolConnsInUse); got != 3 {
		t.Errorf("dbPoolConnsInUse = %f, want 3", got)
	}
	if got := testutil.ToFloat64(r.dbPoolIdleConns); got != 5 {
		t.Errorf("dbPoolIdleConns = %f, want 5", got)
	}
	if got := testutil.ToFloat64(r.dbPoolWaitCount); got != 7 {
		t.Errorf("dbPoolWaitCount = %f, want 7", got)
	}
}

func TestNewCounter(t *testing.T) {
	r := NewRegistry("test")

	c := r.NewCounter("my_counter", "A test counter")
	if c == nil {
		t.Fatal("NewCounter returned nil")
	}

	c.Add(10)

	expected := `
# HELP gorouter_my_counter A test counter
# TYPE gorouter_my_counter counter
gorouter_my_counter{app="gorouter"} 10
`
	if err := testutil.GatherAndCompare(r.registry, bytes.NewBufferString(expected), "gorouter_my_counter"); err != nil {
		t.Fatalf("unexpected counter output: %v", err)
	}
}

func TestNewHistogram(t *testing.T) {
	r := NewRegistry("test")

	buckets := []float64{1, 5, 10}
	h := r.NewHistogram("my_histogram", "A test histogram", buckets)
	if h == nil {
		t.Fatal("NewHistogram returned nil")
	}

	h.Observe(3)
	h.Observe(7)
	h.Observe(15)

	expected := `
# HELP gorouter_my_histogram A test histogram
# TYPE gorouter_my_histogram histogram
gorouter_my_histogram_bucket{app="gorouter",le="1"} 0
gorouter_my_histogram_bucket{app="gorouter",le="5"} 1
gorouter_my_histogram_bucket{app="gorouter",le="10"} 2
gorouter_my_histogram_bucket{app="gorouter",le="+Inf"} 3
gorouter_my_histogram_sum{app="gorouter"} 25
gorouter_my_histogram_count{app="gorouter"} 3
`
	if err := testutil.GatherAndCompare(r.registry, bytes.NewBufferString(expected), "gorouter_my_histogram"); err != nil {
		t.Fatalf("unexpected histogram output: %v", err)
	}
}

func TestHandler(t *testing.T) {
	r := NewRegistry("test")

	h := r.Handler()
	if h == nil {
		t.Fatal("Handler() returned nil")
	}

	// Record a metric so there's content in the /metrics output.
	r.HTTPRequestTotal("h1", "GET", "200")

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "gorouter_http_requests_total") {
		t.Errorf("response body missing gorouter_http_requests_total:\n%s", body)
	}
}
