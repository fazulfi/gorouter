//go:build !prod

package frontendassets

import "embed"

// FS is the empty placeholder used when built without the `prod` build tag.
// Development serves the frontend from the Vite dev server (proxied at
// :5173), so there is nothing to embed here; a zero-value embed.FS keeps
// httpserver.EmbedHandler from panicking when frontend/dist is absent.
var FS embed.FS
