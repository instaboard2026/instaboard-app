// Package ui embeds the Angular production build and serves it as a single-page app.
package ui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// Dist holds the production frontend. Replaced by scripts/build-mac.sh before release builds.
//
//go:embed all:dist
var Dist embed.FS

// HasUI reports whether a real frontend build is embedded (not just the placeholder).
func HasUI() bool {
	b, err := Dist.ReadFile("dist/index.html")
	if err != nil {
		return false
	}
	return !strings.Contains(string(b), "UI not embedded")
}

// Handler serves static assets from Dist and falls back to index.html for SPA routes.
func Handler() http.Handler {
	sub, err := fs.Sub(Dist, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "ui embed missing", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if f, err := sub.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Client-side route — serve index.html.
		f, err := sub.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f)
	})
}
