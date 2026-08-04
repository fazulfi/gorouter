package v1

import (
	"context"
	"net/http"
)

// MediaService is the application seam for the media-providers group. The
// TTS voice routes pass the provider kind and optional language through.
type MediaService interface {
	Voices(ctx context.Context, providerKind, providerID, lang string) (any, error)
}

type mediaGroup struct{ svc MediaService }

func (g *mediaGroup) voices(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g.svc == nil {
			backendUnavailable(w, r)
			return
		}
		out, err := g.svc.Voices(r.Context(), kind, r.URL.Query().Get("provider_id"), r.URL.Query().Get("lang"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (g *mediaGroup) TTSVoices(w http.ResponseWriter, r *http.Request)       { g.voices("tts")(w, r) }
func (g *mediaGroup) DeepgramVoices(w http.ResponseWriter, r *http.Request)   { g.voices("deepgram")(w, r) }
func (g *mediaGroup) ElevenlabsVoices(w http.ResponseWriter, r *http.Request) { g.voices("elevenlabs")(w, r) }
func (g *mediaGroup) InworldVoices(w http.ResponseWriter, r *http.Request)    { g.voices("inworld")(w, r) }
func (g *mediaGroup) MinimaxVoices(w http.ResponseWriter, r *http.Request)    { g.voices("minimax")(w, r) }