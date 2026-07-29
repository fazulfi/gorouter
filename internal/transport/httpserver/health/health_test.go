package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorouter/internal/transport/httpserver/health"
)

func TestPublicHandler_Returns200(t *testing.T) {
	handler := health.PublicHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestPublicHandler_ContentTypeJSON(t *testing.T) {
	handler := health.PublicHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestPublicHandler_ResponseShape(t *testing.T) {
	handler := health.PublicHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("status = %v, want 'ok'", body["status"])
	}
	if _, ok := body["timestamp"]; !ok {
		t.Error("response should include timestamp")
	}
}

func TestPublicHandler_DoesNotLeakSensitiveInfo(t *testing.T) {
	handler := health.PublicHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, key := range []string{"version", "uptime", "db", "goroutines", "memory"} {
		if _, exists := body[key]; exists {
			t.Errorf("public endpoint should not expose %q", key)
		}
	}
}

func TestDetailedHandler_RequiresAuth(t *testing.T) {
	cfg := health.DetailedHandlerConfig{
		StartTime: time.Now(),
		AuthCheck: func(r *http.Request) bool { return false },
	}
	handler := health.DetailedHandler(cfg)
	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestDetailedHandler_ResponseShape(t *testing.T) {
	startTime := time.Now().Add(-10 * time.Minute)

	cfg := health.DetailedHandlerConfig{
		StartTime: startTime,
		Version:   "1.0.0-test",
		AuthCheck: func(r *http.Request) bool { return true },
	}
	handler := health.DetailedHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["version"] != "1.0.0-test" {
		t.Errorf("version = %v, want '1.0.0-test'", body["version"])
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want 'ok'", body["status"])
	}
	if _, ok := body["uptime"]; !ok {
		t.Error("response should include uptime")
	}
	if _, ok := body["goroutines"]; !ok {
		t.Error("response should include goroutines")
	}
	if _, ok := body["memory"]; !ok {
		t.Error("response should include memory")
	}
	if body["db"] == nil {
		t.Error("response should include db health")
	}
}

func TestDetailedHandler_DBConnected(t *testing.T) {
	cfg := health.DetailedHandlerConfig{
		StartTime: time.Now(),
		Version:   "1.0.0",
		AuthCheck: func(r *http.Request) bool { return true },
		DBPing: func(ctx context.Context) error {
			return nil
		},
	}
	handler := health.DetailedHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("connected DB should return 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	db, ok := body["db"].(map[string]interface{})
	if !ok {
		t.Fatal("db field should be an object")
	}
	if db["connected"] != true {
		t.Errorf("db connected = %v, want true", db["connected"])
	}
}

func TestDetailedHandler_DBDisconnected(t *testing.T) {
	cfg := health.DetailedHandlerConfig{
		StartTime: time.Now(),
		Version:   "1.0.0",
		AuthCheck: func(r *http.Request) bool { return true },
		DBPing: func(ctx context.Context) error {
			return errors.New("db unreachable")
		},
	}
	handler := health.DetailedHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("disconnected DB should return 503, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	db, ok := body["db"].(map[string]interface{})
	if !ok {
		t.Fatal("db field should be an object")
	}
	if db["connected"] != false {
		t.Errorf("db connected = %v, want false", db["connected"])
	}
	if body["status"] != "degraded" {
		t.Errorf("status = %v, want 'degraded'", body["status"])
	}
}

func TestDetailedHandler_PoolStats(t *testing.T) {
	cfg := health.DetailedHandlerConfig{
		StartTime: time.Now(),
		Version:   "1.0.0",
		AuthCheck: func(r *http.Request) bool { return true },
		PoolStats: func() health.DBPoolStats {
			return health.DBPoolStats{
				ConnsInUse: 3,
				IdleConns:  5,
				WaitCount:  0,
			}
		},
	}
	handler := health.DetailedHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	db, ok := body["db"].(map[string]interface{})
	if !ok {
		t.Fatal("db field should be an object")
	}
	ps, ok := db["pool_stats"].(map[string]interface{})
	if !ok {
		t.Fatal("pool_stats should be an object")
	}
	if ps["conns_in_use"] != float64(3) {
		t.Errorf("conns_in_use = %v, want 3", ps["conns_in_use"])
	}
	if ps["idle_conns"] != float64(5) {
		t.Errorf("idle_conns = %v, want 5", ps["idle_conns"])
	}
}

func TestDetailedHandler_VersionFromBuildInfo(t *testing.T) {
	cfg := health.DetailedHandlerConfig{
		StartTime: time.Now(),
		Version:   "",
		AuthCheck: func(r *http.Request) bool { return true },
	}
	handler := health.DetailedHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	version, ok := body["version"].(string)
	if !ok || version == "" {
		t.Errorf("version should be non-empty, got %q", version)
	}
}

func TestDetailedHandler_ContentType(t *testing.T) {
	cfg := health.DetailedHandlerConfig{
		StartTime: time.Now(),
		AuthCheck: func(r *http.Request) bool { return true },
	}
	handler := health.DetailedHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestDetailedHandler_DBPingTimeout(t *testing.T) {
	cfg := health.DetailedHandlerConfig{
		StartTime: time.Now(),
		Version:   "1.0.0",
		AuthCheck: func(r *http.Request) bool { return true },
		DBPing: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	handler := health.DetailedHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	db, ok := body["db"].(map[string]interface{})
	if !ok {
		t.Fatal("db field should be an object")
	}
	if db["connected"] != false {
		t.Errorf("db connected = %v, want false on timeout", db["connected"])
	}
}

func TestDetailedHandler_UptimeIsIncreasing(t *testing.T) {
	startTime := time.Now().Add(-2 * time.Minute)

	cfg := health.DetailedHandlerConfig{
		StartTime: startTime,
		Version:   "1.0.0",
		AuthCheck: func(r *http.Request) bool { return true },
	}
	handler := health.DetailedHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	uptime, ok := body["uptime"].(string)
	if !ok {
		t.Fatal("uptime should be a string")
	}
	if !strings.Contains(uptime, "m") {
		t.Errorf("uptime %q should contain minutes (started 2m ago)", uptime)
	}
}

func TestDetailedHandler_ZeroConfig(t *testing.T) {
	handler := health.DetailedHandler(health.DetailedHandlerConfig{})
	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("zero config should return 200, got %d", rec.Code)
	}
}

func TestDetailedHandler_VersionFromEnv(t *testing.T) {
	t.Setenv("VERSION", "2.0.0-test")

	cfg := health.DetailedHandlerConfig{
		StartTime: time.Now(),
		AuthCheck: func(r *http.Request) bool { return true },
	}
	handler := health.DetailedHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["version"] != "2.0.0-test" {
		t.Errorf("version = %v, want '2.0.0-test'", body["version"])
	}
}

func TestDetailedHandler_NilAuthCheckAllowsAll(t *testing.T) {
	cfg := health.DetailedHandlerConfig{
		StartTime: time.Now(),
	}
	handler := health.DetailedHandler(cfg)
	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("nil AuthCheck should allow access, got %d", rec.Code)
	}
}
