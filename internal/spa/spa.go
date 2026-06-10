package spa

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Handler serves a Vue SPA from a dist directory.
// Static files (assets/, favicon.ico) are served directly.
// All other paths fall back to index.html for client-side routing.
type Handler struct {
	distDir string
	fs      http.Handler
}

// NewHandler creates a SPA handler rooted at distDir.
func NewHandler(distDir string) *Handler {
	return &Handler{
		distDir: distDir,
		fs:      http.FileServer(http.Dir(distDir)),
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path

	// Serve static assets directly — if the file exists on disk, serve it.
	if h.isStaticFile(p) {
		h.fs.ServeHTTP(w, r)
		return
	}

	// Asset paths that don't exist on disk get a hard 404 (not the SPA fallback).
	// This prevents the SPA from being served for missing JS/CSS/image files.
	if isAssetPath(p) {
		http.NotFound(w, r)
		return
	}

	// SPA fallback: serve index.html for all other routes (client-side routing).
	http.ServeFile(w, r, filepath.Join(h.distDir, "index.html"))
}

// isAssetPath returns true for paths that should be actual files, not SPA routes.
func isAssetPath(p string) bool {
	return strings.HasPrefix(p, "/assets/") ||
		strings.HasSuffix(p, ".js") ||
		strings.HasSuffix(p, ".css") ||
		strings.HasSuffix(p, ".map") ||
		strings.HasSuffix(p, ".png") ||
		strings.HasSuffix(p, ".jpg") ||
		strings.HasSuffix(p, ".svg") ||
		strings.HasSuffix(p, ".ico") ||
		strings.HasSuffix(p, ".woff") ||
		strings.HasSuffix(p, ".woff2")
}

// isStaticFile returns true if the path corresponds to an actual file on disk.
// This covers /assets/*, /favicon.ico, and any other real files in dist/.
func (h *Handler) isStaticFile(urlPath string) bool {
	// Prevent directory traversal
	if strings.Contains(urlPath, "..") {
		return false
	}

	fp := filepath.Join(h.distDir, filepath.FromSlash(urlPath))
	info, err := os.Stat(fp)
	if err != nil {
		return false
	}
	// Only serve files, not directories (directories fall through to SPA).
	return !info.IsDir()
}
