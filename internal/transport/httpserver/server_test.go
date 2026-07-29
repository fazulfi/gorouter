package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNew_CreatesChiRouter(t *testing.T) {
	s := New()
	if s == nil {
		t.Fatal("expected non-nil router")
	}
}

func TestNew_WithMiddleware(t *testing.T) {
	called := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}

	s := New(mw)
	if s == nil {
		t.Fatal("expected non-nil router")
	}

	s.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Error("expected middleware to be called")
	}
}

func TestNew_MultipleMiddlewareOrder(t *testing.T) {
	var order []string

	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw1-after")
		})
	}

	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw2-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw2-after")
		})
	}

	s := New(mw1, mw2)
	s.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// mw1 wraps mw2, so order should be: mw1-before, mw2-before, handler, mw2-after, mw1-after
	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	for i, e := range expected {
		if order[i] != e {
			t.Errorf("position %d: expected %q, got %q", i, e, order[i])
		}
	}
}

func TestNew_RouteAttachment(t *testing.T) {
	s := New()

	s.Get("/hello", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("world"))
	})

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "world" {
		t.Errorf("expected body 'world', got %q", rr.Body.String())
	}
}

func TestNew_NoMiddleware(t *testing.T) {
	s := New()
	s.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestNew_NotFound(t *testing.T) {
	s := New()
	s.Get("/exists", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/not-exists", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestNew_MethodNotAllowed(t *testing.T) {
	s := New()
	s.Post("/resource", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestNew_ContextPropagation(t *testing.T) {
	s := New()

	type ctxKey string
	const key ctxKey = "user"
	const expectedValue = "test-user"

	// Middleware that adds value to context
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), key, expectedValue)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	s = New(authMW)
	s.Get("/protected", func(w http.ResponseWriter, r *http.Request) {
		val, ok := r.Context().Value(key).(string)
		if !ok || val != expectedValue {
			t.Errorf("expected context value %q, got %q", expectedValue, val)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestNew_PanicRecoveryViaMiddleware(t *testing.T) {
	recoveryMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}

	s := New(recoveryMW)
	s.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("test")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 after panic, got %d", rr.Code)
	}
}

// Test that New accepts nil in variadic middleware list (graceful handling)
func TestNew_NilMiddleware(t *testing.T) {
	s := New(nil)
	if s == nil {
		t.Fatal("expected non-nil router even with nil middleware")
	}

	s.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// Test that the server can be used with a real HTTP server for graceful shutdown
func TestNew_ServerInterface(t *testing.T) {
	s := New()
	s.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: s,
	}

	// Start server
	go func() {
		_ = server.ListenAndServe()
	}()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Logf("shutdown error (expected on 0 port): %v", err)
	}
}
