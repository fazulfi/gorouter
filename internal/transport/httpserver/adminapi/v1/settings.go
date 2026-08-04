package v1

import (
	"context"
	"encoding/json"
	"net/http"

	appsettings "gorouter/internal/app/settings"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/settings"
)

// SettingsService is the application seam for the settings group.
type SettingsService interface {
	Get(ctx context.Context, actor *auth.Actor, key string) (*settings.Setting, error)
	List(ctx context.Context, actor *auth.Actor) ([]settings.Setting, error)
	Update(ctx context.Context, actor *auth.Actor, values map[string]json.RawMessage) error
	ProxyTest(ctx context.Context, actor *auth.Actor, in any) (any, error)
	RequireLogin(ctx context.Context, actor *auth.Actor) (*settings.Setting, error)
	SetRequireLogin(ctx context.Context, actor *auth.Actor, enabled bool) error
}

// ConfigTransferService is the application seam for the manual configuration
// transfer surface (/settings/database). Import requires the confirmation
// echo and is destructive/partial per #378.
type ConfigTransferService interface {
	Export(ctx context.Context, actor *auth.Actor) (*appsettings.ConfigPayload, error)
	Import(ctx context.Context, actor *auth.Actor, payload appsettings.ConfigPayload, confirmation string) (*appsettings.ConfigPayload, error)
}

// TranslatorService is the application seam for the translator surface,
// which is feature-gated (D4).
type TranslatorService interface {
	Translate(ctx context.Context, in any) (any, error)
	Load(ctx context.Context) (any, error)
	Save(ctx context.Context, in any) (any, error)
	Send(ctx context.Context, in any) (any, error)
	ConsoleLogs(ctx context.Context) (any, error)
	ClearConsoleLogs(ctx context.Context) error
}

type settingsGroup struct {
	svc       SettingsService
	transfer  ConfigTransferService
	translator TranslatorService
}

func (g *settingsGroup) Get(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.List(r.Context(), actor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (g *settingsGroup) Update(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var values map[string]json.RawMessage
	if err := decodeBody(r, &values); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.Update(r.Context(), actor, values); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (g *settingsGroup) DatabaseGet(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.transfer == nil {
		backendUnavailable(w, r)
		return
	}
	payload, err := g.transfer.Export(r.Context(), actor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (g *settingsGroup) DatabaseImport(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body struct {
		Payload      appsettings.ConfigPayload `json:"payload"`
		Confirmation string                    `json:"confirmation"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.transfer == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := g.transfer.Import(r.Context(), actor, body.Payload, body.Confirmation)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *settingsGroup) RequireLoginGet(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	s, err := g.svc.RequireLogin(r.Context(), actor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (g *settingsGroup) RequireLoginUpdate(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body struct {
		Value bool `json:"value"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.svc.SetRequireLogin(r.Context(), actor, body.Value); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"value": body.Value})
}

func (g *settingsGroup) ProxyTest(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := g.svc.ProxyTest(r.Context(), actor, body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *settingsGroup) Translate(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	g.translatorRun(w, r, func(ctx context.Context) (any, error) {
		return g.translator.Translate(ctx, body)
	})
}

func (g *settingsGroup) TranslatorLoad(w http.ResponseWriter, r *http.Request) {
	if g.translator == nil {
		backendUnavailable(w, r)
		return
	}
	g.translatorRun(w, r, g.translator.Load)
}

func (g *settingsGroup) TranslatorSave(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	g.translatorRun(w, r, func(ctx context.Context) (any, error) {
		return g.translator.Save(ctx, body)
	})
}

func (g *settingsGroup) TranslatorSend(w http.ResponseWriter, r *http.Request) {
	var body any
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	g.translatorRun(w, r, func(ctx context.Context) (any, error) {
		return g.translator.Send(ctx, body)
	})
}

func (g *settingsGroup) TranslatorConsoleLogs(w http.ResponseWriter, r *http.Request) {
	if g.translator == nil {
		backendUnavailable(w, r)
		return
	}
	g.translatorRun(w, r, g.translator.ConsoleLogs)
}

func (g *settingsGroup) TranslatorConsoleLogsClear(w http.ResponseWriter, r *http.Request) {
	if g.translator == nil {
		backendUnavailable(w, r)
		return
	}
	if err := g.translator.ClearConsoleLogs(r.Context()); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *settingsGroup) TranslatorConsoleLogsStream(w http.ResponseWriter, r *http.Request) {
	backendUnavailable(w, r)
}

func (g *settingsGroup) translatorRun(w http.ResponseWriter, r *http.Request, fn func(context.Context) (any, error)) {
	if g.translator == nil {
		backendUnavailable(w, r)
		return
	}
	out, err := fn(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}