package v1

import (
	"context"
	"net/http"
)

// OAuthService is the application seam for the oauth group, including the
// per-provider status/connect/disconnect surface and the exact upstream
// import flows (codex, cursor, gitlab, iflow, kiro). Token-bearing bodies
// pass through and are redacted by the service at persist time.
type OAuthService interface {
	Status(ctx context.Context, provider string) (any, error)
	Connect(ctx context.Context, provider string, in any) (any, error)
	Disconnect(ctx context.Context, provider string) (any, error)
	Import(ctx context.Context, flow string, in any) (any, error)
}

type oauthGroup struct{ svc OAuthService }

func (g *oauthGroup) provider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g.svc == nil {
			backendUnavailable(w, r)
			return
		}
		provider := pathParam(r, "provider")
		switch r.Method {
		case http.MethodGet:
			out, err := g.svc.Status(r.Context(), provider)
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, out)
		case http.MethodPost:
			var body any
			if err := decodeBody(r, &body); err != nil {
				writeError(w, r, err)
				return
			}
			out, err := g.svc.Connect(r.Context(), provider, body)
			if err != nil {
				writeError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, out)
		case http.MethodDelete:
			if _, err := g.svc.Disconnect(r.Context(), provider); err != nil {
				writeError(w, r, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			writeError(w, r, errInvalid)
		}
	}
}

func (g *oauthGroup) importFlow(flow string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g.svc == nil {
			backendUnavailable(w, r)
			return
		}
		var body any
		if err := decodeBody(r, &body); err != nil {
			writeError(w, r, err)
			return
		}
		out, err := g.svc.Import(r.Context(), flow, body)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (g *oauthGroup) ProviderGet(w http.ResponseWriter, r *http.Request)        { g.provider()(w, r) }
func (g *oauthGroup) ProviderConnect(w http.ResponseWriter, r *http.Request)    { g.provider()(w, r) }
func (g *oauthGroup) ProviderDisconnect(w http.ResponseWriter, r *http.Request) { g.provider()(w, r) }
func (g *oauthGroup) CodexImportToken(w http.ResponseWriter, r *http.Request) {
	g.importFlow("codex/import-token")(w, r)
}
func (g *oauthGroup) CodexBulkImport(w http.ResponseWriter, r *http.Request) {
	g.importFlow("codex/bulk-import")(w, r)
}
func (g *oauthGroup) CursorImport(w http.ResponseWriter, r *http.Request) {
	g.importFlow("cursor/import")(w, r)
}
func (g *oauthGroup) CursorAutoImport(w http.ResponseWriter, r *http.Request) {
	g.importFlow("cursor/auto-import")(w, r)
}
func (g *oauthGroup) GitlabPAT(w http.ResponseWriter, r *http.Request) {
	g.importFlow("gitlab/pat")(w, r)
}
func (g *oauthGroup) IflowCookie(w http.ResponseWriter, r *http.Request) {
	g.importFlow("iflow/cookie")(w, r)
}
func (g *oauthGroup) KiroImport(w http.ResponseWriter, r *http.Request) {
	g.importFlow("kiro/import")(w, r)
}
func (g *oauthGroup) KiroAutoImport(w http.ResponseWriter, r *http.Request) {
	g.importFlow("kiro/auto-import")(w, r)
}
func (g *oauthGroup) KiroImportCLIProxy(w http.ResponseWriter, r *http.Request) {
	g.importFlow("kiro/import-cli-proxy")(w, r)
}
func (g *oauthGroup) KiroAPIKey(w http.ResponseWriter, r *http.Request) {
	g.importFlow("kiro/api-key")(w, r)
}
func (g *oauthGroup) KiroSocialAuthorize(w http.ResponseWriter, r *http.Request) {
	g.importFlow("kiro/social-authorize")(w, r)
}
func (g *oauthGroup) KiroSocialExchange(w http.ResponseWriter, r *http.Request) {
	g.importFlow("kiro/social-exchange")(w, r)
}
