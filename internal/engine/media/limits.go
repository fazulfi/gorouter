package media

// Limit is one numeric bound for a modality. Defined=false means no upstream
// numeric authority exists at the pinned commit; the Value is then the
// transport-safe ceiling derived from the platform authority in Authority
// (phase-3-design.md §10.4). Defined=true entries quote the exact upstream
// line.
type Limit struct {
	Name      string
	Value     int64
	Unit      string
	Defined   bool
	Authority string
}

// up derives a Defined limit from an upstream citation.
func up(name string, value int64, unit, authority string) Limit {
	return Limit{Name: name, Value: value, Unit: unit, Defined: true, Authority: authority}
}

// platform derives a transport-safe ceiling from the platform body authority.
func platform(name string, value int64, unit string) Limit {
	return Limit{Name: name, Value: value, Unit: unit, Defined: false,
		Authority: BodyCeilingAuthority + " (modality-specific upstream limit not defined; transport ceiling applied)"}
}

// limits is the exhaustive per-modality limit registry. Every entry's
// Authority cites the exact upstream file:line at
// decolua/9router 79918c7830695bbca4a45c9fea4a42c3e9fd73d1.
var limits = map[Modality][]Limit{
	ModalityEmbeddings: {
		// No upstream max-token authority exists: the design's illustrative
		// "providers/openai.js:312 — 8192 tokens" citation is absent from the
		// pinned commit (registry/openai.js is 81 lines, no token fields).
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("jina_dimensions_large", 1024, "dimensions", "upstream open-sse/providers/registry/jina-ai.js:28"),
		up("jina_dimensions_small", 768, "dimensions", "upstream open-sse/providers/registry/jina-ai.js:33"),
	},
	ModalityImageGeneration: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("default_n", 1, "images", "upstream open-sse/handlers/imageProviders/openai.js:18"),
		up("default_size", 1024, "px", "upstream open-sse/handlers/imageProviders/openai.js:18 (1024x1024)"),
		up("poll_interval_ms", 1500, "ms", "upstream open-sse/handlers/imageProviders/_base.js:3"),
		up("poll_timeout_ms", 120000, "ms", "upstream open-sse/handlers/imageProviders/_base.js:4"),
	},
	ModalityImageToText: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
	},
	ModalityTTS: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("min_audio_bytes_elevenlabs", 1024, "bytes", "upstream open-sse/handlers/ttsProviders/elevenlabs.js:45"),
		up("min_audio_bytes_edge", 1024, "bytes", "upstream open-sse/handlers/ttsProviders/edgeTts.js:86"),
		up("min_audio_bytes_generic", 100, "bytes", "upstream open-sse/handlers/ttsProviders/_base.js:9"),
		up("voices_ttl_ms", 86400000, "ms", "upstream open-sse/handlers/ttsProviders/elevenlabs.js:4 + edgeTts.js:6 (24h)"),
	},
	ModalitySTT: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("route_max_duration_s", 300, "seconds", "upstream src/app/api/v1/audio/transcriptions/route.js:4 (maxDuration = 300)"),
		up("assemblyai_poll_timeout_ms", 120000, "ms", "upstream open-sse/handlers/sttCore.js:76"),
		up("assemblyai_poll_interval_ms", 2000, "ms", "upstream open-sse/handlers/sttCore.js:77"),
	},
	ModalityVoices: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("voices_ttl_ms", 86400000, "ms", "upstream open-sse/handlers/ttsProviders/elevenlabs.js:4 + edgeTts.js:6 (24h)"),
	},
	ModalityWebSearch: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("global_timeout_ms", 15000, "ms", "upstream open-sse/handlers/search/index.js:14 (GLOBAL_TIMEOUT_MS)"),
		up("default_max_results", 5, "results", "upstream open-sse/providers/registry/*.js defaultMaxResults (brave-search.js:30, exa.js:31, google-pse.js:30, linkup.js:29, searchapi.js:30, searxng.js:30, serper.js:30, tavily.js:31, youcom.js:30)"),
	},
	ModalityWebFetch: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("default_timeout_ms", 15000, "ms", "upstream open-sse/handlers/fetch/index.js:4 (DEFAULT_TIMEOUT_MS)"),
		up("max_characters_firecrawl", 200000, "chars", "upstream open-sse/providers/registry/firecrawl.js:31"),
		up("max_characters_jina", 200000, "chars", "upstream open-sse/providers/registry/jina-reader.js:31"),
		up("max_characters_exa_tavily", 100000, "chars", "upstream open-sse/providers/registry/exa.js:47 + tavily.js:47"),
	},
	ModalityVideoGeneration: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("round_trip_timeout_ms", 120000, "ms", "upstream open-sse/handlers/videoCore.js:8 (VIDEO_FETCH_TIMEOUT_MS)"),
		up("error_slice", 2000, "bytes", "upstream open-sse/handlers/videoCore.js:152"),
	},
	ModalityVideoEdit: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("round_trip_timeout_ms", 120000, "ms", "upstream open-sse/handlers/videoCore.js:8"),
	},
	ModalityVideoExtension: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("round_trip_timeout_ms", 120000, "ms", "upstream open-sse/handlers/videoCore.js:8"),
	},
	ModalityVideoStatus: {
		platform("max_body_bytes", BodyCeiling, "bytes"),
		up("round_trip_timeout_ms", 120000, "ms", "upstream open-sse/handlers/videoCore.js:8"),
		// Client-side poll defaults are a CLI-platform authority, not an
		// upstream numeric limit: cli default 5s poll interval / 600s timeout
		// (audit/08-cli-host-integration.md:142,435).
		up("cli_poll_interval_ms", 5000, "ms", "audit/08-cli-host-integration.md:435"),
		up("cli_wait_timeout_ms", 600000, "ms", "audit/08-cli-host-integration.md:142"),
	},
}

// Limits returns the limit registry for a modality in canonical order.
func Limits(m Modality) []Limit {
	entries := limits[m]
	out := make([]Limit, len(entries))
	copy(out, entries)
	return out
}

// AllLimits returns every limit entry across all modalities (exhaustive;
// used by the authority-coverage test).
func AllLimits() []Limit {
	var out []Limit
	for _, m := range allModalities {
		out = append(out, limits[m]...)
	}
	return out
}
