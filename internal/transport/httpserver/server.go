package httpserver

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func New(middleware ...func(http.Handler) http.Handler) *chi.Mux {
	r := chi.NewRouter()
	for _, mw := range middleware {
		if mw != nil {
			r.Use(mw)
		}
	}
	return r
}
