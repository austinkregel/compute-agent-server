package spa

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupTestDist(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create index.html
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>SPA</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a CSS asset
	if err := os.WriteFile(filepath.Join(assetsDir, "style.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create favicon
	if err := os.WriteFile(filepath.Join(dir, "favicon.ico"), []byte("icon"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHandler_ServesIndex(t *testing.T) {
	dist := setupTestDist(t)
	h := NewHandler(dist)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET / status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); body != "<html>SPA</html>" {
		t.Errorf("GET / body = %q", body)
	}
}

func TestHandler_ServesSPAFallback(t *testing.T) {
	dist := setupTestDist(t)
	h := NewHandler(dist)

	// SPA route — should serve index.html
	req := httptest.NewRequest("GET", "/client/some-id", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /client/some-id status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); body != "<html>SPA</html>" {
		t.Errorf("GET /client/some-id body = %q", body)
	}
}

func TestHandler_ServesAssets(t *testing.T) {
	dist := setupTestDist(t)
	h := NewHandler(dist)

	req := httptest.NewRequest("GET", "/assets/style.css", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /assets/style.css status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); body != "body{}" {
		t.Errorf("GET /assets/style.css body = %q", body)
	}
}

func TestHandler_ServesFavicon(t *testing.T) {
	dist := setupTestDist(t)
	h := NewHandler(dist)

	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /favicon.ico status = %d, want 200", w.Code)
	}
}

func TestHandler_MissingAssetReturns404(t *testing.T) {
	dist := setupTestDist(t)
	h := NewHandler(dist)

	req := httptest.NewRequest("GET", "/assets/nonexistent.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /assets/nonexistent.js status = %d, want 404", w.Code)
	}
}
