package main

import (
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

// frontendHandler returns an http.Handler that serves the embedded Vite SPA
// static export. Falls back to index.html for client-side routing.
func frontendHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("embedded static/ not found: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers for all frontend responses.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' wss: ws:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")

		// Try the exact file first.
		// Use the cleaned path to prevent percent-encoded traversal (%2e%2e).
		path := r.URL.Path
		cleaned := filepath.Clean(path)
		if len(path) > 1 {
			// Sanitize: reject path traversal attempts (literal or encoded).
			if strings.Contains(path, "..") || strings.Contains(cleaned, "..") || cleaned != path {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			f, err := sub.Open(path[1:]) // strip leading /
			if err == nil {
				f.Close()
				// Cache static assets (hashed filenames) aggressively.
				if strings.Contains(path, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback: serve index.html for unknown paths.
		// No caching for the HTML shell.
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
