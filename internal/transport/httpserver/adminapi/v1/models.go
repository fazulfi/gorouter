package v1

import (
	"context"
	"net/http"
)

// ModelsService is the application seam for the models group (/models,
// custom, disabled, test, availability). Bodies pass through as raw JSON;
// the backend lane wires the persistence and engine projection.
type ModelsService interface {
	List(ctx context.Context) (any, error)
	Update(ctx context.Context, in any) (any, error)
	CustomList(ctx context.Context) (any, error)
	CustomCreate(ctx context.Context, in any) (any, error)
	CustomDelete(ctx context.Context, in any) error
	DisabledList(ctx context.Context) (any, error)
	DisabledCreate(ctx context.Context, in any) (any, error)
	DisabledDelete(ctx context.Context, in any) error
	Test(ctx context.Context, in any) (any, error)
	Availability(ctx context.Context, in any) (any, error)
}

type modelsGroup struct{ svc ModelsService }

func (g *modelsGroup) guard(w http.ResponseWriter, r *http.Request) bool {
	if g.svc == nil {
		backendUnavailable(w, r)
		return false
	}
	return true
}

func (g *modelsGroup) List(w http.ResponseWriter, r *http.Request) {
	if !g.guard(w, r) {
		return
	}
	out, err := g.svc.List(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *modelsGroup) Update(w http.ResponseWriter, r *http.Request) {
	if actorFrom(r) == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if !g.guard(w, r) {
		return
	}
	out, err := g.svc.Update(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *modelsGroup) CustomList(w http.ResponseWriter, r *http.Request) {
	if !g.guard(w, r) {
		return
	}
	out, err := g.svc.CustomList(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *modelsGroup) CustomCreate(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if !g.guard(w, r) {
		return
	}
	out, err := g.svc.CustomCreate(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (g *modelsGroup) CustomDelete(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if !g.guard(w, r) {
		return
	}
	if err := g.svc.CustomDelete(r.Context(), body); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *modelsGroup) DisabledList(w http.ResponseWriter, r *http.Request) {
	if !g.guard(w, r) {
		return
	}
	out, err := g.svc.DisabledList(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *modelsGroup) DisabledCreate(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if !g.guard(w, r) {
		return
	}
	out, err := g.svc.DisabledCreate(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (g *modelsGroup) DisabledDelete(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if !g.guard(w, r) {
		return
	}
	if err := g.svc.DisabledDelete(r.Context(), body); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *modelsGroup) Test(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if !g.guard(w, r) {
		return
	}
	out, err := g.svc.Test(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *modelsGroup) Availability(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if !g.guard(w, r) {
		return
	}
	out, err := g.svc.Availability(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}