//go:build prod

// Package frontendassets exposes the production build of the web frontend.
//
// The embedded filesystem is produced by the frontend build into
// frontend/dist and compiled into the binary only when the `prod` build tag
// is set (mandatory for release builds). Development builds compile
// embed_placeholder.go instead and rely on the Vite dev server.
package frontendassets

import "embed"

// FS is the read-only, compiled-in frontend served by
// internal/transport/httpserver.EmbedHandler. The `all:` prefix in the embed
// pattern is required so dotfiles inside frontend/dist are included. The FS
// is immutable at runtime, which is compatible with MemoryDenyWriteExecute
// systemd hardening (no runtime write to the embedded pages is ever needed).
//
//go:embed all:frontend/dist
var FS embed.FS
