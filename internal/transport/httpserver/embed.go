package httpserver

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"gorouter/internal/transport/middleware"
)

// indexFile is the SPA shell document served for the root and for unmatched
// client-side routes.
const indexFile = "index.html"

// hashedAssetRe matches Vite-style content-hashed filenames, e.g.
// "app-1a2b3c4d.js" (8-char hash segment before the extension). Such files
// are content-addressed and can be cached immutably.
var hashedAssetRe = regexp.MustCompile(`-[A-Za-z0-9_-]{8}\.[A-Za-z0-9]+$`)

// inlineScriptRe matches an opening <script> tag so the per-response CSP
// nonce can be injected into inline bootstrap scripts.
var inlineScriptRe = regexp.MustCompile(`(?i)<script\b[^>]*>`)

// EmbedHandler serves the embedded frontend filesystem with the SPA fallback
// contract (design §12, P4-T17):
//
//  1. Explicit allow-list: any path under /api/, any path starting with
//     /health, and /.well-known/* return 404 here and never fall through to
//     the SPA, so a missing API route is never masked.
//  2. Static asset lookup: an existing file under fsys is served with
//     content-type from its extension; hashed assets get immutable caching,
//     HTML gets no-cache.
//  3. SPA fallback: an unmatched GET with no file extension serves
//     index.html. Paths that look like static assets (missing JS/CSS/...) 404
//     instead, and non-GET methods never fall back.
//
// HTML responses pass through middleware.SecurityHeaders so they carry the
// CSP (with a per-response nonce), X-Frame-Options, nosniff, Referrer-Policy
// and Permissions-Policy; inline <script> tags receive the response nonce via
// middleware.NonceFromContext so no 'unsafe-inline' is ever needed.
//
// Wiring: modes.go must mount this handler last, after the model API routes,
// the administration router and the /health endpoints, so registered routes
// take precedence and this handler only ever sees unmatched paths:
//
//	router.Mount("/", httpserver.EmbedHandler(frontendassets.FS))
//
// frontendassets.FS is the embedded production build; the Vite dev server
// (proxied at :5173) serves the frontend in development builds where the
// placeholder FS is empty.
func EmbedHandler(fsys fs.FS) http.Handler {
	if fsys == nil {
		fsys = emptyFS{}
	}
	return embedHandler{fsys: fsys}
}

// emptyFS is a nil-safe fallback so EmbedHandler never panics on an absent
// filesystem; every open fails with fs.ErrNotExist.
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }

type embedHandler struct {
	fsys fs.FS
}

func (h embedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := path.Clean(r.URL.Path)
	if p == "." || p == "/" {
		p = "/" + indexFile
	}
	if blockedPath(p) {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(p, "/")
	if f, err := h.fsys.Open(name); err == nil {
		_ = f.Close()
		h.serveFile(w, r, name)
		return
	}
	if r.Method != http.MethodGet || looksLikeAsset(p) {
		http.NotFound(w, r)
		return
	}
	h.serveHTML(w, r, indexFile)
}

// blockedPath reports whether p is on the explicit allow-list that must 404
// rather than fall through to the SPA (/api/*, /health*, /.well-known/*).
func blockedPath(p string) bool {
	if strings.HasPrefix(p, "/api/") || p == "/api" {
		return true
	}
	if strings.HasPrefix(p, "/health") {
		return true
	}
	if strings.HasPrefix(p, "/.well-known/") || p == "/.well-known" {
		return true
	}
	return false
}

// looksLikeAsset reports whether an unmatched path names a static file rather
// than a client-side route: anything with a non-html extension is treated as
// an asset so a missing asset 404s instead of returning the app shell.
func looksLikeAsset(p string) bool {
	return path.Ext(p) != "" && path.Ext(p) != ".html"
}

func (h embedHandler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	if path.Ext(name) == ".html" {
		h.serveHTML(w, r, name)
		return
	}
	f, err := h.fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if isHashedAsset(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, path.Base(name), time.Time{}, rs)
		return
	}
	data, err := io.ReadAll(f)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(data)
}

// isHashedAsset reports whether name is a content-hashed build artifact (the
// Vite assets directory, or a hash-suffixed filename), which is immutable.
func isHashedAsset(name string) bool {
	return strings.HasPrefix(name, "assets/") || hashedAssetRe.MatchString(path.Base(name))
}

func (h embedHandler) serveHTML(w http.ResponseWriter, r *http.Request, name string) {
	middleware.SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(h.fsys, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if nonce := middleware.NonceFromContext(r.Context()); nonce != "" {
			data = injectNonce(data, nonce)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, bytes.NewReader(data))
	})).ServeHTTP(w, r)
}

// injectNonce adds the per-response CSP nonce to inline <script> tags that
// carry no nonce yet, matching the 'nonce-<...>' directive in the CSP set by
// middleware.SecurityHeaders. External scripts (with a src attribute) are
// left untouched.
func injectNonce(html []byte, nonce string) []byte {
	return inlineScriptRe.ReplaceAllFunc(html, func(tag []byte) []byte {
		if bytes.Contains(tag, []byte("nonce=")) || strings.Contains(strings.ToLower(string(tag)), "src=") {
			return tag
		}
		out := make([]byte, 0, len(tag)+len(nonce)+12)
		out = append(out, tag[:7]...)
		out = append(out, []byte(` nonce="`)...)
		out = append(out, nonce...)
		out = append(out, '"')
		out = append(out, tag[7:]...)
		return out
	})
}
