package daemon

import (
	"context"
	"log"
	"net/http"
	"time"
)

// MountHealth registers /health and /healthz on mux.
func MountHealth(mux *http.ServeMux) {
	h := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write([]byte(`{"status":"ok"}`))
	}
	mux.HandleFunc("GET /health", h)
	mux.HandleFunc("GET /healthz", h)
}

// ListenAndServe starts an HTTP server on addr with the given handler and
// blocks until ctx is done. Shutdown is graceful.
func ListenAndServe(ctx context.Context, addr string, handler http.Handler) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64KB
	}
	go func() {
		log.Printf("HTTP server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()
	<-ctx.Done()
	srv.Shutdown(context.Background())
}
