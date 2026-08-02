package providers

import (
	"fmt"

	"github.com/google/uuid"
)

// GenerateCloudflareConfig returns the deterministic Cloudflare Worker relay
// payload for a proxy pool. It is a pure config generator: no network calls,
// no cloud dependency, and identical inputs always produce identical output
// (decisions #156, #379).
func GenerateCloudflareConfig(poolID uuid.UUID, baseURL string) map[string]any {
	return map[string]any{
		"name":     "gorouter-pool-" + poolID.String(),
		"pool_id":  poolID.String(),
		"base_url": baseURL,
		"script":   relayScript("Cloudflare Worker", baseURL),
	}
}

// GenerateDenoConfig returns the deterministic Deno Deploy relay payload for
// a proxy pool: a deno.json manifest plus the relay module. Pure generator,
// no external calls.
func GenerateDenoConfig(poolID uuid.UUID, baseURL string) map[string]any {
	return map[string]any{
		"name":      "gorouter-pool-" + poolID.String(),
		"pool_id":   poolID.String(),
		"base_url":  baseURL,
		"deno_json": map[string]any{"name": "gorouter-pool-relay", "version": "1.0.0", "tasks": map[string]any{"start": "deno run --allow-net main.ts"}},
		"main_ts":   relayScript("Deno Deploy", baseURL),
	}
}

// GenerateVercelConfig returns the deterministic Vercel edge-runtime relay
// payload for a proxy pool: a vercel.json manifest plus the edge function.
// Pure generator, no external calls.
func GenerateVercelConfig(poolID uuid.UUID, baseURL string) map[string]any {
	return map[string]any{
		"name":          "gorouter-pool-" + poolID.String(),
		"pool_id":       poolID.String(),
		"base_url":      baseURL,
		"vercel_json":   map[string]any{"functions": map[string]any{"api/relay.js": map[string]any{"runtime": "edge"}}, "routes": []map[string]any{{"src": "/.*", "dest": "/api/relay.js"}}},
		"edge_function": relayScript("Vercel Edge", baseURL),
	}
}

// relayScript is the deterministic relay body shared by the deploy payloads.
// The gateway address is embedded at generation time so the deployed relay
// needs no external configuration service.
func relayScript(runtime, baseURL string) string {
	return fmt.Sprintf(`// %s relay for gorouter proxy pools
export default {
  async fetch(request) {
    const target = new URL(request.url, %q);
    const relay = new Request(target, request);
    relay.headers.set("x-gorouter-pool-relay", "true");
    return fetch(relay);
  }
}`, runtime, baseURL)
}
