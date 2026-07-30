package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCorrelation_GeneratesRequestID(t *testing.T) {
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id == "" {
			t.Error("expected non-empty request ID")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCorrelation_GeneratesRequestID_NotEmpty(t *testing.T) {
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if len(id) < 8 {
			t.Errorf("request ID too short: %q (len=%d)", id, len(id))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	_ = rr
}

func TestCorrelation_ReadsXRequestID(t *testing.T) {
	const expectedID = "req-abc-123"

	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id != expectedID {
			t.Errorf("expected %q, got %q", expectedID, id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", expectedID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
}

func TestCorrelation_ReadsXCorrelationID(t *testing.T) {
	const expectedID = "corr-456-xyz"

	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id != expectedID {
			t.Errorf("expected %q, got %q", expectedID, id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Correlation-ID", expectedID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
}

func TestCorrelation_ReadsXTraceID(t *testing.T) {
	const expectedID = "trace-789-def"

	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id != expectedID {
			t.Errorf("expected %q, got %q", expectedID, id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Trace-ID", expectedID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
}

func TestCorrelation_XRequestIDPriority(t *testing.T) {
	// When multiple correlation headers are set, X-Request-ID should take priority
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id != "primary" {
			t.Errorf("expected X-Request-ID to take priority, got %q", id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "primary")
	req.Header.Set("X-Correlation-ID", "secondary")
	req.Header.Set("X-Trace-ID", "tertiary")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
}

func TestCorrelation_ResponseHeaderSet(t *testing.T) {
	var capturedID string
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("X-Request-ID") != capturedID {
		t.Errorf("response X-Request-ID header mismatch: got %q, expected %q",
			rr.Header().Get("X-Request-ID"), capturedID)
	}
}

func TestCorrelation_ContextPropagation(t *testing.T) {
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		// Pass to sub-context
		subCtx := context.WithValue(r.Context(), testKey("test"), id)
		retrieved := subCtx.Value(testKey("test")).(string)
		if retrieved != id {
			t.Errorf("context propagation failed: %q vs %q", retrieved, id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
}

type testKey string

func TestCorrelation_UniquePerRequest(t *testing.T) {
	ids := make(chan string, 2)
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids <- GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/1", nil)
	req2 := httptest.NewRequest(http.MethodGet, "/2", nil)

	rr1 := httptest.NewRecorder()
	rr2 := httptest.NewRecorder()

	handler.ServeHTTP(rr1, req1)
	handler.ServeHTTP(rr2, req2)

	id1 := <-ids
	id2 := <-ids

	if id1 == id2 {
		t.Error("expected unique request IDs for different requests")
	}
}

func TestGetRequestID_EmptyContext(t *testing.T) {
	ctx := context.Background()
	id := GetRequestID(ctx)
	if id != "" {
		t.Errorf("expected empty ID from empty context, got %q", id)
	}
}

func TestCorrelation_MissingHeaders_GeneratesValidID(t *testing.T) {
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id == "" {
			t.Fatal("expected a generated ID")
		}
		// Should be hex or UUID-like
		if strings.ContainsAny(id, " \t\n") {
			t.Errorf("request ID contains whitespace: %q", id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No correlation headers set at all
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
}

func TestCorrelation_EmptyHeaderValue(t *testing.T) {
	handler := Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id == "" {
			t.Fatal("expected a generated ID when header value is empty")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
}
