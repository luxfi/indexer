package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFS embed.FS

// frontendHandler returns an http.Handler that serves the embedded Next.js
// static export. Falls back to index.html for client-side routing.
func frontendHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("embedded static/ not found: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try the exact file first
		f, err := sub.Open(r.URL.Path[1:]) // strip leading /
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for unknown paths
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
