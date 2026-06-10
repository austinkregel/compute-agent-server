package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	headers := map[string]string{
		"X-Content-Type-Options":      "nosniff",
		"Referrer-Policy":             "same-origin",
		"X-Frame-Options":             "DENY",
		"Cross-Origin-Opener-Policy":  "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}

	for name, want := range headers {
		got := w.Header().Get(name)
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestNormalizeSlashes(t *testing.T) {
	var captured string
	handler := NormalizeSlashes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Path
	}))

	tests := []struct {
		path string
		want string
	}{
		{"/api/status", "/api/status"},
		{"//api//status", "/api/status"},
		{"///api///status///", "/api/status/"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if captured != tt.want {
			t.Errorf("path %q → %q, want %q", tt.path, captured, tt.want)
		}
	}
}

func TestRecoverPanic(t *testing.T) {
	handler := RecoverPanic(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}
