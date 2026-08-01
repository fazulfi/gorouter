// Package media provides provider-capability-aware modality support for
// embeddings, images, audio, web, and video requests. Every capability and
// limit entry carries an exact source citation (gorouter audit docs or the
// pinned upstream decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1);
// unproven capabilities fail closed.
package media

import "strings"

// Modality identifies a media request kind. Values match the Phase 3 media
// metric label set (phase-3-design.md:1248) and the audited upstream route
// inventory (audit/01-http-contracts.md:544-557).
type Modality string

const (
	// ModalityEmbeddings is POST /api/v1/embeddings (audit/01-http-contracts.md:544).
	ModalityEmbeddings Modality = "embeddings"
	// ModalityImageGeneration is POST /api/v1/images/generations (audit/01-http-contracts.md:545).
	ModalityImageGeneration Modality = "image_generation"
	// ModalityImageToText is the imageToText service kind (audit/02-provider-matrix.md:58).
	ModalityImageToText Modality = "image_to_text"
	// ModalityTTS is POST /api/v1/audio/speech (audit/01-http-contracts.md:546).
	ModalityTTS Modality = "tts"
	// ModalitySTT is POST /api/v1/audio/transcriptions (audit/01-http-contracts.md:547).
	ModalitySTT Modality = "stt"
	// ModalityVoices is GET /api/v1/audio/voices (audit/01-http-contracts.md:548).
	ModalityVoices Modality = "voices"
	// ModalityWebSearch is POST /api/v1/search (audit/01-http-contracts.md:552).
	ModalityWebSearch Modality = "web_search"
	// ModalityWebFetch is POST /api/v1/web/fetch (audit/01-http-contracts.md:553).
	ModalityWebFetch Modality = "web_fetch"
	// ModalityVideoGeneration is POST /api/v1/videos/generations (audit/01-http-contracts.md:554).
	ModalityVideoGeneration Modality = "video_generation"
	// ModalityVideoEdit is POST /api/v1/videos/edits (audit/01-http-contracts.md:555).
	ModalityVideoEdit Modality = "video_edit"
	// ModalityVideoExtension is POST /api/v1/videos/extensions (audit/01-http-contracts.md:556).
	ModalityVideoExtension Modality = "video_extension"
	// ModalityVideoStatus is GET /api/v1/videos/[id] (audit/01-http-contracts.md:557).
	ModalityVideoStatus Modality = "video_status"
)

// allModalities is the exhaustive, ordered modality set. Do not add entries
// without an upstream route or service-kind citation.
var allModalities = []Modality{
	ModalityEmbeddings,
	ModalityImageGeneration,
	ModalityImageToText,
	ModalityTTS,
	ModalitySTT,
	ModalityVoices,
	ModalityWebSearch,
	ModalityWebFetch,
	ModalityVideoGeneration,
	ModalityVideoEdit,
	ModalityVideoExtension,
	ModalityVideoStatus,
}

// AllModalities returns every supported modality in canonical order.
func AllModalities() []Modality {
	out := make([]Modality, len(allModalities))
	copy(out, allModalities)
	return out
}

// Valid reports whether m is a known modality.
func Valid(m Modality) bool {
	for _, known := range allModalities {
		if known == m {
			return true
		}
	}
	return false
}

// ParseModality normalizes a wire-level modality string. Unknown values
// return ok=false so callers can fail closed.
func ParseModality(s string) (Modality, bool) {
	m := Modality(strings.TrimSpace(strings.ToLower(s)))
	if !Valid(m) {
		return "", false
	}
	return m, true
}
