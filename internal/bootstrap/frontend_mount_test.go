//go:build prod

package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMountFrontendServesEmbeddedIndex(t *testing.T) {
	router := chi.NewRouter()
	mountFrontend(router)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "<!DOCTYPE html>") {
		t.Fatalf("GET / body does not contain frontend index")
	}
}
