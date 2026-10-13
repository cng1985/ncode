package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler serves the embedded Vue application and falls back to index.html for
// client-side routes. API paths are deliberately excluded from the fallback.
func Handler() http.Handler {
	assets, err := fs.Sub(Dist, "dist")
	if err != nil {
		panic("embedded Vue distribution is missing: " + err.Error())
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		requestPath := path.Clean("/" + r.URL.Path)
		if requestPath == "/" {
			requestPath = "/index.html"
		}
		if strings.HasPrefix(requestPath, "/api/") || requestPath == "/healthz" {
			http.NotFound(w, r)
			return
		}

		filePath := strings.TrimPrefix(requestPath, "/")
		if info, statErr := fs.Stat(assets, filePath); statErr == nil && !info.IsDir() {
			if strings.HasPrefix(requestPath, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else if filePath == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			files.ServeHTTP(w, r)
			return
		}
		if path.Ext(requestPath) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		index, readErr := fs.ReadFile(assets, "index.html")
		if readErr != nil {
			http.Error(w, "application unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(index)
		}
	})
}
